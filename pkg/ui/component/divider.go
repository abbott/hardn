package component

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Divider is the rule between the main menu's status rows and its menu: a
// run of Glyphs.Rule in the divider colour from column 0, across the width
// a bar fills. That is the content width under theme.BarsFillContent, as
// Figma draws the rule across the 64-cell panel, or the terminal's width if
// that rule is refused (DEV-026). It has no states.
type Divider struct{}

// Render renders the rule for c.
func (Divider) Render(c Ctx) string {
	rule := c.Theme.Glyphs.Rule
	return span{strings.Repeat(rule, c.barWidth()/lipgloss.Width(rule)), c.Theme.Divider}.String()
}
