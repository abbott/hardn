package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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

// TestOverride checks the colour override file: the committed example
// loads and, holding the current values, changes nothing; a new value
// shows at its profile and only in its palette; and a bad file stops the
// gallery with exit status 1 before any output.
func TestOverride(t *testing.T) {
	_, base, _ := runGallery("divider")
	code, out, errs := runGallery("-override", "uigallery.local.json.example", "divider")
	if code != exitOK || errs != "" || out != base {
		t.Errorf("the example: exit %d, stderr %q, output changed: %v", code, errs, out != base)
	}

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
