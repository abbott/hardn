package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// playGallery runs the playable mode with args, the user typing lines,
// and returns its exit status, the transcript (stdout with the answers
// echoed into it, as a terminal shows them), stderr and the number of
// lines left unread.
func playGallery(lines []string, args ...string) (int, string, string, int) {
	var out, errs bytes.Buffer
	ty := uitest.NewTypist(&out, lines...)
	code := run(append([]string{"play"}, args...), func() *prompt.Reader { return prompt.New(ty, false) }, offTerminal, &out, &errs)
	return code, out.String(), errs.String(), ty.Unread()
}

// lookArgs returns the flags that pick env, a cell of uitest.Core.
func lookArgs(t *testing.T, env term.Env) []string {
	t.Helper()
	i := slices.IndexFunc(profiles, func(p profile) bool { return p.profile == env.Profile })
	args := []string{"-profile", profiles[i].name, "-width", strconv.Itoa(env.Width)}
	if !env.Dark {
		args = append(args, "-theme", "light")
	}
	if !env.Unicode {
		args = append(args, "-ascii")
	}
	if c := newCell(profiles[i], env.Dark, env.Unicode, env.Width); c.env != env {
		t.Fatalf("the flags %v pick %+v, want %+v", args, c.env, env)
	}
	return args
}

// TestPlayGolden plays the paths the playable mode must show end to end,
// on the core set, and pins each transcript: the user-management workflow
// from the main menu, with an item the gallery has no screen for, the
// sudo change asked y/N, then back with 0 to the main menu and out with
// 0; the same workflow with its typed sequence, then q from four
// levels down; the root-login toggle's typed confirmation, a line refused
// first, then q from its sub-menu; the sudo change told to fail its
// second step, which rolls the first back; the input ending at a
// form's question, which exits 130. Then the DNS screen, hardn's own on
// the real engine (next/s2-screen): Add nameserver from the main menu, two
// entries refused before one is taken, applied, and the screen drawn again
// from the host under the result (W1, W2), then out; on each other host
// its item 1 (Ubuntu 26.04's drop-in and reload, Alpine's udhcpc question
// answered y, Proxmox VE's node, the configured host's nothing to change);
// Remove nameserver; the presets, one declined between two applied, the
// second saved over the first; a dry run; the NetworkManager host refusing
// Configure DNS and a preset; the screen with no nameserver configured
// refusing item 3, which it does not show, and Configure DNS; the screen
// with one refusing Remove nameserver; and the two failures, a reload
// without effect (D3) and a save refused, each rolled back. The golden
// files are this package's, testdata/golden/play/<path>.golden, which make
// golden-update rewrites after pkg/ui's.
func TestPlayGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		code  int
	}{
		{"user-management", nil, []string{"7", "0", "1", "1", "1", "1", "y", "0", "0", "0", "0"}, exitOK},
		{"user-management-typed", []string{"-sequence", "typed"}, []string{"1", "1", "1", "1", "bos-san-l01", "q"}, exitOK},
		{"ssh-toggle", nil, []string{"2", "1", "BOS-SAN-L01", "bos-san-l01", "q"}, exitOK},
		{"fail", []string{"-fail", "2", "workflow-step/user-manage-sudo-confirmation"}, []string{"y"}, exitOK},
		{"abort", []string{"sub-menu/01-user"}, []string{"2", "alice"}, prompt.ExitInterrupted},
		{"dns", nil, []string{"3", "2", "999.1.1.1", "192.0.2.54", "192.0.2.53", "y", "0", "0"}, exitOK},
		{"dns-ubuntu2604", []string{"dns/ubuntu2604"}, []string{"1", "y", "0"}, exitOK},
		{"dns-alpine324", []string{"dns/alpine324"}, []string{"1", "y", "y", "0"}, exitOK},
		{"dns-pve9", []string{"dns/pve9"}, []string{"1", "y", "0"}, exitOK},
		{"dns-configured", []string{"dns/configured"}, []string{"1", "0"}, exitOK},
		{"dns-remove", []string{"dns/debian13"}, []string{"3", "2", "y", "0"}, exitOK},
		{"dns-presets", []string{"dns/debian13"}, []string{"4", "y", "5", "n", "6", "y", "0"}, exitOK},
		{"dns-dry-run", []string{"-sequence", "dry-run", "dns/debian13"}, []string{"4", "0"}, exitOK},
		{"dns-networkmanager", []string{"dns/networkmanager"}, []string{"1", "4", "0"}, exitOK},
		{"dns-empty", []string{"dns/empty"}, []string{"3", "1", "0"}, exitOK},
		{"dns-one", []string{"dns/one"}, []string{"3", "0"}, exitOK},
		{"dns-failed-reload", []string{"dns/failed-reload"}, []string{"1", "y", "0"}, exitOK},
		{"dns-failed-save", []string{"dns/failed-save"}, []string{"4", "y", "0"}, exitOK},
	}
	for _, tt := range tests {
		uitest.Golden(t, "play/"+tt.name, uitest.Core(), func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != tt.code || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread; want exit %d", tt.name, args, code, errs, unread, tt.code)
			}
			return sameIDs(t, out)
		})
	}
}

// TestPlaySize checks how play sizes its environment (look.sizeTo), on a
// sizer the test injects, so that it does not depend on the terminal it
// runs in: on a terminal, the terminal's size where -width or -height is
// not given, each flag given winning on its own, even one that is then
// refused, and the terminal not asked when both are; off a terminal, 80
// and 24 whatever size the sizer reports, as on a terminal of unknown
// size. Then, end to end, that the main menu takes the form the size
// gives it: its full form on an 80x40 terminal, and a 24-row terminal's
// form on one of 24 rows, at -height 24 on the 80x40 terminal, and off a
// terminal: the full form too while its compact form is gated
// (screen.CompactMainMenu), else the compact one.
func TestPlaySize(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		w, h          int  // the terminal's size, as the sizer reports it
		tty           bool // whether stdout is a terminal
		width, height int  // the size play takes
		asked         bool // whether play calls the sizer
	}{
		{"off a terminal", nil, 0, 0, false, term.DefaultWidth, term.DefaultHeight, true},
		{"off a terminal, with a size reported", nil, 120, 50, false, term.DefaultWidth, term.DefaultHeight, true},
		{"off a terminal, -height given", []string{"-height", "40"}, 0, 0, false, term.DefaultWidth, 40, true},
		{"on a terminal", nil, 120, 50, true, 120, 50, true},
		{"on a terminal, -width given", []string{"-width", "60"}, 120, 50, true, 60, 50, true},
		{"on a terminal, -height given", []string{"-height", "24"}, 120, 50, true, 120, 24, true},
		{"on a terminal, both given", []string{"-width", "70", "-height", "30"}, 120, 50, true, 70, 30, false},
		{"on a terminal, -width 0 given", []string{"-width", "0"}, 120, 50, true, 0, 50, true},
		{"on a terminal of unknown size", nil, 0, 0, true, term.DefaultWidth, term.DefaultHeight, true},
	}
	for _, tt := range tests {
		fs := flag.NewFlagSet("uigallery play", flag.ContinueOnError)
		l := lookFlags(fs, true)
		if err := fs.Parse(tt.args); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		asked := false
		l.sizeTo(fs, func() (int, int, bool) { asked = true; return tt.w, tt.h, tt.tty })
		if *l.width != tt.width || *l.height != tt.height || asked != tt.asked {
			t.Errorf("%s, %v: %dx%d, sizer called %t; want %dx%d, called %t", tt.name, tt.args, *l.width, *l.height, asked, tt.width, tt.height, tt.asked)
		}
	}
	on := func(w, h int) sizer { return func() (int, int, bool) { return w, h, true } }
	var out, errs bytes.Buffer
	if code := run([]string{"play", "-width", "0"}, noInput, on(120, 50), &out, &errs); code != exitUsage || out.Len() != 0 || !strings.Contains(errs.String(), "-width 0 is not a positive number of cells") {
		t.Errorf("play -width 0 on a terminal: exit %d, stdout %q, stderr %q; want exit %d and -width refused", code, out.String(), errs.String(), exitUsage)
	}
	vm := screen.MainMenuFixture()
	i := slices.IndexFunc(vm.Menu.Items, func(it component.MenuItem) bool { return it.Key == "7" })
	vm.Menu.Items = slices.Insert(vm.Menu.Items, i, updMenuItem) // play's main menu: the Auto Updates screen's item (updates.go)
	drawn := func(height int) string {
		env := term.Fixed(80, colorprofile.ASCII, false, true)
		env.Height = height
		return ansi.Strip(vm.Render(component.NewCtx(env)))
	}
	full, short := drawn(theme.CompactBelow), drawn(term.DefaultHeight)
	if (full == short) == screen.CompactMainMenu || !strings.HasPrefix(full, "hardn") {
		t.Fatalf("the main menu at 80x40 is not its full form, or at 80x24 not the form CompactMainMenu gives:\n%s\n\n%s", full, short)
	}
	for _, tt := range []struct {
		name string
		args []string
		size sizer
		want string
	}{
		{"an 80x40 terminal", nil, on(80, 40), full},
		{"an 80x24 terminal", nil, on(80, 24), short},
		{"an 80x40 terminal, -height 24", []string{"-height", "24"}, on(80, 40), short},
		{"no terminal", nil, offTerminal, short},
	} {
		var out, errs bytes.Buffer
		ty := uitest.NewTypist(&out, "q")
		args := append([]string{"play", "-profile", "none", "-ascii"}, tt.args...)
		code := run(args, func() *prompt.Reader { return prompt.New(ty, false) }, tt.size, &out, &errs)
		if got := ansi.Strip(out.String()); code != exitOK || errs.Len() != 0 || !strings.Contains(got, tt.want+" q\n") {
			t.Errorf("%s: exit %d, stderr %q, transcript\n%s\nwant it to hold the main menu\n%s", tt.name, code, errs.String(), got, tt.want)
		}
	}
}

// TestPlayEnds checks how play ends at each kind of prompt: the end of
// the input aborts with exit status 130 and nothing applied, q quits and
// 0 at the first screen goes back, both with 0, and the record says what
// was applied. b goes nowhere: the main menu refuses it, as any line it
// does not take, and reads the next.
func TestPlayEnds(t *testing.T) {
	// The main menu's 1 leads to the User Management screen on debian13
	// (users.go): Manage User, alice, Don't ask password, a y/N plan.
	sudo := []string{"1", "1", "2", "2"}
	tests := []struct {
		name    string
		args    []string
		lines   []string
		code    int
		applied bool
	}{
		{"the end of the input at the main menu", nil, nil, prompt.ExitInterrupted, false},
		{"the end of the input at a sub-menu", nil, []string{"1"}, prompt.ExitInterrupted, false},
		{"the end of the input at a form's first field", []string{"workflow-step/user-create-user"}, nil, prompt.ExitInterrupted, false},
		{"the end of the input at a y/N confirmation", nil, sudo, prompt.ExitInterrupted, false},
		{"the end of the input at a typed confirmation", nil, []string{"2", "1", "nope"}, prompt.ExitInterrupted, false},
		{"the end of the input after an apply", nil, append(slices.Clone(sudo), "y"), prompt.ExitInterrupted, true},
		{"q at the main menu", nil, []string{"q"}, exitOK, false},
		{"Q at a sub-menu", nil, []string{"3", "Q"}, exitOK, false},
		{"q at a form's field", []string{"workflow-step/user-create-user"}, []string{"bob", "q"}, exitOK, false},
		{"q at a typed confirmation", nil, []string{"2", "1", "q"}, exitOK, false},
		{"0 at the main menu", nil, []string{"0"}, exitOK, false},
		{"b at the main menu, refused", nil, []string{"b", "0"}, exitOK, false},
		{"0 at a first sub-menu", []string{"sub-menu/user-ssh-keys"}, []string{"0"}, exitOK, false},
		{"0 at a first workflow's first field", []string{"workflow-step/user-create-user"}, []string{"0"}, exitOK, false},
		{"0 at a first workflow's confirmation", []string{"workflow-step/user-manage-sudo-confirmation"}, []string{"0"}, exitOK, false},
		{"a dry-run sequence", []string{"workflow-step/ssh-login-toggle-confirmation/dry-run"}, nil, exitOK, false},
	}
	for _, tt := range tests {
		code, out, errs, unread := playGallery(tt.lines, append([]string{"-profile", "plain"}, tt.args...)...)
		_, record, _ := strings.Cut(out, "  simulated system ")
		applied := !strings.Contains(record, "Nothing was applied.")
		if code != tt.code || errs != "" || unread != 0 || applied != tt.applied || applied != strings.Contains(record, "Applied: ") {
			t.Errorf("%s: exit %d, stderr %q, %d lines unread, applied %t; want exit %d, applied %t; the record:\n%s",
				tt.name, code, errs, unread, applied, tt.code, tt.applied, record)
		}
	}
}

// TestPlayFail checks the simulated system's failures: -fail N fails step
// N of the plan and rolls back the steps before it, a step the plan does
// not have fails none, a failure sequence fails where its fixture does,
// and -fail overrides it.
func TestPlayFail(t *testing.T) {
	sudo, user := "workflow-step/user-manage-sudo-confirmation", "workflow-step/user-create-user/failure"
	george := []string{"george", "y", "n", "n", "y"}
	tests := []struct {
		name   string
		args   []string
		lines  []string
		record []string // the record's step titles, in order
	}{
		{"no failure", []string{sudo}, []string{"y"}, []string{
			"Applied: Require george's password for sudo", "Applied: Check the sudo configuration"}},
		{"-fail 1", []string{"-fail", "1", sudo}, []string{"y"}, []string{
			"Failed: Require george's password for sudo (exit status 1)"}},
		{"-fail 2", []string{"-fail", "2", sudo}, []string{"y"}, []string{
			"Applied: Require george's password for sudo", "Failed: Check the sudo configuration (exit status 1)",
			"Rolled back: Require george's password for sudo"}},
		{"-fail 3, past the plan", []string{"-fail", "3", sudo}, []string{"y"}, []string{
			"Applied: Require george's password for sudo", "Applied: Check the sudo configuration"}},
		{"the failure sequence", []string{user}, george, []string{
			"Applied: Create user george", "Failed: Add george to the sudo group (exit status 6)", "Rolled back: Create user george"}},
		{"-fail 3 over the failure sequence", []string{"-fail", "3", user}, george, []string{
			"Applied: Create user george", "Applied: Add george to the sudo group",
			"Failed: Let george use sudo without a password (exit status 1)",
			"Rolled back: Add george to the sudo group", "Rolled back: Create user george"}},
	}
	for _, tt := range tests {
		code, out, errs, _ := playGallery(tt.lines, append([]string{"-profile", "plain"}, tt.args...)...)
		_, record, _ := strings.Cut(out, "  simulated system ")
		// A step's title may wrap: find each, in order, in the record
		// with its line breaks and indents as single spaces.
		flat, at, found := strings.Join(strings.Fields(record), " "), 0, true
		for _, title := range tt.record {
			i := strings.Index(flat[at:], "- "+title)
			if i < 0 {
				found = false
				break
			}
			at += i + len(title)
		}
		if n := strings.Count(record, "\n   -  "); code != exitOK || errs != "" || !found || n != len(tt.record) {
			t.Errorf("%s: exit %d, stderr %q, %d steps recorded; want %q in order; the record:\n%s", tt.name, code, errs, n, tt.record, record)
		}
		if failed := strings.Contains(out, "Operation Failed"); failed != strings.Contains(record, "Failed: ") {
			t.Errorf("%s: the screen shows a failure %t, the record %t", tt.name, failed, !failed)
		}
	}
}

// TestPlayReaches checks the playable mode's links: each leads from an
// item of a menu mock to a mock, or to the DNS, Backup or Environment
// screen on a host (dnsLinks, backupLinks, envLinks), every menu mock's
// screen is one whose menu play binds, an item it refuses is one of its
// menu's and has no link, and every mock can be reached from the main
// menu, but Figma's records of screens hardn now draws itself
// (figmaRecords), each of whose items leads to hardn's screen, and which
// play starts at by name.
func TestPlayReaches(t *testing.T) {
	mocks := map[string]screen.Mock{}
	roots := []string{"main-menu/00-tui-main"}
	for _, m := range screen.Mocks() {
		mocks[m.Name] = m
		menu, ok := menuOf(m.Screen)
		if m.Screen != nil && !ok {
			t.Errorf("%s: play cannot bind its menu", m.Name)
		}
		for key := range m.Refusals {
			_, linked := links[m.Name+"/"+key]
			if linked || !slices.ContainsFunc(menu.Items, func(it component.MenuItem) bool { return it.Key == key }) {
				t.Errorf("%s: refuses item %s, which its menu lacks or a link leads from", m.Name, key)
			}
		}
	}
	for from, to := range dnsLinks {
		i := strings.LastIndex(from, "/")
		menu, ok := menuOf(mocks[from[:i]].Screen)
		_, linked := links[from]
		if _, host := findDNSHost(strings.TrimPrefix(to, dnsPrefix+"/")); !ok || linked || !host ||
			!slices.ContainsFunc(menu.Items, func(it component.MenuItem) bool { return it.Key == from[i+1:] }) {
			t.Errorf("DNS link %s to %s: no menu mock with the item, a mock link from it too, or no such host", from, to)
		}
	}
	for from, to := range backupLinks {
		i := strings.LastIndex(from, "/")
		menu, ok := menuOf(mocks[from[:i]].Screen)
		_, linked := links[from]
		_, dns := dnsLinks[from]
		if _, host := findBackupHost(strings.TrimPrefix(to, backupPrefix+"/")); !ok || linked || dns || !host ||
			!slices.ContainsFunc(menu.Items, func(it component.MenuItem) bool { return it.Key == from[i+1:] }) {
			t.Errorf("Backup link %s to %s: no menu mock with the item, another link from it too, or no such host", from, to)
		}
	}
	for from, to := range envLinks {
		i := strings.LastIndex(from, "/")
		menu, ok := menuOf(mocks[from[:i]].Screen)
		_, linked := links[from]
		_, dns := dnsLinks[from]
		_, bak := backupLinks[from]
		if _, host := findEnvHost(strings.TrimPrefix(to, envPrefix+"/")); !ok || linked || dns || bak || !host ||
			!slices.ContainsFunc(menu.Items, func(it component.MenuItem) bool { return it.Key == from[i+1:] }) {
			t.Errorf("Environment link %s to %s: no menu mock with the item, another link from it too, or no such host", from, to)
		}
	}
	for name, item := range figmaRecords {
		_, mock := mocks[name]
		_, real := backupLinks[item]
		if !mock || !real || !strings.HasPrefix(mocks[name].Source.Label, "Figma ") {
			t.Errorf("record %s: no such mock, not Figma's, or its item %s leads to no screen of hardn's", name, item)
		}
	}
	for from, to := range links {
		i := strings.LastIndex(from, "/")
		menu, ok := menuOf(mocks[from[:i]].Screen)
		if !ok || !slices.ContainsFunc(menu.Items, func(it component.MenuItem) bool { return it.Key == from[i+1:] }) {
			t.Errorf("link %s: no menu mock %s with the item %s", from, from[:i], from[i+1:])
		}
		if _, ok := mocks[to]; !ok {
			t.Errorf("link %s: no mock %s", from, to)
		}
	}
	for name := range figmaRecords {
		roots = append(roots, name)
	}
	reached, next := map[string]bool{}, roots
	for len(next) > 0 {
		name := next[0]
		next = next[1:]
		if reached[name] {
			continue
		}
		reached[name] = true
		for from, to := range links {
			if strings.HasPrefix(from, name+"/") {
				next = append(next, to)
			}
		}
	}
	for name := range mocks {
		if !reached[name] {
			t.Errorf("%s cannot be reached from the main menu", name)
		}
	}
}

// TestPlaySmoke plays the root-login toggle and the create-user form in
// every profile, both themes and both glyph sets, at 20, 60 and 80
// columns: play ends with exit status 0, and no line is wider than the
// terminal but a header, which shows its whole name, and a prompt's line
// with the answer the terminal echoes. Below 27 columns in Unicode
// glyphs, 29 in ASCII and plain mode, the toggle's typed confirmation
// does not show, and the flow cancels it unasked.
func TestPlaySmoke(t *testing.T) {
	for _, p := range profiles {
		for _, th := range []string{"dark", "light"} {
			for _, ascii := range []bool{false, true} {
				for _, w := range []int{20, 60, 80} {
					look := []string{"-profile", p.name, "-theme", th, fmt.Sprintf("-ascii=%t", ascii), fmt.Sprintf("-width=%d", w)}
					lines := []string{"2", "1", "bos-san-l01", "q"}
					narrowest := 27
					if ascii || p.name == "plain" {
						narrowest = 29
					}
					if w < narrowest {
						lines = []string{"2", "1", "q"}
					}
					for _, run := range []struct {
						args, lines []string
					}{
						{look, lines},
						{append(slices.Clone(look), "workflow-step/user-create-user"), []string{"bob", "n", "n", "y"}},
					} {
						code, out, errs, unread := playGallery(run.lines, run.args...)
						where := strings.Join(run.args, " ")
						if code != exitOK || errs != "" || unread != 0 {
							t.Fatalf("%s: exit %d, stderr %q, %d lines unread", where, code, errs, unread)
						}
						ctx := component.NewCtx(newCell(p, th == "dark", !ascii, w).env)
						start := "main-menu/00-tui-main"
						if len(run.args) > len(look) {
							start = run.args[len(look)]
						}
						headers := []string{ansi.Strip(header(ctx, "play "+start)), ansi.Strip(header(ctx, "simulated system"))}
						for _, l := range strings.Split(strings.TrimSuffix(ansi.Strip(out), "\n"), "\n") {
							if slices.Contains(headers, l) {
								continue
							}
							for _, a := range run.lines {
								if cut, ok := strings.CutSuffix(l, " "+a); ok {
									l = cut
									break
								}
							}
							if ansi.StringWidth(l) > w {
								t.Errorf("%s: line %q is wider than %d", where, l, w)
							}
						}
					}
				}
			}
		}
	}
}

// TestPlayUsage checks that play stops with exit status 2, a message and
// no output, reading nothing, for a wrong flag or argument, a sequence
// that ends cancelled among them, and that -h prints its usage.
func TestPlayUsage(t *testing.T) {
	for _, args := range [][]string{
		{"nope"},
		{"sub-menu"},
		{"main-menu", "sub-menu/01-user"},
		{"main-menu", "-fail", "2"},
		{"-fail", "-1"},
		{"-sequence", "nope"},
		{"-sequence", ""},
		{"-sequence", "dry-run", "workflow-step/user-manage-sudo-confirmation/typed"},
		{"-sequence", "cancelled"},
		{"workflow-step/user-create-user/cancelled"},
		{"workflow-step/user-manage-sudo-confirmation/normal/1-confirm"},
		{"-screens"},
		{"-profile", "ansi"},
		{"-width", "0"},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(append([]string{"play"}, args...), func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("play %v: exit %d, stdout %q, stderr %q, stdin read %t; want exit %d, a message, no output", args, code, out.String(), errs.String(), read, exitUsage)
		}
	}
	code, _, errs, _ := playGallery(nil, "-h")
	if code != exitOK || !strings.Contains(errs, playUsage) || !strings.Contains(errs, "-fail") ||
		!strings.Contains(errs, "the terminal width in cells: by default the terminal's own when stdout is a terminal, else 80\n") ||
		!strings.Contains(errs, "the terminal height in lines: by default the terminal's own when stdout is a terminal, else 24. ") {
		t.Errorf("play -h: exit %d, stderr %q; want -width's and -height's help to say they default to the terminal's size", code, errs)
	}
	if code, _, errs := runGallery("-screens", "play"); code != exitUsage || !strings.Contains(errs, `no screen "play"`) {
		t.Errorf("play after a flag: exit %d, stderr %q; want it taken as a screen's name", code, errs)
	}
}

// errBroken is the error of a writer that breaks.
var errBroken = errors.New("broken pipe")

// breaks is a writer that fails the first write holding at, and every
// write after it, as a pipe whose reader has gone does.
type breaks struct {
	w     io.Writer
	at    string
	broke bool
}

func (b *breaks) Write(p []byte) (int, error) {
	if b.broke || bytes.Contains(p, []byte(b.at)) {
		b.broke = true
		return 0, errBroken
	}
	return b.w.Write(p)
}

// TestPlayWriteFails checks that a failed write of the simulated system's
// record exits with status 1 and a message however the user left play,
// by q or by going back from the first screen, and that an abort keeps
// its 130.
func TestPlayWriteFails(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		code  int
		errs  string
	}{
		{"q", []string{"q"}, exitError, "uigallery: play: broken pipe\n"},
		{"0 at the first screen", []string{"0"}, exitError, "uigallery: play: broken pipe\n"},
		{"the end of the input", nil, prompt.ExitInterrupted, ""},
	}
	for _, tt := range tests {
		var out, errs bytes.Buffer
		w := &breaks{w: &out, at: "simulated system"}
		ty := uitest.NewTypist(&out, tt.lines...)
		code := run([]string{"play", "-profile", "plain"}, func() *prompt.Reader { return prompt.New(ty, false) }, offTerminal, w, &errs)
		if code != tt.code || errs.String() != tt.errs || !w.broke {
			t.Errorf("%s: exit %d, stderr %q, the record's write failed %t; want exit %d, stderr %q", tt.name, code, errs.String(), w.broke, tt.code, tt.errs)
		}
	}
}

// TestPlayTouchesNothing checks that the code play runs cannot change the
// system. No file of the gallery, nor of any package of this module that
// it links, found by following their imports, imports os/exec or a net
// package (net/netip, which computes on addresses, aside), names a
// function of os that writes, removes or renames a file, starts a process
// or changes the environment or the working directory, or names anything
// of syscall or golang.org/x/sys/unix but an errno value, a FileInfo's
// Stat_t, and what the line reader (pkg/ui/prompt) uses to ask whether
// stdin is a terminal, to discard typeahead and to wait for a line or
// Ctrl-C, and, in reraise_unix.go alone, to send the process its own
// SIGINT (prompt.Reraise, which the gallery must not call: play returns
// ExitCode's 130 instead).
//
// The engine's scenarios link what does all of that: the real side of the
// seam (host.Real), the engine's signal hold (pkg/engine/hold), and
// pkg/config, which pkg/engine and pkg/output link for its types (it no
// longer links pkg/logging: docs/engine/legacy-left.md §2, step 6). Those
// files (realSide) are exempt, and the test checks
// instead, by name, that nothing play links can reach them. No file but
// host.Real's own names the type, in any form (a literal, new, a
// declaration, a conversion), and those make one only in host.NewReal,
// which no file names. No file names pkg/config's functions (pkg/config,
// unreached itself, aside), but for pkg/config's
// Read, Save and Edit, which reach the host only through the host.System
// they are given, or not at all (the save of hardn.yml, steps.PlanSave). hold.Signals and
// SignalsAs are named only by pkg/engine's default hold, which an
// Options.Hold left nil takes: so outside the gallery only
// pkg/engine/confirm, which passes its caller's Hold on, calls engine.New,
// and nothing calls confirm.Cleanup; and every engine.New and
// confirm.Cleanup the gallery calls takes an Options literal whose Hold is
// the gallery's own (session.hold). So every host.System the engine gets
// is one the gallery gives it, and the hold it takes is the gallery's
// (TestPlayEngine checks that the failed reload's rollback took it). This
// is a check of names, not of types or values: the gallery's own code is
// held to its literals, the linked packages to never naming what reaches
// the real side. The simulated system records; it never applies. Lip
// Gloss's own init effects, such as colorprofile's "tmux info", are
// outside this check (docs/refactor/phase0/side-effects.md §13.1).
func TestPlayTouchesNothing(t *testing.T) {
	const module = "github.com/abbott/hardn/"
	root := filepath.Join("..", "..", "..")
	banned := map[string]bool{
		"WriteFile": true, "Create": true, "CreateTemp": true, "OpenFile": true, "Mkdir": true, "MkdirAll": true,
		"MkdirTemp": true, "Remove": true, "RemoveAll": true, "Rename": true, "Chmod": true, "Chown": true,
		"Lchown": true, "Chtimes": true, "Symlink": true, "Link": true, "Truncate": true, "StartProcess": true,
		"Setenv": true, "Unsetenv": true, "Clearenv": true, "Chdir": true,
	}
	sys := map[string]bool{
		// syscall.Conn, to take a file's descriptor as the file is.
		"Conn": true,
		// Whether stdin is a terminal (tty_linux.go, tty_darwin.go).
		"IoctlGetTermios": true, "TCGETS": true, "TIOCGETA": true,
		// Discarding typeahead (flush_linux.go, flush_darwin.go).
		"IoctlSetInt": true, "TCFLSH": true, "TCIFLUSH": true, "IoctlSetPointerInt": true, "TIOCFLUSH": true,
		// Waiting for a line or Ctrl-C (wait_unix.go).
		"Select": true, "FdSet": true, "FD_SETSIZE": true,
		// A FileInfo's owner, read from its Sys() (host.Owner).
		"Stat_t": true,
	}
	errno := regexp.MustCompile(`^E[A-Z0-9]+$`) // a value errors.Is compares with
	// Ending the process by its own SIGINT after Ctrl-C (prompt.Reraise,
	// for the legacy menus and the config prompts), in reraise_unix.go
	// alone.
	reraise := map[string]bool{"Kill": true, "Getpid": true, "SIGINT": true}
	// realSide are the files play links and never reaches, by why.
	const (
		real    = "host.Real's methods, reached through a host.Real, which only host.NewReal makes"
		holding = "the engine's signal hold, reached through hold.Signals and SignalsAs"
		start   = "hardn's configuration and legacy log, reached through their package's functions"
	)
	realSide := map[string]string{
		"pkg/host/real.go": real, "pkg/host/guard.go": real, "pkg/host/lock.go": real, "pkg/host/append.go": real, "pkg/host/detach.go": real,
		"pkg/host/scan.go": real, "pkg/host/interactive.go": real, "pkg/host/terminal_linux.go": real, "pkg/host/terminal_darwin.go": real,
		"pkg/engine/hold/hold.go": holding, "pkg/engine/hold/restore_darwin.go": holding,
		"pkg/engine/hold/restore_linux.go": holding, "pkg/engine/hold/restore_other.go": holding,
		"pkg/config/config.go": start, "pkg/config/embed.go": start,
	}
	// unreached are the functions of the packages behind realSide that
	// nothing play links may name, by package; filled from their files.
	unreached := map[string]map[string]bool{module + "pkg/config": {}}
	for p, funcs := range unreached {
		paths, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, module)), "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range f.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.IsExported() {
					funcs[fd.Name.Name] = true
				}
			}
		}
		if len(funcs) == 0 {
			t.Fatalf("no functions found in %s", p)
		}
	}
	// pkg/config's functions that reach the host only through the
	// host.System they are given, or not at all, which the save of
	// hardn.yml calls (steps.PlanSave, next/s2-control; Edit since
	// next/s2-config-edit), and the defaults a status host with no
	// hardn.yml is checked against (statusConfig, next/s2-menu-live):
	// play may name them.
	for _, name := range []string{"Read", "Save", "Edit", "DefaultConfig"} {
		if !unreached[module+"pkg/config"][name] {
			t.Fatalf("pkg/config has no function %s", name)
		}
		delete(unreached[module+"pkg/config"], name)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	dirs, seen, files, exempt := []string{"."}, map[string]bool{}, 0, map[string]bool{}
	for len(dirs) > 0 {
		dir := dirs[0]
		dirs = dirs[1:]
		if seen[dir] {
			continue
		}
		seen[dir] = true
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			files++
			abs, err := filepath.Abs(path)
			if err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(absRoot, abs)
			if err != nil {
				t.Fatal(err)
			}
			rel = filepath.ToSlash(rel)
			_, isReal := realSide[rel]
			exempt[rel] = isReal
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			pkgs := map[string]string{} // the name each package goes by in the file: its path
			for _, im := range f.Imports {
				p, _ := strconv.Unquote(im.Path.Value)
				name := p[strings.LastIndex(p, "/")+1:]
				if im.Name != nil {
					name = im.Name.Name
				}
				pkgs[name] = p
				switch {
				case (p == "os/exec" || p == "net" || strings.HasPrefix(p, "net/")) && p != "net/netip" && !isReal:
					t.Errorf("%s imports %s", path, p)
				case strings.HasPrefix(p, module):
					dirs = append(dirs, filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, module))))
				}
			}
			own := module + filepath.ToSlash(filepath.Dir(rel)) // the file's package
			realFile := realSide[rel] == real                   // one of host.Real's own files
			var fn string                                       // the function being walked
			called := map[ast.Expr]bool{}                       // the functions called, as the calls name them
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.FuncDecl:
					fn = n.Name.Name
				case *ast.CompositeLit:
					if realFile && namesReal(n.Type) && fn != "NewReal" {
						t.Errorf("%s makes a host.Real in %s, which only host.NewReal may", path, fn)
					}
					if p := optionsOf(n.Type, pkgs); dir == "." && p != "" && !holdsOwn(n) {
						t.Errorf("%s: %s.Options with a Hold other than the gallery's (session.hold), so the engine could take hold.Signals", path, p)
					}
				case *ast.CallExpr:
					called[n.Fun] = true
					if id, ok := n.Fun.(*ast.Ident); ok && id.Name == "new" && len(n.Args) == 1 && realFile && namesReal(n.Args[0]) && fn != "NewReal" {
						t.Errorf("%s makes a host.Real in %s, which only host.NewReal may", path, fn)
					}
					sel, ok := n.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					x, _ := sel.X.(*ast.Ident)
					if x == nil || !(pkgs[x.Name] == module+"pkg/engine" && sel.Sel.Name == "New" || pkgs[x.Name] == module+"pkg/engine/confirm" && sel.Sel.Name == "Cleanup") {
						return true
					}
					switch lit, _ := n.Args[len(n.Args)-1].(*ast.CompositeLit); {
					case dir != "." && (own != module+"pkg/engine/confirm" || sel.Sel.Name == "Cleanup"):
						t.Errorf("%s calls %s.%s outside the gallery", path, x.Name, sel.Sel.Name)
					case dir == "." && (lit == nil || optionsOf(lit.Type, pkgs) == ""):
						t.Errorf("%s calls %s.%s with Options that are not a literal of them, whose Hold the test cannot see", path, x.Name, sel.Sel.Name)
					}
				case *ast.Ident:
					switch {
					case n.Name == "NewReal" && own == module+"pkg/host" && rel != "pkg/host/real.go":
						t.Errorf("%s names NewReal", path)
					case n.Name == "Real" && own == module+"pkg/host" && !realFile:
						t.Errorf("%s names Real, which only host.Real's own files may", path)
					}
				case *ast.SelectorExpr:
					x, ok := n.X.(*ast.Ident)
					if !ok {
						return true
					}
					p, name := pkgs[x.Name], n.Sel.Name
					switch {
					case p == "os" && banned[name] && !isReal:
						t.Errorf("%s calls os.%s", path, name)
					case (p == "syscall" || p == "golang.org/x/sys/unix") && !isReal && !sys[name] && !errno.MatchString(name) &&
						(filepath.Base(path) != "reraise_unix.go" || !reraise[name]):
						t.Errorf("%s names %s.%s, which the line reader does not use", path, p, name)
					case dir == "." && name == "Reraise":
						t.Errorf("%s names %s.Reraise, which would end the gallery by SIGINT", path, x.Name)
					case p == module+"pkg/host" && name == "NewReal":
						t.Errorf("%s names host.NewReal", path)
					case p == module+"pkg/host" && name == "Real":
						t.Errorf("%s names host.Real, which only its own files may", path)
					case p == module+"pkg/engine/hold" && rel != "pkg/engine/engine.go":
						t.Errorf("%s names hold.%s, which only the engine's default hold may", path, name)
					case unreached[p][name] && own != module+"pkg/config":
						t.Errorf("%s names %s.%s, which play never reaches", path, x.Name, name)
					case (p == module+"pkg/engine" && name == "New" || p == module+"pkg/engine/confirm" && name == "Cleanup") && !called[n]:
						t.Errorf("%s names %s.%s other than to call it", path, x.Name, name)
					}
				}
				return true
			})
		}
	}
	for rel := range realSide {
		if !exempt[rel] {
			t.Errorf("%s is exempt, but play does not link it", rel)
		}
	}
	for _, p := range []string{"pkg/ui/prompt", "pkg/ui/screen", "pkg/ui/component/fixture", "pkg/diff", "pkg/engine", "pkg/engine/steps",
		"pkg/engine/confirm", "pkg/engine/controls/dns", "pkg/host/dns", "pkg/output", "pkg/host/hostfake", "pkg/screens/dns", "pkg/screens/screenkit",
		"pkg/screens/backup", "pkg/engine/controls/backup", "pkg/host/backup",
		"pkg/screens/environment", "pkg/engine/controls/environment", "pkg/host/sudoers",
		"pkg/screens/systemdetails", "pkg/screens/logs", "pkg/engine/details", "pkg/engine/logview",
		"pkg/screens/firewall", "pkg/engine/controls/firewall", "pkg/host/ufw", "pkg/host/pve",
		"pkg/screens/users", "pkg/screens/ssh", "pkg/screens/updates", "pkg/screens/runall", "pkg/engine/runall"} {
		if !seen[filepath.Join(root, filepath.FromSlash(p))] {
			t.Errorf("%s, which play links, was not checked", p)
		}
	}
	if files < 20 {
		t.Errorf("only %d files checked", files)
	}
}

// namesReal reports whether t, in pkg/host, names its type Real.
func namesReal(t ast.Expr) bool {
	id, ok := t.(*ast.Ident)
	return ok && id.Name == "Real"
}

// optionsOf returns the name of the package whose Options t names, of
// pkg/engine's and pkg/engine/confirm's, given the file's imports; "" for
// any other type.
func optionsOf(t ast.Expr, pkgs map[string]string) string {
	sel, ok := t.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Options" {
		return ""
	}
	x, ok := sel.X.(*ast.Ident)
	if p := pkgs[x.Name]; ok && (p == "github.com/abbott/hardn/pkg/engine" || p == "github.com/abbott/hardn/pkg/engine/confirm") {
		return x.Name
	}
	return ""
}

// holdsOwn reports whether composite literal lit sets Hold to the
// gallery's own hold, a session's hold method.
func holdsOwn(lit *ast.CompositeLit) bool {
	for _, e := range lit.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Hold" {
				v, ok := kv.Value.(*ast.SelectorExpr)
				return ok && v.Sel.Name == "hold"
			}
		}
	}
	return false
}
