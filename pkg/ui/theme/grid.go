package theme

import "github.com/abbott/hardn/pkg/ui/term"

// The grid, in terminal cells and lines. Columns count from 0 at the left
// edge of the content. The values come from the Figma export converted by
// role, not by dividing pixels (a cell is 19.2 px there): the content box
// is 64 cells and every screen shares one value column (DEV-008).
//
// Two-column rows (StatusRow, KeyValue, Menu) at the column numbers above
// them:
//
//	0 2   6                    27
//	  [+] SSH Port             Configured non-default (2230)
//	   1) User Management      Create, Configure
//	  10) Logs                 View log file
const (
	// ContentWidth is the width of Figma's content box, 64 cells. Under
	// CapContent it is the most cells any line uses. A narrower terminal
	// uses its own width instead.
	ContentWidth = 64
	// Indent is the left indent of rows, headings and the prompt. Bars and
	// the header start at column 0.
	Indent = 2
	// MarkerCol and MarkerWidth place the marker column: a state glyph,
	// centred, so that a one-cell glyph sits at column 3 where its ASCII
	// marker such as "[+]" fills columns 2 to 4 (DEV-020); or a menu key
	// such as "10)", right-aligned.
	MarkerCol   = Indent
	MarkerWidth = 3
	// LabelCol is where a row's label starts, one cell after the markers.
	LabelCol = MarkerCol + MarkerWidth + 1
	// LabelWidth is the label column's width, including the gap before
	// the value column.
	LabelWidth = 21
	// ValueCol is where a row's value, state or description starts.
	ValueCol = LabelCol + LabelWidth
	// StackCol is where a value starts in a stacked row, under the label
	// (StackBelow, DEV-021).
	StackCol = LabelCol
	// SectionGap is the number of blank lines between sections.
	SectionGap = 1
	// PromptGap is the number of blank lines before the prompt (drift D11).
	PromptGap = 1
)

// Width and height rules from the brief's §1A proposals, one constant per
// rule. They are pending maintainer approval (refactor plan §3.15); if it is
// refused, only these constants and the goldens change.
const (
	// CapContent caps lines at ContentWidth on a terminal wider than it,
	// rather than letting them use the terminal's whole width.
	CapContent = true
	// StackBelow is the terminal width, in columns, below which a
	// two-column row stacks: the label on one line and the value under it.
	// It is ContentWidth, within the brief's "below about 70 columns": from
	// that width up, CapContent gives the same 64-cell content box as at 80
	// columns, so a row that fits side by side there fits here (DEV-021).
	StackBelow = ContentWidth
	// BarsFillContent makes bars (the context bar, title bars and section
	// rules) fill ContentWidth rather than the whole terminal.
	BarsFillContent = true
	// CompactBelow is the terminal height, in rows, below which the main
	// menu renders in its compact form; a taller terminal gets it too when
	// the full form's lines do not fit it. The full menu is 35 lines from
	// StackBelow columns up, and more below, where its rows stack. The
	// compact form is gated (pkg/ui/screen's CompactMainMenu, DISABLED
	// 2026-10-23: the main menu takes its full form at every height), so
	// this and CompactBudget are read only where the gate allows it.
	CompactBelow = 40
	// CompactBudget is the most lines the compact main menu may use,
	// prompt included, so that it fits an 80x24 console: Figma's seven
	// rows with no line under one fit it. A host's eight rows, DNS among
	// them, take one line more, 25, and each line under a row another
	// (docs/engine/menu-live.md §6, open for the maintainer).
	CompactBudget = term.DefaultHeight
)
