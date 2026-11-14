package logforward_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

var clock = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// hardn.yml's servers.
var (
	ymlTCP = config.LogForwarding{Server: "192.0.2.50"}
	ymlUDP = config.LogForwarding{Server: "192.0.2.50", Protocol: "udp"}
	ymlTLS = config.LogForwarding{Server: "logs.example.net", Protocol: "tls", CAFile: caFile}
)

const (
	caFile  = "/etc/ssl/lab-ca.pem"
	foreign = "/etc/rsyslog.d/50-remote.conf"
	stock   = "SYSLOGD_OPTS=\"-t\"\n"
	estab   = "ESTAB 0 0 192.0.2.10:41234 192.0.2.50:514 users:((\"rsyslogd\",pid=612,fd=9))\n" +
		"ESTAB 0 0 192.0.2.10:41236 192.0.2.51:6514 users:((\"rsyslogd\",pid=612,fd=10))\n"
)

// model is a fake host: its packages, and how its commands answer.
type model struct {
	t         *testing.T
	f         *hostfake.System
	facts     detect.Facts
	installed map[string]bool
	ss        *hostfake.Response // nil: estab
	checkBad  func() bool        // rsyslogd -N1 fails
	restarts  int                // the restarts that fail, from the first
}

func newModel(t *testing.T, d detect.Distro, pkgs ...string) *model {
	t.Helper()
	m := &model{t: t, f: hostfake.New(host.Options{}), installed: map[string]bool{},
		facts: detect.Facts{Platform: detect.Platform{Distro: d, Version: "1", Support: detect.Supported}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}}
	if d == detect.DistroAlpine {
		m.facts.Init.Kind = detect.OpenRC
		m.f.AddDir("/etc/conf.d", 0o755, 0, 0)
		m.file(logfwd.ConfD, stock)
		m.file(alpine.InstalledDB, "P:busybox\nV:1.37.0-r30\n\n")
	}
	for _, dir := range []string{"/etc/ssl", "/etc/hardn", "/var/lib", "/var/backups", "/var/log", "/run"} {
		m.f.AddDir(dir, 0o755, 0, 0)
	}
	for _, p := range pkgs {
		m.installed[p] = true
	}
	if m.installed["rsyslog"] { // the package's directory: not there on a host with journald alone
		m.f.AddDir(logfwd.RsyslogDir, 0o755, 0, 0)
	}
	m.f.Handler = m.answer
	return m
}

func (m *model) file(name, data string) *model {
	m.f.AddFile(name, []byte(data), 0o644, 0, 0)
	return m
}

// forwards is m with hardn's file for yml's server.
func (m *model) forwards(yml config.LogForwarding) *model {
	m.t.Helper()
	r := logforward.Read(context.Background(), hostfake.New(host.Options{}), detect.Facts{}, &config.Config{LogForwarding: yml})
	if r.Want == nil {
		m.t.Fatalf("%+v names no server: %v", yml, r.WantErr)
	}
	data, err := logfwd.RsyslogFile(*r.Want)
	if err != nil {
		m.t.Fatal(err)
	}
	return m.file(logfwd.DropIn, string(data))
}

func (m *model) answer(argv []string, _ []byte) hostfake.Response {
	line := strings.Join(argv, " ")
	pkg := argv[len(argv)-1]
	switch {
	case argv[0] == "dpkg-query":
		if m.installed[pkg] {
			return hostfake.Response{Stdout: "install ok installed\t8.2504.0\n"}
		}
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching " + pkg + "\n", ExitCode: 1}
	case strings.HasPrefix(line, "apt-get install -s -y "):
		return hostfake.Response{Stdout: "Inst " + pkg + " (8.2504.0 Debian:13/stable [amd64])\nConf " + pkg + " (8.2504.0 Debian:13/stable [amd64])\n"}
	case strings.HasPrefix(line, "apt-get install -y "):
		m.installed[pkg] = true
		m.f.AddDir(logfwd.RsyslogDir, 0o755, 0, 0)
	case strings.HasPrefix(line, "apt-get remove -y "):
		delete(m.installed, pkg)
	case line == "rsyslogd -N1":
		if m.checkBad != nil && m.checkBad() {
			return hostfake.Response{Stderr: "rsyslogd: error during parsing file /etc/rsyslog.d/50-remote.conf, on or before line 3\n", ExitCode: 1}
		}
	case line == "systemctl restart rsyslog", line == "rc-service syslog restart":
		if m.restarts > 0 {
			m.restarts--
			return hostfake.Response{Stderr: "Job for rsyslog.service failed.\n", ExitCode: 1}
		}
	case line == "ss -Htnp":
		if m.ss != nil {
			return *m.ss
		}
		return hostfake.Response{Stdout: estab}
	case argv[0] == "logger":
	default:
		return hostfake.Response{Stderr: "unscripted: " + line + "\n", ExitCode: 127}
	}
	return hostfake.Response{}
}

// ran are the commands that changed or checked the host, in order: the
// reads of the plan (dpkg-query, ss, apt's simulation) left out.
func (m *model) ran() []string {
	var out []string
	for _, argv := range m.f.Runs() {
		line := strings.Join(argv, " ")
		if argv[0] == "dpkg-query" || argv[0] == "ss" || strings.HasPrefix(line, "apt-get install -s") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func (m *model) engine() *engine.Engine {
	return engine.New(m.f, engine.Options{
		Revert:  revert.Options{Now: func() time.Time { return clock }, Hardn: "/usr/local/bin/hardn"},
		Detect:  func(context.Context, host.System) (detect.Facts, error) { return m.facts, nil },
		Hold:    func() func() { return func() {} },
		PID:     5000,
		Backups: hostbackup.Settings{Path: hostbackup.DefaultPath, Enabled: true},
	})
}

func cfg(yml config.LogForwarding) *config.Config { return &config.Config{LogForwarding: yml} }

// plan plans a alone, as the screen's preplan does.
func (m *model) plan(a logforward.Action, yml config.LogForwarding) ([]engine.Change, error) {
	return logforward.Forward{Action: a}.Plan(context.Background(), m.f, engine.Input{Facts: m.facts, Config: cfg(yml)})
}

// apply plans and applies a on the engine; it returns the plan's change
// ids and the apply's error.
func (m *model) apply(a logforward.Action, yml config.LogForwarding) ([]string, error) {
	m.t.Helper()
	ctx := context.Background()
	e := m.engine()
	p, err := e.Plan(ctx, []engine.Control{logforward.Forward{Action: a}}, cfg(yml))
	if err != nil {
		m.t.Fatal(err)
	}
	if len(p.Refused) > 0 {
		m.t.Fatalf("refused: %v", p.Refused)
	}
	var ids []string
	for _, c := range p.Changes {
		ids = append(ids, strings.TrimPrefix(c.ID, logforward.ID+"/"))
	}
	_, err = e.Apply(ctx, p)
	return ids, err
}

func (m *model) content(name string) (string, bool) {
	data, err := m.f.ReadFile(context.Background(), name)
	return string(data), err == nil
}

func (m *model) status(yml config.LogForwarding) engine.Status {
	m.t.Helper()
	st, err := logforward.Forward{Config: cfg(yml)}.Check(context.Background(), m.f, m.facts)
	if err != nil {
		m.t.Fatal(err)
	}
	return st
}

// TestStatus: the row in every state the mocks show
// (gallery-logforward.md §2), and those a fact not read adds.
func TestStatus(t *testing.T) {
	ubuntu := func() *model { return newModel(t, detect.DistroUbuntu, "rsyslog") }
	down := &hostfake.Response{Stdout: "ESTAB 0 0 192.0.2.10:22 192.0.2.1:50514 users:((\"sshd\",pid=700,fd=4))\n"}
	set, err := logfwd.SetOpts([]byte(stock), logfwd.Server{Address: "192.0.2.50", Port: "514"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		m      *model
		yml    config.LogForwarding
		state  engine.State
		detail string
	}{
		{"off", ubuntu(), ymlTCP, engine.NotConfigured, logforward.DetailOff},
		{"no-server", ubuntu(), config.LogForwarding{}, engine.NotConfigured, logforward.DetailOff},
		{"tcp", ubuntu().forwards(ymlTCP), ymlTCP, engine.Configured, "192.0.2.50:514 TCP"},
		{"tcp-down", func() *model { m := ubuntu().forwards(ymlTCP); m.ss = down; return m }(), ymlTCP, engine.Partial, logforward.DetailNotReached},
		{"udp", func() *model { m := ubuntu().forwards(ymlUDP); m.ss = down; return m }(), ymlUDP, engine.Configured, "192.0.2.50:514 UDP"},
		{"tls", func() *model {
			m := ubuntu().forwards(ymlTLS)
			m.ss = &hostfake.Response{Stdout: "ESTAB 0 0 192.0.2.10:41236 192.0.2.50:6514 users:((\"rsyslogd\",pid=612,fd=10))\n"}
			return m
		}(), ymlTLS, engine.Configured, "logs.example.net:6514 TLS"},
		{"foreign", ubuntu().file(foreign, "*.* @@192.0.2.50:514\n"), ymlTCP, engine.Configured, "192.0.2.50:514 TCP"},
		{"foreign, no hardn.yml server", ubuntu().file(foreign, "*.* @@192.0.2.50:514\n"), config.LogForwarding{}, engine.Configured, "192.0.2.50:514 TCP"},
		{"mismatch", ubuntu().forwards(ymlTCP), config.LogForwarding{Server: "192.0.2.60"}, engine.Partial, logforward.DetailNotWanted},
		{"journald", newModel(t, detect.DistroDebian), ymlTCP, engine.NotConfigured, logforward.DetailOff},
		{"alpine", newModel(t, detect.DistroAlpine).file(logfwd.ConfD, string(set)), ymlUDP, engine.Configured, "192.0.2.50:514 UDP"},
		{"alpine-tcp", newModel(t, detect.DistroAlpine), ymlTCP, engine.NotConfigured, logforward.DetailOff},
		{"not-read", ubuntu().file(foreign, "action(type=\"omfwd\" target=\"192.0.2.50\"\n"), ymlTCP, engine.Unknown, logforward.DetailCantRead},
		{"sockets not read", func() *model {
			m := ubuntu().forwards(ymlTCP)
			m.ss = &hostfake.Response{Err: host.ErrNotFound}
			return m
		}(), ymlTCP, engine.Unknown, logforward.DetailConnUnread},
		{"hardn.yml not valid", ubuntu(), config.LogForwarding{Server: "logs example"}, engine.NotConfigured, logforward.DetailOff},
		{"escape in a foreign file", ubuntu().file(foreign, "*.* @@192.0.2.50\x1b[2J:514\n"), config.LogForwarding{}, engine.Configured, "192.0.2.50^[[2J:514 TCP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.m.status(tc.yml)
			if st.State != tc.state || st.Detail != tc.detail {
				t.Errorf("status = %v %q, want %v %q", st.State, st.Detail, tc.state, tc.detail)
			}
			if strings.ContainsRune(st.Detail+strings.Join(st.Evidence, ""), '\x1b') {
				t.Errorf("an escape reaches the row: %q %q", st.Detail, st.Evidence)
			}
		})
	}
}

// The main menu draws the row only where hardn.yml names a server or the
// host forwards (decision 10).
func TestShown(t *testing.T) {
	m := newModel(t, detect.DistroUbuntu, "rsyslog")
	none := config.LogForwarding{}
	if logforward.Shown(cfg(none), m.status(none)) || logforward.Shown(nil, m.status(none)) {
		t.Error("no server and nothing forwarded: the row is shown")
	}
	bad := config.LogForwarding{Server: "a b"}
	if !logforward.Shown(cfg(ymlTCP), m.status(ymlTCP)) || !logforward.Shown(cfg(bad), m.status(bad)) {
		t.Error("hardn.yml names a server: the row is not shown")
	}
	m.file(foreign, "*.* @192.0.2.50\n")
	if !logforward.Shown(cfg(none), m.status(none)) {
		t.Error("the host forwards: the row is not shown")
	}
	m.file(foreign, "*.* @@192.0.2.50\n").ss = &hostfake.Response{Err: host.ErrNotFound}
	if !logforward.Shown(cfg(none), m.status(none)) {
		t.Error("the host forwards, its sockets not read: the row is not shown")
	}
	m.file(foreign, "*.* @@:1\n")
	if logforward.Shown(cfg(none), m.status(none)) {
		t.Error("settings not read and no server asked: the row is shown")
	}
}

// TestRefusals: every refusal, before anything is written, with its code,
// its sentinel and its words.
func TestRefusals(t *testing.T) {
	ubuntu := func() *model { return newModel(t, detect.DistroUbuntu, "rsyslog") }
	noInit := func(m *model) *model { m.facts.Init = detect.Init{Kind: detect.Systemd, Running: detect.No}; return m }
	for _, tc := range []struct {
		name  string
		m     *model
		a     logforward.Action
		yml   config.LogForwarding
		is    error
		code  string
		words string
	}{
		{"no server", ubuntu(), logforward.ActionSet, config.LogForwarding{}, logforward.ErrNoServer, "LOGFWD_NO_SERVER",
			"hardn.yml names no log server. To go on: add logForwarding's server, then try again"},
		{"not valid", ubuntu(), logforward.ActionSet, config.LogForwarding{Server: "logs\" x=\"y"}, logforward.ErrBadServer, "LOGFWD_BAD_SERVER",
			"hardn.yml's logForwarding is not valid: server is not an IPv4 address or a host name"},
		{"malformed file", ubuntu().file(foreign, "action(type=\"omfwd\" target=\"192.0.2.50\"\n"), logforward.ActionSet, ymlTCP, logforward.ErrNotRead, "LOGFWD_NOT_READ",
			"hardn can't read rsyslog's settings: /etc/rsyslog.d/50-remote.conf:1: an action that does not close"},
		{"rsyslogd refuses its settings", func() *model { m := ubuntu(); m.checkBad = func() bool { return true }; return m }(), logforward.ActionSet, ymlTCP, logforward.ErrNotRead, "LOGFWD_NOT_READ",
			"rsyslogd can't read its settings: rsyslogd: error during parsing file /etc/rsyslog.d/50-remote.conf, on or before line 3. To go on: fix that, then try again"},
		{"set outside hardn", ubuntu().file(foreign, "*.* @@192.0.2.60\n"), logforward.ActionSet, ymlTCP, logforward.ErrOutside, "LOGFWD_SET_OUTSIDE",
			"/etc/rsyslog.d/50-remote.conf already forwards logs, and hardn leaves it as it is"},
		{"a file at hardn's path that hardn did not write", ubuntu().file(logfwd.DropIn, "# mine\n*.* /var/log/all.log\n"), logforward.ActionSet, ymlTCP, logforward.ErrOutside, "LOGFWD_SET_OUTSIDE",
			"/etc/rsyslog.d/60-hardn-forward.conf is not hardn's file, and hardn leaves it as it is. To go on: move that file away, then try again"},
		{"alpine, rsyslog installed", newModel(t, detect.DistroAlpine).file(alpine.InstalledDB, "P:rsyslog\nV:8.2410.0-r1\n\n"), logforward.ActionSet, ymlUDP, logforward.ErrNotRead, "LOGFWD_NOT_READ",
			"hardn can't read the logger's settings: rsyslog is installed beside busybox syslogd, so which one logs is not established"},
		{"alpine, tcp", newModel(t, detect.DistroAlpine), logforward.ActionSet, ymlTCP, logforward.ErrUDPOnly, "LOGFWD_UDP_ONLY",
			"busybox syslogd forwards over UDP only. To go on: set protocol: udp in hardn.yml, then try again"},
		{"alpine, set outside hardn", newModel(t, detect.DistroAlpine).file(logfwd.ConfD, "SYSLOGD_OPTS=\"-R 192.0.2.60\"\n"), logforward.ActionSet, ymlUDP, logforward.ErrOutside, "LOGFWD_SET_OUTSIDE", "/etc/conf.d/syslog already forwards logs"},
		{"alpine, options not read", newModel(t, detect.DistroAlpine).file(logfwd.ConfD, "SYSLOGD_OPTS=\"$X\"\n"), logforward.ActionSet, ymlUDP, logforward.ErrNotRead, "LOGFWD_NOT_READ", "hardn can't read syslogd's options"},
		{"no CA file", ubuntu(), logforward.ActionSet, ymlTLS, logforward.ErrNoCAFile, "LOGFWD_NO_CA_FILE", "hardn.yml's caFile, /etc/ssl/lab-ca.pem, is not a file on this host"},
		{"systemd did not boot the host", noInit(ubuntu()), logforward.ActionSet, ymlTCP, logforward.ErrNoInit, "LOGFWD_NO_INIT", "systemd did not boot this host"},
		{"stop, nothing set", ubuntu(), logforward.ActionStop, ymlTCP, logforward.ErrNothingToStop, "LOGFWD_NOTHING_TO_STOP", "hardn did not set log forwarding here"},
		{"stop, set outside hardn", ubuntu().file(foreign, "*.* @@192.0.2.50\n"), logforward.ActionStop, ymlTCP, logforward.ErrNothingToStop, "LOGFWD_NOTHING_TO_STOP", "hardn did not set"},
		{"stop, not read", ubuntu().file(foreign, "*.* @@:1\n"), logforward.ActionStop, ymlTCP, logforward.ErrNotRead, "LOGFWD_NOT_READ", "hardn can't read rsyslog's settings"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := tc.m.plan(tc.a, tc.yml)
			var ref *steps.Refusal
			if !errors.As(err, &ref) || !errors.Is(err, tc.is) || ref.Code != tc.code || output.CodeOf(err) != tc.code || cs != nil {
				t.Fatalf("Plan = %v, %v; want a refusal %s (%v)", cs, err, tc.code, tc.is)
			}
			if !strings.Contains(err.Error(), tc.words) {
				t.Errorf("words:\n%s\nwant\n%s", err, tc.words)
			}
			if ran := tc.m.ran(); len(ran) > 0 && !slices.Equal(ran, []string{"rsyslogd -N1"}) {
				t.Errorf("a refusal ran %v", ran)
			}
		})
	}
	if _, err := newModel(t, detect.DistroUbuntu, "rsyslog").plan(logforward.ActionNone, ymlTCP); !errors.Is(err, engine.ErrNotMigrated) {
		t.Errorf("ActionNone: %v, want ErrNotMigrated", err)
	}
}

// Proxmox VE with journald alone: rsyslog is the one package hardn installs
// there (steps.Package.Proxmox), so Set server plans as on Debian.
func TestProxmoxInstalls(t *testing.T) {
	deb, err := newModel(t, detect.DistroDebian).plan(logforward.ActionSet, ymlTCP)
	if err != nil {
		t.Fatalf("Debian: Plan = %v", err)
	}
	pve, err := newModel(t, detect.DistroProxmox).plan(logforward.ActionSet, ymlTCP)
	if err != nil {
		t.Fatalf("Proxmox VE: Plan = %v", err)
	}
	if len(pve) != len(deb) || len(pve) == 0 || pve[0].ID != logforward.ChangeInstall {
		t.Errorf("Proxmox VE plans %d changes, Debian %d: want the same, the install first", len(pve), len(deb))
	}
}

func TestSetRsyslog(t *testing.T) {
	m := newModel(t, detect.DistroUbuntu, "rsyslog")
	ids, err := m.apply(logforward.ActionSet, ymlTCP)
	if err != nil || !slices.Equal(ids, []string{"file", "restart", "test"}) {
		t.Fatalf("apply = %v, %v", ids, err)
	}
	want := logfwd.Header + "*.* action(type=\"omfwd\" protocol=\"tcp\"\n    target=\"192.0.2.50\" port=\"514\"\n    queue.type=\"linkedList\")\n"
	if got, _ := m.content(logfwd.DropIn); got != want {
		t.Errorf("hardn's file =\n%s\nwant\n%s", got, want)
	}
	if e, _ := m.f.Entry(logfwd.DropIn); e.Mode.Perm() != 0o644 || e.UID != 0 {
		t.Errorf("hardn's file is %v, owner %d; want 0644, root", e.Mode, e.UID)
	}
	// The check before the plan, the check with hardn's file, the restart, the test line.
	if got := m.ran(); !slices.Equal(got, []string{"rsyslogd -N1", "rsyslogd -N1", "rsyslogd -N1", "systemctl restart rsyslog", "logger -t hardn log forwarding set"}) &&
		!slices.Equal(got, []string{"rsyslogd -N1", "rsyslogd -N1", "systemctl restart rsyslog", "logger -t hardn log forwarding set"}) {
		t.Errorf("ran %q", got)
	}
	if st := m.status(ymlTCP); st.State != engine.Configured {
		t.Errorf("status after = %+v", st)
	}
	// As asked now: nothing to plan.
	if cs, err := m.plan(logforward.ActionSet, ymlTCP); cs != nil || err != nil {
		t.Errorf("a second Set plans %v, %v", cs, err)
	}
}

func TestSetJournald(t *testing.T) {
	m := newModel(t, detect.DistroDebian).file(caFile, "-----BEGIN CERTIFICATE-----\n")
	ids, err := m.apply(logforward.ActionSet, ymlTLS)
	if err != nil || !slices.Equal(ids, []string{"install", "install-tls", "file", "restart", "test"}) {
		t.Fatalf("apply = %v, %v", ids, err)
	}
	if !m.installed["rsyslog"] || !m.installed["rsyslog-gnutls"] {
		t.Errorf("installed = %v", m.installed)
	}
	if got, _ := m.content(logfwd.DropIn); !strings.Contains(got, "StreamDriver=\"gtls\" StreamDriverMode=\"1\"") || !strings.Contains(got, "StreamDriver.CAFile=\""+caFile+"\"") {
		t.Errorf("hardn's file =\n%s", got)
	}
	want := []string{"apt-get install -y rsyslog", "apt-get install -y rsyslog-gnutls", "rsyslogd -N1", "systemctl restart rsyslog", "logger -t hardn log forwarding set"}
	if got := m.ran(); !slices.Equal(got, want) {
		t.Errorf("ran %q, want %q", got, want)
	}
}

// rsyslogd refuses its settings with hardn's file: the file is put back,
// and nothing restarts.
func TestSetCheckFails(t *testing.T) {
	m := newModel(t, detect.DistroDebian)
	m.checkBad = func() bool { _, there := m.content(logfwd.DropIn); return there }
	_, err := m.apply(logforward.ActionSet, ymlTCP)
	if !errors.Is(err, logforward.ErrCheckFailed) || output.CodeOf(err) != "LOGFWD_CHECK_FAILED" {
		t.Fatalf("apply: %v, want ErrCheckFailed", err)
	}
	if _, there := m.content(logfwd.DropIn); there {
		t.Error("hardn's file is left after the rollback")
	}
	if m.installed["rsyslog"] {
		t.Error("rsyslog is left installed after the rollback")
	}
	if slices.Contains(m.ran(), "systemctl restart rsyslog") {
		t.Errorf("rsyslog restarted: %q", m.ran())
	}
}

// The restart fails: the file is put back and rsyslog restarted with it.
func TestSetRestartFails(t *testing.T) {
	m := newModel(t, detect.DistroUbuntu, "rsyslog").forwards(ymlTCP)
	before, _ := m.content(logfwd.DropIn)
	m.restarts = 1
	_, err := m.apply(logforward.ActionSet, config.LogForwarding{Server: "192.0.2.60"})
	if err == nil {
		t.Fatal("apply: no error")
	}
	if got, _ := m.content(logfwd.DropIn); got != before {
		t.Errorf("hardn's file after the rollback =\n%s\nwant\n%s", got, before)
	}
	ran := m.ran()
	if n := len(ran); n < 2 || ran[n-1] != "systemctl restart rsyslog" || ran[n-2] != "systemctl restart rsyslog" {
		t.Errorf("ran %q, want the failed restart, then the rollback's", ran)
	}
}

// hardn.yml names another server: hardn's file is rewritten.
func TestSetMoves(t *testing.T) {
	m := newModel(t, detect.DistroUbuntu, "rsyslog").forwards(ymlTCP)
	cs, err := m.plan(logforward.ActionSet, config.LogForwarding{Server: "192.0.2.60"})
	if err != nil || len(cs) != 3 || !strings.Contains(cs[0].Diff, "-    target=\"192.0.2.50\"") || !strings.Contains(cs[0].Diff, "+    target=\"192.0.2.60\"") {
		t.Fatalf("plan = %v, %v", cs, err)
	}
}

// hardn's file with its action edited out forwards nothing, and is still
// hardn's: the review shows what is there, not a new file.
func TestSetOverEdited(t *testing.T) {
	m := newModel(t, detect.DistroUbuntu, "rsyslog").file(logfwd.DropIn, logfwd.Header+"# off for now\n")
	cs, err := m.plan(logforward.ActionSet, ymlTCP)
	if err != nil || len(cs) != 3 || !strings.Contains(cs[0].Diff, "-# off for now") || strings.Contains(cs[0].Diff, "/dev/null") {
		t.Fatalf("plan = %v, %v", cs, err)
	}
}

func TestStopRsyslog(t *testing.T) {
	m := newModel(t, detect.DistroUbuntu, "rsyslog").forwards(ymlTCP).file(foreign, "*.* /var/log/all.log\n")
	ids, err := m.apply(logforward.ActionStop, ymlTCP)
	if err != nil || !slices.Equal(ids, []string{"file", "restart"}) {
		t.Fatalf("apply = %v, %v", ids, err)
	}
	if _, there := m.content(logfwd.DropIn); there {
		t.Error("hardn's file is still there")
	}
	if _, there := m.content(foreign); !there || !m.installed["rsyslog"] {
		t.Error("Stop took more than hardn's file")
	}
	if got := m.ran(); !slices.Equal(got, []string{"systemctl restart rsyslog"}) {
		t.Errorf("ran %q", got)
	}
}

func TestAlpine(t *testing.T) {
	m := newModel(t, detect.DistroAlpine)
	if err := m.f.Chmod(context.Background(), logfwd.ConfD, 0o640); err != nil {
		t.Fatal(err)
	}
	ids, err := m.apply(logforward.ActionSet, ymlUDP)
	if err != nil || !slices.Equal(ids, []string{"options", "restart", "test"}) {
		t.Fatalf("set = %v, %v", ids, err)
	}
	want := logfwd.Mark + "-R 192.0.2.50:514 -L\nSYSLOGD_OPTS=\"-t -R 192.0.2.50:514 -L\"\n"
	if got, _ := m.content(logfwd.ConfD); got != want {
		t.Errorf("%s =\n%s\nwant\n%s", logfwd.ConfD, got, want)
	}
	if e, _ := m.f.Entry(logfwd.ConfD); e.Mode.Perm() != 0o640 {
		t.Errorf("the file's mode is %v, want it kept", e.Mode)
	}
	if got := m.ran(); !slices.Equal(got, []string{"rc-service syslog restart", "logger -t hardn log forwarding set"}) {
		t.Errorf("ran %q", got)
	}
	if st := m.status(ymlUDP); st.State != engine.Configured {
		t.Errorf("status after = %+v", st)
	}
	ids, err = m.apply(logforward.ActionStop, ymlUDP)
	if err != nil || !slices.Equal(ids, []string{"options", "restart"}) {
		t.Fatalf("stop = %v, %v", ids, err)
	}
	if got, _ := m.content(logfwd.ConfD); got != stock {
		t.Errorf("%s after stop = %q, want it as it was", logfwd.ConfD, got)
	}
}

// A plan writes nothing and runs only reads and rsyslogd's check.
func TestPlanWritesNothing(t *testing.T) {
	for _, m := range []*model{newModel(t, detect.DistroUbuntu, "rsyslog"), newModel(t, detect.DistroDebian), newModel(t, detect.DistroAlpine)} {
		yml := ymlTCP
		if m.facts.Platform.Distro == detect.DistroAlpine {
			yml = ymlUDP
		}
		if cs, err := m.plan(logforward.ActionSet, yml); err != nil || len(cs) == 0 {
			t.Fatalf("plan = %v, %v", cs, err)
		}
		for _, c := range m.f.Calls() {
			switch c.Op {
			case "run", "read", "readdir", "stat", "lstat":
			default:
				t.Errorf("%s: a plan did %s", m.facts.Platform.Distro, c)
			}
		}
		if ran := m.ran(); len(ran) > 0 && !slices.Equal(ran, []string{"rsyslogd -N1"}) {
			t.Errorf("a plan ran %q", ran)
		}
	}
}

func TestNotice(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		m    *model
		a    logforward.Action
		want string
	}{
		{newModel(t, detect.DistroUbuntu, "rsyslog"), logforward.ActionSet, logforward.NoticeAll},
		{newModel(t, detect.DistroDebian), logforward.ActionSet, logforward.NoticeJournald},
		{newModel(t, detect.DistroAlpine), logforward.ActionSet, logforward.NoticeUDP},
		{newModel(t, detect.DistroUbuntu, "rsyslog"), logforward.ActionStop, logforward.NoticeStop},
		{newModel(t, detect.DistroAlpine), logforward.ActionStop, logforward.NoticeStopApk},
	} {
		if got := logforward.Read(ctx, tc.m.f, tc.m.facts, cfg(ymlTCP)).Notice(tc.a); got != tc.want {
			t.Errorf("%s, action %d: %q, want %q", tc.m.facts.Platform.Distro, tc.a, got, tc.want)
		}
	}
}
