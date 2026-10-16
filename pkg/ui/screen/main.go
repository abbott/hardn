package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// MainMenu is the main-menu layout (brief 1B), after Figma's 00-TUI-MAIN.
// Here its full form with no colour and ASCII glyphs, Figma's fixture
// (MainMenuFixture) cut short, the blank lines being theme.SectionGap:
//
//	hardn  v0.4.0              https://github.com/abbott/hardn
//
//	# Debian 12 (Bookworm) #########################################
//
//	  [BOS-SAN-L01]            192.0.2.10
//	                           182 days, 2 hours, 26 minutes
//
//	  [*] Risk Level           Moderate ~ medium security
//
//	  [+] User                 Configured non-root, sudo
//	  [ ] Auto Updates         Not Configured
//
//	----------------------------------------------------------------
//
//	  Select an option         Dry-run Mode Disabled
//
//	   1) User Management      Create, Configure (sudo, SSH keys)
//	  10) Logs                 View log file
//
//	   0) Exit                 q to quit
//
//	  > Enter your choice [0-10, b or q]:
//
// The blocks follow Figma's order: Header, ContextBar, HostBlock,
// RiskSummary, StatusList, Divider, the MenuHeading with the
// ModeIndicator beside it, Menu, then the PromptLine after
// theme.PromptGap blank lines. An empty StatusList is left out with its
// gap. Figma's 35 lines fit a terminal from theme.CompactBelow rows.
//
// On a terminal shorter than that (term.Env.TTY, Env.Height under
// theme.CompactBelow) the main menu renders its compact form, which fits
// theme.CompactBudget lines, an 80x24 console, with Figma's content
// (DEV-052): the Header and the Divider are left out; the ContextBar and
// the HostBlock are one group and the RiskSummary and the StatusList
// another, each with no blank line inside it; and the menu is laid out in
// two columns, its items' keys and labels alone (component.Menu.Compact).
// The host, the risk, every status row, the mode and every item stay on
// screen. Output that is not a terminal has no height to fit, and gets
// the full form.
type MainMenu struct {
	// Header is the wordmark, the version and the repository link.
	Header component.Header
	// Context is the distribution bar.
	Context component.ContextBar
	// Host is the hostname badge, the address and the uptime.
	Host component.HostBlock
	// Risk is the risk level and its factors.
	Risk component.RiskSummary
	// Status is the controls' status rows; with none the block is left
	// out.
	Status component.StatusList
	// Heading is the menu heading's title, such as "Select an option".
	Heading string
	// Mode is the dry-run mode, always shown beside the heading.
	Mode component.ModeIndicator
	// Menu is the numbered menu and its Exit item. Its Compact is set by
	// the layout, from the terminal's height.
	Menu component.Menu
	// Prompt is the prompt's label, such as "Enter your choice".
	Prompt string
}

// Render renders the main menu for c, down to its prompt. See MainMenu for
// the layout and its compact form.
func (vm MainMenu) Render(c component.Ctx) string {
	menu := vm.Menu
	menu.Compact = compact(c.Env)
	return withMenu(c, vm.above(c), menu, vm.Prompt)
}

// Screen returns what nav.Run draws for the main menu, with m as its menu
// in place of vm.Menu: Above holds every block down to the menu heading,
// and Compact is set on a terminal too short for the full form, so that
// nav.Run draws what Render renders. Call it in each round of Run's draw,
// so that the screen shows the state the last action left.
func (vm MainMenu) Screen(c component.Ctx, m nav.Menu) nav.Screen {
	return nav.Screen{Above: vm.above(c), Menu: m, Label: vm.Prompt, Compact: compact(c.Env)}
}

// above renders every block above the menu: the full form's, or the
// compact form's on a short terminal.
func (vm MainMenu) above(c component.Ctx) string {
	mode := vm.Mode
	heading := component.MenuHeading{Title: vm.Heading, Mode: &mode}.Render(c)
	if compact(c.Env) {
		return join(
			stack(vm.Context.Render(c), vm.Host.Render(c)),
			stack(vm.Risk.Render(c), vm.Status.Render(c)),
			heading,
		)
	}
	return join(
		vm.Header.Render(c),
		vm.Context.Render(c),
		vm.Host.Render(c),
		vm.Risk.Render(c),
		vm.Status.Render(c),
		component.Divider{}.Render(c),
		heading,
	)
}

// compact reports whether the main menu takes its compact form in env: on
// a terminal shorter than theme.CompactBelow. Output that is not a
// terminal has no height to fit. An unknown height counts as
// term.DefaultHeight, as term.Resolve counts it.
func compact(env term.Env) bool {
	h := env.Height
	if h < 1 {
		h = term.DefaultHeight
	}
	return env.TTY && h < theme.CompactBelow
}
