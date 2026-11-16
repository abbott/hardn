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

// ReviewWord is a Not Configured status row's word (the maintainer,
// 2026-11-15; docs/engine/gallery-menu-layout.md §8): the row asks to be
// looked at. hardn check, the JSON and the Markdown report keep "Not
// Configured" (String).
const ReviewWord = "Review"

// rowWord is the state's word on a status row: String's, but ReviewWord
// for Not Configured.
func (s StatusState) rowWord() string {
	if s == StatusNotConfigured {
		return ReviewWord
	}
	return s.String()
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
//	  [ ] Firewall             Review ~ no ufw
//	  [-] AppArmor             Alpine default
//
// The state's glyph is centred in the marker column (DEV-020), the label
// starts at theme.LabelCol, and the state's word (rowWord: "Review" for
// Not Configured) at theme.ValueCol, then DetailMark, "~", and the detail,
// both in text.faint, so the word and the detail are not read as one
// phrase (the maintainer, 2026-11-15; docs/engine/gallery-menu-layout.md
// §8). A Configured row with a
// detail shows ConfiguredSign, "+", in place of its word (the maintainer,
// 2026-11-14; docs/engine/gallery-menu-layout.md), with no DetailMark: a
// sign is no word. A Not Applicable row with a detail shows the detail
// alone, in text.faint. A row with no detail shows its word alone. A row
// with KeepWord, or one that shows its Word, is the word and the detail
// one cell after it, as before. The glyph and
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
//	  [ ] Auto Updates         Review ~ no unattended-upgrades
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
	// KeepWord draws the row as hardn check prints it, which is read off
	// a terminal too: the state's own word (String: "Configured", "Not
	// Configured", "Not Applicable"), then the detail one cell after it,
	// with no ConfiguredSign, ReviewWord or DetailMark.
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

// DetailMark stands between a status row's state word and its detail, a
// space either side: "Review ~ no ufw", "Partial ~ root key-only" (the
// maintainer, 2026-11-15). Never after ConfiguredSign.
const DetailMark = "~"

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
	word, ownShown := own, own != "" && !vm.State.sharesGlyph(c.Theme.Glyphs)
	switch {
	case ownShown:
	case vm.KeepWord:
		word = vm.State.String()
	default:
		word = vm.State.rowWord()
	}
	detail := c.text(vm.Detail)
	value := line{{word, state}}
	switch {
	case detail == "":
	case vm.KeepWord || ownShown:
		value = append(value, gap(1), span{detail, t.Detail})
	case vm.State == StatusConfigured:
		value = line{{ConfiguredSign, state}, gap(1), {detail, t.Detail}}
	case vm.State == StatusNotApplicable:
		value = line{{detail, t.Detail}}
	default:
		value = append(value, gap(1), span{DetailMark + " " + detail, t.Detail})
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
