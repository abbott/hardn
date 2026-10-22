package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The glyphs mode: today's status symbols beside candidate sets, and a
// strip of each candidate beside ⛛ ⭘ ⛬.

// TestGlyphsGolden renders each set, and the strip, on the core set, and
// pins what glyphs prints: the note, the section's header, and the set
// or the strip. The golden files are testdata/golden/glyphs/<name>.golden.
// The sets side by side, which no core cell is wide enough for, are
// TestGlyphsSideBySide's.
func TestGlyphsGolden(t *testing.T) {
	for _, name := range glyphNames() {
		uitest.Golden(t, "glyphs/"+name, uitest.Core(), func(env term.Env) string {
			args := append(append([]string{"glyphs"}, lookArgs(t, env)...), name)
			code, out, errs := runGallery(args...)
			if code != exitOK || errs != "" {
				t.Errorf("%v: exit %d, stderr %q; want exit 0", args, code, errs)
			}
			return out
		})
	}
}

// TestGlyphsCurrent checks that the current set draws what the components
// draw, byte for byte: its risk row RiskSummary's, its rows StatusList's,
// a label and a detail too long for the row cut as StatusRow cuts them; at
// widths that stack the rows and widths that do not, in both glyph sets,
// both palettes and three profiles. So a candidate set differs from today's
// rows by its glyphs and colours alone.
func TestGlyphsCurrent(t *testing.T) {
	current := glyphSets()[0]
	rows := append(slices.Clone(glyphRows),
		component.StatusRow{Label: "A label far too long for the label column", State: component.StatusPartial, Detail: "a detail far too long to fit beside it in the row"},
		component.StatusRow{Label: "Firewall", State: component.StatusNotApplicable, Detail: strings.Repeat("x", 80)},
	)
	for _, w := range []int{12, 20, 40, 60, 63, 64, 80, 120} {
		for _, env := range []term.Env{
			term.Fixed(w, colorprofile.TrueColor, true, true),
			term.Fixed(w, colorprofile.ANSI256, false, false),
			term.Fixed(w, colorprofile.ANSI, true, false),
			term.Fixed(w, colorprofile.NoTTY, false, true),
		} {
			c := component.NewCtx(env)
			name := fmt.Sprintf("width %d, profile %v, unicode %t, dark %t", w, env.Profile, env.Unicode, env.Dark)
			if got, want := riskRow(c), glyphRisk.Render(c); got != want {
				t.Errorf("%s: the risk row\n%q\nwant RiskSummary's\n%q", name, got, want)
			}
			var got []string
			for _, r := range rows {
				got = append(got, current.row(c, r))
			}
			if want := (component.StatusList{Rows: rows}).Render(c); strings.Join(got, "\n") != want {
				t.Errorf("%s: the rows\n%q\nwant StatusList's\n%q", name, strings.Join(got, "\n"), want)
			}
		}
	}
}

// TestGlyphsRiskAlone checks the point of the candidate sets, in both glyph
// sets: the risk icon is on the risk row alone, no mark is the risk icon or
// ⛛, and a set's marks for the three states differ from one another but
// where a set folds a state into ⭘ or shows no marker. Today's set shows
// the risk icon twice, its Partial row's too.
func TestGlyphsRiskAlone(t *testing.T) {
	for _, unicode := range []bool{true, false} {
		c := component.NewCtx(term.Fixed(80, colorprofile.TrueColor, unicode, true))
		g := c.Theme.Glyphs
		for _, s := range glyphSets() {
			var rows []string
			for _, r := range glyphRows {
				rows = append(rows, s.row(c, r))
			}
			drawn := ansi.Strip(apart(riskRow(c), strings.Join(rows, "\n")))
			want := 1
			if s.name == "current" {
				want = 2
			}
			if n := strings.Count(drawn, g.Risk); n != want {
				t.Errorf("%s, unicode %t: the risk icon %s %d times, want %d:\n%s", s.name, unicode, g.Risk, n, want, drawn)
			}
			if s.name == "current" {
				continue
			}
			var seen []string
			for _, m := range []stateMark{s.partial, s.unknown, s.na} {
				glyph := m.glyph(c)
				switch {
				case glyph == g.Risk || glyph == g.Configured:
					t.Errorf("%s, unicode %t: a mark is %s", s.name, unicode, glyph)
				case glyph == "" || glyph == g.NotConfigured:
				case slices.Contains(seen, glyph):
					t.Errorf("%s, unicode %t: %s marks two states", s.name, unicode, glyph)
				default:
					seen = append(seen, glyph)
				}
			}
		}
	}
}

// TestGlyphsCells checks that every candidate fits the marker column as
// the family does: one cell as x/ansi measures it (lipgloss.Width), the
// grid's measure, so no glyph with emoji presentation; and each ASCII form
// the column's three cells, or no marker at all in either glyph set.
func TestGlyphsCells(t *testing.T) {
	var glyphs []string
	for _, s := range glyphSets() {
		for _, m := range []stateMark{s.partial, s.unknown, s.na} {
			if m.same != nil {
				continue
			}
			if (m.unicode == "") != (m.ascii == "") {
				t.Errorf("%s: %q and %q, want a marker in both glyph sets or in neither", s.name, m.unicode, m.ascii)
			}
			if m.ascii != "" && ansi.StringWidth(m.ascii) != theme.MarkerWidth {
				t.Errorf("%s: %q is %d cells, want %d", s.name, m.ascii, ansi.StringWidth(m.ascii), theme.MarkerWidth)
			}
			if m.unicode != "" {
				glyphs = append(glyphs, m.unicode)
			}
		}
	}
	for _, grp := range glyphStrip {
		for _, s := range grp.glyphs {
			glyphs = append(glyphs, s.glyph)
		}
	}
	for _, g := range glyphs {
		if n := len([]rune(g)); n != 1 || ansi.StringWidth(g) != 1 {
			t.Errorf("%q: %d code points, %d cells; want one of each", g, n, ansi.StringWidth(g))
		}
	}
	if got := codePoints("⬙?"); got != "U+2B19 U+003F" {
		t.Errorf("codePoints: %q", got)
	}
}

// widestSetLine is the widest line of a set, in cells: SSH Auth's "Not
// Configured password auth on" from the value column.
var widestSetLine = theme.ValueCol + len("Not Configured password auth on")

// TestGlyphsSideBySide checks the sets' layout in plain mode: as many side
// by side as the width holds, widestSetLine cells each and compareGap
// apart, every row of one beside the same row of the others, no line over
// the width; one above the next below two sets' width, and below
// theme.StackBelow, where the rows stack.
func TestGlyphsSideBySide(t *testing.T) {
	step := widestSetLine + compareGap
	for _, tt := range []struct{ width, per int }{
		{63, 1}, {2*widestSetLine + compareGap - 1, 1}, {2*widestSetLine + compareGap, 2},
		{3*widestSetLine + 2*compareGap - 1, 2}, {3*widestSetLine + 2*compareGap, 3}, {5*widestSetLine + 4*compareGap, 5},
	} {
		code, out, errs := runGallery("glyphs", "-profile", "plain", "-width", fmt.Sprint(tt.width))
		if code != exitOK || errs != "" {
			t.Fatalf("width %d: exit %d, stderr %q", tt.width, code, errs)
		}
		uitest.AssertFits(t, out, tt.width)
		lines := strings.Split(out, "\n")
		// The first row of sets: each set's header at k*step, and each of
		// its rows at the first set's column plus k*step, on one line.
		sets := glyphSets()
		head := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "# "+sets[0].title+" ") })
		if head < 0 {
			t.Fatalf("width %d: no header for %s:\n%s", tt.width, sets[0].title, out)
		}
		for k := range tt.per {
			if !strings.HasPrefix(from(lines[head], k*step), "# "+sets[k].title+" ") {
				t.Errorf("width %d: %s's header is not at column %d: %q", tt.width, sets[k].title, k*step, lines[head])
			}
		}
		for _, label := range []string{"Risk Level", "User", "SSH Login", "SSH Port", "AppArmor", "Auto Updates"} {
			i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, label) })
			if i < 0 {
				t.Errorf("width %d: no line holds %q", tt.width, label)
				continue
			}
			col := strings.Index(lines[i], label)
			if n := strings.Count(lines[i], label); n != tt.per {
				t.Errorf("width %d: %q %d times on its first line, want %d: %q", tt.width, label, n, tt.per, lines[i])
			}
			for k := range tt.per {
				if !strings.HasPrefix(from(lines[i], col+k*step), label) {
					t.Errorf("width %d: set %d's %q is not at column %d: %q", tt.width, k+1, label, col+k*step, lines[i])
				}
			}
		}
	}
}

// TestGlyphsSmoke runs glyphs for every profile, both themes and both glyph
// sets, at widths from one that cuts the rows to one that holds every set
// side by side: no line wider than the terminal, the escape sequences the
// profile allows, and in plain mode or ASCII glyphs nothing outside ASCII.
func TestGlyphsSmoke(t *testing.T) {
	for _, p := range profiles {
		for _, th := range []string{"dark", "light"} {
			for _, ascii := range []bool{false, true} {
				for _, w := range []int{20, 60, 80, 200, 320} {
					args := []string{"glyphs", "-profile", p.name, "-theme", th, fmt.Sprintf("-ascii=%t", ascii), fmt.Sprintf("-width=%d", w)}
					code, out, errs := runGallery(args...)
					where := strings.Join(args, " ")
					if code != exitOK || errs != "" {
						t.Fatalf("%s: exit %d, stderr %q", where, code, errs)
					}
					uitest.AssertFits(t, out, w)
					plain := ansi.Strip(out)
					for _, s := range glyphSets() {
						if !strings.Contains(plain, " "+s.title+" ") {
							t.Errorf("%s: no set %s", where, s.title)
						}
					}
					if p.name == "plain" && strings.Contains(out, "\x1b") {
						t.Errorf("%s: an escape sequence in plain mode", where)
					}
					if (p.name == "plain" || ascii) && strings.ContainsFunc(plain, func(r rune) bool { return r > 0x7e }) {
						t.Errorf("%s: a glyph outside ASCII", where)
					}
				}
			}
		}
	}
}

// TestGlyphsUsage checks that a wrong flag or argument stops glyphs with
// exit status 2, a message and no output; that -list names what it would
// render; and that -h prints the usage line.
func TestGlyphsUsage(t *testing.T) {
	for _, args := range [][]string{
		{"nope"},
		{"current", "-profile", "plain"},
		{"-width", "0"},
		{"-height", "0"},
		{"-profile", "ansi"},
		{"-theme", "auto"},
		{"-marker", "status"},
	} {
		code, out, errs := runGallery(append([]string{"glyphs"}, args...)...)
		if code != exitUsage || out != "" || errs == "" {
			t.Errorf("glyphs %v: exit %d, stdout %q, stderr %q; want exit %d, a message and no output", args, code, out, errs, exitUsage)
		}
	}
	every := strings.Join(glyphNames(), "\n") + "\n"
	for _, tt := range []struct {
		args []string
		want string
	}{
		{nil, every},
		{[]string{"all"}, every},
		{[]string{"strip", "square", "current"}, "current\nsquare\nstrip\n"},
		{[]string{"figma"}, "figma\n"},
	} {
		if code, out, errs := runGallery(append([]string{"glyphs", "-list"}, tt.args...)...); code != exitOK || out != tt.want || errs != "" {
			t.Errorf("glyphs -list %v: exit %d, stdout %q, stderr %q; want %q", tt.args, code, out, errs, tt.want)
		}
	}
	if code, out, errs := runGallery("glyphs", "-h"); code != exitOK || out != "" || !strings.Contains(errs, glyphsUsage) {
		t.Errorf("glyphs -h: exit %d, stdout %q, stderr %q; want the usage line", code, out, errs)
	}
	if code, out, _ := runGallery("glyphs", "-profile", "plain", "strip"); code != exitOK || strings.Contains(out, "# Current ") || !strings.Contains(out, "# glyphs strip ") {
		t.Errorf("glyphs strip: exit %d, want the strip alone:\n%s", code, out)
	}
}
