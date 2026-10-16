package component

import "github.com/abbott/hardn/pkg/ui/theme"

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
// theme.StackBelow (DEV-021), as the heading's row does. It has no unknown
// state: hardn always knows its own mode.
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
	return c.fit(append(line{gap(col)}, vm.spans(c)...), c.width())
}

// spans is the indicator's text, which the menu heading's row places at
// its value column.
func (vm ModeIndicator) spans(c Ctx) line {
	t := c.Theme.Mode
	word := span{modeLive, t.Live}
	if vm.DryRun {
		word = span{modeDryRun, t.DryRun}
	}
	return line{{modeLabel, t.Label}, gap(1), word}
}
