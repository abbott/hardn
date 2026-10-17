package component

import (
	"math"
	"slices"
	"strconv"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// AnsweredField is a question with the answer typed to it, as Figma's
// forms leave each one on screen: "1. Username: george", or a
// confirmation's "Disable SSH login for 'root'? (y/n): y". Here with no
// colour, the y/N questions' default in capitals (keys.YesNo, brief 1C):
//
//	0 2   6
//	   1. Username: george
//	   2. Grant sudo privilege? [y/N]: y
//	  Disable SSH login for 'root'? [y/N]: y
//
// A numbered field's number is right-aligned to end at column 4, as a
// menu key is, and its question starts at theme.LabelCol; an unnumbered
// question starts at theme.Indent (DEV-036). The question, the hint and
// the colon are laid out as PromptLine lays out its label and hint, the
// hint generated from Accepts, so a question reads the same asked and
// answered; then the answer follows one cell after the colon in
// Field.Answer, muted (drift D4). When the whole line does not fit, the
// answer goes under the question at theme.StackCol if both then fit
// whole; failing that, the question is cut as PromptLine's is, before the
// hint, to keep the answer beside it; and failing that, as for an answer
// wider than the terminal, the answer goes under the question and both
// are cut with the ellipsis glyph.
//
// With no answer the line ends at the colon, as the question is asked; a
// form echoes the value it applied, such as the default taken by a bare
// Enter. A field with no question renders as the empty string.
type AnsweredField struct {
	// Number is the field's place in its form, shown as "1."; zero or
	// less shows none.
	Number int
	// Label is the question, such as "Username" or "Grant sudo
	// privilege?"; the colon is added.
	Label string
	// Accepts are the inputs the question accepted; their hint is shown in
	// brackets before the colon. Empty shows none, as for a text field.
	Accepts keys.Set
	// Answer is what was typed, or the value applied; empty shows none.
	Answer string
}

// Render renders the field for c. See AnsweredField for the layout.
func (vm AnsweredField) Render(c Ctx) string {
	t, w := c.Theme.Field, c.width()
	label := clean(vm.Label)
	if label == "" {
		return ""
	}
	numbered := vm.Number > 0
	head := line{gap(theme.Indent - 1)} // ask puts a cell before the label
	if numbered {
		n := strconv.Itoa(vm.Number) + "."
		head = line{gap(theme.MarkerCol + theme.MarkerWidth - lipgloss.Width(n)), {n, t.Number}}
	}
	hint, answer := clean(vm.Accepts.Hint()), clean(vm.Answer)
	asked, shown := c.ask(head, label, hint, t.Label, t.Hint, w)
	if !shown && hint == "" && !numbered {
		// ask left nothing but the indent: cut the question itself, so
		// that as much of it as fits, or of the ellipsis, shows.
		asked = slices.Concat(head, line{gap(1), {label + ":", t.Label}})
	}
	if answer == "" {
		return c.fit(asked, w)
	}
	a, aw := span{answer, t.Answer}, lipgloss.Width(answer)
	question, _ := c.ask(head, label, hint, t.Label, t.Hint, math.MaxInt)
	switch {
	case question.width()+1+aw <= w:
		return append(question, gap(1), a).String()
	case question.width() <= w && theme.StackCol+aw <= w:
		return question.String() + "\n" + line{gap(theme.StackCol), a}.String()
	}
	if beside, shown := c.ask(head, label, hint, t.Label, t.Hint, w-1-aw); shown {
		return c.fit(append(beside, gap(1), a), w)
	}
	return c.fit(asked, w) + "\n" + c.fit(line{gap(theme.StackCol), a}, w)
}
