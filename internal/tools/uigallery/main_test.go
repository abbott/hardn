package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/component/fixture"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// goldenDir holds the component goldens, one file per state:
// <component>/<state>.golden.
var goldenDir = filepath.Join("..", "..", "..", "pkg", "ui", "component", "testdata", "golden")

// TestCatalogueMatchesGoldens checks that the gallery shows exactly the
// component states that have a golden file: a golden with no gallery
// entry, or an entry with no golden, fails.
func TestCatalogueMatchesGoldens(t *testing.T) {
	var goldens []string
	err := filepath.WalkDir(goldenDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(goldenDir, path)
		if err != nil {
			return err
		}
		name, ok := strings.CutSuffix(filepath.ToSlash(rel), ".golden")
		if !ok {
			return fmt.Errorf("%s is not a golden file", path)
		}
		goldens = append(goldens, name)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	for _, s := range catalogue() {
		entries = append(entries, s.Name)
	}
	for _, g := range goldens {
		if !slices.Contains(entries, g) {
			t.Errorf("golden %s has no gallery entry", g)
		}
	}
	for _, e := range entries {
		if !slices.Contains(goldens, e) {
			t.Errorf("gallery entry %s has no golden", e)
		}
	}
	if len(goldens) == 0 || len(goldens) != len(entries) {
		t.Errorf("%d goldens and %d gallery entries", len(goldens), len(entries))
	}
}

// TestMatrix checks that -matrix renders in every cell of the component
// goldens' matrix, uitest.Full, with the same names and environments, and
// then in plain mode as uitest.Core has it, at each of Full's widths.
func TestMatrix(t *testing.T) {
	cells, full := matrix(), uitest.Full()
	if len(cells) != len(full)+3 {
		t.Fatalf("%d cells, want Full's %d and 3 plain", len(cells), len(full))
	}
	for i, c := range full {
		if cells[i].name != c.Name || cells[i].env != c.Env {
			t.Errorf("cell %d is %s %+v, want %s %+v", i, cells[i].name, cells[i].env, c.Name, c.Env)
		}
	}
	var plain uitest.Cell
	for _, c := range uitest.Core() {
		if c.Env.Plain {
			plain = c
		}
	}
	for i, w := range []int{60, 80, 120} {
		c := cells[len(full)+i]
		want := plain.Env
		want.Width = w
		if c.name != fmt.Sprintf("plain-%d", w) || c.env != want {
			t.Errorf("plain cell %d is %s %+v, want plain-%d %+v", i, c.name, c.env, w, want)
		}
	}
}

// goldenSections returns the cells of the golden file at path, by cell
// name, decoded from golden notation (pkg/ui/uitest): each output line
// without its closing $, and each Unicode Control Picture back to its
// control character.
func goldenSections(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decode := strings.NewReplacer("\u2421", "\x7f")
	sections, lines, name := map[string]string{}, []string(nil), ""
	flush := func() {
		if name != "" {
			sections[name] = strings.Join(lines, "\n")
		}
	}
	for _, l := range strings.Split(string(b), "\n") {
		if h, ok := strings.CutPrefix(l, "=== "); ok && !strings.HasSuffix(l, "$") {
			flush()
			name, _, _ = strings.Cut(h, ":")
			lines = nil
			continue
		}
		if out, ok := strings.CutSuffix(l, "$"); ok && name != "" {
			lines = append(lines, decode.Replace(strings.Map(func(r rune) rune {
				if r >= 0x2400 && r <= 0x241f {
					return r - 0x2400
				}
				return r
			}, out)))
		}
	}
	flush()
	return sections, nil
}

// written returns s as env's Env.Writer passes it on.
func written(t *testing.T, env term.Env, s string) string {
	t.Helper()
	var b strings.Builder
	if _, err := io.WriteString(env.Writer(&b), s); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestRendersAsGoldens checks that the gallery writes each state exactly
// as its golden file holds it, in every cell of the golden matrix: the
// file's section for the cell, between the gallery's header for the state
// and the blank line after it.
func TestRendersAsGoldens(t *testing.T) {
	g := gallery{}
	cells := matrix()[:len(uitest.Full())]
	for _, s := range catalogue() {
		sections, err := goldenSections(filepath.Join(goldenDir, filepath.FromSlash(s.Name)+".golden"))
		if err != nil {
			t.Fatal(err)
		}
		if len(sections) != len(cells) {
			t.Errorf("%s: the golden file has %d cells, want %d", s.Name, len(sections), len(cells))
		}
		for _, c := range cells {
			out, err := g.render(s, c, s.Name)
			if err != nil {
				t.Fatal(err)
			}
			head := component.SectionHeader{Title: s.Name}.Render(component.NewCtx(c.env))
			want := written(t, c.env, head) + "\n\n" + sections[c.name] + "\n\n"
			if got := written(t, c.env, out); got != want {
				t.Errorf("%s in %s: the gallery writes\n%q\nwant, from the golden file,\n%q", s.Name, c.name, got, want)
			}
		}
	}
}

// noInput is the line reader of a gallery run that is never asked
// anything: its input has ended.
func noInput() *prompt.Reader { return prompt.New(strings.NewReader(""), false) }

// offTerminal is the sizer of a gallery whose stdout is not a terminal,
// as a test's is not: play keeps -width's and -height's defaults, 80 and
// 24, whatever terminal the test runs in.
func offTerminal() (int, int, bool) { return 0, 0, false }

// runGallery runs the gallery with args and returns its exit status, stdout
// and stderr.
func runGallery(args ...string) (int, string, string) {
	var out, errs bytes.Buffer
	code := run(args, noInput, offTerminal, &out, &errs)
	return code, out.String(), errs.String()
}

// TestSmoke runs the gallery in-process for every profile, both themes
// and both glyph sets, at two widths, and checks the output: every state
// under its name, the escape sequences the profile allows and no more,
// and no line wider than the terminal.
func TestSmoke(t *testing.T) {
	names := []string{}
	for _, s := range catalogue() {
		names = append(names, s.Name)
	}
	for _, p := range profiles {
		for _, th := range []string{"dark", "light"} {
			for _, ascii := range []bool{false, true} {
				for _, w := range []int{60, 80} {
					args := []string{"-profile", p.name, "-theme", th, fmt.Sprintf("-ascii=%t", ascii), fmt.Sprintf("-width=%d", w)}
					code, out, errs := runGallery(args...)
					where := strings.Join(args, " ")
					if code != exitOK || errs != "" {
						t.Fatalf("%s: exit %d, stderr %q", where, code, errs)
					}
					plain := ansi.Strip(out)
					for _, n := range names {
						if !strings.Contains(plain, " "+n+" ") {
							t.Errorf("%s: no section for %s", where, n)
						}
					}
					switch p.name {
					case "plain":
						if strings.Contains(out, "\x1b") {
							t.Errorf("%s: an escape sequence in plain mode", where)
						}
					case "none":
						if strings.Contains(out, "38;") || strings.Contains(out, "48;") || !strings.Contains(out, "\x1b[1m") {
							t.Errorf("%s: want bold and no colour", where)
						}
					default:
						if !strings.Contains(out, "\x1b[") {
							t.Errorf("%s: no colour", where)
						}
					}
					if (p.name == "plain" || ascii) && strings.ContainsFunc(plain, func(r rune) bool { return r > 0x7e }) {
						t.Errorf("%s: a glyph outside ASCII", where)
					}
					for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
						if ansi.StringWidth(l) > w {
							t.Errorf("%s: line %q is wider than %d", where, ansi.Strip(l), w)
						}
					}
				}
			}
		}
	}
}

// TestSelect checks -list and the state arguments.
func TestSelect(t *testing.T) {
	all := catalogue()
	var every []string
	for _, s := range all {
		every = append(every, s.Name)
	}
	tests := []struct {
		args []string
		want []string
	}{
		{nil, every},
		{[]string{"all"}, every},
		{[]string{"divider"}, []string{"divider/normal"}},
		{[]string{"modeindicator/dry-run", "badge", "divider/normal"}, []string{"divider/normal", "modeindicator/dry-run", "badge/normal", "badge/long", "badge/empty"}},
		{[]string{"menu"}, []string{"menu/main", "menu/sub", "menu/risk", "menu/disabled", "menu/long", "menu/compact", "menu/empty"}},
	}
	for _, tt := range tests {
		code, out, errs := runGallery(append([]string{"-list"}, tt.args...)...)
		got := strings.Fields(out)
		if code != exitOK || errs != "" || !slices.Equal(got, tt.want) {
			t.Errorf("-list %v: exit %d, stderr %q, listed %v, want %v", tt.args, code, errs, got, tt.want)
		}
	}
	code, out, _ := runGallery("-profile", "plain", "menuheading/no-mode")
	if want := "# menuheading/no-mode " + strings.Repeat("-", 42) + "\n\n  Select an option\n\n"; code != exitOK || out != want {
		t.Errorf("one state: exit %d, output %q, want %q", code, out, want)
	}
	// Each cell's title holds the state's and the cell's whole names, for
	// the longest state name too, which cuts nothing at 60 columns.
	longest := slices.MaxFunc(all, func(a, b fixture.State) int {
		return ansi.StringWidth(a.Name) - ansi.StringWidth(b.Name)
	}).Name
	for _, name := range []string{"divider/normal", longest} {
		code, out, _ = runGallery("-matrix", name)
		for _, c := range matrix() {
			if n := strings.Count(ansi.Strip(out), " "+name+" "+c.name+" "); code != exitOK || n != 1 {
				t.Errorf("-matrix %s: exit %d, cell %s shown %d times", name, code, c.name, n)
			}
		}
	}
}

// TestNarrow checks the gallery's header on terminals too narrow for a
// state's name: it holds the whole name, so that no two states look the
// same, and runs past the terminal's width only as far as the name needs.
func TestNarrow(t *testing.T) {
	for _, p := range []string{"truecolor", "plain"} {
		for _, w := range []int{1, 20, 24} {
			for _, s := range catalogue() {
				args := []string{"-profile", p, fmt.Sprintf("-width=%d", w), s.Name}
				code, out, errs := runGallery(args...)
				head, _, _ := strings.Cut(ansi.Strip(out), "\n")
				if code != exitOK || errs != "" || !strings.Contains(head, " "+s.Name+" ") || ansi.StringWidth(head) > max(w, ansi.StringWidth(s.Name)+headerFrame) {
					t.Errorf("%v: exit %d, stderr %q, header %q; want the whole name, as wide as it needs", args, code, errs, head)
				}
			}
		}
	}
}

// TestUsage checks that a wrong flag or argument stops the gallery with
// exit status 2, a message and no output, and that -h prints the flags'
// help, -height's saying which height shows the main menu's full form.
func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		{"-profile", "ansi"},
		{"-theme", "auto"},
		{"-width", "0"},
		{"-nope"},
		{"menu/nope"},
		{"menu", "-profile", "plain"},
		{"-matrix", "-width", "80"},
		{"-matrix", "-theme", "light", "-ascii"},
		{"-matrix", "-height", "40"},
		{"-height", "0"},
		{"-screens", "-matrix"},
		{"-screens", "nope"},
		{"-screens", "sub-menu", "-width", "60"},
	} {
		code, out, errs := runGallery(args...)
		if code != exitUsage || out != "" || errs == "" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit %d, a message and no output", args, code, out, errs, exitUsage)
		}
	}
	if code, _, errs := runGallery("menu", "-profile", "plain"); code != exitUsage || !strings.Contains(errs, "flag -profile comes after a component or state name; flags go before them") {
		t.Errorf("a flag after a name: exit %d, stderr %q; want exit %d and a hint at the order", code, errs, exitUsage)
	}
	if code, _, errs := runGallery("-h"); code != exitOK || !strings.Contains(errs, "compact form under 40 rows, or when its full form does not fit, so comparing it with 00-TUI-MAIN.png needs -height 40") {
		t.Errorf("-h: exit %d, stderr %q; want %d, and -height's help to say when the main menu takes its compact form", code, errs, exitOK)
	}
}

// TestEastAsian checks that with RUNEWIDTH_EASTASIAN=1, which makes x/ansi
// measure ambiguous-width glyphs such as ─ as two cells, the gallery
// refuses to render, with exit status 1, a message and no output, and
// still lists its states. x/ansi reads the variable at its init, so the
// test runs this test binary again with it set, as the gallery with the
// arguments in UIGALLERY_TEST_ARGS.
func TestEastAsian(t *testing.T) {
	if args, ok := os.LookupEnv("UIGALLERY_TEST_ARGS"); ok {
		os.Exit(run(strings.Fields(args), noInput, offTerminal, os.Stdout, os.Stderr))
	}
	tests := []struct {
		args string
		code int
		out  string
		errs string // what stderr holds, or "" for nothing
	}{
		{"divider", exitError, "", "as 2 cells, and the components assume 1; unset RUNEWIDTH_EASTASIAN"},
		{"-profile plain -width 20", exitError, "", "unset RUNEWIDTH_EASTASIAN"},
		{"-list divider", exitOK, "divider/normal\n", ""},
	}
	for _, tt := range tests {
		cmd := exec.Command(os.Args[0], "-test.run=^TestEastAsian$")
		cmd.Env = append(os.Environ(), "RUNEWIDTH_EASTASIAN=1", "UIGALLERY_TEST_ARGS="+tt.args)
		var out, errs bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errs
		code := 0
		if err := cmd.Run(); err != nil {
			exit := (*exec.ExitError)(nil)
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != tt.code || out.String() != tt.out || (tt.errs == "") != (errs.Len() == 0) || !strings.Contains(errs.String(), tt.errs) {
			t.Errorf("%s with RUNEWIDTH_EASTASIAN=1: exit %d, stdout %q, stderr %q; want exit %d, stdout %q, stderr with %q", tt.args, code, out.String(), errs.String(), tt.code, tt.out, tt.errs)
		}
	}
}

// writeFile writes s to a file in a new temporary directory and returns
// its path.
func writeFile(t *testing.T, s string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "uigallery.local.json")
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestOverrideExample checks the committed template: holding the current
// values, it changes no state in either palette at any colour profile;
// and each value it holds, changed, changes some state at its own profile
// and palette, so that the first check can fail when the template drifts
// from palette.go.
func TestOverrideExample(t *testing.T) {
	const example = "uigallery.local.json.example"
	tmpl, err := readOverride(example)
	if err != nil {
		t.Fatal(err)
	}
	// changes are the template's three values: the profile each shows at,
	// and a different value for it, false when the template leaves it out.
	changes := []struct {
		profile string
		change  func(tokenValues) (tokenValues, bool)
	}{
		{"truecolor", func(v tokenValues) (tokenValues, bool) {
			hex := "#FF0000"
			if strings.EqualFold(v.Hex, hex) {
				hex = "#00FF00"
			}
			return tokenValues{Hex: hex}, v.Hex != ""
		}},
		{"256", func(v tokenValues) (tokenValues, bool) {
			if v.ANSI256 == nil {
				return tokenValues{}, false
			}
			i := (*v.ANSI256 + 1) % 256
			return tokenValues{ANSI256: &i}, true
		}},
		{"16", func(v tokenValues) (tokenValues, bool) {
			if v.ANSI == nil {
				return tokenValues{}, false
			}
			i := (*v.ANSI + 1) % 16
			return tokenValues{ANSI: &i}, true
		}},
	}
	for _, th := range []string{"dark", "light"} {
		values := tmpl.Light
		if th == "dark" {
			values = tmpl.Dark
		}
		if len(values) == 0 {
			t.Errorf("the example sets nothing for the %s palette", th)
		}
		for _, ch := range changes {
			args := []string{"-profile", ch.profile, "-theme", th}
			_, base, _ := runGallery(args...)
			code, out, errs := runGallery(append(args, "-override", example)...)
			if code != exitOK || errs != "" || out != base {
				t.Errorf("the example, %v: exit %d, stderr %q, output changed: %v", args, code, errs, out != base)
			}
			for token, v := range values {
				changed, ok := ch.change(v)
				if !ok {
					continue
				}
				f := overrideFile{}
				if th == "dark" {
					f.Dark = map[string]tokenValues{token: changed}
				} else {
					f.Light = map[string]tokenValues{token: changed}
				}
				b, err := json.Marshal(f)
				if err != nil {
					t.Fatal(err)
				}
				code, out, errs := runGallery(append(args, "-override", writeFile(t, string(b)))...)
				if code != exitOK || errs != "" || out == base {
					t.Errorf("%s changed to %s, %v: exit %d, stderr %q, output changed: %v; want a change", token, b, args, code, errs, out != base)
				}
			}
		}
	}
}

// TestOverride checks the colour override file: a new value shows at its
// profile and only in its palette, and a bad file stops the gallery with
// exit status 1 before any output.
func TestOverride(t *testing.T) {
	file := writeFile(t, `{"dark": {"divider": {"hex": "#123456", "256": 200}}}`)
	tests := []struct {
		args []string
		want string // a colour the output must hold, or "" for no change
	}{
		{[]string{"-profile", "truecolor"}, "38;2;18;52;86m"},
		{[]string{"-profile", "256"}, "38;5;200m"},
		{[]string{"-profile", "16"}, ""},
		{[]string{"-profile", "truecolor", "-theme", "light"}, ""},
	}
	for _, tt := range tests {
		_, before, _ := runGallery(append(tt.args, "divider")...)
		code, after, errs := runGallery(append(tt.args, "-override", file, "divider")...)
		switch {
		case code != exitOK || errs != "":
			t.Errorf("%v: exit %d, stderr %q", tt.args, code, errs)
		case tt.want == "" && after != before:
			t.Errorf("%v: the override changed the output", tt.args)
		case tt.want != "" && !strings.Contains(after, tt.want):
			t.Errorf("%v: output %q does not hold %q", tt.args, after, tt.want)
		}
	}

	for _, bad := range []string{
		`{"dark": {"text.mute": {"hex": "#123456"}}}`,
		`{"light": {"divider": {"hex": "123456"}}}`,
		`{"dark": {"divider": {"16": 16}}}`,
		`{"dark": {"divider": {"rgb": "#123456"}}}`,
		`{"colours": {}}`,
		`{} {}`,
		`not json`,
	} {
		code, out, errs := runGallery("-override", writeFile(t, bad))
		if code != exitError || out != "" || !strings.Contains(errs, "override file") {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit %d and a message", bad, code, out, errs, exitError)
		}
	}
	if code, _, _ := runGallery("-override", filepath.Join(t.TempDir(), "missing.json")); code != exitError {
		t.Errorf("a missing file: exit %d, want %d", code, exitError)
	}
}

// screenGoldenDir holds the screen goldens, one file per drawing:
// <layout>/<screen>.golden, and <layout>/<screen>/<sequence>/<step>.golden
// for a workflow's steps.
var screenGoldenDir = filepath.Join("..", "..", "..", "pkg", "ui", "screen", "testdata", "golden")

// TestScreensRenderAsGoldens checks that -screens writes each mock's
// drawings exactly as their golden files hold them, in each golden cell
// the flags can name: the core set, and the main menu at 80x40 on both
// themes and at 60x40. Each drawing comes between the gallery's headers and source
// note and the blank line after it, a step without the blank lines that
// open it.
func TestScreensRenderAsGoldens(t *testing.T) {
	cells := map[string][]string{
		"truecolor-dark-unicode-80":     nil,
		"plain-80":                      {"-profile", "plain"},
		"truecolor-dark-unicode-60":     {"-width", "60"},
		"truecolor-dark-unicode-80x40":  {"-height", "40"},
		"truecolor-light-unicode-80x40": {"-height", "40", "-theme", "light"},
		"truecolor-dark-unicode-60x40":  {"-width", "60", "-height", "40"},
	}
	for _, m := range screen.Mocks() {
		for name, args := range cells {
			if strings.HasSuffix(name, "x40") && !strings.HasPrefix(m.Name, "main-menu/") {
				continue
			}
			code, got, errs := runGallery(append(append([]string{"-screens"}, args...), m.Name)...)
			env := term.Fixed(80, colorprofile.TrueColor, true, !strings.Contains(name, "light"))
			switch name {
			case "plain-80":
				env = term.Fixed(80, colorprofile.NoTTY, false, true)
			case "truecolor-dark-unicode-60", "truecolor-dark-unicode-60x40":
				env.Width = 60
			}
			if strings.HasSuffix(name, "x40") {
				env.Height = 40
			}
			c := component.NewCtx(env)
			gap := "\n\n"
			want := written(t, env, header(c, m.Name)+gap+source(c, m.Source)+gap)
			for _, n := range m.Names() {
				sections, err := goldenSections(filepath.Join(screenGoldenDir, filepath.FromSlash(n)+".golden"))
				if err != nil {
					t.Fatal(err)
				}
				section, ok := sections[name]
				if !ok {
					t.Fatalf("%s: no cell %s", n, name)
				}
				if step, ok := strings.CutPrefix(n, m.Name+"/"); ok {
					want += written(t, env, header(c, step)+gap)
					section = strings.TrimLeft(section, "\n")
				}
				want += section + gap
			}
			if code != exitOK || errs != "" || got != want {
				t.Errorf("%s in %s: exit %d, stderr %q, the gallery writes\n%q\nwant, from the golden files,\n%q", m.Name, name, code, errs, got, want)
			}
		}
	}
}

// TestScreenSelect checks -screens with -list and the screen arguments,
// and that every header the screens take shows its whole name.
func TestScreenSelect(t *testing.T) {
	var every []string
	for _, m := range screen.Mocks() {
		every = append(every, m.Names()...)
		for _, n := range m.Names() {
			title := strings.TrimPrefix(n, m.Name+"/")
			if w := ansi.StringWidth(title) + headerFrame; w > theme.ContentWidth || ansi.StringWidth(m.Name)+headerFrame > theme.ContentWidth {
				t.Errorf("%s: its header would be cut", n)
			}
		}
	}
	sudo := "workflow-step/user-manage-sudo-confirmation"
	tests := []struct {
		args []string
		want []string
	}{
		{nil, every},
		{[]string{"all"}, every},
		{[]string{"main-menu"}, []string{"main-menu/00-tui-main"}},
		{[]string{sudo + "/failure", "sub-menu/01-user"}, []string{"sub-menu/01-user", sudo + "/failure/1-confirm", sudo + "/failure/2-failed"}},
		{[]string{sudo + "/dry-run/1-planned"}, []string{sudo + "/dry-run/1-planned"}},
	}
	for _, tt := range tests {
		code, out, errs := runGallery(append([]string{"-screens", "-list"}, tt.args...)...)
		if got := strings.Fields(out); code != exitOK || errs != "" || !slices.Equal(got, tt.want) {
			t.Errorf("-screens -list %v: exit %d, stderr %q, listed %v, want %v", tt.args, code, errs, got, tt.want)
		}
	}
	// One step: the mock's header and note, then the step under its own.
	code, out, _ := runGallery("-screens", "-profile", "plain", sudo+"/normal/2-applied")
	plain := strings.Split(out, "\n")
	if code != exitOK || !strings.HasPrefix(plain[0], "# "+sudo+" ") || !strings.Contains(out, "\n# normal/2-applied ") ||
		strings.Contains(out, "normal/1-confirm") || !strings.HasSuffix(out, "# [+] Operation Successful\n\n") {
		t.Errorf("one step: exit %d, output\n%s", code, out)
	}
}

// TestScreenSmoke runs -screens in-process for every profile, both themes
// and both glyph sets, at 80 and 60 columns and at the narrow widths where
// a typed confirmation stops fitting (29 to 32 columns, below which the
// flow cancels it), and at 1 and 20, and checks the output: the exit
// status 0; every mock under its header; every sequence step by step, or,
// one that ends otherwise at the width (screen.Diverged), whole under its
// own name's header; the escape sequences the profile allows and no more;
// only ASCII where asked, data such as 01-USER's "UID ≥ 1000" included
// (DEV-036); and no line wider than the terminal but a header, which
// shows its whole name.
func TestScreenSmoke(t *testing.T) {
	narrowed := 0
	var typed []string
	for _, m := range screen.Mocks() {
		for _, s := range m.Sequences {
			for _, a := range s.Answers {
				typed = append(typed, a.Line)
			}
		}
	}
	for _, p := range profiles {
		for _, th := range []string{"dark", "light"} {
			for _, ascii := range []bool{false, true} {
				for _, w := range []int{1, 20, 29, 30, 31, 32, 60, 80} {
					args := []string{"-screens", "-profile", p.name, "-theme", th, fmt.Sprintf("-ascii=%t", ascii), fmt.Sprintf("-width=%d", w)}
					code, out, errs := runGallery(args...)
					where := strings.Join(args, " ")
					if code != exitOK || errs != "" {
						t.Fatalf("%s: exit %d, stderr %q", where, code, errs)
					}
					c := newCell(profiles[slices.Index(profiles, p)], th == "dark", !ascii, w)
					ctx := component.NewCtx(c.env)
					lines := strings.Split(ansi.Strip(out), "\n")
					headers := map[string]bool{}
					has := func(title string) bool {
						h := ansi.Strip(header(ctx, title))
						headers[h] = true
						return slices.Contains(lines, h)
					}
					for _, m := range screen.Mocks() {
						if !has(m.Name) {
							t.Errorf("%s: no header for %s", where, m.Name)
						}
						for _, s := range m.Sequences {
							_, err := s.Play(ctx)
							var d *screen.Diverged
							if errors.As(err, &d) {
								narrowed++
								if !has(s.Name) {
									t.Errorf("%s: %s/%s ends %s, and has no header of its own", where, m.Name, s.Name, screen.Ending(d.Ended))
								}
								continue
							}
							for _, step := range s.Steps() {
								if !has(s.Name + "/" + step) {
									t.Errorf("%s: no header for %s/%s/%s", where, m.Name, s.Name, step)
								}
							}
						}
					}
					switch p.name {
					case "plain":
						if strings.Contains(out, "\x1b") {
							t.Errorf("%s: an escape sequence in plain mode", where)
						}
					case "none":
						if strings.Contains(out, "38;") || strings.Contains(out, "48;") || !strings.Contains(out, "\x1b[1m") {
							t.Errorf("%s: want bold and no colour", where)
						}
					default:
						if !strings.Contains(out, "\x1b[") {
							t.Errorf("%s: no colour", where)
						}
					}
					if (p.name == "plain" || ascii) && strings.ContainsFunc(ansi.Strip(out), func(r rune) bool { return r > 0x7e }) {
						t.Errorf("%s: a glyph outside ASCII", where)
					}
					for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
						if headers[ansi.Strip(l)] {
							continue
						}
						// A prompt's line ends with the space the line
						// reader writes and the answer the terminal
						// echoes, which the terminal wraps.
						for _, a := range typed {
							if cut, ok := strings.CutSuffix(l, " "+a); ok {
								l = cut
								break
							}
						}
						if ansi.StringWidth(l) > w {
							t.Errorf("%s: line %q is wider than %d", where, ansi.Strip(l), w)
						}
					}
				}
			}
		}
	}
	if narrowed == 0 {
		t.Error("no sequence ended otherwise at any width: the narrow path went untested")
	}
}
