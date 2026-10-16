package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
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

// runGallery runs the gallery with args and returns its exit status, stdout
// and stderr.
func runGallery(args ...string) (int, string, string) {
	var out, errs bytes.Buffer
	code := run(args, &out, &errs)
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
	code, out, _ = runGallery("-matrix", "divider")
	for _, c := range matrix() {
		if n := strings.Count(ansi.Strip(out), " divider/normal "+c.name+" "); code != exitOK || n != 1 {
			t.Errorf("-matrix: exit %d, cell %s shown %d times", code, c.name, n)
		}
	}
}

// TestUsage checks that a wrong flag or argument stops the gallery with
// exit status 2, a message and no output.
func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		{"-profile", "ansi"},
		{"-theme", "auto"},
		{"-width", "0"},
		{"-nope"},
		{"menu/nope"},
		{"-matrix", "-width", "80"},
		{"-matrix", "-theme", "light", "-ascii"},
	} {
		code, out, errs := runGallery(args...)
		if code != exitUsage || out != "" || errs == "" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q; want exit %d, a message and no output", args, code, out, errs, exitUsage)
		}
	}
	if code, _, _ := runGallery("-h"); code != exitOK {
		t.Errorf("-h: exit %d, want %d", code, exitOK)
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
