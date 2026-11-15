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
	caFile  = "/etc/ssl/lab-ca.pem"
)

var (
	ymlTCP = config.SyslogServers{{Server: "192.0.2.50"}}
	ymlUDP = config.SyslogServers{{Server: "192.0.2.50", Protocol: "udp"}}
	tcp    = logfwd.Server{Address: "192.0.2.50", Port: "514", Proto: logfwd.TCP}
	udp2   = logfwd.Server{Address: "192.0.2.52", Port: "514", Proto: logfwd.UDP}
	tls    = logfwd.Server{Address: "logs.example.net", Port: "6514", Proto: logfwd.TLS, CAFile: caFile}
	// Three, as hardn.yml names them: TLS by name, TCP, UDP.
	ymlThree = config.SyslogServers{{Server: "logs.example.net", Protocol: "tls", CAFile: caFile}, ymlTCP[0], {Server: "192.0.2.52", Protocol: "udp"}}
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

func (m *model) own(t *testing.T, s ...logfwd.Server) *model {
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

// play runs the screen on m with yml as hardn.yml's syslogServers and lines
// typed.
func play(m *model, yml config.SyslogServers, dryRun bool, lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	p.err = lfscreen.Run(context.Background(), lfscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   m.f, Facts: m.facts, Config: &config.Config{SyslogServers: yml}, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
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
	set, err := logfwd.SetOpts([]byte(stock), []logfwd.Server{{Address: "192.0.2.50", Port: "514"}})
	if err != nil {
		t.Fatal(err)
	}
	open := func(m *model) *model { m.open = true; return m }
	for _, tc := range []struct {
		name  string
		m     *model
		yml   config.SyslogServers
		rows  []string // "Key Value Note", in order
		items []string // "key label: description"
	}{
		{"off", newModel(detect.DistroUbuntu, true), ymlTCP, []string{"Logger rsyslog", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set servers: 192.0.2.50:514 TCP"}},
		{"no-server", newModel(detect.DistroUbuntu, true), config.SyslogServers{}, []string{"Logger rsyslog", "Server None", "hardn.yml No server"}, []string{"1 Set servers: No server in hardn.yml"}},
		{"tcp", open(newModel(detect.DistroUbuntu, true).own(t, tcp)), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP Open", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"tcp-down", newModel(detect.DistroUbuntu, true).own(t, tcp), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP not reached", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"sockets not read", func() *model { m := newModel(detect.DistroUbuntu, true).own(t, tcp); m.noSS = true; return m }(), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"foreign", open(newModel(detect.DistroUbuntu, true).file(foreign, "*.* @@192.0.2.50\n")), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP Open", "File " + foreign, "Set by Not hardn", "hardn.yml 192.0.2.50:514 TCP"},
			[]string{"1 Set servers: Set outside hardn (unavailable)"}},
		{"mismatch", open(newModel(detect.DistroUbuntu, true).own(t, tcp)), config.SyslogServers{{Server: "192.0.2.60"}},
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP Open", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.60:514 TCP differs"},
			[]string{"1 Set servers: 192.0.2.60:514 TCP", "2 Stop forwarding: Undo hardn's change"}},
		// The design's screen (design-syslog-servers.md §2.4): three servers, one not reached.
		{"three, one not reached", open(newModel(detect.DistroUbuntu, true).own(t, tls, tcp, udp2)), ymlThree,
			[]string{"Logger rsyslog", "Server logs.example.net:6514 TLS not reached", "CA File " + caFile, "Server 192.0.2.50:514 TCP Open", "Server 192.0.2.52:514 UDP losses unseen",
				"File " + logfwd.DropIn, "Set by hardn", "hardn.yml 3 servers"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"hardn's two, three asked", open(newModel(detect.DistroUbuntu, true).own(t, tcp, udp2)), ymlThree,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP Open", "Server 192.0.2.52:514 UDP losses unseen", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 3 servers differs"},
			[]string{"1 Set servers: 3 servers", "2 Stop forwarding: Undo hardn's change"}},
		{"hardn's as asked, another file beside it", open(newModel(detect.DistroUbuntu, true).own(t, tcp).file("/etc/rsyslog.d/70-more.conf", "*.* @192.0.2.52\n")), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP Open", "File " + logfwd.DropIn, "Set by hardn", "hardn.yml 192.0.2.50:514 TCP differs"},
			[]string{"1 Set servers: 192.0.2.50:514 TCP", "2 Stop forwarding: Undo hardn's change"}},
		{"several set outside hardn, in two files", open(newModel(detect.DistroUbuntu, true).file(foreign, "*.* @@192.0.2.50\n").file("/etc/rsyslog.d/70-more.conf", "*.* @192.0.2.52\n")), ymlTCP,
			[]string{"Logger rsyslog", "Server 192.0.2.50:514 TCP Open", "Server 192.0.2.52:514 UDP losses unseen", "File 2 files", "Set by Not hardn", "hardn.yml 192.0.2.50:514 TCP differs"},
			[]string{"1 Set servers: Set outside hardn (unavailable)"}},
		{"alpine, one tcp of two", newModel(detect.DistroAlpine, false), config.SyslogServers{ymlUDP[0], {Server: "192.0.2.53"}},
			[]string{"Logger busybox syslogd", "Server None", "hardn.yml 2 servers"}, []string{"1 Set servers: busybox syslogd is UDP only"}},
		{"journald, proxmox", newModel(detect.DistroProxmox, false), ymlTCP, []string{"Logger journald alone no syslog forwarder", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set servers: Installs rsyslog"}},
		{"journald", newModel(detect.DistroDebian, false), ymlTCP, []string{"Logger journald alone no syslog forwarder", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set servers: Installs rsyslog"}},
		{"alpine", newModel(detect.DistroAlpine, false).file(logfwd.ConfD, string(set)), ymlUDP,
			[]string{"Logger busybox syslogd", "Server 192.0.2.50:514 UDP losses unseen", "File " + logfwd.ConfD, "Set by hardn", "hardn.yml 192.0.2.50:514 UDP"},
			[]string{"1 Stop forwarding: Undo hardn's change"}},
		{"alpine-tcp", newModel(detect.DistroAlpine, false), ymlTCP, []string{"Logger busybox syslogd", "Server None", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set servers: busybox syslogd is UDP only"}},
		{"not-read, malformed", newModel(detect.DistroUbuntu, true).file(foreign, "action(type=\"omfwd\" target=\"192.0.2.50\"\n"), ymlTCP,
			[]string{"Logger rsyslog", "Server", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set servers: Can't read rsyslog's settings"}},
		{"not-read, alpine", newModel(detect.DistroAlpine, false).file(logfwd.ConfD, "SYSLOGD_OPTS=\"$X\"\n"), ymlUDP,
			[]string{"Logger busybox syslogd", "Server", "hardn.yml 192.0.2.50:514 UDP"}, []string{"1 Set servers: Can't read syslogd's options"}},
		{"no logger hardn reads", newModel("arch", false), ymlTCP, []string{"Logger", "Server", "hardn.yml 192.0.2.50:514 TCP"}, []string{"1 Set servers: Can't tell the logger"}},
		{"hardn.yml not valid", newModel(detect.DistroUbuntu, true), config.SyslogServers{{Server: "logs example"}},
			[]string{"Logger rsyslog", "Server None", "hardn.yml Not valid"}, []string{"1 Set servers: hardn.yml is not valid"}},
		{"an escape in a file's server", newModel(detect.DistroUbuntu, true).file(foreign, "*.* @192.0.2.50\x1b[2J\n"), config.SyslogServers{},
			[]string{"Logger rsyslog", "Server 192.0.2.50^[[2J:514 UDP losses unseen", "File " + foreign, "Set by Not hardn", "hardn.yml No server"},
			[]string{"1 Set servers: Set outside hardn (unavailable)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rd := ctl.Read(ctx, tc.m.f, tc.m.facts, &config.Config{SyslogServers: tc.yml})
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

// Set servers applied: the review, the question, hardn's file, the restart
// and the test line; then the screen again, Configured, with Stop
// forwarding, which applied leaves the host as it was.
func TestSetThenStop(t *testing.T) {
	m := newModel(detect.DistroUbuntu, true)
	p := play(m, ymlTCP, false, "1", "y", "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 2 {
		t.Fatalf("err %v, %d unread, %d runs:\n%s", p.err, p.unread, len(p.runs), p.out)
	}
	want := []string{"1) Set servers", "Every message the host logs goes to the server, as it is logged.", "Write hardn's forwarding file", "$ rsyslogd -N1",
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

// Several servers: the review a Server row each, the notice and the
// question in the plural, one file, one restart, one test line; then the
// screen, a row per server, and Stop forwarding takes them all out.
func TestSetSeveral(t *testing.T) {
	m := newModel(detect.DistroUbuntu, true)
	two := config.SyslogServers{ymlTCP[0], {Server: "192.0.2.52", Protocol: "udp"}}
	p := play(m, two, false, "1", "y", "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 2 {
		t.Fatalf("err %v, %d unread, %d runs:\n%s", p.err, p.unread, len(p.runs), p.out)
	}
	want := []string{"1) Set servers 2 servers", "Every message the host logs goes to each server, as it is logged.",
		"Server 192.0.2.50:514 TCP", "Server 192.0.2.52:514 UDP losses unseen", "Write hardn's forwarding file",
		"+ target=\"192.0.2.50\" port=\"514\"", "+ target=\"192.0.2.52\" port=\"514\"", "$ rsyslogd -N1",
		"Forward logs to 2 servers? [y/N, 0 or q]:", "Configured 2 servers", "Server 192.0.2.50:514 TCP Open", "Server 192.0.2.52:514 UDP losses unseen",
		"hardn.yml 2 servers", "1) Stop forwarding", "Stop forwarding logs? [y/N, 0 or q]:", "Not Configured not forwarding"}
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

// journald alone: Set servers installs rsyslog first, and says what else
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
		yml   config.SyslogServers
		words string
	}{
		{"no server", newModel(detect.DistroUbuntu, true), config.SyslogServers{}, "hardn.yml names no syslog server. Add syslogServers, then try again."},
		{"not valid", newModel(detect.DistroUbuntu, true), config.SyslogServers{{Server: "a;b"}}, "hardn.yml's syslogServers is not valid: server 1: server is not an IPv4 address or a host name. Fix that, then try again."},
		{"a server twice", newModel(detect.DistroUbuntu, true), config.SyslogServers{ymlTCP[0], ymlUDP[0]}, "hardn.yml's syslogServers is not valid: server 2: the same address and port as server 1. Fix that, then try again."},
		{"alpine, tcp", newModel(detect.DistroAlpine, false), ymlTCP, "busybox syslogd forwards over UDP only. Set protocol: udp for 192.0.2.50 in hardn.yml, then try again."},
		{"alpine, one tcp of two", newModel(detect.DistroAlpine, false), config.SyslogServers{{Server: "192.0.2.52", Protocol: "udp"}, {Server: "logs.example.net"}},
			"busybox syslogd forwards over UDP only. Set protocol: udp for logs.example.net in hardn.yml, then try again."},
		{"one CA file missing", newModel(detect.DistroUbuntu, true), ymlThree, "logs.example.net's caFile, /etc/ssl/lab-ca.pem, is not a file on this host."},
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

// The view's keys are the screen's: Set servers 1, Stop forwarding beside
// it 2.
func TestKeys(t *testing.T) {
	if screen.LogForwardFirstKey != "1" || screen.LogForwardSecondKey != "2" {
		t.Error("the items' keys moved")
	}
}
