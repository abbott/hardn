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
//	  > Enter selection [0-10 or q]:
//
// The blocks follow Figma's order: Header, ContextBar, HostBlock,
// RiskSummary, StatusList, Divider, the MenuHeading with the
// ModeIndicator beside it, Menu, then the PromptLine after
// theme.PromptGap blank lines. An empty StatusList is left out with its
// gap. From theme.StackBelow columns up Figma's content takes 35 lines,
// which fit a terminal from theme.CompactBelow rows; below that width the
// rows stack (DEV-021), and at 60 columns it takes 57.
//
// On a terminal (term.Env.TTY) shorter than theme.CompactBelow, or too
// short for the full form, prompt included, the main menu renders its
// compact form, which fits theme.CompactBudget lines, an 80x24 console,
// with Figma's content from theme.StackBelow columns up (DEV-052): the
// Header and the Divider are left out; the ContextBar keeps the blank line
// under it, as a bar always has, Figma's and the sub-screens' title bar
// alike; the RiskSummary and the StatusList are one group, with no blank
// line inside it; the menu's items start on the line under the heading,
// with no blank line between; and the menu is laid out in two columns,
// its items' keys and labels alone (component.Menu.Compact). Here plain,
// at 80x24, cut short:
//
//	# Debian 12 (Bookworm) #########################################
//
//	  [BOS-SAN-L01]            192.0.2.10
//	                           182 days, 2 hours, 26 minutes
//
//	  [*] Risk Level           Moderate ~ medium security
//	  [+] User                 Configured non-root, sudo
//	  [ ] Auto Updates         Not Configured
//
//	  Select an option         Dry-run Mode Disabled
//	   1) User Management       6) Dry-Run
//	   5) Backup               10) Logs
//
//	   0) Exit                 q to quit
//
//	  > Enter selection [0-10 or q]:
//
// The host, the risk, every status row, the mode and every item stay on
// screen, Figma's content in theme.CompactBudget lines: the blank line
// the heading leaves out is the one the bar keeps. Below
// theme.StackBelow columns the compact form stacks its rows too, 34 lines
// at 60 columns, so a 60-column terminal shows the full form from 57 rows
// and the compact form below. Output that is not a terminal has no height
// to fit, and gets the full form. StatusBlocks draws the blocks above the
// heading alone, in the same forms, for a caller that draws its own menu
// under them: hardn's live main menu, until its menu moves onto this
// layout (slice 2b).
type MainMenu struct {
	// Header is the wordmark, the version and the repository link.
	Header component.Header
	// Context is the distribution bar.
	Context component.ContextBar
	// Host is the hostname badge, the address and the uptime.
	Host component.HostBlock
	// Risk is the risk level, one line.
	Risk component.RiskSummary
	// Status is the controls' status rows, the risk level's factors as
	// lines under the rows they are about; with no rows the block is left
	// out.
	Status component.StatusList
	// Heading is the menu heading's title, such as "Select an option".
	Heading string
	// Mode is the dry-run mode, always shown beside the heading.
	Mode component.ModeIndicator
	// Menu is the numbered menu and its Exit item. Its Compact is set by
	// the layout, from the terminal's height.
	Menu component.Menu
	// Prompt is the prompt's label, such as "Enter selection".
	Prompt string
}

// Render renders the main menu for c, down to its prompt. See MainMenu for
// the layout and its compact form.
func (vm MainMenu) Render(c component.Ctx) string {
	if out, ok := vm.full(c); ok {
		return out
	}
	return vm.form(c, true)
}

// Screen returns what nav.Run draws for the main menu, with m as its menu
// in place of vm.Menu: Above holds every block down to the menu heading,
// and Compact, which lays the menu out in two columns on the line under
// the heading, is set on a terminal too short for the full form drawn with
// m, the menu nav.Run draws, so that nav.Run draws what Render renders
// for a view-model whose Menu holds m's items. Call it in each round of
// Run's draw, so that the screen shows the state the last action left.
func (vm MainMenu) Screen(c component.Ctx, m nav.Menu) nav.Screen {
	vm.Menu = m.View()
	short := vm.compact(c)
	return nav.Screen{Above: vm.above(c, short), Menu: m, Label: vm.Prompt, Compact: short}
}

// form renders the main menu down to its prompt, in its compact form when
// short is set and in its full form otherwise.
func (vm MainMenu) form(c component.Ctx, short bool) string {
	menu := vm.Menu
	menu.Compact = short
	return withMenu(c, vm.above(c, short), menu, vm.Prompt)
}

// above renders every block above the menu: the full form's, or the
// compact form's when short is set, whose menu is drawn on the line under
// the heading (withMenu, nav.Screen.Compact).
func (vm MainMenu) above(c component.Ctx, short bool) string {
	mode := vm.Mode
	heading := component.MenuHeading{Title: vm.Heading, Mode: &mode}.Render(c)
	return join(append(vm.blocks(c, short), heading)...)
}

// blocks renders the blocks above the menu heading, which the status
// model fills: the full form's Header, ContextBar, HostBlock,
// RiskSummary, StatusList and Divider, or, when short is set, the compact
// form's ContextBar, HostBlock, and the RiskSummary and StatusList as one
// group.
func (vm MainMenu) blocks(c component.Ctx, short bool) []string {
	if short {
		return []string{vm.Context.Render(c), vm.Host.Render(c), stack(vm.Risk.Render(c), vm.Status.Render(c))}
	}
	return []string{vm.Header.Render(c), vm.Context.Render(c), vm.Host.Render(c), vm.Risk.Render(c), vm.Status.Render(c),
		component.Divider{}.Render(c)}
}

// StatusBlocks renders the blocks the status model fills, down to the full
// form's Divider, for a caller that draws its own menu under them, its
// heading and prompt included, in below lines, after theme.SectionGap
// blank lines: hardn's live main menu, whose heading, mode indicator,
// items and prompt are still pkg/menu's (refactor plan slice 2,
// next/s2-menu-live; slice 2b). Their form is the one Render would choose
// for the screen they make with that menu: the full form's blocks on
// output that is not a terminal, and on a terminal from
// theme.CompactBelow rows that holds them, the gap and below; else the
// compact form's (see MainMenu). vm.Heading, Mode, Menu and Prompt are
// not drawn.
func (vm MainMenu) StatusBlocks(c component.Ctx, below int) string {
	if !c.Env.TTY || height(c) >= theme.CompactBelow {
		if full := join(vm.blocks(c, false)...); fullFits(c, lines(full)+theme.SectionGap+below) {
			return full
		}
	}
	return join(vm.blocks(c, true)...)
}

// compact reports whether the main menu takes its compact form in c.
func (vm MainMenu) compact(c component.Ctx) bool {
	_, ok := vm.full(c)
	return !ok
}

// full reports whether the main menu takes its full form in c, and if so
// returns it: when it fits (fullFits), prompt included. Below
// theme.StackBelow columns the rows stack, and the full form may not fit
// 40 rows (57 lines at 60 columns).
func (vm MainMenu) full(c component.Ctx) (string, bool) {
	if c.Env.TTY && height(c) < theme.CompactBelow {
		return "", false
	}
	out := vm.form(c, false)
	return out, fullFits(c, lines(out))
}

// fullFits reports whether a full form of n lines is drawn in c: on
// output that is not a terminal, which has no height to fit, and on a
// terminal from theme.CompactBelow rows whose height holds the n lines.
func fullFits(c component.Ctx, n int) bool {
	return !c.Env.TTY || height(c) >= theme.CompactBelow && n <= height(c)
}

// height is c's terminal height; an unknown one counts as
// term.DefaultHeight, as term.Resolve counts it.
func height(c component.Ctx) int {
	if c.Env.Height < 1 {
		return term.DefaultHeight
	}
	return c.Env.Height
}
