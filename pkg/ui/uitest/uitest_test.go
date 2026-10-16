package uitest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/ui/term"
)

func TestFull(t *testing.T) {
	cells := Full()
	if len(cells) != 48 {
		t.Fatalf("Full has %d cells, want 48", len(cells))
	}
	if err := checkCells(cells); err != nil {
		t.Fatal(err)
	}
	profiles := map[string]colorprofile.Profile{}
	for _, c := range colours {
		profiles[c.name] = c.profile
	}
	for _, c := range cells {
		var col, bg, glyphs string
		var width int
		if _, err := fmt.Sscanf(strings.ReplaceAll(c.Name, "-", " "), "%s %s %s %d", &col, &bg, &glyphs, &width); err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		want := term.Fixed(width, profiles[col], glyphs == "unicode", bg == "dark")
		if c.Env != want {
			t.Errorf("%s: Env is %+v, want %+v", c.Name, c.Env, want)
		}
		if c.Env.Plain || c.Env.Profile < colorprofile.ASCII {
			t.Errorf("%s: a Full cell is plain; plain mode belongs to Core", c.Name)
		}
	}
}

func TestCore(t *testing.T) {
	want := map[string]term.Env{
		"truecolor-dark-unicode-80": term.Fixed(80, colorprofile.TrueColor, true, true),
		"plain-80":                  term.Fixed(80, colorprofile.NoTTY, false, true),
		"truecolor-dark-unicode-60": term.Fixed(60, colorprofile.TrueColor, true, true),
	}
	cells := Core()
	if len(cells) != len(want) {
		t.Fatalf("Core has %d cells, want %d", len(cells), len(want))
	}
	for _, c := range cells {
		if c.Env != want[c.Name] {
			t.Errorf("%s: Env is %+v, want %+v", c.Name, c.Env, want[c.Name])
		}
	}
	plain := cells[1].Env
	if !plain.Plain || plain.Unicode || plain.HasColor() {
		t.Errorf("plain-80 is %+v, want plain, ASCII glyphs and no colour", plain)
	}
}

func TestCheckCells(t *testing.T) {
	env := term.Fixed(80, colorprofile.TrueColor, true, true)
	tests := []struct {
		name  string
		cells []Cell
		ok    bool
	}{
		{"valid", []Cell{{"a-1.b", env}, {"c", env}}, true},
		{"none", nil, false},
		{"empty name", []Cell{{"", env}}, false},
		{"upper case", []Cell{{"Dark", env}}, false},
		{"colon", []Cell{{"a:b", env}}, false},
		{"duplicate", []Cell{{"a", env}, {"a", env}}, false},
	}
	for _, tt := range tests {
		if err := checkCells(tt.cells); (err == nil) != tt.ok {
			t.Errorf("%s: checkCells error %v, want ok=%t", tt.name, err, tt.ok)
		}
	}
}

// TestWritten checks that output reaches the golden file through
// Env.Writer at the cell's profile: one full-colour render, five results.
func TestWritten(t *testing.T) {
	in := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8700")).Bold(true).Render("x")
	tests := []struct {
		profile colorprofile.Profile
		want    string
	}{
		{colorprofile.TrueColor, "\x1b[1;38;2;255;135;0mx\x1b[m"},
		{colorprofile.ANSI256, "\x1b[1;38;5;208mx\x1b[m"},
		{colorprofile.ANSI, "\x1b[1;91mx\x1b[m"}, // 208 → 9 (x/ansi v0.11.8 color.go:736)
		{colorprofile.ASCII, "\x1b[1mx\x1b[m"},
		{colorprofile.NoTTY, "x"},
	}
	for _, tt := range tests {
		got, err := written(term.Fixed(80, tt.profile, true, true), in)
		if err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%v: got %q, want %q", tt.profile, got, tt.want)
		}
	}
}

func TestNotate(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", []string{"$"}},
		{"a ", []string{"a $"}},
		{"a\n", []string{"a$", "$"}},
		{"\x1b[1mb\x1b[m\tc\r\x7f", []string{"␛[1mb␛[m␉c␍␡$"}},
		{"$\n=== x", []string{"$$", "=== x$"}},
	}
	for _, tt := range tests {
		if got := notate(tt.in); !slices.Equal(got, tt.want) {
			t.Errorf("notate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRepresentable(t *testing.T) {
	tests := []struct {
		in string
		ok bool
	}{
		{"\x1b[1m⛛ [+] …\x1b[m\n", true},
		{"a ␛ b", false},      // a Control Picture the notation uses
		{"a \u009b b", false}, // a C1 control
		{"a \xff b", false},   // invalid UTF-8
	}
	for _, tt := range tests {
		if err := representable(tt.in); (err == nil) != tt.ok {
			t.Errorf("representable(%q) error %v, want ok=%t", tt.in, err, tt.ok)
		}
	}
}

func TestOverflows(t *testing.T) {
	tests := []struct {
		name  string
		out   string
		width int
		want  []string
	}{
		{"fits exactly", "abcd\nab", 4, nil},
		{"escapes take no cells", "\x1b[1;38;2;255;135;0mabcd\x1b[m", 4, nil},
		{"one line over", "ab\nabcde\nabc", 4, []string{"line 2 is 5 cells wide, over the width of 4: abcde"}},
		{"wide characters take two cells", "日本語", 5, []string{"line 1 is 6 cells wide, over the width of 5: 日本語"}},
		{"shown in golden notation", "\x1b[1mabcde\x1b[m", 4, []string{"line 1 is 5 cells wide, over the width of 4: ␛[1mabcde␛[m"}},
	}
	for _, tt := range tests {
		if got := overflows(tt.out, tt.width); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// goldenFile builds a golden file of cells a, b and c, with outputs from
// out.
func goldenFile(out map[string]string, names ...string) string {
	var sections []section
	for _, n := range names {
		c := newCell(colours[0], true, true, 80)
		c.Name = n
		sections = append(sections, section{name: n, header: header(c), lines: notate(out[n])})
	}
	return format("TestX", sections)
}

// TestCompare shows that a mismatch is reported by cell, with a diff of
// the changed lines.
func TestCompare(t *testing.T) {
	base := map[string]string{
		"a": "one\ntwo\nthree",
		"b": "\x1b[1mbold\x1b[m\nsame\nsame\nsame\nsame\nsame\nend",
		"c": "x",
	}
	changed := map[string]string{"a": base["a"], "b": "\x1b[4mbold\x1b[m\nsame\nsame\nsame\nsame\nsame\nfinish", "c": base["c"]}
	golden := goldenFile(base, "a", "b", "c")

	if got := compare(golden, goldenFile(base, "a", "b", "c")); got != "" {
		t.Errorf("equal files: got report %q", got)
	}

	want := `1 of 3 cells differ: b

cell b (- golden, + render):
@@ golden line 1, render line 1 @@
- ␛[1mbold␛[m$
+ ␛[4mbold␛[m$
  same$
  same$
@@ golden line 5, render line 5 @@
  same$
  same$
- end$
+ finish$
`
	if got := compare(golden, goldenFile(changed, "a", "b", "c")); got != want {
		t.Errorf("one cell changed: got\n%s\nwant\n%s", got, want)
	}

	got := compare(golden, goldenFile(base, "a", "c", "d"))
	for _, line := range []string{
		"0 of 3 cells differ\n",
		"cells not in the golden file: d\n",
		"cells in the golden file but not rendered: b\n",
	} {
		if !strings.Contains(got, line) {
			t.Errorf("cell added and removed: report lacks %q:\n%s", line, got)
		}
	}

	if got := compare(golden, goldenFile(base, "c", "b", "a")); !strings.Contains(got, "order") {
		t.Errorf("cells reordered: got %q", got)
	}
}

func TestLineDiff(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want string
	}{
		{"equal", []string{"x", "y"}, []string{"x", "y"}, ""},
		{"added at end", []string{"x"}, []string{"x", "y"}, "@@ golden line 1, render line 1 @@\n  x\n+ y\n"},
		{"removed at start", []string{"x", "y"}, []string{"y"}, "@@ golden line 1, render line 1 @@\n- x\n  y\n"},
		{"from nothing", nil, []string{"x"}, "@@ golden line 1, render line 1 @@\n+ x\n"},
	}
	for _, tt := range tests {
		if got := lineDiff(tt.a, tt.b); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestGolden runs the harness end to end on its own golden file: one
// full-colour render, written at one cell per colour profile and in plain
// mode, so the file shows each profile's escape sequences and the
// notation.
func TestGolden(t *testing.T) {
	var cells []Cell
	for _, c := range colours {
		cells = append(cells, newCell(c, true, true, 20))
	}
	cells = append(cells, plainCell(20))
	Golden(t, "harness", cells, func(env term.Env) string {
		accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8700")).Bold(true)
		heading := lipgloss.NewStyle().Underline(true)
		return accent.Render("hardn") + " v0.4.0\n" + heading.Render("Select") + "  \n"
	})
}
