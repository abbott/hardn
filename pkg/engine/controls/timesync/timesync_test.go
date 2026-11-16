package timesync_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	tshost "github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/host/timesync/timefake"
	"github.com/abbott/hardn/pkg/output"
)

var (
	clock = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	want  = config.NTPServers{"192.0.2.10", "192.0.2.11"}
	none  = config.NTPServers(nil)
)

const (
	site     = tshost.SourcesDir + "/site.sources"
	mainFile = "/etc/systemd/timesyncd.conf"
)

// model is a fake host and its time service.
type model struct {
	t     *testing.T
	f     *hostfake.System
	sim   *timefake.Sim
	facts detect.Facts
}

// newModel is a host of d whose time service is service under its init,
// its packages pkgs, in sync on stock.
func newModel(t *testing.T, d detect.Distro, service, stock string, pkgs ...string) *model {
	t.Helper()
	m := &model{t: t, f: hostfake.New(host.Options{}),
		facts: detect.Facts{Platform: detect.Platform{Distro: d, Version: "1", Support: detect.Supported}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}}
	m.sim = &timefake.Sim{Read: func(name string) ([]byte, error) { return m.f.ReadFile(context.Background(), name) }, Service: service, Synced: stock, Stock: stock, Reachable: []string{"192.0.2.10", "192.0.2.12"}, Fallback: "ntp.ubuntu.com", Packages: map[string]bool{}}
	for _, p := range pkgs {
		m.sim.Packages[p] = true
	}
	for _, dir := range []string{"/etc/hardn", "/etc/chrony", "/etc/systemd", "/etc/conf.d", "/var/lib", "/var/backups", "/var/log", "/run"} {
		m.f.AddDir(dir, 0o755, 0, 0)
	}
	if d == detect.DistroAlpine {
		m.facts.Init.Kind = detect.OpenRC
		m.file(alpine.InstalledDB, "P:busybox\nV:1.37.0-r30\n\n")
	}
	m.f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := m.sim.Answer(argv); ok {
			return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}
		}
		return hostfake.Response{Stderr: "unscripted: " + strings.Join(argv, " ") + "\n", ExitCode: 127}
	}
	return m
}

func ubuntu(t *testing.T) *model {
	m := newModel(t, detect.DistroUbuntu, "chrony", "2.ntp.ubuntu.com", "chrony")
	m.f.AddDir(tshost.SourcesDir, 0o755, 0, 0)
	return m.file(tshost.ChronyConfDebian, timefake.UbuntuConf).file(timefake.UbuntuPoolsFile, timefake.UbuntuPools)
}

func debian(t *testing.T) *model {
	m := newModel(t, detect.DistroDebian, "chrony", "2.debian.pool.ntp.org", "chrony")
	m.f.AddDir(tshost.SourcesDir, 0o755, 0, 0)
	return m.file(tshost.ChronyConfDebian, timefake.DebianConf)
}

func rocky(t *testing.T) *model {
	return newModel(t, detect.DistroRocky, "chronyd", "2.rocky.pool.ntp.org", "chrony").file(tshost.ChronyConfEL, timefake.ELConf)
}

func timesyncd(t *testing.T) *model {
	return newModel(t, detect.DistroUbuntu, "systemd-timesyncd", "ntp.ubuntu.com", "systemd-timesyncd").file(mainFile, "[Time]\n#NTP=\n")
}

func alpineHost(t *testing.T) *model {
	return newModel(t, detect.DistroAlpine, "ntpd", "pool.ntp.org").file(tshost.ConfD, timefake.AlpineConfD)
}

func (m *model) file(name, data string) *model {
	m.f.AddFile(name, []byte(data), 0o640, 0, 0)
	return m
}

func (m *model) with(edit func(*model)) *model { edit(m); return m }

// ran are the commands that changed the host, in order: the reads left out.
func (m *model) ran() []string {
	var out []string
	for _, argv := range m.f.Runs() {
		line := strings.Join(argv, " ")
		if strings.Contains(line, "restart") || strings.Contains(line, "reload") {
			out = append(out, line)
		}
	}
	return out
}

// writes are the calls that changed a file outside hardn's own state.
func (m *model) writes() []string {
	var out []string
	for _, c := range m.f.Calls() {
		switch c.Op {
		case "write-atomic", "write-in-place", "append", "remove", "removeall", "rename", "mkdir", "chmod", "chown":
			if !strings.HasPrefix(c.Path, "/var/") && !strings.HasPrefix(c.Path, "/run/") {
				out = append(out, c.String())
			}
		}
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

func cfg(yml config.NTPServers) *config.Config { return &config.Config{NTPServers: yml} }

func (m *model) plan(a timesync.Action, yml config.NTPServers) ([]engine.Change, error) {
	return timesync.Sync{Action: a}.Plan(context.Background(), m.f, engine.Input{Facts: m.facts, Config: cfg(yml)})
}

// apply plans and applies a on the engine, between is run after the plan;
// it returns the plan's change ids and the apply's error.
func (m *model) apply(a timesync.Action, yml config.NTPServers, between ...func()) ([]string, error) {
	m.t.Helper()
	ctx := context.Background()
	e := m.engine()
	p, err := e.Plan(ctx, []engine.Control{timesync.Sync{Action: a}}, cfg(yml))
	if err != nil {
		m.t.Fatal(err)
	}
	if len(p.Refused) > 0 {
		m.t.Fatalf("refused: %v", p.Refused)
	}
	var ids []string
	for _, c := range p.Changes {
		ids = append(ids, strings.TrimPrefix(c.ID, timesync.ID+"/"))
	}
	for _, f := range between {
		f()
	}
	_, err = e.Apply(ctx, p)
	return ids, err
}

func (m *model) content(name string) string {
	m.t.Helper()
	data, err := m.f.ReadFile(context.Background(), name)
	if err != nil {
		m.t.Fatal(err)
	}
	return string(data)
}

func (m *model) gone(name string) bool {
	_, err := m.f.Lstat(context.Background(), name)
	return err != nil
}

func (m *model) status(yml config.NTPServers) engine.Status {
	m.t.Helper()
	st, err := timesync.Sync{Config: cfg(yml)}.Check(context.Background(), m.f, m.facts)
	if err != nil {
		m.t.Fatal(err)
	}
	return st
}

// set is m after Set servers with yml.
func (m *model) set(yml config.NTPServers) *model {
	m.t.Helper()
	if _, err := m.apply(timesync.ActionSet, yml); err != nil {
		m.t.Fatal(err)
	}
	return m
}

// problems are TestStatus's rows the main menu draws, by name, with the
// problem's words and the host's value (engine.Status.Problem and Value;
// gallery-menu-layout.md, decision 17 (a)): no row while the clock is in
// sync, or in a container; no time service only where hardn.yml names
// servers.
var problems = map[string]struct{ problem, value string }{
	"not-synced":          {timesync.ProblemNotInSync, "2 servers"},
	"stopped":             {"chrony not running", ""},
	"no-service":          {timesync.ProblemNoService, ""},
	"mismatch":            {timesync.ProblemNotWanted, "2 servers"},
	"not-read":            {timesync.ProblemCantRead, ""},
	"clock not read":      {timesync.ProblemCantRead, ""},
	"no-service, unasked": {"", ""},
}

// TestStatus: the row in every state the mocks show (gallery-timesync.md
// §2), and those a fact not read adds.
func TestStatus(t *testing.T) {
	for _, tc := range []struct {
		name   string
		m      *model
		yml    config.NTPServers
		state  engine.State
		detail string
	}{
		{"default", ubuntu(t), none, engine.Configured, "2.ntp.ubuntu.com"},
		{"off", ubuntu(t), want, engine.Configured, "2.ntp.ubuntu.com"},
		{"hardn", ubuntu(t).set(want), want, engine.Configured, "192.0.2.10"},
		{"not-synced", ubuntu(t).set(config.NTPServers{"192.0.2.98", "192.0.2.99"}), config.NTPServers{"192.0.2.98", "192.0.2.99"}, engine.Partial, timesync.DetailNotInSync},
		{"stopped", ubuntu(t).with(func(m *model) { m.sim.Stopped = true }), want, engine.NotConfigured, "chrony not running"},
		{"no-service", newModel(t, detect.DistroDebian, "", ""), want, engine.NotConfigured, timesync.DetailNoService},
		{"no-service, unasked", newModel(t, detect.DistroDebian, "", ""), none, engine.NotConfigured, timesync.DetailNoService},
		{"foreign", ubuntu(t).file(site, "server 192.0.2.77\n"), want, engine.Configured, "2.ntp.ubuntu.com"},
		{"mismatch", ubuntu(t).set(want), config.NTPServers{"192.0.2.10", "192.0.2.12"}, engine.Partial, timesync.DetailNotWanted},
		{"timesyncd", timesyncd(t), want, engine.Configured, "ntp.ubuntu.com"},
		{"alpine", alpineHost(t), want, engine.Configured, timesync.DetailInSync},
		{"container", ubuntu(t).with(func(m *model) { m.facts.Container.Detected = detect.Yes }), want, engine.NotApplicable, timesync.DetailContainer},
		{"not-read", ubuntu(t).file(tshost.ChronyConfDebian, "server\n"), want, engine.Unknown, timesync.DetailCantRead},
		{"clock not read", ubuntu(t).with(func(m *model) { m.sim.Missing = map[string]bool{"chronyc": true} }), want, engine.Unknown, timesync.DetailCantRead},
		{"hardn.yml not valid", ubuntu(t), config.NTPServers{"a b"}, engine.Configured, "2.ntp.ubuntu.com"},
		{"escape in the server's name", ubuntu(t).with(func(m *model) { m.sim.Synced = "ntp\x1b[2J.example" }), none, engine.Configured, "ntp^[[2J.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := tc.m.status(tc.yml)
			if st.State != tc.state || st.Detail != tc.detail {
				t.Errorf("status = %v %q, want %v %q", st.State, st.Detail, tc.state, tc.detail)
			}
			if strings.ContainsRune(st.Detail+strings.Join(st.Evidence, ""), '\x1b') {
				t.Errorf("an escape reaches the row: %q %q", st.Detail, st.Evidence)
			}
			if want := problems[tc.name]; st.Problem != want.problem || st.Value != want.value {
				t.Errorf("problem %q, value %q; want %q, %q", st.Problem, st.Value, want.problem, want.value)
			}
		})
	}
}

// TestRefusals: every refusal, before anything is written, with its code,
// its sentinel and its words.
func TestRefusals(t *testing.T) {
	stopped := func(m *model) { m.sim.Stopped = true }
	for _, tc := range []struct {
		name  string
		m     *model
		a     timesync.Action
		yml   config.NTPServers
		is    error
		code  string
		words string
	}{
		{"no servers", ubuntu(t), timesync.ActionSet, none, timesync.ErrNoServers, "NTP_NO_SERVERS", "hardn.yml names no time servers. To go on: add ntpServers, then try again"},
		{"not valid", ubuntu(t), timesync.ActionSet, config.NTPServers{"192.0.2.10 prefer"}, timesync.ErrBadServers, "NTP_BAD_SERVERS",
			"hardn.yml's ntpServers is not valid: server 1 is not an IPv4 address or a host name"},
		{"container", ubuntu(t).with(func(m *model) { m.facts.Container.Detected = detect.Yes }), timesync.ActionSet, want, timesync.ErrContainer, "NTP_CONTAINER", "This is a container: its host keeps the time"},
		{"not read", ubuntu(t).file(tshost.ChronyConfDebian, "server\n"), timesync.ActionSet, want, timesync.ErrNotRead, "NTP_NOT_READ",
			"hardn can't read chrony's settings: /etc/chrony/chrony.conf:1: a server with no name. To go on: fix that"},
		{"no service", newModel(t, detect.DistroDebian, "", ""), timesync.ActionSet, want, timesync.ErrNoService, "NTP_NO_SERVICE", "This host has no time service hardn sets, and hardn installs none"},
		{"stopped", ubuntu(t).with(stopped), timesync.ActionSet, want, timesync.ErrNoService, "NTP_NO_SERVICE", "chrony is not running, and hardn does not start it. To go on: start it, then try again"},
		{"set outside hardn", ubuntu(t).file(site, "server 192.0.2.77\n"), timesync.ActionSet, want, timesync.ErrOutside, "NTP_SET_OUTSIDE",
			"/etc/chrony/sources.d/site.sources already sets time servers, and hardn leaves it as it is"},
		{"set outside hardn, beside hardn's own", ubuntu(t).set(want).file(site, "server 192.0.2.77\n"), timesync.ActionSet, config.NTPServers{"192.0.2.12"}, timesync.ErrOutside, "NTP_SET_OUTSIDE",
			"/etc/chrony/sources.d/site.sources already sets time servers"},
		{"timesyncd, cleared after hardn's file", timesyncd(t).file("/etc/systemd/timesyncd.conf.d/90-site.conf", "[Time]\nNTP=\n"), timesync.ActionSet, want, timesync.ErrOutside, "NTP_SET_OUTSIDE",
			"/etc/systemd/timesyncd.conf.d/90-site.conf clears the time servers hardn sets, and hardn leaves it as it is. To go on: remove that setting, then try again"},
		{"a file at hardn's path that hardn did not write", ubuntu(t).file(tshost.ChronySources, "# mine\n"), timesync.ActionSet, want, timesync.ErrOutside, "NTP_SET_OUTSIDE",
			"/etc/chrony/sources.d/hardn.sources is not hardn's file, and hardn leaves it as it is. To go on: move that file away, then try again"},
		{"timesyncd's main file names a server", timesyncd(t).file(mainFile, "[Time]\nNTP=a.example\n"), timesync.ActionSet, want, timesync.ErrOutside, "NTP_SET_OUTSIDE", "/etc/systemd/timesyncd.conf already sets time servers"},
		{"alpine, options not read", alpineHost(t).file(tshost.ConfD, "NTPD_OPTS=\"-N\"\n"), timesync.ActionSet, want, timesync.ErrNotRead, "NTP_NOT_READ", "hardn can't read ntpd's options: /etc/conf.d/ntpd:1: no -p"},
		{"alpine, not as hardn writes it back", alpineHost(t).file(tshost.ConfD, "NTPD_OPTS='-N -p pool.ntp.org'\n"), timesync.ActionSet, want, timesync.ErrNotRead, "NTP_NOT_READ", "NTPD_OPTS is not written as hardn writes it back"},
		{"another init", ubuntu(t).with(func(m *model) { m.facts.Init.Kind = detect.OpenRC }), timesync.ActionSet, want, timesync.ErrNotRead, "NTP_NOT_READ", "hardn can't read chrony's settings: whether chrony runs"},
		{"remove, nothing set", ubuntu(t), timesync.ActionRemove, want, timesync.ErrNothingToRemove, "NTP_NOTHING_TO_REMOVE", "hardn did not set time servers here"},
		{"remove, set outside hardn", ubuntu(t).file(site, "server 192.0.2.77\n"), timesync.ActionRemove, want, timesync.ErrNothingToRemove, "NTP_NOTHING_TO_REMOVE", "hardn did not set"},
		{"remove, not read", ubuntu(t).file(tshost.ChronyConfDebian, "server\n"), timesync.ActionRemove, want, timesync.ErrNotRead, "NTP_NOT_READ", "hardn can't read chrony's settings"},
		{"remove, stopped", ubuntu(t).set(want).with(stopped), timesync.ActionRemove, want, timesync.ErrNoService, "NTP_NO_SERVICE", "chrony is not running"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(tc.m.writes()) + len(tc.m.ran())
			cs, err := tc.m.plan(tc.a, tc.yml)
			var ref *steps.Refusal
			if !errors.As(err, &ref) || !errors.Is(err, tc.is) || ref.Code != tc.code || output.CodeOf(err) != tc.code || cs != nil {
				t.Fatalf("Plan = %v, %v; want a refusal %s (%v)", cs, err, tc.code, tc.is)
			}
			if !strings.Contains(err.Error(), tc.words) {
				t.Errorf("words:\n%s\nwant\n%s", err, tc.words)
			}
			if after := len(tc.m.writes()) + len(tc.m.ran()); after != before {
				t.Errorf("a refusal wrote or restarted: %v %v", tc.m.writes(), tc.m.ran())
			}
		})
	}
	if _, err := ubuntu(t).plan(timesync.ActionNone, want); !errors.Is(err, engine.ErrNotMigrated) {
		t.Errorf("ActionNone: %v, want ErrNotMigrated", err)
	}
}

// Ubuntu 26.04: hardn's own file, Ubuntu's pools switched off, one reload
// and no restart; Remove servers puts both files back byte for byte.
func TestChronyDropIn(t *testing.T) {
	m := ubuntu(t)
	ids, err := m.apply(timesync.ActionSet, want)
	if err != nil || !slices.Equal(ids, []string{"file", "edit/ubuntu-ntp-pools.sources"}) {
		t.Fatalf("Set = %v, %v", ids, err)
	}
	if got := m.content(tshost.ChronySources); got != tshost.Header+"server 192.0.2.10 iburst\nserver 192.0.2.11 iburst\n" {
		t.Errorf("hardn's file:\n%s", got)
	}
	if got := m.content(timefake.UbuntuPoolsFile); strings.Count(got, "\n"+tshost.Off+"pool ") != 5 || strings.ReplaceAll(got, tshost.Off, "") != timefake.UbuntuPools {
		t.Errorf("Ubuntu's pools:\n%s", got)
	}
	if e, _ := m.f.Entry(timefake.UbuntuPoolsFile); e.Mode.Perm() != 0o640 {
		t.Errorf("the pools' mode is %v, not kept", e.Mode)
	}
	if m.content(tshost.ChronyConfDebian) != timefake.UbuntuConf || !slices.Equal(m.ran(), []string{"chronyc reload sources"}) {
		t.Errorf("main file edited, or ran %v", m.ran())
	}
	if st := m.status(want); st.State != engine.Configured || st.Detail != "192.0.2.10" {
		t.Errorf("status = %+v", st)
	}
	if cs, err := m.plan(timesync.ActionSet, want); err != nil || cs != nil {
		t.Errorf("a second Set plans %v, %v", cs, err)
	}
	// hardn.yml moved: hardn's file rewritten, the pools left as they are.
	moved := config.NTPServers{"192.0.2.12"}
	if ids, err = m.apply(timesync.ActionSet, moved); err != nil || !slices.Equal(ids, []string{"file"}) || !strings.Contains(m.content(tshost.ChronySources), "server 192.0.2.12 iburst") {
		t.Fatalf("moved = %v, %v", ids, err)
	}
	ids, err = m.apply(timesync.ActionRemove, moved)
	if err != nil || !slices.Equal(ids, []string{"file", "edit/ubuntu-ntp-pools.sources"}) {
		t.Fatalf("Remove = %v, %v", ids, err)
	}
	if !m.gone(tshost.ChronySources) || m.content(timefake.UbuntuPoolsFile) != timefake.UbuntuPools {
		t.Errorf("not as it was:\n%s", m.content(timefake.UbuntuPoolsFile))
	}
	if !slices.Equal(m.ran(), []string{"chronyc reload sources", "chronyc reload sources", "chronyc reload sources"}) {
		t.Errorf("ran %v", m.ran())
	}
	if st := m.status(moved); st.Detail != "2.ntp.ubuntu.com" {
		t.Errorf("status after Remove = %+v", st)
	}
}

// Debian's chrony, Proxmox VE's too: the pool is in the main file, which
// chronyd reads at start alone, so a restart.
func TestChronyMainFile(t *testing.T) {
	m := debian(t)
	ids, err := m.apply(timesync.ActionSet, want)
	if err != nil || !slices.Equal(ids, []string{"file", "edit/chrony.conf", "restart"}) || !slices.Equal(m.ran(), []string{"systemctl restart chrony"}) {
		t.Fatalf("Set = %v, %v, ran %v", ids, err, m.ran())
	}
	if got := m.content(tshost.ChronyConfDebian); got != strings.Replace(timefake.DebianConf, "pool 2.debian", tshost.Off+"pool 2.debian", 1) {
		t.Errorf("main file:\n%s", got)
	}
	if _, err = m.apply(timesync.ActionRemove, want); err != nil || m.content(tshost.ChronyConfDebian) != timefake.DebianConf || !m.gone(tshost.ChronySources) {
		t.Fatalf("Remove: %v\n%s", err, m.content(tshost.ChronyConfDebian))
	}
}

// Rocky Linux: no directory to write in, so /etc/chrony.conf is edited in
// place and put back byte for byte.
func TestChronyInPlace(t *testing.T) {
	m := rocky(t)
	ids, err := m.apply(timesync.ActionSet, want)
	if err != nil || !slices.Equal(ids, []string{"edit/chrony.conf", "restart"}) || !slices.Equal(m.ran(), []string{"systemctl restart chronyd"}) {
		t.Fatalf("Set = %v, %v, ran %v", ids, err, m.ran())
	}
	if got := m.content(tshost.ChronyConfEL); !strings.Contains(got, "#hardn: pool 2.rocky.pool.ntp.org iburst\n# Added by hardn (NTP Settings): the 2 lines below.\nserver 192.0.2.10 iburst\nserver 192.0.2.11 iburst\n\n") {
		t.Errorf("main file:\n%s", got)
	}
	if !m.gone(tshost.ChronySources) {
		t.Error("hardn's sources file written where chrony reads none")
	}
	if cs, err := m.plan(timesync.ActionSet, want); err != nil || cs != nil {
		t.Errorf("a second Set plans %v, %v", cs, err)
	}
	if _, err = m.apply(timesync.ActionSet, config.NTPServers{"192.0.2.12"}); err != nil || strings.Contains(m.content(tshost.ChronyConfEL), "192.0.2.10") || strings.Count(m.content(tshost.ChronyConfEL), tshost.Off) != 1 {
		t.Fatalf("moved: %v\n%s", err, m.content(tshost.ChronyConfEL))
	}
	if ids, err = m.apply(timesync.ActionRemove, want); err != nil || !slices.Equal(ids, []string{"edit/chrony.conf", "restart"}) || m.content(tshost.ChronyConfEL) != timefake.ELConf {
		t.Fatalf("Remove = %v, %v\n%s", ids, err, m.content(tshost.ChronyConfEL))
	}
}

// A file edited between the plan and the apply is refused, and left as its
// editor left it: by the engine, which plans again and compares, and by the
// change itself, which reads the file again before it writes.
func TestChangedSince(t *testing.T) {
	for name, tc := range map[string]struct {
		m    func() *model
		file string
	}{"ubuntu's pools": {func() *model { return ubuntu(t) }, timefake.UbuntuPoolsFile}, "chrony.conf": {func() *model { return rocky(t) }, tshost.ChronyConfEL},
		"conf.d": {func() *model { return alpineHost(t) }, tshost.ConfD}} {
		m := tc.m()
		edited := "# edited since\n" + m.content(tc.file)
		if _, err := m.apply(timesync.ActionSet, want, func() { m.file(tc.file, edited) }); !errors.Is(err, engine.ErrPlanChanged) {
			t.Errorf("%s: apply = %v", name, err)
		}
		if m.content(tc.file) != edited || !m.gone(tshost.ChronySources) || len(m.ran()) != 0 {
			t.Errorf("%s: not left alone, or hardn's file kept, or ran %v:\n%s", name, m.ran(), m.content(tc.file))
		}

		m = tc.m()
		cs, err := m.plan(timesync.ActionSet, want)
		if err != nil {
			t.Fatal(err)
		}
		m.file(tc.file, edited)
		for _, c := range cs {
			if c.Target != tc.file {
				continue
			}
			if _, err := c.Apply(context.Background(), m.f, engine.Run{}); !errors.Is(err, timesync.ErrChangedSince) || output.CodeOf(err) != "NTP_CHANGED_SINCE" {
				t.Errorf("%s: the change's apply = %v", name, err)
			}
		}
		if m.content(tc.file) != edited {
			t.Errorf("%s: written over:\n%s", name, m.content(tc.file))
		}
	}
}

// A restart or reload that fails rolls the files back.
func TestRollback(t *testing.T) {
	for name, tc := range map[string]struct {
		m     *model
		files []string
	}{
		"reload":    {ubuntu(t), []string{timefake.UbuntuPoolsFile, tshost.ChronyConfDebian}},
		"restart":   {rocky(t), []string{tshost.ChronyConfEL}},
		"timesyncd": {timesyncd(t), []string{mainFile}},
		"alpine":    {alpineHost(t), []string{tshost.ConfD}},
	} {
		var before []string
		for _, f := range tc.files {
			before = append(before, tc.m.content(f))
		}
		tc.m.sim.FailRestarts = 1
		if _, err := tc.m.apply(timesync.ActionSet, want); err == nil {
			t.Errorf("%s: the apply passed", name)
		}
		for i, f := range tc.files {
			if tc.m.content(f) != before[i] {
				t.Errorf("%s: %s not put back:\n%s", name, f, tc.m.content(f))
			}
		}
		if !tc.m.gone(tshost.ChronySources) || !tc.m.gone(tshost.TimesyncdDropIn) || !tc.m.gone("/etc/systemd/timesyncd.conf.d") {
			t.Errorf("%s: hardn's file or its directory left", name)
		}
		if ran := tc.m.ran(); len(ran) != 2 || ran[0] != ran[1] {
			t.Errorf("%s: ran %v, want the command and its undo", name, ran)
		}
	}
}

func TestTimesyncd(t *testing.T) {
	m := timesyncd(t)
	ids, err := m.apply(timesync.ActionSet, want)
	if err != nil || !slices.Equal(ids, []string{"file", "restart"}) || !slices.Equal(m.ran(), []string{"systemctl restart systemd-timesyncd"}) {
		t.Fatalf("Set = %v, %v, ran %v", ids, err, m.ran())
	}
	if got := m.content(tshost.TimesyncdDropIn); got != tshost.Header+"[Time]\nNTP=192.0.2.10 192.0.2.11\n" {
		t.Errorf("drop-in:\n%s", got)
	}
	if st := m.status(want); st.State != engine.Configured || st.Detail != "192.0.2.10" {
		t.Errorf("status = %+v", st)
	}
	if cs, err := m.plan(timesync.ActionSet, want); err != nil || cs != nil {
		t.Errorf("a second Set plans %v, %v", cs, err)
	}
	if ids, err = m.apply(timesync.ActionRemove, want); err != nil || !slices.Equal(ids, []string{"file", "restart"}) || !m.gone(tshost.TimesyncdDropIn) || m.content(mainFile) != "[Time]\n#NTP=\n" {
		t.Fatalf("Remove = %v, %v", ids, err)
	}
	if st := m.status(want); st.Detail != "ntp.ubuntu.com" {
		t.Errorf("status after Remove = %+v", st)
	}
}

func TestAlpine(t *testing.T) {
	m := alpineHost(t)
	ids, err := m.apply(timesync.ActionSet, want)
	if err != nil || !slices.Equal(ids, []string{"edit/ntpd", "restart"}) || !slices.Equal(m.ran(), []string{"rc-service ntpd restart"}) {
		t.Fatalf("Set = %v, %v, ran %v", ids, err, m.ran())
	}
	if got := m.content(tshost.ConfD); !strings.HasSuffix(got, "# Set by hardn (NTP Settings), in place of: -p pool.ntp.org\nNTPD_OPTS=\"-N -p 192.0.2.10 -p 192.0.2.11\"\n") {
		t.Errorf("conf.d:\n%s", got)
	}
	if cs, err := m.plan(timesync.ActionSet, want); err != nil || cs != nil {
		t.Errorf("a second Set plans %v, %v", cs, err)
	}
	if _, err = m.apply(timesync.ActionRemove, want); err != nil || m.content(tshost.ConfD) != timefake.AlpineConfD {
		t.Fatalf("Remove: %v\n%s", err, m.content(tshost.ConfD))
	}
}

// A plan reads and writes nothing, and restarts nothing.
func TestPlanWritesNothing(t *testing.T) {
	for _, m := range []*model{ubuntu(t), debian(t), rocky(t), timesyncd(t), alpineHost(t)} {
		cs, err := m.plan(timesync.ActionSet, want)
		if err != nil || len(cs) == 0 {
			t.Fatalf("Plan = %v, %v", cs, err)
		}
		if w, r := m.writes(), m.ran(); len(w)+len(r) != 0 {
			t.Errorf("a plan wrote %v, ran %v", w, r)
		}
		for _, c := range cs {
			if len([]rune(c.Title)) > engine.TitleMax {
				t.Errorf("title %q is over %d cells", c.Title, engine.TitleMax)
			}
		}
	}
}

// The review's notice: NTS where the host's own servers are authenticated
// (decision 4), the cluster's line on Proxmox VE (decision 18), the Pi's
// clock on Raspberry Pi OS.
func TestNotice(t *testing.T) {
	ctx := context.Background()
	u := ubuntu(t)
	if got := timesync.Read(ctx, u.f, u.facts, cfg(want)).Notice(timesync.ActionSet, u.facts); got != timesync.NoticeSet+" "+timesync.NoticeNTS {
		t.Errorf("Ubuntu 26.04: %q", got)
	}
	p := debian(t)
	p.facts.Platform.Distro = detect.DistroProxmox
	rd := timesync.Read(ctx, p.f, p.facts, cfg(want))
	if got := rd.Notice(timesync.ActionSet, p.facts); got != timesync.NoticeSet+" "+timesync.NoticeCluster {
		t.Errorf("Proxmox VE: %q", got)
	}
	if got := rd.Notice(timesync.ActionRemove, p.facts); got != timesync.NoticeRemove {
		t.Errorf("Remove: %q", got)
	}
	if cs, err := p.plan(timesync.ActionSet, want); err != nil || len(cs) != 3 {
		t.Errorf("Proxmox VE plans %v, %v", cs, err)
	}
	if timesync.NoticeCluster != "On a cluster, set every node the same way: the nodes need synchronised clocks." {
		t.Errorf("NoticeCluster = %q", timesync.NoticeCluster)
	}
	// Raspberry Pi OS: the Pi's clock (design-custom-distros.md, decision 3 (a)).
	pi := debian(t)
	pi.facts.Platform.Variant = detect.VariantRaspiOS
	rd = timesync.Read(ctx, pi.f, pi.facts, cfg(want))
	const clock = "Most Raspberry Pi models have no clock of their own: if no server is reached, the time is wrong after a reboot."
	if got := rd.Notice(timesync.ActionSet, pi.facts); got != timesync.NoticeSet+" "+clock || timesync.NoticePiClock != clock {
		t.Errorf("Raspberry Pi OS: %q", got)
	}
	if got := rd.Notice(timesync.ActionRemove, pi.facts); got != timesync.NoticeRemove {
		t.Errorf("Raspberry Pi OS, Remove: %q", got)
	}
	if got := rd.Notice(timesync.ActionSet, debian(t).facts); strings.Contains(got, "Raspberry") || strings.Contains(got, "cluster") {
		t.Errorf("Debian: %q", got)
	}
}
