package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The glyphs mode (refactor plan P4, next/p4-glyph-compare): the status
// rows' symbols, for the maintainer to judge in his own terminal and font.
// Figma draws three, each with its colour: ⛛ a measure in place
// (status.ok), ⭘ one off (status.bad), and ⛬ the overall risk level (by
// band). The states Figma draws no symbol for took ⛬ (Partial), ? (Unknown)
// and ∅ (Not Applicable) in DEV-012 to DEV-014 and DEV-024, so ⛬ marks
// Partial as well as the risk level; it also marks warnings, lockout risk
// and, as Partial, Reboot pending, which this mode does not draw. This
// mode shows today's set beside candidate sets, built from the measured
// shortlist, in which ⛬ marks the risk row alone, and a strip of each
// candidate beside ⛛ ⭘ ⛬ (docs/engine/gallery-glyphs.md).
//
// Like compare.go's mock it draws the rows itself, on the components'
// grid, styled through the theme's slots' Render methods (paint) and
// measured with x/ansi: no component or theme changed. Its current set
// draws what RiskSummary and StatusList draw, byte for byte
// (TestGlyphsCurrent), so that a set differs from it only by its glyphs and
// colours.

// glyphsUsage is the glyphs mode's usage line.
const glyphsUsage = "usage: go run ./internal/tools/uigallery glyphs [flags] [current | figma | diamond | diamond-dot | square | strip | all]..."

// stripName names the strip among the arguments.
const stripName = "strip"

// stateMark is how a set shows one of the states Figma draws no symbol
// for: the glyph in the marker column and the colour of the glyph and the
// word.
type stateMark struct {
	// unicode and ascii are the glyph in each glyph set; both empty for no
	// marker.
	unicode, ascii string
	// same, when set, takes the glyph from the theme in place of unicode
	// and ascii: today's glyph, or one of Figma's.
	same func(theme.Glyphs) string
	// paint colours the glyph and the state's word.
	paint func(*theme.Theme) paint
	// token is that colour's token, which the legend names.
	token string
	// quiet sets the row's label in text.muted too, the whole row muted.
	quiet bool
}

// glyph returns the mark's glyph in c's glyph set, "" for no marker.
func (m stateMark) glyph(c component.Ctx) string {
	switch {
	case m.same != nil:
		return m.same(c.Theme.Glyphs)
	case c.Env.Unicode:
		return m.unicode
	}
	return m.ascii
}

// glyphSet is one set of status symbols: Figma's ⛛ and ⭘ for Configured
// and Not Configured, the risk icon on the risk row, and its own marks for
// Partial, Unknown and Not Applicable.
type glyphSet struct {
	name  string // its argument
	title string // its header
	// partial, unknown and na are its marks for those states.
	partial, unknown, na stateMark
}

// The colour tokens the legends name.
const (
	tokenWarn    = "status.warn"
	tokenUnknown = "status.unknown"
	tokenMuted   = "text.muted"
)

// glyphSets returns the sets, in the order "all" shows them: today's, then
// the candidates, each with ⛬ on the risk row alone. trial is the
// maintainer's combination of 2026-10-21: Figma's ⭘ in status.warn for
// Partial, ? and – in text.muted for Unknown and Not Applicable; power is
// the same with ⏼ U+23FC (power on-off) in status.warn for Partial, which
// the maintainer asked to see beside it. figma folds the three
// states into Figma's own glyphs and colours, the reading in which no gap
// is real; diamond and diamond-dot are the measurements' sets A and B, a
// new glyph for each state; square takes the heaviest Partial and Unknown
// candidates and no marker for Not Applicable.
func glyphSets() []glyphSet {
	warn := func(t *theme.Theme) paint { return t.Status.Partial.Render }
	blue := func(t *theme.Theme) paint { return t.Status.Unknown.Render }
	grey := func(t *theme.Theme) paint { return t.Status.NotApplicable.Render }
	off := func(g theme.Glyphs) string { return g.NotConfigured }
	none := stateMark{paint: grey, token: tokenMuted, quiet: true}
	return []glyphSet{
		{
			name: "current", title: "Current",
			partial: stateMark{same: func(g theme.Glyphs) string { return g.Partial }, paint: warn, token: tokenWarn},
			unknown: stateMark{same: func(g theme.Glyphs) string { return g.Unknown }, paint: blue, token: tokenUnknown},
			na:      stateMark{same: func(g theme.Glyphs) string { return g.NotApplicable }, paint: grey, token: tokenMuted},
		},
		{
			name: "trial", title: "Trial",
			partial: stateMark{same: off, paint: warn, token: tokenWarn},
			unknown: stateMark{unicode: "?", ascii: "[?]", paint: grey, token: tokenMuted},
			na:      stateMark{unicode: "–", ascii: "[-]", paint: grey, token: tokenMuted},
		},
		{
			name: "power", title: "Power",
			partial: stateMark{unicode: "⏼", ascii: "[~]", paint: warn, token: tokenWarn},
			unknown: stateMark{unicode: "?", ascii: "[?]", paint: grey, token: tokenMuted},
			na:      stateMark{unicode: "–", ascii: "[-]", paint: grey, token: tokenMuted},
		},
		{
			name: "figma", title: "Figma's three",
			partial: stateMark{same: off, paint: warn, token: tokenWarn},
			unknown: stateMark{same: off, paint: grey, token: tokenMuted},
			na:      none,
		},
		{
			name: "diamond", title: "Half diamond",
			partial: stateMark{unicode: "⬙", ascii: "[~]", paint: warn, token: tokenWarn},
			unknown: stateMark{unicode: "⁇", ascii: "[?]", paint: blue, token: tokenUnknown},
			na:      stateMark{unicode: "–", ascii: "[-]", paint: grey, token: tokenMuted},
		},
		{
			name: "diamond-dot", title: "Diamond dot",
			partial: stateMark{unicode: "◈", ascii: "[~]", paint: warn, token: tokenWarn},
			unknown: stateMark{unicode: "?", ascii: "[?]", paint: blue, token: tokenUnknown},
			na:      stateMark{unicode: "⊟", ascii: "[-]", paint: grey, token: tokenMuted},
		},
		{
			name: "square", title: "Half square",
			partial: stateMark{unicode: "◧", ascii: "[~]", paint: warn, token: tokenWarn},
			unknown: stateMark{unicode: "⍰", ascii: "[?]", paint: blue, token: tokenUnknown},
			na:      none,
		},
	}
}

// glyphRows are the seven rows of 00-TUI-MAIN, in its order, in states that
// show all five, each detail a line the checks give: Partial, Not
// Configured and Unknown next to one another, so that a set which folds a
// state into ⭘ shows it beside a row that is off.
var glyphRows = []component.StatusRow{
	{Label: "User", State: component.StatusConfigured, Detail: "non-root, sudo"},
	{Label: "Firewall", State: component.StatusConfigured, Detail: "deny policy"},
	{Label: "SSH Login", State: component.StatusPartial, Detail: "root key-only"},
	{Label: "SSH Auth", State: component.StatusNotConfigured, Detail: "password auth on"},
	{Label: "SSH Port", State: component.StatusUnknown, Detail: "can't read"},
	{Label: "AppArmor", State: component.StatusNotApplicable, Detail: "set by the host"},
	{Label: "Auto Updates", State: component.StatusConfigured, Detail: "unattended-upgrades"},
}

// glyphRisk is the risk row: Figma's, an amber ⛬ Moderate, where today's
// Partial is the same glyph in the same colour. Its level is Figma's, not
// scored from the rows.
var glyphRisk = component.RiskSummary{Band: component.RiskModerate, Gloss: "medium security"}

// markOf returns the set's mark for state st, and false for Configured and
// Not Configured, which every set draws with Figma's glyphs.
func (s glyphSet) markOf(st component.StatusState) (stateMark, bool) {
	switch st {
	case component.StatusConfigured, component.StatusNotConfigured:
		return stateMark{}, false
	case component.StatusPartial:
		return s.partial, true
	case component.StatusNotApplicable:
		return s.na, true
	}
	return s.unknown, true
}

// look returns the glyph, the paint of the glyph and word, and the paint of
// the label of a row in state st.
func (s glyphSet) look(c component.Ctx, st component.StatusState) (string, paint, paint) {
	t := c.Theme
	if m, ok := s.markOf(st); ok {
		label := paint(t.Status.Label.Render)
		if m.quiet {
			label = t.Text.Muted.Render
		}
		return m.glyph(c), m.paint(t), label
	}
	if st == component.StatusConfigured {
		return t.Glyphs.Configured, t.Status.Configured.Render, t.Status.Label.Render
	}
	return t.Glyphs.NotConfigured, t.Status.NotConfigured.Render, t.Status.Label.Render
}

// gridRow lays out a row on the components' grid as Ctx.columns does:
// head, the marker and the label, then value from theme.ValueCol; below
// theme.StackBelow, or when head reaches the value column, head alone on
// its line and value under it at theme.StackCol (DEV-021). A line past the
// content width is cut with the ellipsis glyph (fitLine).
func gridRow(c component.Ctx, head, value []piece) string {
	w := contentWidth(c.Env)
	if termWidth(c.Env) < theme.StackBelow || widthOf(head) >= theme.ValueCol {
		return fitLine(c, head, w) + "\n" + fitLine(c, slices.Concat([]piece{pad(theme.StackCol)}, value), w)
	}
	return fitLine(c, slices.Concat(head, []piece{pad(theme.ValueCol - widthOf(head))}, value), w)
}

// fitLine renders line in at most limit cells, as Ctx.fit renders a line:
// a wider one is cut where the ellipsis glyph still fits after it, the
// spaces before the cut dropped, the glyph ending the last piece that keeps
// any text, in its paint; with no text left, as much of the glyph as fits.
func fitLine(c component.Ctx, line []piece, limit int) string {
	if widthOf(line) <= limit {
		return draw(line...)
	}
	e := c.Theme.Glyphs.Ellipsis
	room := limit - ansi.StringWidth(e)
	var kept []piece
	for _, p := range line {
		w := ansi.StringWidth(p.text)
		if w > room {
			kept = append(kept, piece{ansi.Truncate(p.text, max(0, room), ""), p.paint})
			break
		}
		kept = append(kept, p)
		room -= w
	}
	for len(kept) > 0 {
		last := &kept[len(kept)-1]
		if last.text = strings.TrimRight(last.text, " "); last.text != "" {
			last.text += e
			return draw(kept...)
		}
		kept = kept[:len(kept)-1]
	}
	return ansi.Truncate(e, max(0, limit), "")
}

// riskRow draws glyphRisk as RiskSummary draws it: the risk icon in the
// band's colour, "Risk Level", the level word and the gloss after
// Glyphs.Approx.
func riskRow(c component.Ctx) string {
	t, g := c.Theme, c.Theme.Glyphs
	band := paint(t.Risk.Moderate.Render)
	head := append(markerTo(g.Risk, band), piece{"Risk Level", t.Risk.Label.Render})
	return gridRow(c, head, []piece{{glyphRisk.Band.String(), band}, pad(1), {g.Approx + " " + glyphRisk.Gloss, t.Risk.Gloss.Render}})
}

// row draws r as StatusRow draws it, with the set's glyph and colour for
// its state: the label cut to the label column when the row does not
// stack, the state's word in the state's colour, the detail one cell after
// it in Status.Detail.
func (s glyphSet) row(c component.Ctx, r component.StatusRow) string {
	glyph, p, labelPaint := s.look(c, r.State)
	label := r.Label
	if termWidth(c.Env) >= theme.StackBelow {
		label = cut(c, label, theme.LabelWidth-1)
	}
	value := []piece{{r.State.String(), p}}
	if r.Detail != "" {
		value = append(value, pad(1), piece{r.Detail, c.Theme.Status.Detail.Render})
	}
	return gridRow(c, append(markerTo(glyph, p), piece{label, labelPaint}), value)
}

// codePoints names the code points of s: "U+2B19".
func codePoints(s string) string {
	var out []string
	for _, r := range s {
		out = append(out, fmt.Sprintf("U+%04X", r))
	}
	return strings.Join(out, " ")
}

// legend draws the set's key, a line for each of its marks: the glyph in
// its colour, the state in text.muted, then the glyph's code point (in
// Unicode glyphs) and the colour's token; for no marker, "no marker" and
// the token.
func (s glyphSet) legend(c component.Ctx) string {
	t := c.Theme
	var lines []string
	for _, st := range []component.StatusState{component.StatusPartial, component.StatusUnknown, component.StatusNotApplicable} {
		m, _ := s.markOf(st)
		glyph := m.glyph(c)
		var value []piece
		switch {
		case glyph == "":
			value = []piece{{"no marker", t.Text.Muted.Render}, pad(1)}
		case c.Env.Unicode:
			value = []piece{{codePoints(glyph), t.Text.Muted.Render}, pad(1)}
		}
		value = append(value, piece{m.token, t.Text.Faint.Render})
		lines = append(lines, gridRow(c, append(markerTo(glyph, m.paint(t)), piece{st.String(), t.Text.Muted.Render}), value))
	}
	return strings.Join(lines, "\n")
}

// body draws the set under its header: its legend, the risk row, and the
// seven rows, set apart as the main menu sets its blocks.
func (s glyphSet) body(c component.Ctx) string {
	rows := make([]string, len(glyphRows))
	for i, r := range glyphRows {
		rows[i] = s.row(c, r)
	}
	return apart(s.legend(c), riskRow(c), strings.Join(rows, "\n"))
}

// stripGlyph is one candidate in the strip: the glyph and a short name.
type stripGlyph struct{ glyph, name string }

// glyphStrip is the measured shortlist, by the state each glyph is a
// candidate for, with today's ? and ∅ for comparison. Warning is the
// optional fourth gap: ⛬ also marks a warning message today.
var glyphStrip = []struct {
	state  string
	glyphs []stripGlyph
}{
	{"Partial", []stripGlyph{{"⬙", "diamond, bottom half black"}, {"◈", "diamond in a diamond"}, {"◧", "square, left half black"}, {"⏼", "power on-off"}}},
	{"Unknown", []stripGlyph{{"⁇", "double question mark"}, {"⍰", "quad question"}, {"?", "question mark (current)"}}},
	{"Not Applicable", []stripGlyph{{"–", "en dash"}, {"⊟", "squared minus"}, {"⛝", "squared saltire"}, {"∅", "empty set (current)"}}},
	{"Warning", []stripGlyph{{"❢", "heavy exclamation mark"}}},
}

// strip draws each candidate alone in the marker column, then ⛛ ⭘ ⛬ from
// the label column, two cells apart, as a row leaves two cells after its
// marker for the ink that runs past its cell; all in Text.Primary, so that
// size and weight compare without colour; then its code point and name
// from the value column. It has no ASCII form: in ASCII glyphs it is a
// sentence saying so.
func strip(c component.Ctx) string {
	t, g := c.Theme, c.Theme.Glyphs
	if !c.Env.Unicode {
		return prose(c, "The strip compares Unicode glyphs and has no ASCII form.")
	}
	family := piece{g.Configured + "  " + g.NotConfigured + "  " + g.Risk, t.Text.Primary.Render}
	var blocks []string
	for _, grp := range glyphStrip {
		lines := []string{draw(pad(theme.Indent), piece{grp.state, t.Section.Title.Render})}
		for _, s := range grp.glyphs {
			head := append(markerTo(s.glyph, t.Text.Primary.Render), family)
			lines = append(lines, gridRow(c, head, []piece{{codePoints(s.glyph), t.Text.Muted.Render}, pad(1), {s.name, t.Text.Faint.Render}}))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return apart(blocks...)
}

// glyphsNote is the body of the note the mode opens with.
func glyphsNote(c component.Ctx) []string {
	g := c.Theme.Glyphs
	family := g.Configured + " " + g.NotConfigured + " " + g.Risk
	lists := "Each list is the risk row and Figma's seven rows in all five states. Current is today's glyphs and colours."
	if c.Env.Unicode {
		lists += " The strip sets each candidate beside " + family + " in one colour, to compare size and weight."
	}
	return []string{
		"Glyphs mode: Figma's " + family + " beside candidates for Partial, Unknown and Not Applicable. " +
			"In every candidate set " + g.Risk + " marks the risk row alone; its other uses today " +
			"(lockout risk, warnings, Reboot pending) are not shown.",
		lists,
		"Nothing is decided and no component changed: docs/engine/gallery-glyphs.md.",
	}
}

// selectGlyphs returns the sets args name, in the order "all" shows them,
// and whether they name the strip: every set and the strip for no argument
// or "all". An argument that starts with "-" is a flag the flag package
// left unparsed, after a name.
func selectGlyphs(args []string) ([]glyphSet, bool, error) {
	all := glyphSets()
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, true, nil
	}
	for _, a := range args {
		switch {
		case a == stripName || slices.ContainsFunc(all, func(s glyphSet) bool { return s.name == a }):
		case strings.HasPrefix(a, "-"):
			return nil, false, fmt.Errorf("flag %s comes after a name; flags go before them", a)
		default:
			return nil, false, fmt.Errorf("no set %q; the names are %s", a, strings.Join(glyphNames(), ", "))
		}
	}
	sets := slices.DeleteFunc(all, func(s glyphSet) bool { return !slices.Contains(args, s.name) })
	return sets, slices.Contains(args, stripName), nil
}

// glyphNames are the sets' names and the strip's, in order.
func glyphNames() []string {
	var names []string
	for _, s := range glyphSets() {
		names = append(names, s.name)
	}
	return append(names, stripName)
}

// glyphs is the glyphs mode: it parses args, writes the note, the sets they
// name and the strip if named to stdout, a write each, reports errors on
// stderr, and returns the exit status. Where -width is not given it takes
// the width of the terminal stdout is from size, as compare does
// (look.sizeTo), so that a wide terminal shows the sets side by side.
func glyphs(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery glyphs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, glyphsUsage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the sets, and the strip, it would render, and exit")
	look := lookFlags(fs, true)
	fs.Lookup("height").Usage = fmt.Sprintf("the terminal height in lines (default: the terminal's own when stdout is a terminal, else %d); glyphs draws nothing by it", term.DefaultHeight)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "uigallery: glyphs: "+format+"\n", a...)
		return exitUsage
	}
	sets, withStrip, err := selectGlyphs(fs.Args())
	if err != nil {
		return usage("%v", err)
	}
	if *list {
		for _, s := range sets {
			fmt.Fprintln(stdout, s.name)
		}
		if withStrip {
			fmt.Fprintln(stdout, stripName)
		}
		return exitOK
	}
	look.sizeTo(fs, size)
	c, err := look.cell()
	if err != nil {
		return usage("%v", err)
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	write := func(what string, render func() (string, error)) bool {
		out, err := render()
		if err == nil {
			_, err = io.WriteString(c.env.Writer(stdout), out)
		}
		if err != nil {
			fmt.Fprintf(stderr, "uigallery: glyphs: %s in %s: %v\n", what, c.name, err)
		}
		return err == nil
	}
	if !write("the note", func() (string, error) { return g.renderGlyphsNote(c) }) {
		return exitError
	}
	if len(sets) > 0 && !write("the sets", func() (string, error) { return g.renderSets(c, sets) }) {
		return exitError
	}
	if withStrip && !write(stripName, func() (string, error) { return g.renderStrip(c) }) {
		return exitError
	}
	return exitOK
}

// renderGlyphsNote returns the mode's note in cell c, an info Callout, and
// a blank line after it.
func (g gallery) renderGlyphsNote(c cell) (string, error) {
	ctx, err := g.ctx(c.env)
	if err != nil {
		return "", err
	}
	return component.Callout{Kind: component.MessageInfo, Body: glyphsNote(ctx)}.Render(ctx) + sectionBreak, nil
}

// renderSets returns sets in cell c under the gallery's header "glyphs
// sets", each under a SectionHeader with its title, and a blank line after
// them. As many as fit the terminal's width sit side by side, compareGap
// cells apart, each as wide as the widest line of any set, its header's
// rule too; the rest follow in rows. Below theme.StackBelow, where the rows
// stack, one is above the next. Every set has as many lines, so the rows of
// sets side by side line up.
func (g gallery) renderSets(c cell, sets []glyphSet) (string, error) {
	ctx, err := g.ctx(c.env)
	if err != nil {
		return "", err
	}
	bodies, widest := make([]string, len(sets)), 0
	for i, s := range sets {
		bodies[i] = s.body(ctx)
		widest = max(widest, ansi.StringWidth(s.title)+headerFrame)
		for _, l := range strings.Split(bodies[i], "\n") {
			widest = max(widest, ansi.StringWidth(l))
		}
	}
	per, heads := 1, ctx
	if termWidth(c.env) >= theme.StackBelow {
		per = max(1, (termWidth(c.env)+compareGap)/(widest+compareGap))
	}
	if per > 1 {
		env := c.env
		env.Width = widest
		if heads, err = g.ctx(env); err != nil {
			return "", err
		}
	}
	var rows []string
	for i := 0; i < len(sets); i += per {
		row := ""
		for j := i; j < min(i+per, len(sets)); j++ {
			block := apart(component.SectionHeader{Title: sets[j].title}.Render(heads), bodies[j])
			if j == i {
				row = block
				continue
			}
			row = beside(row, block, (j-i)*(widest+compareGap)-compareGap, compareGap)
		}
		rows = append(rows, row)
	}
	return apart(header(ctx, "glyphs sets"), apart(rows...)) + sectionBreak, nil
}

// renderStrip returns the strip in cell c under the gallery's header
// "glyphs strip", and a blank line after it.
func (g gallery) renderStrip(c cell) (string, error) {
	ctx, err := g.ctx(c.env)
	if err != nil {
		return "", err
	}
	return apart(header(ctx, "glyphs "+stripName), strip(ctx)) + sectionBreak, nil
}
