package screen

import (
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// TitleTrail is whether a sub-screen's title bar shows the screen's
// section trail before its title: "User Management › Manage User" in
// place of Figma's "Manage User".
//
// DISABLED 2026-10-17: proposal P-08, decided by the maintainer. Every
// sub-screen, the sub-menu and the workflow step, opens with Figma's
// single title bar on bar.bg, showing the screen's own title, and the
// trail, which merges the brief's Breadcrumb into the bar, is off by
// default. Flip it to true to show "User Management › Manage User" in the
// bar: the layouts keep each screen's parents in their view-models
// (component.TitleBar.Parents), so the constant is the only change needed,
// besides the goldens. A trail never names the product: no "hardn"
// segment. The main menu has no title bar, so it is the same either way.
const TitleTrail = false

// titleBar returns the title bar a sub-screen draws: bar with its trail
// when withTrail is set, else its title alone. The layouts pass
// TitleTrail; the tests pass true too, to hold the trail's form.
func titleBar(bar component.TitleBar, withTrail bool) component.TitleBar {
	if !withTrail {
		bar.Parents = nil
	}
	return bar
}

// join joins the blocks that render something with theme.SectionGap
// blank lines, leaving out each empty block with its gap.
func join(blocks ...string) string {
	return glue(strings.Repeat("\n", theme.SectionGap+1), blocks)
}

// stack joins the blocks that render something line under line, with no
// blank line between them: in the main menu's compact form, the risk line
// and the status rows, and the menu heading and the menu.
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
// joined as blocks, or a compact menu (menu.Compact) on the line under
// above, as nav.Screen.Compact draws it; then theme.PromptGap blank lines
// and the PromptLine labelled label, its hint generated from the menu's
// keys and the navigation inputs, as nav.Menu.Accepts generates it. With
// nothing above the prompt, the prompt is alone.
func withMenu(c component.Ctx, above string, menu component.Menu, label string) string {
	prompt := component.PromptLine{Label: label, Accepts: append(menu.Bindings(), keys.Navigation()...)}.Render(c)
	gather := join
	if menu.Compact {
		gather = stack
	}
	body := gather(above, menu.Render(c))
	if body == "" {
		return prompt
	}
	return body + strings.Repeat("\n", theme.PromptGap+1) + prompt
}
