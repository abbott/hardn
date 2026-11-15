package timesync_test

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
	ctl "github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	tshost "github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/host/timesync/timefake"
	"github.com/abbott/hardn/pkg/output"
	tsscreen "github.com/abbott/hardn/pkg/screens/timesync"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

func clock() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) }

var want = config.NTPServers{"192.0.2.10", "192.0.2.11"}

// model is Ubuntu 26.04 with chrony, in sync on Ubuntu's pools.
type model struct {
	f     *hostfake.System
	sim   *timefake.Sim
	facts detect.Facts
}

func newModel() *model {
	m := &model{f: hostfake.New(host.Options{}),
		facts: detect.Facts{Platform: detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}}
	for _, dir := range []string{"/etc/hardn", tshost.SourcesDir, "/var/lib", "/var/backups", "/var/log", "/run"} {
		m.f.AddDir(dir, 0o755, 0, 0)
	}
	m.f.AddFile(tshost.ChronyConfDebian, []byte(timefake.UbuntuConf), 0o644, 0, 0)
	m.f.AddFile(timefake.UbuntuPoolsFile, []byte(timefake.UbuntuPools), 0o644, 0, 0)
	m.sim = &timefake.Sim{Service: "chrony", Synced: "2.ntp.ubuntu.com", Stock: "2.ntp.ubuntu.com", Reachable: []string{"192.0.2.10"}, Packages: map[string]bool{"chrony": true},
		Read: func(name string) ([]byte, error) { return m.f.ReadFile(context.Background(), name) }}
	m.f.Handler = func(argv []string, _ []byte) hostfake.Response {
		if r, ok := m.sim.Answer(argv); ok {
			return hostfake.Response{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.ExitCode}
		}
		return hostfake.Response{ExitCode: 127}
	}
	return m
}

func (m *model) has(name string) bool {
	_, err := m.f.Lstat(context.Background(), name)
	return err == nil
}

type played struct {
	out    string
	err    error
	unread int
	runs   []output.Run
}

// play runs the screen on m with yml as hardn.yml's ntpServers and lines
// typed.
func play(m *model, yml config.NTPServers, dryRun bool, lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	p.err = tsscreen.Run(context.Background(), tsscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   m.f, Facts: m.facts, Config: &config.Config{NTPServers: yml}, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
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

// TestView: the reading as the screen draws it; every word of the host's
// made safe; what is not read said, never "None".
func TestView(t *testing.T) {
	esc := tshost.Reading{Service: tshost.Chrony, Running: detect.Yes, Clock: tshost.InSync, Synced: "a\x1b[2Jb", File: "/etc/x\x1b[0m",
		Servers:  []tshost.Server{{Name: "s\x1b[1m", From: tshost.FromOther, File: "/etc/chrony/sources.d/f\x1b[2J.sources"}, {Name: "192.0.2.10", From: tshost.FromHardn}},
		Restores: []tshost.Server{{Name: "r\x1b[1m"}}}
	v := tsscreen.View(ctl.Reading{Host: esc, Want: []string{"192.0.2.10"}})
	drawn := strings.Join([]string{v.Synced, v.File, v.Servers[0].Name, v.Servers[0].File, v.Restores[0].Name, v.Status.Rows[0].Detail}, "")
	if strings.ContainsRune(drawn, '\x1b') || v.Servers[0].File != "f^[[2J.sources" || v.Servers[1].From != screen.TimeFromHardn || v.Servers[1].File != "" ||
		v.Service != screen.TimeChrony || v.Clock != screen.TimeClockInSync {
		t.Errorf("the view: %+v", v)
	}
	for _, tc := range []struct {
		name  string
		host  tshost.Reading
		check func(screen.TimeSync) bool
	}{
		{"stopped", tshost.Reading{Service: tshost.Timesyncd, Running: detect.No, Clock: tshost.NotInSync}, func(v screen.TimeSync) bool {
			return v.Stopped && v.Service == screen.TimeTimesyncd && v.Clock == screen.TimeClockNotInSync && v.Unread == ""
		}},
		{"no service", tshost.Reading{}, func(v screen.TimeSync) bool {
			return v.Service == screen.TimeNoService && !v.Stopped && v.Clock == screen.TimeClockUnknown
		}},
		{"a container", tshost.Reading{Container: true, Clock: tshost.InSync}, func(v screen.TimeSync) bool {
			return v.Container && v.Service == screen.TimeNoService && v.Status.Rows[0].State == component.StatusNotApplicable
		}},
		{"chrony not read", tshost.Reading{Service: tshost.Chrony, Running: detect.Yes, Err: fs.ErrPermission}, func(v screen.TimeSync) bool { return v.Unread == "Can't read chrony's settings" }},
		{"ntpd not read", tshost.Reading{Service: tshost.Busybox, Running: detect.Yes, Err: fs.ErrPermission}, func(v screen.TimeSync) bool { return v.Unread == "Can't read ntpd's options" }},
		{"the service not told", tshost.Reading{Err: fs.ErrPermission}, func(v screen.TimeSync) bool {
			var words []string
			for _, r := range v.Rows() {
				words = append(words, r.Value)
			}
			items := v.MenuItems()
			return v.Unread == "Can't tell the time service" && !slices.Contains(words, "None") && len(items) == 1 && !items[0].Disabled
		}},
	} {
		if v := tsscreen.View(ctl.Reading{Host: tc.host, WantErr: config.ErrNTPServers}); !tc.check(v) || !v.WantBad {
			t.Errorf("%s: %+v", tc.name, v)
		}
	}
}

// TestSetThenRemove: Set servers through the review and y, the screen then
// on hardn's server with Remove servers; Remove servers, the pools back.
func TestSetThenRemove(t *testing.T) {
	m := newModel()
	p := play(m, want, false, "1", "y", "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 2 {
		t.Fatalf("Run = %v, %d unread, %d runs:\n%s", p.err, p.unread, len(p.runs), p.out)
	}
	for _, w := range []string{"1) Set servers          192.0.2.10, 192.0.2.11", "Set time servers? [y/N, 0 or q]:", ctl.NoticeSet[:40], "authenticated (NTS)", "$ chronyc reload sources",
		"+ 192.0.2.10", "Server               192.0.2.10 by hardn", "1) Remove servers       Undo hardn's change", "Remove hardn's time servers? [y/N, 0 or q]:", ctl.NoticeRemove,
		"Server               ntp-bootstrap.ubuntu.com"} {
		if !strings.Contains(p.out, w) {
			t.Errorf("no %q in:\n%s", w, p.out)
		}
	}
	if data, _ := m.f.ReadFile(context.Background(), timefake.UbuntuPoolsFile); m.has(tshost.ChronySources) || string(data) != timefake.UbuntuPools {
		t.Errorf("not as it was:\n%s", data)
	}
}

// TestNothingApplied: n, and a dry run, change nothing; a refusal is said
// before anything is asked.
func TestNothingApplied(t *testing.T) {
	for name, tc := range map[string]struct {
		yml   config.NTPServers
		dry   bool
		lines []string
		says  string
	}{
		"n":          {want, false, []string{"1", "n", "0"}, "Set time servers? [y/N, 0 or q]:"},
		"dry run":    {want, true, []string{"1", "0"}, "Write hardn's time servers"},
		"no servers": {nil, false, []string{"1", "0"}, "hardn.yml names no time servers."},
		"not valid":  {config.NTPServers{"a b"}, false, []string{"1", "0"}, "hardn.yml's ntpServers is not valid: server 1 is not an IPv4"},
	} {
		m := newModel()
		p := play(m, tc.yml, tc.dry, tc.lines...)
		if p.err != nil || p.unread != 0 || !strings.Contains(p.out, tc.says) || m.has(tshost.ChronySources) || (tc.dry || len(tc.yml) != 2) && strings.Contains(p.out, "[y/N") {
			t.Errorf("%s: Run = %v, %d unread:\n%s", name, p.err, p.unread, p.out)
		}
		for _, argv := range m.f.Runs() {
			if strings.Contains(strings.Join(argv, " "), "re") && argv[0] != "dpkg-query" && (argv[1] == "reload" || argv[1] == "restart") {
				t.Errorf("%s: ran %v", name, argv)
			}
		}
	}
}

func TestRunNeedsItsOptions(t *testing.T) {
	if err := tsscreen.Run(context.Background(), tsscreen.Options{}); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Run with no options = %v", err)
	}
}
