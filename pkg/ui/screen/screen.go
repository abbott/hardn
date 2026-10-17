package screen

import (
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// join joins the blocks that render something with theme.SectionGap
// blank lines, leaving out each empty block with its gap.
func join(blocks ...string) string {
	return glue(strings.Repeat("\n", theme.SectionGap+1), blocks)
}

// stack joins the blocks that render something line under line, with no
// blank line between them: one group of the main menu's compact form.
func stack(blocks ...string) string {
	return glue("\n", blocks)
}

// lines counts the lines of a drawing.
func lines(s string) int { return strings.Count(s, "\n") + 1 }

// glue joins the non-empty blocks with sep.
func glue(sep string, blocks []string) string {
	var kept []string
	for _, b := range blocks {
		if b != "" {
			kept = append(kept, b)
		}
	}
	return strings.Join(kept, sep)
}

// withMenu renders a menu screen as nav.Run draws it: above and the menu,
// joined as blocks, then theme.PromptGap blank lines and the PromptLine
// labelled label, its hint generated from the menu's keys and the
// navigation inputs, as nav.Menu.Accepts generates it. With nothing above
// the prompt, the prompt is alone.
func withMenu(c component.Ctx, above string, menu component.Menu, label string) string {
	prompt := component.PromptLine{Label: label, Accepts: append(menu.Bindings(), keys.Navigation()...)}.Render(c)
	body := join(above, menu.Render(c))
	if body == "" {
		return prompt
	}
	return body + strings.Repeat("\n", theme.PromptGap+1) + prompt
}
