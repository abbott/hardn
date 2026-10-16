// Package uitest is the golden-file harness for pkg/ui: the render
// matrices (Full and Core), Golden, which renders one case in every cell of
// a matrix and compares the result with one golden file, and AssertFits.
//
// It is test support: import it only from _test.go files. Production code
// must not import it, because it imports testing and, at init, defines an
// -update flag on the process's command line (flag.CommandLine).
//
// # Golden files
//
// Golden(t, name, cells, render) keeps testdata/golden/<name>.golden in the
// calling test's package directory. After a short header, the file holds
// one section per cell, in the order of cells:
//
//	=== truecolor-dark-unicode-80: width 80, height 24, profile truecolor, dark, glyphs unicode
//	  ␛[38;2;192;196;216mtext.primary␛[m$
//
// The words after the cell's name describe its whole Env: the size,
// profile, background and glyph set, plain mode when on, and any other
// field that differs from what term.Fixed would set, such as "input is not
// a tty". A name therefore cannot misdescribe its cell.
//
// A section holds what the terminal would receive: render's string written
// through the cell's Env.Writer, which downsamples it to the cell's fixed
// profile, as production output will be. The notation keeps the files
// reviewable as plain text, with no decoding:
//   - every control character except newline is shown as its Unicode
//     Control Picture, U+2400 plus its code (DEL is ␡), so ESC is ␛ (U+241B);
//   - every line ends with $, so trailing spaces stay visible, and output
//     that ends with a newline ends with a line holding only $.
//
// Golden fails on output this notation could not show unambiguously:
// invalid UTF-8, C1 control characters, and the Control Pictures it uses
// (U+2400 to U+2421). On a mismatch it names the cells that changed and
// shows a line diff of each, plus any cells added or removed. The diff
// numbers lines as in the golden file, and as in the file -update would
// write.
//
// # Updating
//
// After an intended change, regenerate the files and give the reason in the
// review description:
//
//	make golden-update               # every package under pkg/ui that uses uitest
//	go test ./pkg/ui/theme -update   # one package
//
// The -update flag is defined here, once. A package that uses uitest must
// not define its own -update: the flag package panics when a name is
// defined twice ("flag redefined", flag/flag.go:1020-1029 in Go 1.27).
// pkg/characterization and pkg/menu define their own, which is fine while
// they do not import uitest, because each package's tests build a separate
// binary. A test binary that does not link uitest rejects -update as an
// unknown flag, so make golden-update passes it only to the packages whose
// test binaries link uitest, directly or through another package.
//
// # Determinism
//
// Golden reads neither the terminal, the environment nor the clock: the
// output depends only on each cell's Env and on render. Two guards keep it
// that way:
//   - render runs twice per cell, and different results fail the test, as
//     from a renderer that ranges over a map or reads the clock;
//   - Golden stops when x/ansi measures East Asian ambiguous-width
//     characters as two cells, which RUNEWIDTH_EASTASIAN=1 sets at its init
//     (method.go:20-25 in x/ansi v0.11.8), because lipgloss.Width, and so
//     every layout and AssertFits, would then measure differently.
package uitest

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/term"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden from the render instead of comparing (pkg/ui/uitest)")

// Cell is one render environment of a matrix, with the name that labels its
// section in a golden file.
type Cell struct {
	// Name is unique within a matrix and uses only a-z, 0-9, '-' and '.'.
	Name string
	Env  term.Env
}

// colour is one value of Full's colour axis.
type colour struct {
	name    string
	profile colorprofile.Profile
}

// The axes of Full, in order. "none" is colorprofile.ASCII: a terminal with
// no colour that keeps bold and underline.
var (
	colours = []colour{
		{"truecolor", colorprofile.TrueColor},
		{"256", colorprofile.ANSI256},
		{"16", colorprofile.ANSI},
		{"none", colorprofile.ASCII},
	}
	widths = []int{60, 80, 120}
)

// Full returns the component matrix of brief Phase 6, 48 cells: colour
// {truecolor, 256, 16, none} × background {dark, light} × glyphs {unicode,
// ascii} × width {60, 80, 120}. Cells are named after their values in that
// order, as "truecolor-dark-unicode-80", and the first axis varies slowest.
// "none" is colorprofile.ASCII: no colour, but bold and underline are kept.
// Plain mode is in Core.
func Full() []Cell {
	var cells []Cell
	for _, c := range colours {
		for _, dark := range []bool{true, false} {
			for _, unicode := range []bool{true, false} {
				for _, w := range widths {
					cells = append(cells, newCell(c, dark, unicode, w))
				}
			}
		}
	}
	return cells
}

// Core returns the core set of brief Phase 6, for screens and workflow
// steps:
//   - truecolor-dark-unicode-80, the Figma target;
//   - plain-80: plain mode at 80 columns, as for piped output: no escape
//     sequences at all (colorprofile.NoTTY) and ASCII glyphs;
//   - truecolor-dark-unicode-60, the narrow width.
func Core() []Cell {
	return []Cell{
		newCell(colours[0], true, true, 80),
		plainCell(80),
		newCell(colours[0], true, true, 60),
	}
}

func newCell(c colour, dark, unicode bool, width int) Cell {
	bg, glyphs := axisNames(dark, unicode)
	return Cell{
		Name: fmt.Sprintf("%s-%s-%s-%d", c.name, bg, glyphs, width),
		Env:  term.Fixed(width, c.profile, unicode, dark),
	}
}

func plainCell(width int) Cell {
	return Cell{Name: fmt.Sprintf("plain-%d", width), Env: term.Fixed(width, colorprofile.NoTTY, false, true)}
}

// axisNames returns the words for a background and a glyph set used in
// cell names and section headers.
func axisNames(dark, unicode bool) (bg, glyphs string) {
	bg, glyphs = "light", "ascii"
	if dark {
		bg = "dark"
	}
	if unicode {
		glyphs = "unicode"
	}
	return bg, glyphs
}

// Golden renders each cell with render, checks that every line fits the
// cell's width (AssertFits), and compares the result with
// testdata/golden/<name>.golden, or rewrites that file under -update. See
// the package doc for the file's notation and the determinism guards.
func Golden(t *testing.T, name string, cells []Cell, render func(term.Env) string) {
	t.Helper()
	if !filepath.IsLocal(filepath.FromSlash(name)) {
		t.Fatalf("uitest: golden name %q is not a relative path", name)
	}
	if err := checkCells(cells); err != nil {
		t.Fatalf("uitest: %v", err)
	}
	if w := ansi.StringWidth("─"); w != 1 {
		t.Fatalf("uitest: x/ansi measures the ambiguous-width ─ as %d cells, and the goldens assume 1; unset RUNEWIDTH_EASTASIAN", w)
	}
	sections := make([]section, 0, len(cells))
	for _, c := range cells {
		out, err := written(c.Env, render(c.Env))
		if err != nil {
			t.Fatalf("uitest: cell %s: %v", c.Name, err)
		}
		if again, err := written(c.Env, render(c.Env)); err != nil || again != out {
			t.Errorf("uitest: cell %s: a second render differs; render must depend only on the Env", c.Name)
		}
		if err := representable(out); err != nil {
			t.Fatalf("uitest: cell %s: %v", c.Name, err)
		}
		for _, msg := range overflows(out, c.Env.Width) {
			t.Errorf("uitest: cell %s: %s", c.Name, msg)
		}
		sections = append(sections, section{name: c.Name, header: header(c), lines: notate(out)})
	}
	got := format(t.Name(), sections)
	path := filepath.Join("testdata", "golden", filepath.FromSlash(name)+".golden")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("uitest: %v (run make golden-update to create it)", err)
	}
	if report := compare(string(want), got); report != "" {
		t.Errorf("uitest: %s does not match the render.\n%s\nIf the change is intended, run make golden-update and give the reason in the review description.", path, report)
	}
}

// AssertFits fails t for every line of out wider than width cells, as
// lipgloss.Width measures it: escape sequences take no cells. Golden checks
// every cell this way at the cell's Env.Width.
//
// A line that holds a tab fails too, whatever its width: lipgloss.Width
// counts a tab, like every control character, as no cells (ansi.StringWidth,
// width.go:79-119 in x/ansi v0.11.8), but a terminal moves the cursor to the
// next tab stop. The other control characters move the cursor back or down,
// so they can make a line measure wider than it shows, never narrower.
func AssertFits(t *testing.T, out string, width int) {
	t.Helper()
	for _, msg := range overflows(out, width) {
		t.Error(msg)
	}
}

// overflows describes each line of out wider than width cells, and each
// line whose width it cannot know because it holds a tab outside an escape
// sequence.
func overflows(out string, width int) []string {
	var msgs []string
	for i, line := range strings.Split(out, "\n") {
		if strings.Contains(ansi.Strip(line), "\t") {
			msgs = append(msgs, fmt.Sprintf("line %d holds a tab, which lipgloss.Width counts as no cells; render spaces instead: %s", i+1, strings.Map(picture, line)))
		}
		if w := lipgloss.Width(line); w > width {
			msgs = append(msgs, fmt.Sprintf("line %d is %d cells wide, over the width of %d: %s", i+1, w, width, strings.Map(picture, line)))
		}
	}
	return msgs
}

func checkCells(cells []Cell) error {
	if len(cells) == 0 {
		return errors.New("no cells")
	}
	bad := func(r rune) bool { return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '.' }
	seen := map[string]bool{}
	for _, c := range cells {
		if c.Name == "" || strings.ContainsFunc(c.Name, bad) {
			return fmt.Errorf("cell name %q: use only a-z, 0-9, '-' and '.'", c.Name)
		}
		if seen[c.Name] {
			return fmt.Errorf("cell name %q is used twice", c.Name)
		}
		seen[c.Name] = true
	}
	return nil
}

// written returns s as env.Writer passes it on: downsampled to env.Profile.
// It writes s in one call, as Env.Writer requires.
func written(env term.Env, s string) (string, error) {
	var b strings.Builder
	_, err := io.WriteString(env.Writer(&b), s)
	return b.String(), err
}

// representable reports output that golden notation cannot show
// unambiguously.
func representable(s string) error {
	if !utf8.ValidString(s) {
		return errors.New("the output is not valid UTF-8")
	}
	for i, r := range s {
		if (r >= 0x80 && r <= 0x9f) || (r >= 0x2400 && r <= 0x2421) {
			return fmt.Errorf("the output holds %U at byte %d, which golden notation cannot show unambiguously", r, i)
		}
	}
	return nil
}

// picture maps a control character other than newline to its Unicode
// Control Picture.
func picture(r rune) rune {
	switch {
	case r < 0x20 && r != '\n':
		return 0x2400 + r
	case r == 0x7f:
		return 0x2421 // ␡
	}
	return r
}

// notate returns out in golden notation, one entry per line.
func notate(out string) []string {
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.Map(picture, l) + "$"
	}
	return lines
}

// header returns the "=== " line of c's section. It describes every field
// of c.Env, so two different Envs never share a description, whatever their
// cells are named. The size, profile, background and glyph set are always
// shown, and plain mode when it is on. The fields term.Fixed derives from
// those (TTY, InputTTY, Animate, Theme and Plain) are shown where they
// differ from what Fixed would set, as for a hand-built Env.
func header(c Cell) string {
	e := c.Env
	bg, glyphs := axisNames(e.Dark, e.Unicode)
	h := fmt.Sprintf("%s%s: width %d, height %d, profile %s, %s, glyphs %s",
		sectionPrefix, c.Name, e.Width, e.Height, profileName(e.Profile), bg, glyphs)
	fixed := term.Fixed(e.Width, e.Profile, e.Unicode, e.Dark)
	if e.Plain || fixed.Plain {
		h += ", " + choose(e.Plain, "plain", "not plain")
	}
	for _, f := range []struct {
		got, fixed bool
		on, off    string
	}{
		{e.TTY, fixed.TTY, "output is a tty", "output is not a tty"},
		{e.InputTTY, fixed.InputTTY, "input is a tty", "input is not a tty"},
		{e.Animate, fixed.Animate, "animated", "not animated"},
	} {
		if f.got != f.fixed {
			h += ", " + choose(f.got, f.on, f.off)
		}
	}
	if e.Theme != fixed.Theme {
		h += ", theme " + e.Theme.String()
	}
	return h
}

// choose returns on when b is true and off otherwise.
func choose(b bool, on, off string) string {
	if b {
		return on
	}
	return off
}

// profileName returns the word for p in section headers: Full's colour
// names, "notty" (no escape sequences at all) and "unknown". These words
// belong to uitest. Profile.String's belong to colorprofile, which has
// renamed its Ascii constant to ASCII while String still returns "Ascii"
// (profile.go:18-41 in v0.4.3); finishing that rename would otherwise
// rewrite every golden file.
func profileName(p colorprofile.Profile) string {
	for _, c := range colours {
		if c.profile == p {
			return c.name
		}
	}
	if p == colorprofile.NoTTY {
		return "notty"
	}
	return fmt.Sprintf("unknown (%d)", int(p))
}

// sectionPrefix starts a section's header line. Output lines always end
// with $ and header lines never do, so the two cannot be confused.
const sectionPrefix = "=== "

// section is one cell's part of a golden file.
type section struct {
	name   string
	header string   // the "=== " line
	lines  []string // the output, in golden notation
	first  int      // the line of lines[0] in the file, from 1; set by parse
}

// format returns the golden file for test's sections.
func format(test string, sections []section) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Golden file of %s, written by pkg/ui/uitest. Do not edit: regenerate with make golden-update.\n", test)
	b.WriteString("# One section per cell: \"=== <cell>: <env>\", then the output as written through term.Env.Writer.\n")
	b.WriteString("# Control characters are Unicode Control Pictures (ESC is ␛) and every line ends with $.\n")
	for _, s := range sections {
		b.WriteString("\n" + s.header + "\n")
		for _, l := range s.lines {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// parse splits a golden file into its header lines and sections. Blank
// lines between sections are dropped.
func parse(file string) (head []string, sections []section) {
	for i, l := range strings.Split(strings.TrimSuffix(file, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, sectionPrefix) && !strings.HasSuffix(l, "$"):
			name, _, _ := strings.Cut(strings.TrimPrefix(l, sectionPrefix), ":")
			// The header is line i+1; the section's output starts on the next.
			sections = append(sections, section{name: name, header: l, first: i + 2})
		case len(sections) == 0:
			head = append(head, l)
		case l != "":
			s := &sections[len(sections)-1]
			s.lines = append(s.lines, l)
		}
	}
	return head, sections
}

// compare returns "" when the golden file want equals got, and otherwise a
// report that names the cells that changed, with a line diff of each. The
// diff's line numbers are lines of the files: of want, the golden file, and
// of got, the file -update would write.
func compare(want, got string) string {
	if want == got {
		return ""
	}
	wantHead, wantSections := parse(want)
	gotHead, gotSections := parse(got)
	byName := map[string]section{}
	for _, s := range wantSections {
		byName[s.name] = s
	}
	var changed, added, diffs []string
	if !slices.Equal(wantHead, gotHead) {
		diffs = append(diffs, "file header (- golden, + render):\n"+lineDiff(wantHead, gotHead, 1, 1))
	}
	for _, g := range gotSections {
		w, ok := byName[g.name]
		delete(byName, g.name)
		switch {
		case !ok:
			added = append(added, g.name)
		case w.header != g.header || !slices.Equal(w.lines, g.lines):
			changed = append(changed, g.name)
			d := fmt.Sprintf("cell %s (- golden, + render):\n", g.name)
			if w.header != g.header {
				d += "- " + w.header + "\n+ " + g.header + "\n"
			}
			diffs = append(diffs, d+lineDiff(w.lines, g.lines, w.first, g.first))
		}
	}
	var removed []string
	for _, s := range wantSections {
		if _, ok := byName[s.name]; ok {
			removed = append(removed, s.name)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d cells differ", len(changed), len(gotSections))
	if len(changed) > 0 {
		b.WriteString(": " + strings.Join(changed, ", "))
	}
	b.WriteString("\n")
	if len(added) > 0 {
		b.WriteString("cells not in the golden file: " + strings.Join(added, ", ") + "\n")
	}
	if len(removed) > 0 {
		b.WriteString("cells in the golden file but not rendered: " + strings.Join(removed, ", ") + "\n")
	}
	if len(diffs) == 0 && len(added) == 0 && len(removed) == 0 {
		b.WriteString("the cells match, but their order or the blank lines between them differ\n")
	}
	for _, d := range diffs {
		b.WriteString("\n" + d)
	}
	return b.String()
}

// diffContext is the number of unchanged lines lineDiff shows around a
// change.
const diffContext = 2

// lineDiff returns a line diff of a and b from a longest common
// subsequence: "- " marks a line only in a, "+ " a line only in b, and "  "
// an unchanged line near a change. Each hunk starts with the numbers of its
// first line in a and in b, where a[0] is line aFirst and b[0] is line
// bFirst. Equal a and b give "".
func lineDiff(a, b []string, aFirst, bFirst int) string {
	// lcs[i][j] is the length of the longest common subsequence of a[i:]
	// and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type edit struct {
		mark, line string
		ai, bi     int // the index in a and in b where the edit applies
	}
	var edits []edit
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			edits = append(edits, edit{"  ", a[i], i, j})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			edits = append(edits, edit{"- ", a[i], i, j})
			i++
		default:
			edits = append(edits, edit{"+ ", b[j], i, j})
			j++
		}
	}
	nearChange := func(k int) bool {
		for _, e := range edits[max(0, k-diffContext):min(len(edits), k+diffContext+1)] {
			if e.mark != "  " {
				return true
			}
		}
		return false
	}
	var out strings.Builder
	last := -2
	for k, e := range edits {
		if !nearChange(k) {
			continue
		}
		if k != last+1 {
			fmt.Fprintf(&out, "@@ golden line %d, render line %d @@\n", aFirst+e.ai, bFirst+e.bi)
		}
		out.WriteString(e.mark + e.line + "\n")
		last = k
	}
	return out.String()
}
