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
//	  [+] SSH Port             + non-default (2230)
//	  [ ] AppArmor             Not Configured
//
// The state's glyph is centred in the marker column (DEV-020), the label
// starts at theme.LabelCol, and the state's word at theme.ValueCol, with
// the detail one cell after it in text.faint. A Configured row with a
// detail shows ConfiguredSign, "+", in place of its word (the maintainer,
// 2026-11-14; docs/engine/gallery-menu-layout.md): the rows that need work
// keep their words and are the loud ones. A Configured row with no detail
// keeps the word, and so does one with KeepWord or a Word. The glyph and
// the word, or the sign, take the state's colour, so the state never rests
// on colour alone: Partial shares Not Configured's glyph, and its colour
// and word tell it (the maintainer, 2026-10-21), so neither shows Word. A
// label wider than the label column, less the cell before the value
// column, is cut with the ellipsis glyph, so a list stays one row per
// line; so is a value that runs past the content width (DEV-025). Below
// theme.StackBelow the row stacks: the word and detail go under the label
// at theme.StackCol (DEV-021).
//
// Lines are drawn directly under the row, LineIndent cells in from the
// value column, under a "+" row's detail, each after LineMark and a space,
// mark and text in one colour; under the stacked value, as far in from its
// indent, when the row stacks; each cut with the ellipsis glyph at the
// content width, never wrapped; here with no colour and ASCII glyphs,
// under the column numbers:
//
//	0 2   6                    27
//	  [ ] Auto Updates         Not Configured no unattended-upgrades
//	                             ~ Up 182 days, no updates
//
// The main menu draws the risk level's factors so, under the rows they are
// about (the maintainer, 2026-10-21; DEV-059).
//
// An Info row is a fact with a problem and no state: the main menu's DNS,
// NTP and Syslog rows, drawn only while one has a problem (the maintainer,
// 2026-11-14). Glyphs.Bullet is its mark, it has no state word, its
// Detail, the host's value, is at the value column, and the problem is a
// line under it; with Problem, the Detail is the problem itself:
//
//	0 2   6                    27
//	   -  Syslog               192.0.2.50:514 TCP
//	                             ~ Unreachable
//	   -  NTP                  No time service
type StatusRow struct {
	// Label is the control's name, such as "SSH Port"; empty shows none.
	Label string
	// State sets the glyph, the colour and the word, which Word may
	// replace.
	State StatusState
	// Word is shown in place of the state's word, in the state's colour,
	// as Figma's SSH Login screen shows the port ("2230/tcp"). Empty
	// shows State.String(), or ConfiguredSign. A state whose marker
	// another state shares, Partial and Not Configured, keeps its own word
	// and Word is not shown: without colour the word alone tells the two
	// apart (the maintainer, 2026-10-21), so such a row puts its value in
	// Detail.
	Word string
	// Detail follows the word, such as "non-root, sudo"; empty shows
	// none.
	Detail string
	// Lines are drawn under the row, in order; empty ones are skipped.
	Lines []StatusLine
	// KeepWord keeps a Configured row's word where it would show
	// ConfiguredSign: hardn check's rows, which are read off a terminal
	// too.
	KeepWord bool
	// Info draws the row as a fact with no state: Glyphs.Bullet as its
	// mark, in Status.Line, no state word, Detail at the value column in
	// the detail's colour. State, Word and KeepWord are not read.
	Info bool
	// Problem, on an Info row, draws Detail in Status.LineWarn: the row
	// has no value, and its Detail says what is wrong ("Can't read").
	Problem bool
}

// ConfiguredSign stands for "Configured" before a Configured row's detail.
const ConfiguredSign = "+"

// LineMark opens a line under a status row, and LineIndent is how far in
// from the row's value the line starts: under the detail of a row with
// ConfiguredSign (the maintainer, 2026-11-14). A "-" there reads as a
// minus under "+".
const (
	LineMark   = "~"
	LineIndent = 2
)

// StatusLine is a line under a StatusRow, such as the main menu's "No
// password to become root".
type StatusLine struct {
	// Text is the line, after the mark.
	Text string
	// Warn draws the mark and the text in Status.LineWarn, status.warn:
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
	own := c.text(vm.Word)
	word := own
	if word == "" || vm.State.sharesGlyph(c.Theme.Glyphs) {
		word = vm.State.String()
	}
	detail := c.text(vm.Detail)
	if vm.State == StatusConfigured && detail != "" && own == "" && !vm.KeepWord {
		word = ConfiguredSign
	}
	value := line{{word, state}}
	if detail != "" {
		value = append(value, gap(1), span{detail, t.Detail})
	}
	head := marker(vm.State.glyph(c.Theme.Glyphs), state)
	if vm.Info {
		head = marker(c.Theme.Glyphs.Bullet, t.Line)
		style := t.Detail
		if vm.Problem {
			style = t.LineWarn
		}
		value = line{{detail, style}}
	}
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
			values = append(values, line{gap(LineIndent), {LineMark + " " + text, style}})
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
