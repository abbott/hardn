package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// CompactMainMenu is whether the main menu takes its compact form on a
// short terminal.
//
// DISABLED 2026-10-23. Flip to true to restore. With it true, the main
// menu takes its compact form on a terminal (term.Env.TTY) shorter than
// theme.CompactBelow, 40 rows, or too short for its full form, prompt
// included: the Header and the Divider left out, the risk line and the
// status rows one group, and the menu in two columns of keys and labels on
// the line under its heading (component.Menu.Compact, nav.Screen.Compact),
// so that Figma's content fits theme.CompactBudget lines, an 80x24
// console (DEV-052); and StatusBlocks draws the blocks a caller's own menu
// goes under in the same forms. It is off because the maintainer adopted
// slice 2b's decision 5 with the full form at every height (2026-10-23):
// "i'd like to avoid using a compact menu. if we need to reduce row
// counts, we can explore that later, but a two-column menu is not a
// desired method" (refactor plan §3.15, its compact main menu withdrawn;
// docs/engine/gallery-dryrun.md §4). The compact form's code, its tests
// and its goldens are kept: the layouts take the gate as a parameter
// (render, screen, statusBlocks), which Render, Screen and StatusBlocks
// pass this constant and the tests pass true, as TitleTrail's tests do.
// Flipped, Render draws what main-menu/00-tui-main-compact's golden
// holds, and only golden comparisons fail: regenerate them with make
// golden-update, then go test ./pkg/menu -update, whose goldens are its
// own. TestHeights follows the constant; TestFullAtEveryHeight and
// pkg/menu's TestMainMenuHeights, which pin the full form, skip. Fewer
// rows, when wanted, are for later, by other means.
const CompactMainMenu = false

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
//	  Select an option         Mode Live
//
//	   1) User Management      Create, Configure (sudo, SSH keys)
//	  10) Logs                 View log file
//
//	   0) Exit                 q to quit
//
//	    > Enter selection [0-10 or q]:
//
// The blocks follow Figma's order: Header, ContextBar, HostBlock,
// RiskSummary, StatusList, Divider, the MenuHeading with the
// ModeIndicator beside it, Menu, then the PromptLine after
// theme.PromptGap blank lines. An empty StatusList is left out with its
// gap. An Alert, which Figma does not draw, stands between the StatusList
// and the Divider, in the compact form after the rows: three lines and a
// gap more. From theme.StackBelow columns up Figma's content takes 35 lines,
// and a host's eight rows, DNS among them, 36, with each line under a row
// one more; below that width the rows stack (DEV-021), and at 60 columns
// Figma's content takes 57. The main menu takes this form at every height
// (CompactMainMenu): on a terminal shorter than its lines, its top
// scrolls off, the prompt staying on the terminal's last line.
//
// Gated by CompactMainMenu, its compact form: on a terminal shorter than
// theme.CompactBelow, or too short for the full form, prompt included, it
// fits theme.CompactBudget lines, an 80x24 console, with Figma's content
// from theme.StackBelow columns up (DEV-052); a host's eight rows, DNS
// among them, take one line more, 25, so that at 80x24 its first line
// scrolls off, and each line under a row another. In the compact form the
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
//	  Select an option         Mode Live
//	   1) User Management       6) Dry-Run
//	   5) Backup               10) Logs
//
//	   0) Exit                 q to quit
//
//	    > Enter selection [0-10 or q]:
//
// The host, the risk, every status row, the mode and every item stay on
// screen, Figma's content in theme.CompactBudget lines: the blank line
// the heading leaves out is the one the bar keeps. Below
// theme.StackBelow columns the compact form stacks its rows too, 34 lines
// at 60 columns, so a 60-column terminal shows the full form from 57 rows
// and the compact form below. Output that is not a terminal has no height
// to fit, and gets the full form. StatusBlocks draws the blocks above the
// heading alone, in the same forms, for a caller that draws its own menu
// under them, as hardn's live main menu did until its menu moved onto this
// layout (slice 2b, next/s2b-menu).
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
	// Alert is a fact the main menu raises between the status rows and the
	// Divider, nil for none (Alert).
	Alert *Alert
	// Heading is the menu heading's title, such as "Select an option".
	Heading string
	// Mode is the mode hardn runs in, live or plan only, and what set
	// plan only, always shown beside the heading.
	Mode component.ModeIndicator
	// Menu is the numbered menu and its Exit item. Its Compact is set by
	// the layout, from the terminal's height, where CompactMainMenu allows
	// the compact form.
	Menu component.Menu
	// Prompt is the prompt's label, such as "Enter selection".
	Prompt string
}

// Render renders the main menu for c, down to its prompt: its full form,
// or, where CompactMainMenu allows it, the form the terminal calls for.
// See MainMenu.
func (vm MainMenu) Render(c component.Ctx) string {
	return vm.render(c, CompactMainMenu)
}

// render renders the main menu for c in its full form, or in its compact
// form where compact is set and the terminal calls for it. Render passes
// CompactMainMenu; the tests pass true too, to hold the compact form while
// it is gated.
func (vm MainMenu) render(c component.Ctx, compact bool) string {
	if out, ok := vm.full(c, compact); ok {
		return out
	}
	return vm.form(c, true)
}

// Screen returns what nav.Run draws for the main menu, with m as its menu
// in place of vm.Menu: Above holds every block down to the menu heading,
// and Compact, which lays the menu out in two columns on the line under
// the heading, is set where CompactMainMenu allows it on a terminal too
// short for the full form drawn with m, the menu nav.Run draws, so that
// nav.Run draws what Render renders for a view-model whose Menu holds m's
// items. Call it in each round of Run's draw, so that the screen shows
// the state the last action left.
func (vm MainMenu) Screen(c component.Ctx, m nav.Menu) nav.Screen {
	return vm.screen(c, m, CompactMainMenu)
}

// screen is Screen with the compact form allowed where compact is set:
// Screen passes CompactMainMenu, the tests true too.
func (vm MainMenu) screen(c component.Ctx, m nav.Menu, compact bool) nav.Screen {
	vm.Menu = m.View()
	short := vm.compact(c, compact)
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
// RiskSummary, StatusList, Alert and Divider, or, when short is set, the
// compact form's ContextBar, HostBlock, the RiskSummary and StatusList as
// one group, and the Alert. The Alert's route is vm.Menu's item.
func (vm MainMenu) blocks(c component.Ctx, short bool) []string {
	alert := vm.Alert.render(c, vm.Menu)
	if short {
		return []string{vm.Context.Render(c), vm.Host.Render(c), stack(vm.Risk.Render(c), vm.Status.Render(c)), alert}
	}
	return []string{vm.Header.Render(c), vm.Context.Render(c), vm.Host.Render(c), vm.Risk.Render(c), vm.Status.Render(c),
		alert, component.Divider{}.Render(c)}
}

// selectOption leads an alert's route to its fix: the menu's item, by its
// key and label (the maintainer's words, 2026-11-08).
const selectOption = "Select option:"

// CVEUnfixed is the title of the alert for an sshd that lacks
// CVE-2026-123158's fix, on the main menu and SSH Login's screen.
const CVEUnfixed = "CVE-2026-123158"

// Alert is a fact the main menu raises, such as an sshd that lacks a
// CVE's fix: a warning callout between the status rows and the Divider,
// SectionGap blank lines from each, its bar in the marker column's centre
// (column 3, under a one-cell status glyph, inside "[+]") and its text at
// column 5, as the maintainer drew it (2026-11-08; DEV-075). Here with no
// colour and ASCII glyphs:
//
//	  [-] AppArmor             Not Applicable Alpine default
//
//	   # Warning: CVE-2026-123158
//	   # Upgrade openssh.
//	   # Select option: 2) SSH Login
//
//	----------------------------------------------------------------
//
// The title follows the warning's word; the fix is a body line, in the
// body's colour, and the route's lead too, the item's key and label in
// the menu's styles (Callout.Choice). The route names the menu's own key
// for Item, so it cannot name another; with no item of that label it is
// left out. SSH Login's screen raises the same alert with no fix, its
// route to Upgrade openssh (SubMenu.Alert).
type Alert struct {
	// Title names the fact: "CVE-2026-123158".
	Title string
	// Fix is what to do: "Upgrade openssh."; empty for none.
	Fix string
	// Item is the label of the main menu's item where the fix is done:
	// "SSH Login".
	Item string
}

// render renders a for c, the route from m's item labelled a.Item; ""
// for a nil a.
func (a *Alert) render(c component.Ctx, m component.Menu) string {
	if a == nil {
		return ""
	}
	vm := component.Callout{Kind: component.MessageWarn, Title: a.Title, Body: []string{a.Fix},
		Indent: theme.MarkerCol + theme.MarkerWidth/2, ChoiceLead: selectOption}
	for _, it := range m.Items {
		if it.Label == a.Item {
			vm.Choice = &it
			break
		}
	}
	return vm.Render(c)
}

// StatusBlocks renders the blocks the status model fills, down to the full
// form's Divider, for a caller that draws its own menu under them, its
// heading and prompt included, in below lines, after theme.SectionGap
// blank lines. hardn's live main menu drew its legacy menu under them so
// from refactor plan slice 2's next/s2-menu-live until slice 2b's
// next/s2b-menu put that menu on this layout (Screen); nothing in hardn
// calls it since, and it is kept with the compact form it can choose.
// vm.Heading, Mode, Menu and Prompt are not drawn. The blocks take the
// full form; where CompactMainMenu allows it, the form Render would
// choose for the screen they make with that menu: the full form's blocks
// on output that is not a terminal, and on a terminal from
// theme.CompactBelow rows that holds them, the gap and below; else the
// compact form's (see MainMenu).
func (vm MainMenu) StatusBlocks(c component.Ctx, below int) string {
	return vm.statusBlocks(c, below, CompactMainMenu)
}

// statusBlocks is StatusBlocks with the compact form allowed where compact
// is set: StatusBlocks passes CompactMainMenu, the tests true too.
func (vm MainMenu) statusBlocks(c component.Ctx, below int, compact bool) string {
	if !compact || !c.Env.TTY || height(c) >= theme.CompactBelow {
		if full := join(vm.blocks(c, false)...); !compact || fullFits(c, lines(full)+theme.SectionGap+below) {
			return full
		}
	}
	return join(vm.blocks(c, true)...)
}

// compact reports whether the main menu takes its compact form in c, the
// compact form allowed where compact is set.
func (vm MainMenu) compact(c component.Ctx, compact bool) bool {
	_, ok := vm.full(c, compact)
	return !ok
}

// full reports whether the main menu takes its full form in c, and if so
// returns it: always where compact is not set; else when it fits
// (fullFits), prompt included. Below theme.StackBelow columns the rows
// stack, and the full form may not fit 40 rows (57 lines at 60 columns).
func (vm MainMenu) full(c component.Ctx, compact bool) (string, bool) {
	if compact && c.Env.TTY && height(c) < theme.CompactBelow {
		return "", false
	}
	out := vm.form(c, false)
	return out, !compact || fullFits(c, lines(out))
}

// fullFits reports whether a full form of n lines fits c where the
// compact form is allowed: on output that is not a terminal, which has no
// height to fit, and on a terminal from theme.CompactBelow rows whose
// height holds the n lines.
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
