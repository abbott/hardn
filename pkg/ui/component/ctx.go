package component

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
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
	// Whole cuts nothing and breaks no row: a value or a bullet item stays on
	// its line whole, however long, past the content width. For output that is
	// not a terminal, which a file or a tool holds and no width limits
	// (OffTerminal, DEV-067). NewCtx leaves it false, as every screen has it.
	Whole bool
}

// NewCtx returns the render context for env, building its theme once.
func NewCtx(env term.Env) Ctx {
	return Ctx{Env: env, Theme: theme.New(env)}
}

// OffTerminal returns the render context of what is written off a terminal,
// such as hardn system-details > file: plain mode at term.DefaultWidth, whatever
// the environment says, and nothing cut (Whole, DEV-067). Write what it renders
// through its Env's Writer, which strips every escape sequence.
func OffTerminal() Ctx {
	c := NewCtx(term.Fixed(term.DefaultWidth, colorprofile.NoTTY, false, true))
	c.Whole = true
	return c
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
// for the whole glyph, the line is as much of the ellipsis as fits. With
// Whole set no line is cut.
func (c Ctx) fit(l line, limit int) string {
	if c.Whole || l.width() <= limit {
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
// result is as much of the ellipsis as fits. With Whole set s is not cut.
func (c Ctx) truncate(s string, limit int) string {
	if c.Whole || lipgloss.Width(s) <= limit {
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

// paint returns n cells painted in fill, a callout's accent bar: spaces on
// its background or, with no colour to paint them, a run of Glyphs.Bar
// (DEV-015, DEV-032).
func (c Ctx) paint(fill lipgloss.Style, n int) span {
	if c.Env.HasColor() {
		return span{strings.Repeat(" ", max(0, n)), fill}
	}
	return span{strings.Repeat(c.Theme.Glyphs.Bar, max(0, n)), fill}
}

// shade returns a run of Glyphs.Shade n cells wide in style, a bar's
// Fill: ░ in bar.shade, or uncoloured with no colour, and # in the ASCII
// set; never a background (the maintainer, 2026-10-21; DEV-018).
func (c Ctx) shade(style lipgloss.Style, n int) span {
	return span{strings.Repeat(c.Theme.Glyphs.Shade, max(0, n)), style}
}

// rule returns a run of Glyphs.Rule n cells wide in style: ╌, or - in the
// ASCII set (DEV-072).
func (c Ctx) rule(style lipgloss.Style, n int) span {
	g := c.Theme.Glyphs.Rule
	return span{strings.Repeat(g, max(0, n)/lipgloss.Width(g)), style}
}

// barFrame is the cells a bar keeps around its text: the stub, a cell
// either side of the text and at least one cell of fill.
const barFrame = 4

// bar renders a context or title bar across c.barWidth(): a one-cell
// stub, the text on the terminal's own background with a cell either side,
// and the fill, the stub and fill drawn as shade draws them (DEV-018).
// Text too wide for the bar is cut; a bar with no text, or too narrow for
// any, is all fill.
func (c Ctx) bar(fill lipgloss.Style, text line) string {
	w := c.barWidth()
	if text.width() == 0 || w <= barFrame {
		return c.shade(fill, w).String()
	}
	body := c.fit(text, w-barFrame)
	return c.shade(fill, 1).String() + " " + body + " " + c.shade(fill, w-3-lipgloss.Width(body)).String()
}

// wrap breaks plain text s into lines of at most limit cells, at spaces
// only, so a word with a hyphen, such as a package name in a command,
// stays whole: lipgloss.Wrap also breaks after every hyphen (x/ansi
// wrap.go:406-417), which split "policycoreutils-python-utils" in a
// refusal's fix (2026-11-10). A word wider than limit, which must break
// inside, breaks as lipgloss.Wrap breaks it with the line before it
// (wrap.go:12-19, x/ansi wrap.go:293-463). A line break in s ends a line.
// The lines keep no trailing spaces, and empty ones are dropped. With
// limit under 1, s is one line.
//
// A command in the text is kept whole where it fits a line, so that it
// copies as one line: a span in backticks, as
// "(`dnf install policycoreutils-python-utils`),", and the rest of a line
// from a "$" word, Glyphs.Command before a command hardn runs (DEV-038),
// as a log's "run 1: $ systemctl stop <unit>". Either is laid out as one
// word from the word it starts in to the word it ends in, so where it
// doesn't fit the line it starts the next. One wider than limit wraps word
// by word as other text does, its words starting no command of their own.
func wrap(s string, limit int) []string {
	if limit < 1 {
		return []string{s}
	}
	var out []string
	emit := func(l string) {
		if l = strings.TrimRight(l, " "); l != "" {
			out = append(out, l)
		}
	}
	for _, para := range strings.Split(s, "\n") {
		// wide is how much of para is left after a command too wide to
		// keep whole: until then, its words wrap as text.
		cur, wide := "", -1
		for para != "" {
			i := strings.IndexFunc(para, notBreak)
			if i < 0 {
				i = len(para)
			}
			j := strings.IndexFunc(para[i:], isBreak)
			if j < 0 {
				j = len(para) - i
			}
			if wide < 0 || len(para) <= wide {
				if k := commandEnd(para, i, i+j); k > i+j && lipgloss.Width(para[i:k]) <= limit {
					j = k - i
				} else if k > i+j {
					wide = len(para) - k
				}
			}
			word, next := para[i:i+j], cur+para[:i+j]
			para = para[i+j:]
			switch {
			case lipgloss.Width(next) <= limit:
				cur = next
			case lipgloss.Width(word) > limit:
				ls := strings.Split(lipgloss.Wrap(next, limit, ""), "\n")
				for _, l := range ls[:len(ls)-1] {
					emit(l)
				}
				cur = ls[len(ls)-1]
			default:
				emit(cur)
				cur = word
			}
		}
		emit(cur)
	}
	return out
}

// commandEnd is where the command that the word para[start:end] starts in
// ends (wrap): the end of the word holding the backtick that closes the
// word's open one, or, for the word "$" with text after it, the end of
// para. A word in no command, a backtick never closed, and a "$" with
// nothing after it end at end.
func commandEnd(para string, start, end int) int {
	if para[start:end] == "$" && strings.TrimSpace(para[end:]) != "" {
		return len(para)
	}
	for strings.Count(para[start:end], "`")%2 == 1 {
		k := strings.IndexByte(para[end:], '`')
		if k < 0 {
			break
		}
		end += k + 1
		if n := strings.IndexFunc(para[end:], isBreak); n >= 0 {
			end += n
		} else {
			end = len(para)
		}
	}
	return end
}

// isBreak reports whether wrap may break a line at r: a space as x/ansi
// takes one, but for the no-break space (x/ansi wrap.go:348).
func isBreak(r rune) bool { return unicode.IsSpace(r) && r != ' ' }

// notBreak is isBreak negated.
func notBreak(r rune) bool { return !isBreak(r) }

// clean makes a view-model string one line of plain text: escape sequences
// are removed, tabs and line breaks become spaces, other control
// characters and invalid UTF-8 are dropped, typographic quotes become
// ASCII ones (C12: ‘ ’ and “ ” are East Asian ambiguous width), and
// surrounding spaces are trimmed. Text that then measures no cells, such
// as a lone zero-width space, is empty, so it shows as a missing value.
// What remains measures as it shows.
func clean(s string) string {
	s = ansi.Strip(strings.ToValidUTF8(s, ""))
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case r == '\u2018' || r == '\u2019':
			return '\''
		case r == '\u201C' || r == '\u201D':
			return '"'
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

// asciiText maps the typography view-model text may hold, beyond the
// typographic quotes clean maps everywhere, to ASCII: ≥ to >=, ≤ to <=,
// ≠ to !=, … to ..., the en and em dashes – and — to -, × to x, → to ->
// and • to * (no glyph of either set is •). Ctx.text applies it with the
// ASCII glyph set (term.Env.Unicode false), which the terminals that
// cannot draw these characters get: TERM linux or dumb, a locale with no
// UTF-8, a CJK locale, plain mode, --ascii (DEV-036).
var asciiText = strings.NewReplacer(
	"\u2265", ">=",
	"\u2264", "<=",
	"\u2260", "!=",
	"\u2026", "...",
	"\u2013", "-",
	"\u2014", "-",
	"\u00D7", "x",
	"\u2192", "->",
	"\u2022", "*",
)

// text makes a view-model string one line of plain text for c, as clean
// does, and with the ASCII glyph set maps its typography to ASCII
// (asciiText), so that ASCII output shows no character the glyph set
// exists to avoid. What must be typed or opened as shown keeps its
// characters, through clean alone: a menu's keys, a prompt's hint, which
// lists the inputs it accepts, and a report's log path; code keeps them
// too (raw). An answer AnsweredField echoes is mapped: it says what was
// typed or taken, not what must be typed.
func (c Ctx) text(s string) string {
	if s = clean(s); !c.Env.Unicode {
		s = asciiText.Replace(s)
	}
	return s
}

// tabWidth is the cells a tab takes in code, as Lip Gloss's own default
// (style.go:14 in lipgloss v2.0.6).
const tabWidth = 4

// raw makes code (a diff line, a target, one line of a command or of
// captured output) one line of plain text that keeps its characters:
// escape sequences are removed, a tab becomes tabWidth spaces, a line
// break becomes a space, and other control characters and invalid UTF-8
// are dropped. A character that draws as nothing, or as a blank other than
// the space, is shown as its code point, such as <U+200B>: a format
// character (Cf) such as a zero-width space or a bidi override, a
// variation selector or other default-ignorable character, or a separator
// such as a no-break space. So a plan never hides a character in a line
// that looks like another. Unlike clean it trims nothing and maps no
// quotes, so that a plan shows a file's line as it is.
func raw(s string) string {
	var b strings.Builder
	for _, r := range ansi.Strip(strings.ToValidUTF8(s, "")) {
		switch {
		case r == '\t':
			b.WriteString(strings.Repeat(" ", tabWidth))
		case r == '\n' || r == '\r':
			b.WriteByte(' ')
		case unicode.IsControl(r):
		case invisible(r):
			fmt.Fprintf(&b, "<U+%04X>", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// invisible reports whether r draws as nothing, or as a blank other than
// the space, and so is shown in code by its code point (raw).
func invisible(r rune) bool {
	return r != ' ' && unicode.In(r, unicode.Cf, unicode.Zs, unicode.Zl, unicode.Zp,
		unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point)
}

// commandLines returns the lines of the command s, each made by raw: a
// command with a heredoc keeps its line breaks rather than reading as one
// line. Blank lines before and after the command, the spaces before it and
// the spaces at the end of each line are left out; a command with nothing
// to show has no lines.
func commandLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		lines = append(lines, strings.TrimRight(raw(l), " "))
	}
	for len(lines) > 0 && strings.TrimLeft(lines[0], " ") == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > 0 {
		lines[0] = strings.TrimLeft(lines[0], " ")
	}
	return lines
}

// command lays out the command s (commandLines) from column col in style,
// as code: its first line after prefix, such as Glyphs.Command and a space,
// and each further line under the first line's text, where a heredoc's
// lines start. A command with nothing to show is no lines.
func (c Ctx) command(col int, prefix, s string, style lipgloss.Style) []string {
	var out []string
	for i, l := range commandLines(s) {
		if i == 0 {
			out = append(out, c.code(col, prefix+l, style)...)
			continue
		}
		out = append(out, c.code(col+lipgloss.Width(prefix), l, style)...)
	}
	return out
}

// code lays out the code s, made by raw, from column col in style. It is
// never cut: where the content width ends it breaks, at the last space
// after its indent or else inside a word, and each further piece starts
// at col with Glyphs.Wrap (DEV-038). The spaces at a break open the next
// piece, after the glyph, where they show: a break at a space reads apart
// from a break inside a word, and an indent carries over. So a plan shows
// every character it will write but the spaces at a line's end. A run of
// spaces too long to show beside the glyph, on a line a few cells wide, is
// left out, and a line of spaces is an empty line. Only when not one more
// character fits beside col is the rest cut with the ellipsis glyph.
func (c Ctx) code(col int, s string, style lipgloss.Style) []string {
	return c.codeFrom(col, span{}, s, style)
}

// codeFrom lays out the code s as code does, its first piece after open,
// a span in a style of its own that takes cells of that line, such as the
// countdown's Glyphs.Command and a space. An empty open is code's layout.
func (c Ctx) codeFrom(col int, open span, s string, style lipgloss.Style) []string {
	if s = strings.TrimRight(s, " "); s == "" {
		return []string{""}
	}
	w := c.width()
	var out []string
	for lead := ""; ; {
		head := codeHead(s, max(0, w-col-lipgloss.Width(open.text)-lipgloss.Width(lead)))
		rest, ok := strings.CutPrefix(s, head)
		if head == "" || !ok {
			return append(out, c.fit(line{gap(col), open, {lead + s, style}}, w))
		}
		if rest == "" {
			return append(out, line{gap(col), open, {lead + head, style}}.String())
		}
		if rest[0] != ' ' {
			if i := strings.LastIndexByte(head, ' '); i > 0 && strings.TrimLeft(head[:i], " ") != "" {
				head = head[:i]
			}
		}
		if head = strings.TrimRight(head, " "); head == "" {
			// Only spaces fit: the run cannot show, so it is left out.
			s = strings.TrimLeft(s, " ")
			continue
		}
		out = append(out, line{gap(col), open, {lead + head, style}}.String())
		s, lead, open = s[len(head):], c.Theme.Glyphs.Wrap, span{}
	}
}

// codeHead is the start of s that fits in limit cells, as ansi.Truncate has it,
// reading no more of s than it takes to find it, so that a line lays out in time
// its length, however long, and not its square. A part of s whose every cell fits
// says nothing of what follows it, so a part twice the size is read then, as
// grapheme clusters can take more bytes to a cell than a rune does.
func codeHead(s string, limit int) string {
	for n := (limit + 1) * utf8.UTFMax; n < len(s); n *= 2 {
		cut := n
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if head := ansi.Truncate(s[:cut], limit, ""); len(head) < cut {
			return head
		}
	}
	return ansi.Truncate(s, limit, "")
}

// known returns s in style, shown as text shows it, or the word Unknown
// in alt when s is empty.
func (c Ctx) known(s string, style, alt lipgloss.Style) line {
	if s = c.text(s); s != "" {
		return line{{s, style}}
	}
	return line{{unknown, alt}}
}
