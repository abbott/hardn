package updates_test

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	updatesctl "github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/host/alpine"
	updscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The flow's words the tests look for.
const (
	planned = "No actual changes were made"
	enable  = "Enable automatic updates"
	disable = "Disable automatic updates"
	askOn   = "Turn automatic updates on?"
	askOff  = "Turn automatic updates off?"
)

// readOnly is an item shown as unavailable (P-22).
const readOnly = "read-only"

// TestHosts: on every host, both modes, the screen draws the row the
// control's check reads, as the main menu draws it, and the item by the
// rule (Enable while off, Disable while on by hardn, unavailable while on
// not by hardn and on Proxmox VE), writing nothing and making no engine.
func TestHosts(t *testing.T) {
	type want struct{ detail, item string }
	wants := map[string]want{
		"debian13": {"unattended-upgrades", readOnly}, "ubuntu2604": {"unattended-upgrades", readOnly}, "alpine323": {"no apk-cron", enable},
		"debian-installer": {"no unattended-upgrades", enable}, "debian-off": {"upgrades off", enable}, "lists-stale": {"lists not refreshed", enable},
		"timer-off": {"upgrade timer off", enable}, "by-hardn-debian": {"unattended-upgrades", disable}, "by-hardn-alpine": {"apk-cron", disable},
		"edited": {"unattended-upgrades", disable}, "crond-shared": {"apk-cron", disable}, "dormant-jobs": {"no apk-cron", enable},
		"config-files": {"no unattended-upgrades", enable}, "legacy-alpine": {"no apk-cron", enable}, "pve9": {"updated by hand", readOnly},
		"not-read":      {"can't read", enable},
		"edited-alpine": {"apk-cron", disable}, "enable-off": {"upgrades off", enable}, "own-file": {"unattended-upgrades", disable},
		"masked": {"upgrades off", enable}, "missing": {"upgrades off", enable}, "no-origin": {"upgrades off", enable},
		"extra-packages": {"no unattended-upgrades", enable}, "later-file": {"upgrades off", enable}, "no-record": {"unattended-upgrades", disable},
		"corrupt-record": {"apk-cron", disable}, "purge-extra": {"unattended-upgrades", disable}, "apk-del-extra": {"apk-cron", disable},
	}
	ctx := context.Background()
	for _, name := range append(hosts[:len(hosts):len(hosts)], variants...) {
		w, ok := wants[name]
		if !ok {
			t.Fatalf("%s: no want", name)
		}
		for _, dryRun := range []bool{false, true} {
			m := newHost(t, name)
			if name != "pve9" { // Not Applicable there: the engine never checks it
				if st, err := (updatesctl.Auto{}).Check(ctx, m.f, m.facts); (err != nil) != (name == "not-read") || st.Detail != w.detail {
					t.Errorf("%s: the check reads %q, %v; want %q", name, st.Detail, err, w.detail)
				}
			}
			n := len(m.f.Calls())
			p := play(m, dryRun, "0")
			if p.err != nil || p.engines != 0 || p.unread != 0 || len(p.runs) != 0 {
				t.Errorf("%s, plan only %v: Run = %v, %d engines, %d unread", name, dryRun, p.err, p.engines, p.unread)
			}
			row := strings.SplitN(p.out, "\n", 4)[2] // the title bar, a gap, the row
			if !strings.Contains(row, "Auto Updates") || !strings.HasSuffix(strings.TrimSpace(row), w.detail) {
				t.Errorf("%s: the row %q, want %q", name, row, w.detail)
			}
			item := w.item
			if item == readOnly {
				item = disable
				if name == "pve9" {
					item = enable
				}
			}
			if !strings.Contains(p.out, "1) "+item) || strings.Contains(p.out, "Unavailable") != (w.item == readOnly) {
				t.Errorf("%s: the item, want %s:\n%s", name, w.item, p.out)
			}
			if wr := writes(m, n); len(wr) > 0 {
				t.Errorf("%s, plan only %v: drawing wrote %v", name, dryRun, wr)
			}
		}
	}
}

// TestRefusals: every refusal of gallery-updates.md §5 the screen can
// reach is the control's, said before anything is asked, in both modes,
// the item still offered with what is in the way (R1), writing nothing;
// the unavailable item (P-22) runs no engine.
func TestRefusals(t *testing.T) {
	for _, tt := range []struct {
		host, words, item string
	}{
		{"timer-off", "apt-daily-upgrade.timer is off, and hardn leaves apt's timers to you.", "Upgrade timer is off"},
		{"not-read", "apt can't read its settings", "Can't read apt's settings"},
		{"masked", "apt-daily-upgrade.service is masked", "Upgrade job is masked"},
		{"missing", "apt-daily-upgrade.service is missing. Reinstall apt, then try again.", "Upgrade job is missing"},
		{"no-origin", "50unattended-upgrades allows no origin", "No origin allowed"},
		{"extra-packages", "Installing unattended-upgrades would also change other packages: Inst libc6", "Install unattended-upgrades"},
		{"enable-off", "turns apt's daily jobs off. Remove that line, then try again.", "Periodic jobs are off"},
		{"edited", "Not undone: " + hardnFile + " changed since hardn wrote it", "52hardn-auto-upgrades changed"},
		{"edited-alpine", alpine.Script + " changed since", "apk-cron's script changed"},
		{"no-record", "Updates stay on without hardn's file: 20auto-upgrades turns them on, and hardn has no record that it installed unattended-upgrades. Turn them off by hand.",
			"20auto-upgrades keeps updates on"},
		{"corrupt-record", "hardn can't read its record of what it changed", "Can't read hardn's record"},
		{"purge-extra", "would also change other packages", "Undo hardn's changes"},
		{"apk-del-extra", "would also change other packages", "Undo hardn's changes"},
	} {
		for _, dryRun := range []bool{false, true} {
			m := newHost(t, tt.host)
			n := len(m.f.Calls())
			p := play(m, dryRun, "1", "0")
			if p.err != nil || !shows(p.out, tt.words) || asked(p.out) || p.engines != 1 || len(p.runs) != 0 {
				t.Errorf("%s, plan only %v: Run = %v, %d engines; want %q before asking:\n%s", tt.host, dryRun, p.err, p.engines, tt.words, p.out)
			}
			if !strings.Contains(p.out, "\n      "+tt.item+"\n") {
				t.Errorf("%s: the item's description, want %q:\n%s", tt.host, tt.item, p.out)
			}
			if wr := writes(m, n); len(wr) > 0 {
				t.Errorf("%s: a refusal wrote %v", tt.host, wr)
			}
		}
	}
	for _, host := range []string{"pve9", "debian13", "ubuntu2604"} {
		m := newHost(t, host)
		n := len(m.f.Calls())
		p := play(m, false, "1", "0")
		if p.err != nil || p.engines != 0 || asked(p.out) || !strings.Contains(p.out[strings.Index(p.out, "Enter your choice"):], "is unavailable.") {
			t.Errorf("%s: Run = %v, %d engines:\n%s", host, p.err, p.engines, p.out)
		}
		if wr := writes(m, n); len(wr) > 0 {
			t.Errorf("%s: wrote %v", host, wr)
		}
	}
}

// TestApply: Enable on alpine323 and debian-installer, Disable on
// by-hardn-debian and by-hardn-alpine, each planned before asking and again
// for the flow, asked y/N, applied, and the screen drawn again from a
// fresh reading, its item the other direction; the record written, then
// deleted.
func TestApply(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		host, question, after string
		record                bool
	}{
		{"alpine323", askOn, disable, true}, {"debian-installer", askOn, disable, true},
		{"by-hardn-debian", askOff, enable, false}, {"by-hardn-alpine", askOff, enable, false},
	} {
		m := newHost(t, tt.host)
		n := len(m.f.Calls())
		p := play(m, false, "1", "y", "0")
		ss := screens(p.out)
		if p.err != nil || p.unread != 0 || p.engines != 2 || len(p.runs) != 1 || p.runs[0].Err != nil || !strings.Contains(p.out, tt.question+" [y/N") {
			t.Errorf("%s: Run = %v, %d engines, runs %+v:\n%s", tt.host, p.err, p.engines, p.runs, p.out)
		}
		if len(ss) != 2 || !strings.Contains(ss[1], "1) "+tt.after) { // the screen, then the screen again
			t.Errorf("%s: %d screens, the last not %s:\n%s", tt.host, len(ss), tt.after, p.out)
		}
		if _, err := m.f.Lstat(ctx, recordPath); (err == nil) != tt.record || len(writes(m, n)) == 0 {
			t.Errorf("%s: the record %v, want there %v", tt.host, err, tt.record)
		}
	}
}

// TestFailedApply: Enable on later-file fails its read-back, as a file
// read after hardn's sets the value back; the change is rolled back, said,
// and the screen goes on, drawn again as it was.
func TestFailedApply(t *testing.T) {
	m := newHost(t, "later-file")
	p := play(m, false, "1", "y", "0")
	ss := screens(p.out)
	if _, err := m.f.Lstat(context.Background(), hardnFile); p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || err == nil {
		t.Errorf("Run = %v, runs %+v, hardn's file %v:\n%s", p.err, p.runs, err, p.out)
	}
	if len(ss) != 2 || !strings.Contains(ss[1], "1) "+enable) {
		t.Errorf("not drawn again:\n%s", p.out)
	}
}

// TestReview: each direction's review is the row as the change leaves it,
// then the control's notes (updates.Auto's Notes); n declines, writing
// nothing, and the screen is drawn again.
func TestReview(t *testing.T) {
	for _, tt := range []struct {
		host  string
		notes []string
	}{
		{"debian-off", []string{"Auto Updates On"}},
		{"dormant-jobs", []string{"Auto Updates On", "Also starts logrotate"}},
		{"config-files", []string{"50unattended-upgrades Kept there before hardn"}},
		{"crond-shared", []string{"Auto Updates Off", "crond Kept running before hardn"}},
		{"by-hardn-debian", []string{"python3-dbus Kept apt autoremove removes it", "python3-distro-info Kept apt autoremove removes it"}},
	} {
		m := newHost(t, tt.host)
		n := len(m.f.Calls())
		p := play(m, false, "1", "n", "0")
		for _, note := range tt.notes {
			if !shows(p.out, note) {
				t.Errorf("%s: %q not in the review:\n%s", tt.host, note, p.out)
			}
		}
		if p.err != nil || !asked(p.out) || len(screens(p.out)) != 2 {
			t.Errorf("%s: Run = %v:\n%s", tt.host, p.err, p.out)
		}
		if wr := writes(m, n); len(wr) > 0 {
			t.Errorf("%s: declined, wrote %v", tt.host, wr)
		}
	}
}

// TestPlanOnlyWritesNothing: under plan only the item opens and stops at
// its plan, both directions, nothing applied or asked.
func TestPlanOnlyWritesNothing(t *testing.T) {
	for _, host := range []string{"debian-installer", "debian-off", "alpine323", "legacy-alpine", "by-hardn-debian", "by-hardn-alpine"} {
		m := newHost(t, host)
		n := len(m.f.Calls())
		p := play(m, true, "1", "0")
		if p.err != nil || !shows(p.out, planned) || asked(p.out) || p.unread != 0 || len(p.runs) != 1 {
			t.Errorf("%s: Run = %v, did not stop at the plan:\n%s", host, p.err, p.out)
		}
		if wr := writes(m, n); len(wr) > 0 {
			t.Errorf("%s: plan only wrote %v", host, wr)
		}
	}
}

// TestApplyContext: the apply alone runs under ApplyContext's context, one
// made and stopped per apply, none under plan only; a cancelled one rolls
// the change back, and the screen goes on.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		dryRun bool
		made   int
		lines  []string
	}{{false, 1, []string{"1", "y", "0"}}, {true, 0, []string{"1", "0"}}} {
		m := newHost(t, "debian-off")
		made, stopped := 0, 0
		p := playWith(m, tc.dryRun, func(o *updscreen.Options) {
			o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
				made++
				c, cancel := context.WithCancel(ctx)
				return c, func() { stopped++; cancel() }
			}
		}, tc.lines...)
		if p.err != nil || made != tc.made || stopped != made {
			t.Errorf("plan only %v: Run = %v, %d contexts made, %d stopped, want %d:\n%s", tc.dryRun, p.err, made, stopped, tc.made, p.out)
		}
	}
	m := newHost(t, "debian-off")
	p := playWith(m, false, func(o *updscreen.Options) {
		o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c, cancel
		}
	}, "1", "y", "0")
	_, err := m.f.Lstat(context.Background(), hardnFile)
	if ss := screens(p.out); p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || err == nil || len(ss) != 2 || !strings.Contains(ss[1], "1) "+enable) {
		t.Errorf("cancelled: Run = %v, runs %+v, hardn's file %v:\n%s", p.err, p.runs, err, p.out)
	}
}

// TestNavigation: 0 returns, q quits, and the end of the input aborts,
// from the screen and the question, writing nothing.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
		want  error
	}{
		{"debian-off", []string{"0"}, nil}, {"debian-off", []string{"q"}, nav.ErrQuit}, {"debian-off", []string{"1", "q"}, nav.ErrQuit},
		{"debian-off", nil, prompt.ErrAborted}, {"debian-off", []string{"1"}, prompt.ErrAborted}, {"debian-off", []string{"9", "2", "0"}, nil},
		{"by-hardn-debian", []string{"1", "q"}, nav.ErrQuit}, {"debian13", []string{"1", "q"}, nav.ErrQuit},
	} {
		m := newHost(t, tt.host)
		n := len(m.f.Calls())
		p := play(m, false, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil) != (p.err == nil) || p.unread != 0 {
			t.Errorf("%s %q: Run = %v, want %v, %d unread:\n%s", tt.host, tt.lines, p.err, tt.want, p.unread, p.out)
		}
		if wr := writes(m, n); len(wr) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, wr)
		}
	}
}

// TestRunNeeds: Run refuses, wrapping fs.ErrInvalid and writing nothing, no
// Prompter's In or Out, no System or NewEngine. A nil Config is none.
func TestRunNeeds(t *testing.T) {
	m := newHost(t, "debian-off")
	n := len(m.f.Calls())
	var out bytes.Buffer
	in := prompt.New(strings.NewReader("0\n"), false)
	newEngine := func(func(engine.Event)) *engine.Engine { return nil }
	for _, o := range []updscreen.Options{
		{}, {System: m.f, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in}, System: m.f, NewEngine: newEngine},
		{Prompter: nav.Prompter{Out: &out}, System: m.f, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: m.f},
	} {
		if err := updscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Run(%+v) = %v, want fs.ErrInvalid", o, err)
		}
	}
	if len(m.f.Calls()) > n || out.Len() > 0 {
		t.Errorf("refused options made %d calls, wrote %q", len(m.f.Calls())-n, out.String())
	}
}

// TestCutShort: a reading cut short (the context done) never draws an
// empty screen: the row and the section say Can't read.
func TestCutShort(t *testing.T) {
	m := newHost(t, "debian-off")
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := updscreen.Run(ctx, updscreen.Options{
		Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)},
		System:   m.f, Facts: m.facts, NewEngine: m.engine,
	})
	if err != nil || !strings.Contains(out.String(), "Unknown") || !strings.Contains(out.String(), "Upgrades             Can't read") ||
		!strings.Contains(out.String(), enable+"\n      Can't read") {
		t.Errorf("Run = %v:\n%s", err, out.String())
	}
}

// TestHeights (the gallery's, gallery-updates.md §3): every host's screen
// is as many lines at 80x24 as at 80x40, Unicode and ASCII, no line over 80
// cells: every section in full at every height. The lines are §3's.
func TestHeights(t *testing.T) {
	want := map[string]int{
		"alpine323": 17, "dormant-jobs": 18, "legacy-alpine": 18, "not-read": 18,
		"by-hardn-alpine": 19, "debian-installer": 20, "debian-off": 20, "timer-off": 20, "crond-shared": 20, "config-files": 20, "lists-stale": 21,
		"by-hardn-debian": 21, "debian13": 22, "ubuntu2604": 22, "edited": 22, "pve9": 22,
	}
	for _, rows := range []int{24, 40} {
		for _, unicode := range []bool{true, false} {
			lang := map[bool]string{true: "LANG=en_US.UTF-8", false: "LANG=C"}[unicode]
			env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: rows, Environ: []string{"TERM=xterm-256color", lang}})
			for _, name := range hosts {
				var out bytes.Buffer
				playWith(newHost(t, name), false, func(o *updscreen.Options) {
					o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)}
				})
				plain := escapes.ReplaceAllString(out.String(), "")
				if got := drawn(plain); len(got) != 1 || got[0] != want[name] {
					t.Errorf("%s at 80x%d, unicode %v: %v lines, want %d", name, rows, unicode, got, want[name])
				}
				if i := slices.IndexFunc(strings.Split(plain, "\n"), func(l string) bool { return len([]rune(l)) > 80 }); i >= 0 {
					t.Errorf("%s at 80x%d, unicode %v: line %d over 80 cells", name, rows, unicode, i)
				}
			}
		}
	}
}

// escapes are the terminal's SGR sequences.
var escapes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// drawn are the heights of the screens out shows, in order: each from its
// title bar to its prompt, which ends " or q]:", the answer echoed after.
func drawn(out string) []int {
	var got []int
	start := 0
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.Contains(l, " or q]:") && strings.Contains(l, "Enter your choice") {
			for start < i && strings.TrimSpace(lines[start]) == "" {
				start++
			}
			got = append(got, i-start+1)
			start = i + 1
		}
	}
	return got
}
