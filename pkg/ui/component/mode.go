package component

import (
	"slices"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// The ModeIndicator's words, the maintainer's (refactor plan slice 2b,
// decisions 2 and 3, adopted 2026-10-23; docs/engine/gallery-dryrun.md
// §4): the label, the word for each mode, and what set plan only, named
// after the word. They replace origin/main's "Dry-run Mode" "Disabled" and
// "Enabled" (pkg/style style.go:793-824), which pkg/style wrote on the main
// menu alone, until slice 2b's unit (d) removed them with it.
const (
	modeLabel    = "Mode"
	modeLive     = "Live"
	modePlanOnly = "Plan only"
	modeFromFlag = "-n"
	modeFromFile = "hardn.yml"
)

// ModeSource is what set plan only, which the ModeIndicator names after
// its word (decision 2). The zero value names nothing.
type ModeSource int

const (
	// ModeSourceNone names nothing: every live session, and plan only set
	// inside the session, where the user has just set it on screen. hardn
	// had two such switches, item 6 and Run All's option 2, until slice
	// 2b's unit (d) removed them: its plan only now always has a source.
	ModeSourceNone ModeSource = iota
	// ModeSourceFlag is -n or --dry-run on the command line: "(-n)".
	ModeSourceFlag
	// ModeSourceFile is hardn.yml's dryRun: "(hardn.yml)".
	ModeSourceFile
)

// word is the source's word, without its parentheses; "" for none.
func (s ModeSource) word() string {
	switch s {
	case ModeSourceFlag:
		return modeFromFlag
	case ModeSourceFile:
		return modeFromFile
	}
	return ""
}

// ModeIndicator shows the mode hardn runs in, in the maintainer's words
// (DEV-065): "Mode Live" when changes apply, the word in status.warn as
// Figma draws the live value; "Mode Plan only" when they are only shown
// as a plan, the word in status.ok as origin/main colours dry run; and,
// after plan only's word, what set it, "(-n)" or "(hardn.yml)", in
// text.faint. Live names nothing, whatever set it, and nor does plan only
// set inside the session (decision 2). The label is in text.muted. The
// word carries the mode, so it never rests on colour alone.
//
// Figma draws it at the value column of the menu heading's line, here
// with no colour:
//
//	0 2   6                    27
//	  Select an option         Mode Plan only (hardn.yml)
//
// Alone it keeps that column, and stacks to theme.StackCol below
// theme.StackBelow (DEV-021), as the heading's row does. The word is what
// tells the modes apart, so on a terminal too narrow for the whole text
// the label gives way first, then the source, each whole or left out, and
// the word last (decision 3): the source stays down to 27 columns,
// "Plan only (hardn.yml)" from theme.StackCol. The word then moves left
// of its column only as far as it must to stay whole, and is cut only on
// a terminal narrower than itself. It has no unknown state: hardn always
// knows its own mode.
type ModeIndicator struct {
	// DryRun is true in plan only, when changes are only shown as a plan.
	// The zero value is the live mode, as Figma draws it.
	DryRun bool
	// Source is what set plan only. The live mode names none, whatever
	// Source holds.
	Source ModeSource
}

// Render renders the indicator for c. See ModeIndicator for the layout.
func (vm ModeIndicator) Render(c Ctx) string {
	col := theme.ValueCol
	if c.stacked() {
		col = theme.StackCol
	}
	return vm.at(c, col)
}

// at renders the indicator on a line of its own from column col, which
// the word leaves only to stay whole. The menu heading's row places it
// under a heading too long to share its line.
func (vm ModeIndicator) at(c Ctx, col int) string {
	w := c.width()
	text := vm.spans(c, w-col)
	return c.fit(append(line{gap(min(col, w-text.width()))}, text...), w)
}

// spans is the indicator's text in at most room cells, which the menu
// heading's row places at its value column: the whole text where it
// fits; else without the label; else the word alone, which may still be
// wider than room.
func (vm ModeIndicator) spans(c Ctx, room int) line {
	t := c.Theme.Mode
	word := line{{modeLive, t.Live}}
	var from line
	if vm.DryRun {
		word = line{{modePlanOnly, t.DryRun}}
		if s := vm.Source.word(); s != "" {
			from = line{gap(1), {"(" + s + ")", t.Source}}
		}
	}
	for _, l := range []line{slices.Concat(line{{modeLabel, t.Label}, gap(1)}, word, from), slices.Concat(word, from)} {
		if l.width() <= room {
			return l
		}
	}
	return word
}
