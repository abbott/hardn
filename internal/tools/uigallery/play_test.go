package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// playGallery runs the playable mode with args, the user typing lines,
// and returns its exit status, the transcript (stdout with the answers
// echoed into it, as a terminal shows them), stderr and the number of
// lines left unread.
func playGallery(lines []string, args ...string) (int, string, string, int) {
	var out, errs bytes.Buffer
	ty := uitest.NewTypist(&out, lines...)
	code := run(append([]string{"play"}, args...), func() *prompt.Reader { return prompt.New(ty, false) }, &out, &errs)
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
// sudo change asked y/N, then back with b and 0 to the main menu and out
// with 0; the same workflow with its typed sequence, then q from four
// levels down; the root-login toggle's typed confirmation, a line refused
// first, then q from its sub-menu; the sudo change told to fail its
// second step, which rolls the first back; and the input ending at a
// form's question, which exits 130. The golden files are this package's,
// testdata/golden/play/<path>.golden; make golden-update rewrites only
// pkg/ui's, so after an intended change run
// go test ./internal/tools/uigallery -update.
func TestPlayGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		code  int
	}{
		{"user-management", nil, []string{"4", "1", "1", "1", "1", "y", "b", "0", "b", "0"}, exitOK},
		{"user-management-typed", []string{"-sequence", "typed"}, []string{"1", "1", "1", "1", "bos-san-l01", "q"}, exitOK},
		{"ssh-toggle", nil, []string{"2", "1", "BOS-SAN-L01", "bos-san-l01", "q"}, exitOK},
		{"fail", []string{"-fail", "2", "workflow-step/user-manage-sudo-confirmation"}, []string{"y"}, exitOK},
		{"abort", []string{"sub-menu/01-user"}, []string{"2", "alice"}, prompt.ExitInterrupted},
	}
	for _, tt := range tests {
		uitest.Golden(t, "play/"+tt.name, uitest.Core(), func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != tt.code || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread; want exit %d", tt.name, args, code, errs, unread, tt.code)
			}
			return out
		})
	}
}

// TestPlayEnds checks how play ends at each kind of prompt: the end of
// the input aborts with exit status 130 and nothing applied, q quits and
// 0 or b at the first screen goes back, both with 0, and the record says
// what was applied.
func TestPlayEnds(t *testing.T) {
	sudo := []string{"1", "1", "1", "1"}
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
		{"b at the main menu", nil, []string{"b"}, exitOK, false},
		{"0 at a first sub-menu", []string{"sub-menu/user-ssh-keys"}, []string{"0"}, exitOK, false},
		{"b at a first workflow's first field", []string{"workflow-step/user-create-user"}, []string{"b"}, exitOK, false},
		{"0 at a first workflow's confirmation", []string{"workflow-step/user-manage-sudo-confirmation"}, []string{"0"}, exitOK, false},
		{"a dry-run sequence", []string{"workflow-step/ssh-login-toggle-confirmation/dry-run"}, nil, exitOK, false},
	}
	for _, tt := range tests {
		code, out, errs, unread := playGallery(tt.lines, append([]string{"-profile", "plain"}, tt.args...)...)
		_, record, _ := strings.Cut(out, "# simulated system ")
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
		_, record, _ := strings.Cut(out, "# simulated system ")
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
// item of a menu mock to a mock, every menu mock's screen is one whose
// menu play binds, and every mock can be reached from the main menu.
func TestPlayReaches(t *testing.T) {
	mocks := map[string]screen.Mock{}
	for _, m := range screen.Mocks() {
		mocks[m.Name] = m
		if _, ok := menuOf(m.Screen); m.Screen != nil && !ok {
			t.Errorf("%s: play cannot bind its menu", m.Name)
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
	reached, next := map[string]bool{}, []string{"main-menu/00-tui-main"}
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
// with the answer the terminal echoes. Below 30 columns the toggle's
// typed confirmation does not show, and the flow cancels it unasked.
func TestPlaySmoke(t *testing.T) {
	for _, p := range profiles {
		for _, th := range []string{"dark", "light"} {
			for _, ascii := range []bool{false, true} {
				for _, w := range []int{20, 60, 80} {
					look := []string{"-profile", p.name, "-theme", th, fmt.Sprintf("-ascii=%t", ascii), fmt.Sprintf("-width=%d", w)}
					lines := []string{"2", "1", "bos-san-l01", "q"}
					if w < 30 {
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
		code := run(append([]string{"play"}, args...), func() *prompt.Reader { read = true; return noInput() }, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("play %v: exit %d, stdout %q, stderr %q, stdin read %t; want exit %d, a message, no output", args, code, out.String(), errs.String(), read, exitUsage)
		}
	}
	code, _, errs, _ := playGallery(nil, "-h")
	if code != exitOK || !strings.Contains(errs, playUsage) || !strings.Contains(errs, "-fail") {
		t.Errorf("play -h: exit %d, stderr %q", code, errs)
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
		code := run([]string{"play", "-profile", "plain"}, func() *prompt.Reader { return prompt.New(ty, false) }, w, &errs)
		if code != tt.code || errs.String() != tt.errs || !w.broke {
			t.Errorf("%s: exit %d, stderr %q, the record's write failed %t; want exit %d, stderr %q", tt.name, code, errs.String(), w.broke, tt.code, tt.errs)
		}
	}
}

// TestPlayTouchesNothing checks that the code play runs cannot change the
// system: no file of the gallery, nor of the pkg/ui packages it plays,
// imports os/exec or a net package, or names a function of os that
// writes, removes or renames a file, starts a process or changes the
// environment or the working directory. The simulated system records; it
// never applies. Lip Gloss's own init effects, such as colorprofile's
// "tmux info", are outside this check (docs/refactor/phase0/side-effects.md
// §13.1).
func TestPlayTouchesNothing(t *testing.T) {
	banned := map[string]bool{
		"WriteFile": true, "Create": true, "CreateTemp": true, "OpenFile": true, "Mkdir": true, "MkdirAll": true,
		"MkdirTemp": true, "Remove": true, "RemoveAll": true, "Rename": true, "Chmod": true, "Chown": true,
		"Lchown": true, "Chtimes": true, "Symlink": true, "Link": true, "Truncate": true, "StartProcess": true,
		"Setenv": true, "Unsetenv": true, "Clearenv": true, "Chdir": true,
	}
	dirs := []string{"."}
	for _, p := range []string{"component", "flow", "form", "keys", "nav", "prompt", "screen", "term", "theme"} {
		dirs = append(dirs, filepath.Join("..", "..", "..", "pkg", "ui", p))
	}
	files := 0
	for _, dir := range dirs {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			files++
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			osName := ""
			for _, im := range f.Imports {
				p, _ := strconv.Unquote(im.Path.Value)
				switch {
				case p == "os/exec", p == "net", strings.HasPrefix(p, "net/"):
					t.Errorf("%s imports %s", path, p)
				case p == "os":
					osName = "os"
					if im.Name != nil {
						osName = im.Name.Name
					}
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if x, ok := sel.X.(*ast.Ident); ok && osName != "" && x.Name == osName && banned[sel.Sel.Name] {
					t.Errorf("%s calls os.%s", path, sel.Sel.Name)
				}
				return true
			})
		}
	}
	if files < 20 {
		t.Errorf("only %d files checked", files)
	}
}
