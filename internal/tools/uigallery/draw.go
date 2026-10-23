package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The gallery's own drawing, for the rows its mocks draw on the
// components' grid (the glyphs mode, glyphs.go): the compare mode's
// helpers (next/p3-gallery-engine), kept when that mode went, its mock of
// P-42 being the components' own drawing since next/s2-row-grammar.
//
// The gallery may not name Lip Gloss's types (depguard), so it styles its
// runs through the theme's slots' Render methods, held as values (paint),
// and measures and cuts with x/ansi.

// sideGap is the cells between two renders set side by side.
const sideGap = 4

// sectionBreak is what sets two blocks apart: theme.SectionGap blank
// lines between them, as the components and pkg/output set them.
var sectionBreak = strings.Repeat("\n", theme.SectionGap+1)

// apart joins blocks with theme.SectionGap blank lines between them.
func apart(blocks ...string) string { return strings.Join(blocks, sectionBreak) }

// paint is a style slot's Render method, held as a value, which styles a
// run of text without naming Lip Gloss's Style.
type paint func(...string) string

// piece is a run of plain text and the slot that paints it; a nil paint
// leaves it as it is.
type piece struct {
	text  string
	paint paint
}

// pad is n spaces in no style.
func pad(n int) piece { return piece{text: strings.Repeat(" ", max(0, n))} }

// draw renders a line of pieces. An empty piece renders as nothing, rather
// than as an empty pair of escape sequences.
func draw(line ...piece) string {
	var b strings.Builder
	for _, p := range line {
		switch {
		case p.text == "":
		case p.paint == nil:
			b.WriteString(p.text)
		default:
			b.WriteString(p.paint(p.text))
		}
	}
	return b.String()
}

// widthOf is the cells line takes.
func widthOf(line []piece) int {
	w := 0
	for _, p := range line {
		w += ansi.StringWidth(p.text)
	}
	return w
}

// termWidth is env's width, or term.DefaultWidth when it has none, as the
// components take it.
func termWidth(env term.Env) int {
	if env.Width < 1 {
		return term.DefaultWidth
	}
	return env.Width
}

// contentWidth is the width the components lay out in: the terminal's,
// capped at theme.ContentWidth under theme.CapContent.
func contentWidth(env term.Env) int {
	if theme.CapContent {
		return min(termWidth(env), theme.ContentWidth)
	}
	return termWidth(env)
}

// cut shortens plain text s to at most limit cells, ending it with the
// ellipsis glyph when it is cut, as the components cut: the spaces before
// the cut are dropped, and with no text left the result is as much of the
// glyph as fits.
func cut(c component.Ctx, s string, limit int) string {
	if ansi.StringWidth(s) <= limit {
		return s
	}
	e := c.Theme.Glyphs.Ellipsis
	kept := strings.TrimRight(ansi.Truncate(s, max(0, limit-ansi.StringWidth(e)), ""), " ")
	if kept == "" {
		return ansi.Truncate(e, max(0, limit), "")
	}
	return kept + e
}

// wrapText breaks plain text s into lines of at most limit cells at its
// spaces alone, and inside a word only when the word is wider than a line
// on its own, which it then fills line by line.
// Unlike the components' wrap (x/ansi's and Lip Gloss's), it never breaks
// after a hyphen, so that a change's id stays whole. The words are s's
// runs of non-spaces, one space apart on a line. With limit under 1, s is
// one line.
func wrapText(s string, limit int) []string {
	if limit < 1 {
		return []string{s}
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && ansi.StringWidth(line)+1+ansi.StringWidth(word) <= limit {
			line += " " + word
			continue
		}
		if line != "" {
			out = append(out, line)
		}
		for ansi.StringWidth(word) > limit {
			head := ansi.Truncate(word, limit, "")
			if head == "" {
				head = string([]rune(word)[:1])
			}
			out, word = append(out, head), word[len(head):]
		}
		line = word
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// markerTo returns glyph in p centred in the marker column, as the
// components place a marker (DEV-020), and the spaces that bring the line
// to theme.LabelCol.
func markerTo(glyph string, p paint) []piece {
	lead := theme.MarkerCol + max(0, theme.MarkerWidth-ansi.StringWidth(glyph))/2
	return []piece{pad(lead), {glyph, p}, pad(theme.LabelCol - lead - ansi.StringWidth(glyph))}
}

// prose renders text as muted prose, as an info message is (P-42 R5):
// Text.Muted from theme.Indent, wrapped at spaces to the content width,
// with no glyph and no word.
func prose(c component.Ctx, text string) string {
	var out []string
	for _, l := range wrapText(text, contentWidth(c.Env)-theme.Indent) {
		out = append(out, draw(pad(theme.Indent), piece{l, c.Theme.Text.Muted.Render}))
	}
	return strings.Join(out, "\n")
}

// beside sets the lines of left and right side by side: each line of left
// padded to width cells, gap cells, then the line of right. A line with
// nothing on the right keeps no trailing spaces.
func beside(left, right string, width, gap int) string {
	l, r := strings.Split(left, "\n"), strings.Split(right, "\n")
	out := make([]string, max(len(l), len(r)))
	for i := range out {
		var a, b string
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			b = r[i]
		}
		out[i] = a
		if b != "" {
			out[i] += strings.Repeat(" ", max(0, width-ansi.StringWidth(a))+gap) + b
		}
	}
	return strings.Join(out, "\n")
}
