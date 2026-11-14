package logforward_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	ctl "github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	lfscreen "github.com/abbott/hardn/pkg/screens/logforward"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

func clock() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }

const (
	foreign = "/etc/rsyslog.d/50-remote.conf"
	stock   = "SYSLOGD_OPTS=\"-t\"\n"
)

var (
	ymlTCP = config.LogForwarding{Server: "192.0.2.50"}
	ymlUDP = config.LogForwarding{Server: "192.0.2.50", Protocol: "udp"}
	tcp    = logfwd.Server{Address: "192.0.2.50", Port: "514", Proto: logfwd.TCP}
)

// model is a fake host: rsyslog installed or not, and how its commands
// answer.
type model struct {
	f       *hostfake.System
	facts   detect.Facts
	rsyslog bool
	open    bool // ss shows rsyslogd's connection to 192.0.2.50:514
	noSS    bool // ss is not installed
}

func newModel(d detect.Distro, rsyslog bool) *model {
	m := &model{f: hostfake.New(host.Options{}), rsyslog: rsyslog,
		facts: detect.Facts{Platform: detect.Platform{Distro: d, Version: "1", Support: detect.Supported}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}}
	for _, dir := range []string{"/etc/rsyslog.d", "/etc/conf.d", "/var/lib", "/var/backups", "/run"} {
		m.f.AddDir(dir, 0o755, 0, 0)
	}
	if d == detect.DistroAlpine {
		m.facts.Init.Kind = detect.OpenRC
		m.file(logfwd.ConfD, stock)
		m.file(alpine.InstalledDB, "P:busybox\nV:1.37.0-r30\n\n")
	}
	m.f.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd, pkg := strings.Join(argv, " "), argv[len(argv)-1]
		switch {
		case argv[0] == "dpkg-query" && m.rsyslog:
			return hostfake.Response{Stdout: "install ok installed\t8.2504.0\n"}
		case argv[0] == "dpkg-query":
			return hostfake.Response{ExitCode: 1}
		case strings.HasPrefix(cmd, "apt-get install -s -y "):
			return hostfake.Response{Stdout: "Inst " + pkg + " (8.2504.0 Debian:13/stable [amd64])\n"}
		case cmd == "apt-get install -y rsyslog":
			m.rsyslog = true
		case cmd == "apt-get remove -y rsyslog":
			m.rsyslog = false
		case cmd == "ss -Htnp" && m.noSS:
			return hostfake.Response{Err: host.ErrNotFound}
		case cmd == "ss -Htnp" && m.open:
			return hostfake.Response{Stdout: "ESTAB 0 0 192.0.2.10:41234 192.0.2.50:514 users:((\"rsyslogd\",pid=612,fd=9))\n"}
		case cmd == "systemctl restart rsyslog":
			m.open = true
		case cmd == "ss -Htnp", cmd == "rsyslogd -N1", cmd == "rc-service syslog restart", argv[0] == "logger":
		default:
			return hostfake.Response{ExitCode: 127}
		}
		return hostfake.Response{}
	}
	return m
}

func (m *model) file(name, data string) *model {
	m.f.AddFile(name, []byte(data), 0o644, 0, 0)
	return m
}

func (m *model) own(t *testing.T, s logfwd.Server) *model {
	t.Helper()
	data, err := logfwd.RsyslogFile(s)
	if err != nil {
		t.Fatal(err)
	}
	return m.file(logfwd.DropIn, string(data))
}

func (m *model) has(name string) bool {
	_, err := m.f.ReadFile(context.Background(), name)
	return err == nil
}

// changed are the commands that change the host, of those run.
func (m *model) changed() []string {
	var out []string
	for _, argv := range m.f.Runs() {
		if cmd := strings.Join(argv, " "); strings.HasPrefix(cmd, "apt-get install -y") || strings.Contains(cmd, "restart") || argv[0] == "logger" {
			out = append(out, cmd)
		}
	}
	return out
}

type played struct {
	out    string
	runs   []output.Run
	err    error
	unread int
}

// play runs the screen on m with yml as hardn.yml's logForwarding and lines
// typed.
func play(m *model, yml config.LogForwarding, dryRun bool, lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	p.err = lfscreen.Run(context.Background(), lfscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   m.f, Facts: m.facts, Config: &config.Config{LogForwarding: yml}, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
		NewEngine: func(progress func(engine.Event)) *engine.Engine {
			return engine.New(m.f, engine.Options{Revert: revert.Options{Now: clock, Hardn: "/usr/local/bin/hardn"}, Progress: progress,
				Detect: func(context.Context, host.System) (detect.Facts, error) { return m.facts, nil },
				Hold:   func() func() { return func() {} }, PID: 5000, Backups: hostbackup.Settings{Path: hostbackup.DefaultPath, Enabled: true}})
		},
		Ran: func(r output.Run) { p.runs = append(p.runs, r) },
	})
	p.out, p.unread = out.String(), ty.Unread()
	return p
}

// flat is out on one line, its spaces single: a callout wraps its words.
func flat(out string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(out, "\n# ", " ")), " ")
}

// TestView: the control's reading as the screen draws it, in every state
// the mocks show (gallery-logforward.md §2) and those a fact not read adds.
func TestView(t *testing.T) {
	ctx := context.Background()
	set, err := logfwd.SetOpts([]byte(stock), logfwd.Server{Address: "192.0.2.50", Port: "514"})
	if err != nil {
		t.Fatal(err)
	}
	open := func(m *model) *model { m.open = true; return m }
	for _, tc := range []struct {
		name  string
		m     *model
		yml   config.LogForwarding
		rows  []string // "Key Value Note", in order
		items []string // "key label: description"
	}{
		{"off", newModel(detect.DistroUbuntu, true), ymlTCP, []string{"Logger rsyslog", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set server: 192.0.2.50:514 TCP"}},
		{"no-server", newModel(detect.DistroUbuntu, true), config.LogForwarding{}, []string{"Logger rsyslog", "Server None", "hardn.yml No server"}, []string{"1 Set server: No server in hardn.yml"}},
		{"tcp", open(newModel(detect.DistroUbuntu, true).own(t, tcp)), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514", "Protocol TCP not encrypted", "Connection Open", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"tcp-down", newModel(detect.DistroUbuntu, true).own(t, tcp), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514", "Protocol TCP not encrypted", "Connection None", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"sockets not read", func() *model { m := newModel(detect.DistroUbuntu, true).own(t, tcp); m.noSS = true; return m }(), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514", "Protocol TCP not encrypted", "Connection", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"foreign", open(newModel(detect.DistroUbuntu, true).file(foreign, "*.* @@192.0.2.50\n")), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514", "Protocol TCP not encrypted", "Connection Open", "File " + foreign, "Set by Not hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Set server: Set outside hardn (unavailable)"}},
		{"mismatch", open(newModel(detect.DistroUbuntu, true).own(t, tcp)), config.LogForwarding{Server: "192.0.2.60"},
			[]string{"Logger rsyslog", "Server 192.0.2.50:514", "Protocol TCP not encrypted", "Connection Open", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.60:514 TCP differs"},
			[]string{"1 Set server: 192.0.2.60:514 TCP", "2 Stop forwarding: Undo hardn's change"}},
		{"journald, proxmox", newModel(detect.DistroProxmox, false), ymlTCP, []string{"Logger journald alone no syslog forwarder", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set server: Installs rsyslog"}},
		{"journald", newModel(detect.DistroDebian, false), ymlTCP, []string{"Logger journald alone no syslog forwarder", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set server: Installs rsyslog"}},
		{"alpine", newModel(detect.DistroAlpine, false).file(logfwd.ConfD, string(set)), ymlUDP,
			[]string{"Logger busybox syslogd", "Server 192.0.2.50:514", "Protocol UDP not encrypted, losses unseen", "File " + logfwd.ConfD, "Set by hardn", "hardn.yml 192.0.2.50:514 UDP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"alpine-tcp", newModel(detect.DistroAlpine, false), ymlTCP, []string{"Logger busybox syslogd", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set server: busybox syslogd is UDP only"}},
		{"not-read, malformed", newModel(detect.DistroUbuntu, true).file(foreign, "action(type=\"omfwd\" target=\"192.0.2.50\"\n"), ymlTCP,
			[]string{"Logger rsyslog", "Server", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set server: Can't read rsyslog's settings"}},
		{"not-read, alpine", newModel(detect.DistroAlpine, false).file(logfwd.ConfD, "SYSLOGD_OPTS=\"$X\"\n"), ymlUDP,
			[]string{"Logger busybox syslogd", "Server", "hardn.yml 192.0.2.50:514 UDP"}, []string{"1 Set server: Can't read syslogd's options"}},
		{"no logger hardn reads", newModel("arch", false), ymlTCP, []string{"Logger", "Server", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set server: Can't tell the logger"}},
		{"hardn.yml not valid", newModel(detect.DistroUbuntu, true), config.LogForwarding{Server: "logs example"},
			[]string{"Logger rsyslog", "Server None", "hardn.yml Not valid"}, []string{"1 Set server: hardn.yml is not valid"}},
		{"an escape in a file's server", newModel(detect.DistroUbuntu, true).file(foreign, "*.* @192.0.2.50\x1b[2J\n"), config.LogForwarding{},
			[]string{"Logger rsyslog", "Server 192.0.2.50^[[2J:514", "Protocol UDP not encrypted, losses unseen", "File " + foreign, "Set by Not hardn", "hardn.yml No server"},
			[]string{"1 Set server: Set outside hardn (unavailable)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rd := ctl.Read(ctx, tc.m.f, tc.m.facts, &config.Config{LogForwarding: tc.yml})
			v := lfscreen.View(rd)
			var rows, items []string
			for _, kv := range v.Rows() {
				rows = append(rows, strings.TrimSpace(kv.Key+" "+kv.Value+" "+kv.Note))
			}
			for _, it := range v.MenuItems() {
				s := it.Key + " " + it.Label + ": " + it.Description
				if it.Disabled {
					s += " (unavailable)"
				}
				items = append(items, s)
			}
			if !slices.Equal(rows, tc.rows) || !slices.Equal(items, tc.items) {
				t.Errorf("rows %q\nwant %q\nitems %q\nwant %q", rows, tc.rows, items, tc.items)
			}
			if len(v.Status.Rows) != 1 || v.Status.Rows[0].Label != ctl.Title || v.Status.Rows[0].Detail != rd.Status().Detail {
				t.Errorf("the status row %+v, the control's %+v", v.Status.Rows, rd.Status())
			}
			if strings.Contains(v.SubMenu().Render(component.OffTerminal()), "\x1b[2J") {
				t.Error("an escape of the host's reaches the screen")
			}
		})
	}
}

// Set server applied: the review, the question, hardn's file, the restart
// and the test line; then the screen again, Configured, with Stop
// forwarding, which applied leaves the host as it was.
func TestSetThenStop(t *testing.T) {
	m := newModel(detect.DistroUbuntu, true)
	p := play(m, ymlTCP, false, "1", "y", "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 2 {
		t.Fatalf("err %v, %d unread, %d runs:\n%s", p.err, p.unread, len(p.runs), p.out)
	}
	want := []string{"1) Set server", "Every message the host logs goes to the server, as it is logged.", "Write hardn's forwarding file", "$ rsyslogd -N1",
		"Forward logs to 192.0.2.50? [y/N, 0 or q]:", "Configured 192.0.2.50:514 TCP", "1) Stop forwarding", "Logs stay on the host alone. rsyslog stays installed.",
		"Delete hardn's forwarding file", "Stop forwarding logs? [y/N, 0 or q]:", "Not Configured not forwarding"}
	at, out := 0, flat(p.out)
	for _, w := range want {
		i := strings.Index(out[at:], flat(w))
		if i < 0 {
			t.Fatalf("no %q, in order, in:\n%s", w, p.out)
		}
		at += i
	}
	if got := m.changed(); !slices.Equal(got, []string{"systemctl restart rsyslog", "logger -t hardn log forwarding set", "systemctl restart rsyslog"}) {
		t.Errorf("ran %q", got)
	}
	if m.has(logfwd.DropIn) {
		t.Error("hardn's file is left after Stop forwarding")
	}
}

// journald alone: Set server installs rsyslog first, and says what else
// that does.
func TestSetJournald(t *testing.T) {
	m := newModel(detect.DistroDebian, false)
	p := play(m, ymlTCP, false, "1", "y", "0")
	if p.err != nil || !m.rsyslog || !m.has(logfwd.DropIn) || !strings.Contains(flat(p.out), "rsyslog is installed to forward the journal. It also keeps its own log files under /var/log.") ||
		!strings.Contains(p.out, "$ apt-get install -y rsyslog") {
		t.Errorf("err %v, rsyslog %t:\n%s", p.err, m.rsyslog, p.out)
	}
}

// n, and a dry run, change nothing; a dry run asks nothing.
func TestNothingApplied(t *testing.T) {
	m := newModel(detect.DistroUbuntu, true)
	if p := play(m, ymlTCP, false, "1", "n", "0"); p.err != nil || m.has(logfwd.DropIn) || len(m.changed()) != 0 || !strings.Contains(p.out, "[y/N") {
		t.Errorf("n: err %v, ran %q:\n%s", p.err, m.changed(), p.out)
	}
	m = newModel(detect.DistroUbuntu, true)
	if p := play(m, ymlTCP, true, "1", "0"); p.err != nil || p.unread != 0 || m.has(logfwd.DropIn) || len(m.changed()) != 0 || strings.Contains(p.out, "[y/N") ||
		!strings.Contains(p.out, "Write hardn's forwarding file") {
		t.Errorf("dry run: err %v, %d unread, ran %q:\n%s", p.err, p.unread, m.changed(), p.out)
	}
}

// A refusal is said before anything is asked, and the screen drawn again;
// an unavailable item says so and makes no engine.
func TestRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		m     *model
		yml   config.LogForwarding
		words string
	}{
		{"no server", newModel(detect.DistroUbuntu, true), config.LogForwarding{}, "hardn.yml names no log server. Add logForwarding's server, then try again."},
		{"not valid", newModel(detect.DistroUbuntu, true), config.LogForwarding{Server: "a;b"}, "hardn.yml's logForwarding is not valid: server is not an IPv4 address or a host name. Fix that, then try again."},
		{"alpine, tcp", newModel(detect.DistroAlpine, false), ymlTCP, "busybox syslogd forwards over UDP only. Set protocol: udp in hardn.yml, then try again."},
		{"not read", newModel(detect.DistroUbuntu, true).file(foreign, "*.* @@:514\n"), ymlTCP, "hardn can't read rsyslog's settings: /etc/rsyslog.d/50-remote.conf:1: a forwarding line with no host. Fix that, then try again."},
	} {
		p := play(tc.m, tc.yml, false, "1", "0")
		if p.err != nil || p.unread != 0 || len(p.runs) != 0 || strings.Contains(p.out, "[y/N") || !strings.Contains(flat(p.out), flat(tc.words)) || len(tc.m.changed()) != 0 ||
			strings.Count(p.out, "Select an option") != 2 {
			t.Errorf("%s: err %v, %d unread, %d runs, ran %q:\n%s", tc.name, p.err, p.unread, len(p.runs), tc.m.changed(), p.out)
		}
	}
	m := newModel(detect.DistroUbuntu, true).file(foreign, "*.* @@192.0.2.60\n")
	if p := play(m, ymlTCP, false, "1", "0"); p.err != nil || strings.Contains(p.out, "[y/N") || strings.Contains(p.out, "Checking") || len(m.changed()) != 0 {
		t.Errorf("set outside hardn: err %v:\n%s", p.err, p.out)
	}
}

func TestRunNeedsItsOptions(t *testing.T) {
	if err := lfscreen.Run(context.Background(), lfscreen.Options{}); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Run with no options: %v, want fs.ErrInvalid", err)
	}
}

// The view's keys are the screen's: Set server 1, Stop forwarding beside
// it 2.
func TestKeys(t *testing.T) {
	if screen.LogForwardFirstKey != "1" || screen.LogForwardSecondKey != "2" {
		t.Error("the items' keys moved")
	}
}
