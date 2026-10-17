package component

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// FieldHead is what a form field shows above a prompt line of its own
// when it has more to show than its question, or when its question would
// be cut beside its hint: the title, then the description of what to
// type, then the options to choose from. Here with no colour, a head with
// a description and, under it, one with options:
//
//	0 2   6
//	   1. Username
//	      Specify username to create
//
//	   2. Select a user
//
//	   1) george
//	   2) alice
//
// The title is laid out as an AnsweredField's question, the number
// right-aligned to end at column 4 and the title at theme.LabelCol, or
// unnumbered at theme.Indent, but with no hint and no colon: the prompt
// line under the head asks it, and the answer is echoed as an
// AnsweredField (pkg/ui/form). A title too long for the line is cut with
// the ellipsis glyph. The description is prose in Field.Description,
// wrapped at spaces from theme.LabelCol. The options are a Menu's rows,
// set apart from what is above them by theme.SectionGap blank lines, as
// Figma sets a menu apart from its heading (USER-MANAGE_USER-SELECT_USER),
// keyed and laid out as the Menu lays out its items, stacked as a whole
// as it stacks them (DEV-046). A field with no title renders as the empty
// string.
type FieldHead struct {
	// Number is the field's place in its form, shown as "2."; zero or
	// less shows none.
	Number int
	// Title is the question, such as "Select a user".
	Title string
	// Description says what to type, such as "Specify username to
	// create"; empty shows none.
	Description string
	// Options are the choices, each keyed with its number from 1, as a
	// menu's items are; empty shows none.
	Options []MenuItem
}

// Render renders the head for c. See FieldHead for the layout.
func (vm FieldHead) Render(c Ctx) string {
	t, w := c.Theme.Field, c.width()
	title := c.text(vm.Title)
	if title == "" {
		return ""
	}
	head := line{gap(theme.Indent)}
	if vm.Number > 0 {
		n := strconv.Itoa(vm.Number) + "."
		head = line{gap(theme.MarkerCol + theme.MarkerWidth - lipgloss.Width(n)), {n, t.Number}, gap(1)}
	}
	out := []string{c.fit(append(head, span{title, t.Label}), w)}
	if d := c.text(vm.Description); d != "" {
		for _, l := range wrap(d, w-theme.LabelCol) {
			out = append(out, c.fit(line{gap(theme.LabelCol), {l, t.Description}}, w))
		}
	}
	if options := (Menu{Items: vm.Options}).Render(c); options != "" {
		out = append(out, strings.Repeat("\n", theme.SectionGap)+options)
	}
	return strings.Join(out, "\n")
}
