package main

import (
	"fmt"
	"regexp"
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

// The comparison mode (P-42): the current pattern, rendered by the real
// components, beside a proposed one the gallery draws itself.

// markers are -marker's values.
var markers = []string{markerBullet, markerStatus}

// mocks are every pair of -marker's and -column's values.
func mocks() []mock {
	var out []mock
	for _, m := range markers {
		for _, col := range []string{columnAligned, columnFigma} {
			out = append(out, mock{marker: m, column: col})
		}
	}
	return out
}

// TestCompareGolden renders each section with each -marker value and the
// default -column, aligned, on the core set, and pins what compare
// prints: the note, the section's header, and its current and proposed
// renders, one above the other at 80 and 60 columns. The golden files are
// testdata/golden/compare/<marker>/<section>.golden. -column figma's rows
// are the stacked ones the 60-column files pin (TestCompareFigmaColumn).
func TestCompareGolden(t *testing.T) {
	for _, m := range markers {
		for _, s := range compareSections() {
			uitest.Golden(t, "compare/"+m+"/"+s.name, uitest.Core(), func(env term.Env) string {
				args := append(append([]string{"compare"}, lookArgs(t, env)...), "-marker", m, s.name)
				code, out, errs := runGallery(args...)
				if code != exitOK || errs != "" {
					t.Errorf("%v: exit %d, stderr %q; want exit 0", args, code, errs)
				}
				return out
			})
		}
	}
}

// plainCtx is the render context of plain mode at width w.
func plainCtx(w int) component.Ctx {
	return component.NewCtx(term.Fixed(w, colorprofile.NoTTY, false, true))
}

// stateWords matches a step's state word.
var stateWords = regexp.MustCompile(`Pending|Running|Done|Failed|Rolled back|Skipped`)

// rowOf returns the cell columns of title and of the state word after it
// on line, stripped of escape sequences, and false for a line that is not
// a two-column row of title.
func rowOf(line, title string) (titleCol, stateCol int, ok bool) {
	i := strings.Index(line, title)
	if i < 0 {
		return 0, 0, false
	}
	rest := line[i+len(title):]
	m := stateWords.FindStringIndex(rest)
	if m == nil || strings.TrimSpace(rest[:m[0]]) != "" {
		return 0, 0, false
	}
	return ansi.StringWidth(line[:i]), ansi.StringWidth(line[:i+len(title)+m[0]]), true
}

// TestCompareRows checks the proposed rows' grid in their two-column form,
// at 80 columns in plain mode and in Unicode glyphs, with each marker and
// the aligned status column: every title at theme.LabelCol, and every
// state word of a section, the failure's rolled-back rows with its steps,
// in one column, at theme.ValueCol or after it.
func TestCompareRows(t *testing.T) {
	for _, c := range []component.Ctx{plainCtx(80), component.NewCtx(term.Fixed(80, colorprofile.TrueColor, true, true))} {
		for _, m := range markers {
			for _, s := range compareSections() {
				want, ok := stepRows[s.name]
				if !ok {
					continue
				}
				var cols []int
				for _, line := range strings.Split(ansi.Strip(apart(s.proposed(c, mock{marker: m, column: columnAligned})...)), "\n") {
					for _, title := range cmpTitles {
						tc, sc, ok := rowOf(line, title)
						if !ok {
							continue
						}
						if tc != theme.LabelCol {
							t.Errorf("%s, %s: title at column %d, want %d: %q", s.name, m, tc, theme.LabelCol, line)
						}
						cols = append(cols, sc)
					}
				}
				if len(cols) != want {
					t.Errorf("%s, %s: %d rows, want %d", s.name, m, len(cols), want)
				}
				if distinct := slices.Compact(slices.Sorted(slices.Values(cols))); len(distinct) != 1 || distinct[0] < theme.ValueCol {
					t.Errorf("%s, %s: state words at columns %v; want one column, from %d", s.name, m, cols, theme.ValueCol)
				}
			}
		}
	}
}

// checkStacked checks that every step row of the proposed render out is
// in its stacked form (DEV-021): each title from theme.LabelCol alone on
// its line, its state word on the next line at theme.StackCol; and that
// it holds want such rows. Columns are cells of out stripped of its
// escape sequences, so that a style or a wide glyph moves no column.
func checkStacked(t *testing.T, name, out string, want int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(out), "\n")
	stacked := 0
	for i, line := range lines {
		for _, title := range cmpTitles {
			j := strings.Index(line, title)
			if j < 0 || ansi.StringWidth(line[:j]) != theme.LabelCol {
				continue
			}
			if _, _, ok := rowOf(line, title); ok || !strings.HasSuffix(line, title) {
				t.Errorf("%s: %q is not a title alone on its line", name, line)
				continue
			}
			next := ""
			if i+1 < len(lines) {
				next = lines[i+1]
			}
			loc := stateWords.FindStringIndex(next)
			if loc == nil || strings.TrimSpace(next[:loc[0]]) != "" || ansi.StringWidth(next[:loc[0]]) != theme.StackCol {
				t.Errorf("%s: under %q comes %q, want its state word at column %d", name, line, next, theme.StackCol)
			}
			stacked++
		}
	}
	if stacked != want {
		t.Errorf("%s: %d stacked rows, want %d", name, stacked, want)
	}
}

// stepRows are the step rows of each section that has them: the failure's
// rolled-back rows with its steps.
var stepRows = map[string]int{"steps": 5, "failure": 7, "success": 5}

// TestCompareStacks checks that below theme.StackBelow the proposed rows
// stack (DEV-021), with each marker and either status column: at 60
// columns, each title alone on its line, its state word on the next at
// theme.StackCol; in plain mode and in Unicode glyphs.
func TestCompareStacks(t *testing.T) {
	for _, c := range []component.Ctx{plainCtx(60), component.NewCtx(term.Fixed(60, colorprofile.TrueColor, true, true))} {
		for _, m := range mocks() {
			for _, s := range compareSections()[:3] {
				name := fmt.Sprintf("%s, %+v, unicode %t", s.name, m, c.Env.Unicode)
				checkStacked(t, name, apart(s.proposed(c, m)...), stepRows[s.name])
			}
		}
	}
}

// TestCompareFigmaColumn checks -column figma (P-42's choice b2): the
// status column is theme.ValueCol, so the 2230 change's titles, which
// reach it, stack their rows at every width, 64, 80 and 140 as at 60; a
// list whose titles end before it keeps two columns from
// theme.StackBelow, its state words at theme.ValueCol; and a title of 20
// cells ends before it, one of 21 reaches it, as a key reaches KeyValue's
// value column.
func TestCompareFigmaColumn(t *testing.T) {
	for _, w := range []int{64, 80, 140} {
		for _, m := range markers {
			for _, s := range compareSections()[:3] {
				checkStacked(t, fmt.Sprintf("%s, %s, figma, %d", s.name, m, w), apart(s.proposed(plainCtx(w), mock{marker: m, column: columnFigma})...), stepRows[s.name])
			}
		}
	}
	short := []component.Step{{Title: "ufw: allow 2230/tcp", State: component.StepDone}, {Title: "Drop SSH port 22", State: component.StepRolledBack}}
	for _, w := range []int{64, 80} {
		lines := strings.Split(ansi.Strip(newStatusRows(plainCtx(w), mock{marker: markerBullet, column: columnFigma}, short).render(short)), "\n")
		if len(lines) != len(short) {
			t.Errorf("width %d: %d lines for %d short titles, want a row each: %q", w, len(lines), len(short), lines)
			continue
		}
		for i, l := range lines {
			if tc, sc, ok := rowOf(l, short[i].Title); !ok || tc != theme.LabelCol || sc != theme.ValueCol {
				t.Errorf("width %d: %q, want the title at %d and its state at %d", w, l, theme.LabelCol, theme.ValueCol)
			}
		}
	}
	for n, want := range map[int]bool{theme.ValueCol - theme.LabelCol - 1: false, theme.ValueCol - theme.LabelCol: true} {
		steps := []component.Step{{Title: strings.Repeat("x", n), State: component.StepDone}}
		if got := newStatusRows(plainCtx(80), mock{marker: markerBullet, column: columnFigma}, steps).stacked; got != want {
			t.Errorf("a title of %d cells: stacked %t, want %t", n, got, want)
		}
	}
}

// TestCompareGrammar checks the proposed side's grammar in every section,
// with each marker and status column, in plain mode and in Unicode
// glyphs, at 80 and 60 columns: no state or message word is followed by a
// colon; none of origin/main's message glyphs, ✓ ✗ ℹ, appears, nor the
// ASCII message markers [!] and [i]; and [+], which is also the ASCII
// Configured marker, appears only on the rows of -marker status.
func TestCompareGrammar(t *testing.T) {
	colon := regexp.MustCompile(`(Pending|Running|Done|Failed|Rolled back|Skipped|Success|Info|Warning|Error|Cancelled|Urgent):`)
	for _, w := range []int{80, 60} {
		for _, c := range []component.Ctx{plainCtx(w), component.NewCtx(term.Fixed(w, colorprofile.TrueColor, true, true))} {
			for _, mk := range mocks() {
				m := mk.marker
				for _, s := range compareSections() {
					out := ansi.Strip(apart(s.proposed(c, mk)...))
					name := fmt.Sprintf("%s, %+v, %d, unicode %t", s.name, mk, w, c.Env.Unicode)
					if loc := colon.FindStringIndex(out); loc != nil {
						t.Errorf("%s: %q is followed by a colon", name, out[loc[0]:loc[1]])
					}
					for _, g := range []string{"✓", "✗", "ℹ", "[!]", "[i]"} {
						if strings.Contains(out, g) {
							t.Errorf("%s: holds %s", name, g)
						}
					}
					if strings.Contains(out, "[+]") && (m != markerStatus || s.name == "messages" || s.name == "countdown") {
						t.Errorf("%s: holds [+]", name)
					}
				}
			}
		}
	}
}

// TestCompareCountdownHeight checks that the proposed countdown's waiting
// and urgent blocks have the same height, from 99:59 to 0:00, without a
// port and with one, at every width from 14 to 140, in both glyph sets.
func TestCompareCountdownHeight(t *testing.T) {
	for w := 14; w <= 140; w++ {
		for _, unicode := range []bool{true, false} {
			c := component.NewCtx(term.Fixed(w, colorprofile.TrueColor, unicode, true))
			for _, port := range []int{0, cmpPort} {
				want := strings.Count(countdownCallout(c, component.ConfirmCountdown{Remaining: 165, Port: port}), "\n")
				for _, left := range []int{5999, 600, 181, component.UrgentBelow, component.UrgentBelow - 1, 5, 0} {
					if got := strings.Count(countdownCallout(c, component.ConfirmCountdown{Remaining: left, Port: port}), "\n"); got != want {
						t.Errorf("width %d, unicode %t, port %d: %d seconds take %d lines, 2:45 %d", w, unicode, port, left, got+1, want+1)
					}
				}
			}
		}
	}
}

// TestCompareWords checks that the words the proposed side copies, which
// the components hold unexported, are still what the current side says:
// the countdown's sentences and command, "Urgent", and the log sentence;
// and that the failing command and its output are the ErrorReport's on
// both sides.
func TestCompareWords(t *testing.T) {
	c := plainCtx(80)
	current := apart(currentCountdown(c)...) + apart(currentFailure(c)...)
	for _, s := range []string{cmpReverts, cmpKeep, fmt.Sprintf(cmpKeepOn, cmpPort), cmpConfirm, cmpUrgent + ":", fmt.Sprintf(cmpCheckLog, cmpLogPath)} {
		if !strings.Contains(current, s) {
			t.Errorf("the current side does not say %q", s)
		}
	}
	block := commandBlock(c, cmpCommand, cmpOutput)
	if !strings.Contains(block, cmpCommand) || !strings.Contains(apart(currentFailure(c)...), block) || !strings.Contains(apart(proposedFailure(c, mock{marker: markerBullet, column: columnAligned})...), block) {
		t.Errorf("the command's block %q is not the same on both sides", block)
	}
}

// TestCompareProse checks R5's wrap: prose breaks at spaces alone, so that
// the change's id, which holds a hyphen, stays whole on its line at every
// width that holds it, from theme.Indent, no line over the width; and a
// word wider than the line fills it line by line.
func TestCompareProse(t *testing.T) {
	const id = "20261020T120000Z-xxxxxxxx"
	for w := theme.Indent + len(id); w <= 120; w++ {
		out := ansi.Strip(prose(plainCtx(w), cmpUntil))
		uitest.AssertFits(t, out, w)
		if strings.Count(out, id) != strings.Count(cmpUntil, id) {
			t.Errorf("width %d: the id %s is broken:\n%s", w, id, out)
		}
		for _, l := range strings.Split(out, "\n") {
			if len(l) <= theme.Indent || !strings.HasPrefix(l, strings.Repeat(" ", theme.Indent)) || l[theme.Indent] == ' ' {
				t.Errorf("width %d: %q does not start at column %d", w, l, theme.Indent)
			}
		}
	}
	if got, want := wrapText("a "+id, 10), []string{"a", "20261020T1", "20000Z-xxx", "xxxxx"}; !slices.Equal(got, want) {
		t.Errorf("wrapText of a word wider than the line: %q, want %q", got, want)
	}
}

// TestCompareCommandLine checks that the proposed countdown's command,
// broken past the width, goes on after Glyphs.Wrap and a space, as the
// components continue a line of code.
func TestCompareCommandLine(t *testing.T) {
	for _, c := range []component.Ctx{plainCtx(22), component.NewCtx(term.Fixed(22, colorprofile.TrueColor, true, true))} {
		lines := strings.Split(ansi.Strip(strings.Join(commandLine(c, cmpConfirm), "\n")), "\n")
		lead := strings.Repeat(" ", theme.LabelCol) + c.Theme.Glyphs.Wrap + " "
		if len(lines) < 2 || !strings.HasPrefix(lines[1], lead) || strings.HasPrefix(lines[1], lead+" ") {
			t.Errorf("unicode %t: %q, want the second line to start %q", c.Env.Unicode, lines, lead)
		}
		uitest.AssertFits(t, strings.Join(lines, "\n"), 22)
	}
}

// TestComparePairs checks that in every section, with each marker and
// status column, the current and the proposed render have as many blocks
// at theme.ContentWidth, the width each side takes side by side, so that
// renderPair sets block beside block rather than each side whole.
func TestComparePairs(t *testing.T) {
	for _, c := range []component.Ctx{plainCtx(theme.ContentWidth), component.NewCtx(term.Fixed(theme.ContentWidth, colorprofile.TrueColor, true, true))} {
		for _, m := range mocks() {
			for _, s := range compareSections() {
				if cur, pro := len(s.current(c)), len(s.proposed(c, m)); cur != pro {
					t.Errorf("%s, %+v, unicode %t: %d blocks current, %d proposed", s.name, m, c.Env.Unicode, cur, pro)
				}
			}
		}
	}
}

// from returns plain line l from column col, "" when l ends before it.
func from(l string, col int) string {
	if len(l) < col {
		return ""
	}
	return l[col:]
}

// TestCompareSideBySide checks the two forms in plain mode: from 132
// columns the two renders are side by side, the proposed one from column
// 68, no line over the width, each message beside its callout, and a
// later block beside its counterpart, the failure's "Operation Failed"
// and the success's "Operation Successful" beside their own; at 131
// columns one is above the other.
func TestCompareSideBySide(t *testing.T) {
	for _, tt := range []struct{ section, title string }{{"failure", "# Operation Failed"}, {"success", "# Operation Successful"}} {
		for _, m := range mocks() {
			_, out, _ := runGallery("compare", "-profile", "plain", "-width", "132", "-marker", m.marker, "-column", m.column, tt.section)
			lines := strings.Split(out, "\n")
			if i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, tt.title) }); i < 0 || !strings.HasPrefix(from(lines[i], theme.ContentWidth+compareGap), tt.title) {
				t.Errorf("%s, %+v at 132 columns: %q is not beside its own:\n%s", tt.section, m, tt.title, out)
			}
		}
	}
	right := theme.ContentWidth + compareGap
	for _, s := range compareSections() {
		code, out, errs := runGallery("compare", "-profile", "plain", "-width", "132", s.name)
		if code != exitOK || errs != "" {
			t.Fatalf("%s: exit %d, stderr %q", s.name, code, errs)
		}
		uitest.AssertFits(t, out, 132)
		lines := strings.Split(out, "\n")
		if i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "# Current ") }); i < 0 || !strings.HasPrefix(from(lines[i], right), "# Proposed ") {
			t.Errorf("%s at 132 columns: no line holds both labels, Proposed at column %d:\n%s", s.name, right, out)
		}
		code, out, errs = runGallery("compare", "-profile", "plain", "-width", "131", s.name)
		lines = strings.Split(out, "\n")
		if code != exitOK || errs != "" || !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "# Proposed ") }) {
			t.Errorf("%s at 131 columns: exit %d, stderr %q; want Proposed under Current:\n%s", s.name, code, errs, out)
		}
	}
	_, out, _ := runGallery("compare", "-profile", "plain", "-width", "132", "messages")
	lines := strings.Split(out, "\n")
	for _, m := range cmpMessages[1:5] {
		i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, ": "+m.Text) })
		if i < 0 || i+1 >= len(lines) || !strings.HasPrefix(from(lines[i], right), "# ") || from(lines[i+1], right) != "# "+m.Text {
			t.Errorf("the %s message is not beside its callout:\n%s", m.Kind, out)
		}
	}
}

// TestCompareUsage checks that a wrong flag or argument stops compare with
// exit status 2, a message and no output, and that -list names the
// sections it would render.
func TestCompareUsage(t *testing.T) {
	for _, args := range [][]string{
		{"-marker", "glyph"},
		{"-column", "wide"},
		{"nope"},
		{"steps", "-profile", "plain"},
		{"-width", "0"},
		{"-profile", "ansi"},
		{"-nope"},
	} {
		code, out, errs := runGallery(append([]string{"compare"}, args...)...)
		if code != exitUsage || out != "" || errs == "" {
			t.Errorf("compare %v: exit %d, stdout %q, stderr %q; want exit %d, a message and no output", args, code, out, errs, exitUsage)
		}
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{nil, "steps\nfailure\nsuccess\nmessages\ncountdown\n"},
		{[]string{"countdown", "steps"}, "steps\ncountdown\n"},
		{[]string{"steps", "all"}, "steps\nfailure\nsuccess\nmessages\ncountdown\n"},
	} {
		if code, out, errs := runGallery(append([]string{"compare", "-list"}, tt.args...)...); code != exitOK || out != tt.want || errs != "" {
			t.Errorf("compare -list %v: exit %d, stdout %q, stderr %q; want %q", tt.args, code, out, errs, tt.want)
		}
	}
}
