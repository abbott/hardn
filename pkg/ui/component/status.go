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
// the state's colour, so the state never rests on colour alone. A label
// wider than the label column, less the cell before the value column, is
// cut with the ellipsis glyph, so a list stays one row per line; so is a
// value that runs past the content width (DEV-025). Below
// theme.StackBelow the row stacks: the word and detail go under the label
// at theme.StackCol (DEV-021).
type StatusRow struct {
	// Label is the control's name, such as "SSH Port"; empty shows none.
	Label string
	// State sets the glyph, the colour and, unless Word is set, the word.
	State StatusState
	// Word is shown in place of the state's word, in the state's colour,
	// as Figma's SSH Login screen shows the port ("2230/tcp") and
	// origin/main a firewall that is on but not configured ("Enabled",
	// pkg/security status.go:134-135). Empty shows State.String().
	Word string
	// Detail follows the word, such as "non-root, sudo"; empty shows
	// none.
	Detail string
}

// Render renders the row for c. See StatusRow for the layout.
func (vm StatusRow) Render(c Ctx) string {
	t := c.Theme.Status
	state := vm.State.style(t)
	label := clean(vm.Label)
	if !c.stacked() {
		label = c.truncate(label, theme.LabelWidth-1)
	}
	word := clean(vm.Word)
	if word == "" {
		word = vm.State.String()
	}
	value := line{{word, state}}
	if d := clean(vm.Detail); d != "" {
		value = append(value, gap(1), span{d, t.Detail})
	}
	head := marker(vm.State.glyph(c.Theme.Glyphs), state)
	if label != "" {
		head = append(head.to(theme.LabelCol), span{label, t.Label})
	}
	return c.columns(head, value)
}

// StatusList is the main menu's status rows, one StatusRow per control,
// in order, on the shared grid. A list with no rows renders as the empty
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
