package component

import (
	"slices"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// PromptLine is the prompt under a menu or a question: Glyphs.Prompt, the
// label, the hint in brackets and a colon, as Figma draws
// "❯ Enter your choice [0-10 or q]:". Here with no colour and ASCII
// glyphs:
//
//	0 2
//	  > Enter your choice [0-10 or q]:
//
// The hint is generated from Accepts (keys.Set.Hint), never written by
// hand. The line starts at theme.Indent (C11, DEV-029) and ends at the
// colon, with no trailing space: the line reader writes one space after
// it, where the terminal's own cursor then stands (C8, DEV-029). On a
// terminal too narrow for the whole line the label is cut first with the
// ellipsis glyph, and left out once none of its letters fits, so the hint
// stays whole as long as it can.
type PromptLine struct {
	// Label asks the question, such as "Enter your choice". With no hint,
	// the colon follows it in its style.
	Label string
	// Accepts are the inputs the prompt accepts; their hint is shown in
	// brackets. Empty shows no hint.
	Accepts keys.Set
}

// Render renders the prompt for c. See PromptLine for the layout.
func (vm PromptLine) Render(c Ctx) string {
	t, w := c.Theme.Prompt, c.width()
	head := line{gap(theme.Indent), {c.Theme.Glyphs.Prompt, t.Glyph}}
	out, _ := c.ask(head, clean(vm.Label), clean(vm.Accepts.Hint()), t.Label, t.Hint, w)
	return c.fit(out, w)
}

// ask returns head, then a space and the label, then the hint in brackets
// and the colon or, with no hint, the colon alone in the label's style, in
// at most limit cells. When they do not fit, the label is cut first with
// the ellipsis glyph, and left out once none of its letters fits, so the
// hint stays whole as long as it can; shown reports whether any of the
// label is left. With neither label nor hint, it is head alone.
// PromptLine and AnsweredField share it, so a question reads the same
// asked and answered.
func (c Ctx) ask(head line, label, hint string, labelStyle, hintStyle lipgloss.Style, limit int) (out line, shown bool) {
	end := line{{":", labelStyle}}
	if hint != "" {
		end = line{gap(1), {"[" + hint + "]:", hintStyle}}
	}
	room := limit - head.width() - 1 - end.width()
	// The cut label shows some of the label when it is whole or longer
	// than the ellipsis: a letter wider than the room left beside the
	// ellipsis, such as a double-width one, leaves the ellipsis alone.
	if cut := c.truncate(label, room); label != "" && (cut == label || lipgloss.Width(cut) > lipgloss.Width(c.Theme.Glyphs.Ellipsis)) {
		return slices.Concat(head, line{gap(1), {cut, labelStyle}}, end), true
	}
	if hint != "" {
		return slices.Concat(head, end), false
	}
	return head, false
}
