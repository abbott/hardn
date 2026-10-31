package component

import (
	"strings"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// CodeLine is one line of a CodeBlock: its text, in text.muted as captured
// output is, or with Warn in the warning's colour.
type CodeLine struct {
	Text string
	Warn bool
}

// CodeBlock is lines of captured output in a block of their own, where a
// section's rows would stand: here with ASCII glyphs, the Logs screen's lines
// (DEV-066).
//
//	0     6
//	      2026/10/20 12:00:05 ERROR: Failed to configure firewall:
//	      | exit status 1
//	      2026/10/20 12:05:11 SUCCESS: debian trixie detected
//
// The lines are code (README §9.1, "Code"; Ctx.code): no bullet, from
// theme.LabelCol, every character kept in both glyph sets, what a terminal would
// act on dropped, never cut, a line too long breaking after Glyphs.Wrap. An empty
// line is an empty row, and a block with no lines renders as the empty string.
type CodeBlock struct {
	Lines []CodeLine
}

// Render renders the lines for c.
func (vm CodeBlock) Render(c Ctx) string {
	return strings.Join(vm.Rows(c), "\n")
}

// Rows are the rows the lines take for c, a line too long taking more than one:
// what a screen counts to fit lines to a terminal.
func (vm CodeBlock) Rows(c Ctx) []string {
	var out []string
	for _, l := range vm.Lines {
		style := c.Theme.Output
		if l.Warn {
			style = c.Theme.KeyValue.ValueWarn
		}
		out = append(out, c.code(theme.LabelCol, raw(l.Text), style)...)
	}
	return out
}
