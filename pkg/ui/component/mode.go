package component

import (
	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// The ModeIndicator's words, as origin/main shows them on the main menu
// (pkg/style style.go:793-824, Menu.Render) and Figma draws the live mode.
const (
	modeLabel  = "Dry-run Mode"
	modeDryRun = "Enabled"
	modeLive   = "Disabled"
)

// ModeIndicator shows whether hardn runs in dry-run mode, in origin/main's
// words: "Dry-run Mode Disabled" when changes apply, the word in
// status.warn as Figma draws it, and "Dry-run Mode Enabled" when they are
// only previewed, the word in status.ok as origin/main colours it
// (DEV-016). The word carries the mode, so it never rests on colour alone.
// Proposal P-03 decides the dry-run model; until then the indicator shows
// the mode the view-model states, in these words.
//
// Figma draws it at the value column of the menu heading's line, here with
// no colour:
//
//	0 2   6                    27
//	  Select an option         Dry-run Mode Disabled
//
// Alone it keeps that column, and stacks to theme.StackCol below
// theme.StackBelow (DEV-021), as the heading's row does. The word is what
// tells the modes apart, so on a terminal too narrow for the whole text
// the label gives way first: it is cut with the ellipsis glyph, and left
// out once none of its letters fits. The word then moves left of its
// column only as far as it must to stay whole, and is cut only on a
// terminal narrower than itself. It has no unknown state: hardn always
// knows its own mode.
type ModeIndicator struct {
	// DryRun is true when changes are only previewed. The zero value is
	// the live mode, as Figma draws it.
	DryRun bool
}

// Render renders the indicator for c. See ModeIndicator for the layout.
func (vm ModeIndicator) Render(c Ctx) string {
	col := theme.ValueCol
	if c.stacked() {
		col = theme.StackCol
	}
	w := c.width()
	text := vm.spans(c, w-col)
	return c.fit(append(line{gap(min(col, w-text.width()))}, text...), w)
}

// spans is the indicator's text in at most room cells, which the menu
// heading's row places at its value column. When the whole text is wider,
// the label is cut to leave room for the word, and left out when no letter
// of it would remain; the word alone may still be wider than room.
func (vm ModeIndicator) spans(c Ctx, room int) line {
	t := c.Theme.Mode
	word := span{modeLive, t.Live}
	if vm.DryRun {
		word = span{modeDryRun, t.DryRun}
	}
	labelRoom := room - lipgloss.Width(word.text) - 1
	if labelRoom < lipgloss.Width(modeLabel) && labelRoom <= lipgloss.Width(c.Theme.Glyphs.Ellipsis) {
		return line{word}
	}
	return line{{c.truncate(modeLabel, labelRoom), t.Label}, gap(1), word}
}
