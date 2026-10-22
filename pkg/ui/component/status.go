package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// StatusState is the state of a hardening control: brief Phase 4's
// Configured, NotConfigured, Partial, NotApplicable and Unknown.
type StatusState int

// The status states. The zero value is StatusUnknown: a state hardn could
// not check, which the status model prefers to a guess.
const (
	StatusUnknown StatusState = iota
	StatusConfigured
	StatusNotConfigured
	StatusPartial
	StatusNotApplicable
)

// String returns the state's word, which a StatusRow shows when it has no
// Word. Configured and Not Configured are Figma's and origin/main's; the
// words for the states Figma does not draw are the brief's state names
// (DEV-024). A value outside the states is "Unknown".
func (s StatusState) String() string {
	switch s {
	case StatusConfigured:
		return "Configured"
	case StatusNotConfigured:
		return "Not Configured"
	case StatusPartial:
		return "Partial"
	case StatusNotApplicable:
		return "Not Applicable"
	}
	return unknown
}

// statusStates is every status state.
var statusStates = [...]StatusState{StatusUnknown, StatusConfigured, StatusNotConfigured, StatusPartial, StatusNotApplicable}

// sharesGlyph reports whether another state draws s's marker in g, as
// Partial draws Not Configured's: then, without colour, only the word
// tells s from it.
func (s StatusState) sharesGlyph(g theme.Glyphs) bool {
	for _, o := range statusStates {
		if o != s && o.glyph(g) == s.glyph(g) {
			return true
		}
	}
	return false
}

// glyph returns the state's marker in g.
func (s StatusState) glyph(g theme.Glyphs) string {
	switch s {
	case StatusConfigured:
		return g.Configured
	case StatusNotConfigured:
		return g.NotConfigured
	case StatusPartial:
		return g.Partial
	case StatusNotApplicable:
		return g.NotApplicable
	}
	return g.Unknown
}

// style returns the state's style in t, which colours both its glyph and
// its word.
func (s StatusState) style(t theme.StatusStyles) lipgloss.Style {
	switch s {
	case StatusConfigured:
		return t.Configured
	case StatusNotConfigured:
		return t.NotConfigured
	case StatusPartial:
		return t.Partial
	case StatusNotApplicable:
		return t.NotApplicable
	}
	return t.Unknown
}

// StatusRow is one control's status; here with no colour and ASCII
// glyphs, under the column numbers:
//
//	0 2   6                    27
//	  [+] SSH Port             Configured non-default (2230)
//	  [ ] AppArmor             Not Configured
//
// The state's glyph is centred in the marker column (DEV-020), the label
// starts at theme.LabelCol, and the state's word at theme.ValueCol, with
// the detail one cell after it in text.faint. The glyph and the word take
// the state's colour, so the state never rests on colour alone: Partial
// shares Not Configured's glyph, and its colour and word tell it (the
// maintainer, 2026-10-21), so neither shows Word. A label wider than the
// label column, less the cell before the value column, is cut with the
// ellipsis glyph, so a list stays one row per line; so is a value that
// runs past the content width (DEV-025). Below theme.StackBelow the row
// stacks: the word and detail go under the label at theme.StackCol
// (DEV-021).
//
// Lines are drawn directly under the row, at the value column, each after
// Glyphs.Bullet and a space, bullet and text in one colour; under the
// stacked value, at its indent, when the row stacks; each cut with the
// ellipsis glyph at the content width, never wrapped; here with no colour
// and ASCII glyphs, under the column numbers:
//
//	0 2   6                    27
//	  [ ] Auto Updates         Not Configured no unattended-upgrades
//	                           - Up 182 days, no updates
//
// The main menu draws the risk level's factors so, under the rows they are
// about (the maintainer, 2026-10-21; DEV-059). Elsewhere a row has none.
type StatusRow struct {
	// Label is the control's name, such as "SSH Port"; empty shows none.
	Label string
	// State sets the glyph, the colour and the word, which Word may
	// replace.
	State StatusState
	// Word is shown in place of the state's word, in the state's colour,
	// as Figma's SSH Login screen shows the port ("2230/tcp"). Empty
	// shows State.String(). A state whose marker another state shares,
	// Partial and Not Configured, keeps its own word and Word is not
	// shown: without colour the word alone tells the two apart (the
	// maintainer, 2026-10-21), so such a row puts its value in Detail.
	Word string
	// Detail follows the word, such as "non-root, sudo"; empty shows
	// none.
	Detail string
	// Lines are drawn under the row, in order; empty ones are skipped.
	// pkg/output gives them on the main menu only.
	Lines []StatusLine
}

// StatusLine is a line under a StatusRow, such as the main menu's "No
// password to become root".
type StatusLine struct {
	// Text is the line, after the bullet.
	Text string
	// Warn draws the bullet and the text in Status.LineWarn, status.warn:
	// a cost hardn established. The zero value draws them in Status.Line,
	// text.muted: a line that rests on something hardn could not
	// establish.
	Warn bool
}

// Render renders the row for c. See StatusRow for the layout.
func (vm StatusRow) Render(c Ctx) string {
	t := c.Theme.Status
	state := vm.State.style(t)
	label := c.text(vm.Label)
	if !c.stacked() {
		label = c.truncate(label, theme.LabelWidth-1)
	}
	word := c.text(vm.Word)
	if word == "" || vm.State.sharesGlyph(c.Theme.Glyphs) {
		word = vm.State.String()
	}
	value := line{{word, state}}
	if d := c.text(vm.Detail); d != "" {
		value = append(value, gap(1), span{d, t.Detail})
	}
	head := marker(vm.State.glyph(c.Theme.Glyphs), state)
	if label != "" {
		head = append(head.to(theme.LabelCol), span{label, t.Label})
	}
	values := []line{value}
	for _, l := range vm.Lines {
		style := t.Line
		if l.Warn {
			style = t.LineWarn
		}
		if text := c.text(l.Text); text != "" {
			values = append(values, line{{c.Theme.Glyphs.Bullet + " " + text, style}})
		}
	}
	return c.columns(head, values...)
}

// StatusList is the main menu's status rows, one StatusRow per control,
// in order, on the shared grid, each row's lines under it with no blank
// line between the rows. A list with no rows renders as the empty
// string, so the caller leaves out the block and its gap: neither Figma
// nor origin/main has wording for a host with no controls.
type StatusList struct {
	Rows []StatusRow
}

// Render renders the rows for c, one after another.
func (vm StatusList) Render(c Ctx) string {
	out := make([]string, len(vm.Rows))
	for i, r := range vm.Rows {
		out[i] = r.Render(c)
	}
	return strings.Join(out, "\n")
}
