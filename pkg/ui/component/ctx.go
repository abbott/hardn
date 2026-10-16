package component

import (
	"slices"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// unknown is the word shown for a value hardn could not read, as
// origin/main shows an address it could not read (pkg/menu main_menu.go).
const unknown = "Unknown"

// Ctx is the render context every component renders with: the environment
// and the theme built once from it. Build it with NewCtx: a Ctx with no
// Theme, such as the zero Ctx, makes every Render panic.
type Ctx struct {
	Env   term.Env
	Theme *theme.Theme
}

// NewCtx returns the render context for env, building its theme once.
func NewCtx(env term.Env) Ctx {
	return Ctx{Env: env, Theme: theme.New(env)}
}

// termWidth is the terminal's width, or term.DefaultWidth when the Env
// has none, as term.Resolve falls back when it finds no size.
func (c Ctx) termWidth() int {
	if c.Env.Width < 1 {
		return term.DefaultWidth
	}
	return c.Env.Width
}

// width is the content width: the terminal's, capped at
// theme.ContentWidth under theme.CapContent.
func (c Ctx) width() int {
	if theme.CapContent {
		return min(c.termWidth(), theme.ContentWidth)
	}
	return c.termWidth()
}

// barWidth is the width a bar fills: the content width under
// theme.BarsFillContent, else the terminal's.
func (c Ctx) barWidth() int {
	if theme.BarsFillContent {
		return c.width()
	}
	return c.termWidth()
}

// stacked reports whether two-column layouts stack (theme.StackBelow).
func (c Ctx) stacked() bool {
	return c.termWidth() < theme.StackBelow
}

// span is a run of plain text in one style.
type span struct {
	text  string
	style lipgloss.Style
}

// String renders the span. An empty span renders as nothing, rather than
// as an empty pair of escape sequences.
func (s span) String() string {
	if s.text == "" {
		return ""
	}
	return s.style.Render(s.text)
}

// gap is n spaces in no style.
func gap(n int) span {
	return span{text: strings.Repeat(" ", max(0, n))}
}

// line is one output line under construction.
type line []span

// width is the line's width in cells.
func (l line) width() int {
	w := 0
	for _, s := range l {
		w += lipgloss.Width(s.text)
	}
	return w
}

// String renders the line.
func (l line) String() string {
	var b strings.Builder
	for _, s := range l {
		b.WriteString(s.String())
	}
	return b.String()
}

// to returns l followed by the spaces that bring it to column col.
func (l line) to(col int) line {
	return append(slices.Clip(l), gap(col-l.width()))
}

// fit renders l in at most limit cells. A wider line is cut where the
// ellipsis glyph still fits after it; the spaces before the cut are
// dropped, in whichever spans they are, and the ellipsis ends the last
// span that keeps any text, in that span's style. So a cut looks the same
// wherever the line's spans begin and end. With no text left, or no room
// for the whole glyph, the line is as much of the ellipsis as fits.
func (c Ctx) fit(l line, limit int) string {
	if l.width() <= limit {
		return l.String()
	}
	ellipsis := c.Theme.Glyphs.Ellipsis
	room := limit - lipgloss.Width(ellipsis)
	var kept line
	for _, s := range l {
		w := lipgloss.Width(s.text)
		if w > room {
			kept = append(kept, span{ansi.Truncate(s.text, max(0, room), ""), s.style})
			break
		}
		kept = append(kept, s)
		room -= w
	}
	for len(kept) > 0 {
		last := &kept[len(kept)-1]
		if last.text = strings.TrimRight(last.text, " "); last.text != "" {
			last.text += ellipsis
			return kept.String()
		}
		kept = kept[:len(kept)-1]
	}
	return ansi.Truncate(ellipsis, max(0, limit), "")
}

// truncate shortens plain text s to at most limit cells, ending it with
// the ellipsis glyph when it is cut. As in fit, the spaces before the cut
// are dropped, and with no text left, or no room for the whole glyph, the
// result is as much of the ellipsis as fits.
func (c Ctx) truncate(s string, limit int) string {
	if lipgloss.Width(s) <= limit {
		return s
	}
	ellipsis := c.Theme.Glyphs.Ellipsis
	kept := strings.TrimRight(ansi.Truncate(s, max(0, limit-lipgloss.Width(ellipsis)), ""), " ")
	if kept == "" {
		return ansi.Truncate(ellipsis, max(0, limit), "")
	}
	return kept + ellipsis
}

// columns lays out a label and its values on the grid, as layout does. It
// stacks below theme.StackBelow, or when the label reaches the value
// column.
func (c Ctx) columns(label line, values ...line) string {
	return c.layout(c.stacked() || label.width() >= theme.ValueCol, label, values...)
}

// layout lays out a label and its values. The label carries its own
// indent; the first value sits beside it at theme.ValueCol and each
// further value under that. When stack is set the label is on its own
// line, then each value at theme.StackCol (DEV-021). Empty values are
// skipped, and with none the label is alone.
func (c Ctx) layout(stack bool, label line, values ...line) string {
	w := c.width()
	values = slices.DeleteFunc(slices.Clone(values), func(v line) bool { return v.width() == 0 })
	if len(values) == 0 {
		return c.fit(label, w)
	}
	var out []string
	if stack {
		out = append(out, c.fit(label, w))
	}
	for i, v := range values {
		head := line{gap(theme.ValueCol)}
		switch {
		case stack:
			head = line{gap(theme.StackCol)}
		case i == 0:
			head = label.to(theme.ValueCol)
		}
		out = append(out, c.fit(slices.Concat(head, v), w))
	}
	return strings.Join(out, "\n")
}

// marker places a state glyph in the marker column, centred (DEV-020).
// A label after it starts at theme.LabelCol: line.to pads up to it, which
// marker leaves to the caller so that a row with no label ends with the
// glyph rather than with spaces.
func marker(glyph string, style lipgloss.Style) line {
	lead := theme.MarkerCol + max(0, theme.MarkerWidth-lipgloss.Width(glyph))/2
	return line{gap(lead), {glyph, style}}
}

// badge renders s as a badge at most limit cells wide: a cell of padding
// either side in the badge colours or, with no colour to paint it,
// Glyphs.BadgeOpen and BadgeClose (DEV-019).
func (c Ctx) badge(s string, limit int) span {
	left, right := " ", " "
	if !c.Env.HasColor() {
		left, right = c.Theme.Glyphs.BadgeOpen, c.Theme.Glyphs.BadgeClose
	}
	s = c.truncate(s, limit-lipgloss.Width(left+right))
	return span{left + s + right, c.Theme.Badge}
}

// paint returns n cells painted in fill: spaces on its background or, with
// no colour to paint them, a run of Glyphs.Bar (DEV-015, DEV-018).
func (c Ctx) paint(fill lipgloss.Style, n int) span {
	if c.Env.HasColor() {
		return span{strings.Repeat(" ", max(0, n)), fill}
	}
	return span{strings.Repeat(c.Theme.Glyphs.Bar, max(0, n)), fill}
}

// rule returns a run of Glyphs.Rule n cells wide in style.
func (c Ctx) rule(style lipgloss.Style, n int) span {
	g := c.Theme.Glyphs.Rule
	return span{strings.Repeat(g, max(0, n)/lipgloss.Width(g)), style}
}

// barFrame is the cells a bar keeps around its text: the stub, a cell
// either side of the text and at least one cell of fill.
const barFrame = 4

// bar renders a context or title bar across c.barWidth(): a one-cell
// stub, the text on the terminal's own background with a cell either side,
// and the fill (DEV-018), painted as paint paints them. Text too wide for
// the bar is cut; a bar with no text, or too narrow for any, is all fill.
func (c Ctx) bar(fill lipgloss.Style, text line) string {
	w := c.barWidth()
	if text.width() == 0 || w <= barFrame {
		return c.paint(fill, w).String()
	}
	body := c.fit(text, w-barFrame)
	return c.paint(fill, 1).String() + " " + body + " " + c.paint(fill, w-3-lipgloss.Width(body)).String()
}

// clean makes a view-model string one line of plain text: escape sequences
// are removed, tabs and line breaks become spaces, other control
// characters and invalid UTF-8 are dropped, and surrounding spaces are
// trimmed. Text that then measures no cells, such as a lone zero-width
// space, is empty, so it shows as a missing value. What remains measures
// as it shows.
func clean(s string) string {
	s = ansi.Strip(strings.ToValidUTF8(s, ""))
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	if s = strings.TrimSpace(s); lipgloss.Width(s) == 0 {
		return ""
	}
	return s
}

// known returns s in style, or the word Unknown in alt when s is empty.
func known(s string, style, alt lipgloss.Style) line {
	if s = clean(s); s != "" {
		return line{{s, style}}
	}
	return line{{unknown, alt}}
}
