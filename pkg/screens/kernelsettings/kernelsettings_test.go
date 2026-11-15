package kernelsettings_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	ctl "github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/host"
	hostbackup "github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	khost "github.com/abbott/hardn/pkg/host/sysctl"
	"github.com/abbott/hardn/pkg/host/sysctl/sysctlfake"
	"github.com/abbott/hardn/pkg/output"
	ksscreen "github.com/abbott/hardn/pkg/screens/kernelsettings"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

func clock() time.Time { return time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) }

// rocky is Rocky Linux 10's values of 2026-11-14.
var rocky = map[string]int{"net.ipv4.conf.*.accept_redirects": 1, "net.ipv6.conf.*.accept_redirects": 1, khost.PtraceKey: 0}

// model is a host with eth0, procps's sysctl and systemd.
type model struct {
	f     *hostfake.System
	sim   *sysctlfake.Sim
	facts detect.Facts
}

func newModel(over map[string]int) *model {
	m := &model{f: hostfake.New(host.Options{}),
		facts: detect.Facts{Platform: detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}, Init: detect.Init{Kind: detect.Systemd, Running: detect.Yes}}}
	for _, dir := range []string{"/etc/hardn", "/etc/sysctl.d", "/var/lib", "/var/backups", "/var/log", "/run"} {
		m.f.AddDir(dir, 0o755, 0, 0)
	}
	for name, data := range sysctlfake.Files(sysctlfake.Values([]string{"eth0"}, over)) {
		m.f.AddFile(name, []byte(data), 0o644, 0, 0)
	}
	m.f.AddFile(sysctlfake.ProcpsPath, nil, 0o755, 0, 0)
	m.sim = &sysctlfake.Sim{Sys: m.f}
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

// play runs the screen on m with lines typed.
func play(m *model, dryRun bool, lines ...string) played {
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, lines...)
	var p played
	p.err = ksscreen.Run(context.Background(), ksscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)},
		System:   m.f, Facts: m.facts, DryRun: dryRun, LogPath: output.LegacyLogPath, Now: clock,
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

// TestView: the reading as the screen draws it: each row's weakest value
// and its words, the file that holds a key by its base name, made safe;
// what is not read said; Remove settings' rows at their earlier values.
func TestView(t *testing.T) {
	m := newModel(rocky)
	m.f.AddFile("/etc/sysctl.d/99-\x1b[2Jx.conf", []byte("kernel.yama.ptrace_scope = 0\n"), 0o644, 0, 0)
	v := ksscreen.View(ctl.Read(context.Background(), m.f, m.facts))
	if len(v.Rows) != len(khost.Specs) || v.File != "" || v.Unread != "" || v.Container || v.NotAtBoot {
		t.Fatalf("the view: %+v", v)
	}
	want := map[string]screen.KernelRow{
		"ICMP redirects":      {Label: "ICMP redirects", Value: 1, Words: "accepted", State: screen.KernelBelow, SetsWords: "ignored"},
		"Reverse-path filter": {Label: "Reverse-path filter", Value: 1, Words: "strict", State: screen.KernelPass, Sets: 2, SetsWords: "loose"},
		"Forwarding":          {Label: "Forwarding", Words: "off", State: screen.KernelNoVerdict, SetsWords: "off"},
		"Process tracing":     {Section: 1, Label: "Process tracing", Words: "unrestricted", State: screen.KernelHeld, Sets: 1, SetsWords: "children only", By: "99-^[[2Jx.conf"},
		"Files in /tmp":       {Section: 2, Label: "Files in /tmp", Value: 1, Words: "protected", State: screen.KernelPass, Sets: 1, SetsWords: "protected"},
	}
	for _, r := range v.Rows {
		if w, ok := want[r.Label]; ok && r != w {
			t.Errorf("%s: %+v", r.Label, r)
		}
	}
	if st := v.Status.Rows[0]; st.State != component.StatusPartial || st.Detail != "1 set outside hardn" {
		t.Errorf("the row: %+v", st)
	}

	m = newModel(map[string]int{"net.ipv4.conf.*.send_redirects": 0})
	m.f.AddFile(khost.File, []byte(khost.Header+"# before: net.ipv4.conf.all.send_redirects = 1\nnet.ipv4.conf.*.send_redirects = 0\n# before: kernel.kptr_restrict = 0\nkernel.kptr_restrict = 1\n"), 0o644, 0, 0)
	v = ksscreen.View(ctl.Read(context.Background(), m.f, m.facts))
	if v.File != khost.File || len(v.Restores) != 2 || v.Restores[0] != (screen.KernelRow{Label: "Sending redirects", Value: 1, Words: "on"}) ||
		v.Restores[1] != (screen.KernelRow{Section: 1, Label: "Kernel addresses", Words: "shown to users"}) {
		t.Errorf("hardn's file: %q, restores %+v", v.File, v.Restores)
	}

	m = newModel(rocky)
	m.f.AddFile(khost.ProcPath("kernel.dmesg_restrict"), []byte("?\n"), 0o644, 0, 0)
	v = ksscreen.View(ctl.Read(context.Background(), m.f, m.facts))
	if items := v.MenuItems(); v.Unread != "Can't read the settings" || len(items) != 1 || items[0].Disabled || v.Status.Rows[0].State != component.StatusUnknown {
		t.Errorf("not read: %+v", v)
	}

	m = newModel(rocky)
	m.facts.Container.Detected = detect.Yes
	if v = ksscreen.View(ctl.Read(context.Background(), m.f, m.facts)); !v.Container || v.Status.Rows[0].State != component.StatusNotApplicable {
		t.Errorf("a container: %+v", v)
	}
	m = newModel(nil)
	m.facts.Platform.Distro = detect.DistroProxmox
	if v = ksscreen.View(ctl.Read(context.Background(), m.f, m.facts)); !v.Proxmox {
		t.Errorf("Proxmox VE: %+v", v)
	}
}

// TestApplyThenRemove: Apply settings through the review and y, the screen
// then at the baseline with hardn's file and Remove settings; Remove
// settings, the earlier values back.
func TestApplyThenRemove(t *testing.T) {
	m := newModel(rocky)
	p := play(m, false, "1", "y", "1", "y", "0")
	if p.err != nil || p.unread != 0 || len(p.runs) != 2 {
		t.Fatalf("Run = %v, %d unread, %d runs:\n%s", p.err, p.unread, len(p.runs), p.out)
	}
	for _, w := range []string{"Not Configured 4 to set", "ICMP redirects       1 accepted, hardn sets 0", "1) Apply settings       Set 4 settings",
		"Apply kernel settings? [y/N, 0 or q]:", ctl.NoticeApply[:40], "gdb -p", "+net.ipv6.conf.*.accept_redirects = 0", "+# before: net.ipv6.conf.eth0.accept_redirects = 1",
		"$ sysctl -w kernel.yama.ptrace_scope=1", "+ 4 items applied", "ICMP redirects       0 ignored", "File                 " + khost.File,
		"1) Remove settings      Undo hardn's change", "Remove hardn's kernel settings? [y/N, 0 or q]:", ctl.NoticeRemove,
		"$ sysctl -w kernel.yama.ptrace_scope=0", "Process tracing      0 unrestricted"} {
		if !strings.Contains(p.out, w) {
			t.Errorf("no %q in:\n%s", w, p.out)
		}
	}
	for _, w := range []string{"ip_forward", "accept_ra", "all.rp_filter"} {
		if strings.Contains(p.out, w) {
			t.Errorf("%q in:\n%s", w, p.out)
		}
	}
	if v := khost.ReadValue(context.Background(), m.f, "net.ipv6.conf.eth0.accept_redirects"); m.has(khost.File) || v.N != 1 {
		t.Errorf("not as it was: %+v", v)
	}
}

// TestNothingApplied: n, and a dry run, change nothing; a refusal is said
// before anything is asked.
func TestNothingApplied(t *testing.T) {
	foreign := func(m *model) { m.f.AddFile(khost.File, []byte("kernel.sysrq = 0\n"), 0o644, 0, 0) }
	noTool := func(m *model) { _ = m.f.Remove(context.Background(), sysctlfake.ProcpsPath) }
	for name, tc := range map[string]struct {
		edit  func(*model)
		dry   bool
		lines []string
		says  string
		asks  bool
	}{
		"n":                {nil, false, []string{"1", "n", "0"}, "Apply kernel settings? [y/N, 0 or q]:", true},
		"dry run":          {nil, true, []string{"1", "0"}, "Write hardn's kernel settings", false},
		"not hardn's file": {foreign, false, []string{"1", "0"}, khost.File + " is not hardn's file, and hardn", false},
		"no sysctl":        {noTool, false, []string{"1", "0"}, "This host has no sysctl, and hardn installs none.", false},
	} {
		m := newModel(rocky)
		if tc.edit != nil {
			tc.edit(m)
		}
		before, _ := m.f.ReadFile(context.Background(), khost.File)
		p := play(m, tc.dry, tc.lines...)
		after, _ := m.f.ReadFile(context.Background(), khost.File)
		if p.err != nil || p.unread != 0 || !strings.Contains(p.out, tc.says) || string(before) != string(after) || tc.asks != strings.Contains(p.out, "[y/N") {
			t.Errorf("%s: Run = %v, %d unread:\n%s", name, p.err, p.unread, p.out)
		}
		if len(m.f.Runs()) != 0 || khost.ReadValue(context.Background(), m.f, khost.PtraceKey).N != 0 {
			t.Errorf("%s: ran %v", name, m.f.Runs())
		}
	}
}

// TestUnavailable: at the baseline, and in a container, Apply settings is
// shown and takes no key.
func TestUnavailable(t *testing.T) {
	m := newModel(map[string]int{"net.ipv4.conf.*.send_redirects": 0})
	p := play(m, false, "1", "0")
	if p.err != nil || !strings.Contains(p.out, "Nothing to set") || !strings.Contains(p.out, "Apply settings is unavailable.") || strings.Contains(p.out, "[y/N") {
		t.Errorf("at the baseline: %v\n%s", p.err, p.out)
	}
	m = newModel(rocky)
	m.facts.Container.Detected = detect.Yes
	p = play(m, false, "1", "0")
	if p.err != nil || !strings.Contains(p.out, "Not Applicable in a container") || !strings.Contains(p.out, "In a container: hardn sets none of these.") || strings.Contains(p.out, "hardn sets 0") {
		t.Errorf("a container: %v\n%s", p.err, p.out)
	}
}

func TestRunNeedsItsOptions(t *testing.T) {
	if err := ksscreen.Run(context.Background(), ksscreen.Options{}); !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("Run with no options = %v", err)
	}
}
