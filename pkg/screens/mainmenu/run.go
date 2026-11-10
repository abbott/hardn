package mainmenu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// Options is what the main menu runs with: what cmd/hardn fills, on the one
// main menu it runs. Each item's screen is cmd/hardn's, which assembles it
// on the plan/apply engine, so that this package runs no screen's engine
// itself. An item's screen returns nil when the user goes back to the main
// menu (nav.ErrBack too), nav.ErrQuit when they quit with q, and an error
// wrapping prompt.ErrAborted at the end of the input or on Ctrl-C. Each
// screen that edits hardn.yml adopts the save into Config, so that the main
// menu and the next screens take hardn.yml as saved. A main menu whose item
// is chosen without its screen set panics, as one drawn without Status or
// Terminal does: cmd/hardn fills every field.
type Options struct {
	// Config is the configuration every screen reads: the mode indicator's
	// plan only or live (DryRun, DryRunSource).
	Config *config.Config

	// Users is the User Management item (1): pkg/screens/users. No change
	// it makes saves hardn.yml (gallery-users.md §5, decision 7 (a)).
	Users func() error
	// SSH is the SSH Login item (2): pkg/screens/ssh. No change it makes
	// saves hardn.yml (gallery-ssh.md §5, decision 9 (a)).
	SSH func() error
	// Firewall is the Firewall item (3): pkg/screens/firewall.
	Firewall func() error
	// FirewallPVE, asked as the main menu is drawn, says whether the host
	// is Proxmox VE, where item 3 reads "View pve-firewall"
	// (gallery-firewall.md §5, decision 7); nil is never.
	FirewallPVE func() bool
	// FirewallEL, asked as the main menu is drawn, says whether the host
	// is Rocky Linux or AlmaLinux, where item 3 reads "Configure firewalld"
	// (el-firewalld.md §1.6); nil is never.
	FirewallEL func() bool
	// DNS is the DNS item (4): pkg/screens/dns, which edits hardn.yml's
	// list in Config.
	DNS func() error
	// Updates is the Auto Updates item (5): pkg/screens/updates.
	Updates func() error
	// RunAll is the Run All item (6): pkg/screens/runall on the planner. No
	// change it makes saves hardn.yml.
	RunAll func() error
	// Backup is the Backup item (7): pkg/screens/backup.
	Backup func() error
	// Environment is the Environment item (8): pkg/screens/environment,
	// which saves no setting of hardn.yml.
	Environment func() error
	// SystemDetails is the System Details item (9): pkg/screens/systemdetails.
	// It reads and changes nothing else, under plan only as live.
	SystemDetails func() error
	// Logs is the Logs item (10): pkg/screens/logs.
	Logs func() error

	// Status reads the main menu's header, host block, risk level and
	// status rows on every pass (Status, through cmd/hardn's host), and
	// returns the main menu's view-model with those blocks filled; Run
	// fills in the heading, mode indicator, items and prompt and draws the
	// whole layout. An error ends the main menu: a cancelled context's as
	// Ctrl-C's (prompt.ErrInterrupted).
	Status func(ctx context.Context) (screen.MainMenu, error)
	// Terminal is the terminal the main menu is drawn on: stdout's, asked
	// again on every pass, so that each pass draws for the terminal as it
	// is then.
	Terminal func() term.Env
	// Warned says that loading hardn.yml warned on stderr: the main menu
	// then draws below the warning, where it would clear the screen and
	// lose it.
	Warned bool

	// Input returns the line reader the main menu asks through: the one
	// every prompt shares (prompt.Stdin).
	Input func() *prompt.Reader
	// Out is stdout, where the main menu is drawn and cleared.
	Out io.Writer
}

// The main menu's words on its layout: origin/main's heading and prompt,
// unchanged (refactor plan slice 2b, decision 5; P-29 (a) still open).
const (
	mainHeading = "Select an option"
	mainPrompt  = "Enter selection"
)

// errAgain ends a pass of the main menu once an item's screen has
// returned and the screen is cleared (show), so that the next pass reads
// the status again and draws the main menu on the cleared screen.
var errAgain = errors.New("mainmenu: the main menu again")

// Run runs the main menu until the user leaves it. The main menu is
// pkg/ui/screen's main-menu layout in its full form (refactor plan slice
// 2b): the status blocks (Options.Status), "Select an option" with the mode
// indicator, the items (items), and "Enter selection" with its hint
// generated from them, asked through Options.Input. The screen is cleared
// as the main menu starts, on a terminal alone (term.Env.Clear), and not
// when hardn.yml warned (Options.Warned); each item's screen opens on a
// cleared screen, and the screen is cleared again as it returns (show).
// Each pass reads the status again and draws the main menu on the terminal
// as it is then. Every clear leaves topRows blank rows at the top of the
// terminal (clearTop), so that every screen, the main menu's first drawing
// too, starts on the third row. A line no item takes is refused under the
// prompt, "Invalid option. Please try again.", and asked again, the screen
// above kept (nav.Ask).
//
// 0 (Exit) and q clear the screen, topRows blank rows left at its top
// (clearTop), so that the shell's prompt comes on the third row too, and
// return nil. Any other end is the error returned, for the caller to end
// hardn with: the end of the input or
// Ctrl-C at the prompt or in a screen (prompt.ErrAborted, prompt.ExitCode:
// 130; prompt.ErrInterrupted, prompt.Reraise), a status not read, a write
// that failed. Nothing bounds a pass: ctx is the caller's, and a status
// command that blocks holds the screen until it returns or Ctrl-C ends
// hardn (docs/engine/menu-live.md §5).
func Run(ctx context.Context, o Options) error {
	if o.Warned {
		o.belowWarning(o.navGap())
	} else {
		o.clearTop(o.navGap())
	}
	for {
		switch err := o.pass(ctx); {
		case errors.Is(err, errAgain):
		case err == nil, errors.Is(err, nav.ErrQuit):
			o.clearTop(0) // the shell's prompt follows
			return nil
		default:
			return err
		}
	}
}

// pass draws the main menu once, its status read again (readStatus) on
// the terminal as it is now, and runs what the user chooses, through
// nav.Run, which draws the layout's Screen, asks, refuses a line no item
// takes under the prompt and asks again, and runs the item's action. It
// returns errAgain once an item's screen has returned, so that the next
// pass draws the main menu again; nil for 0, the Exit item; nav.ErrQuit
// for q; and the error that ends the main menu otherwise.
func (o Options) pass(ctx context.Context) error {
	env := o.terminal()
	vm, err := o.readStatus(ctx)
	if err != nil {
		return err
	}
	vm.Heading, vm.Mode, vm.Prompt = mainHeading, o.mode(), mainPrompt
	if o.Input == nil {
		panic("mainmenu: the main menu has no input (Options.Input)")
	}
	p := nav.Prompter{Ctx: component.NewCtx(env), In: o.Input(), Out: env.Writer(o.out())}
	drawn := vm.Screen(p.Ctx, o.items())
	return p.Run(func() nav.Screen { return drawn })
}

// terminal is the terminal as it is now (Options.Terminal).
func (o Options) terminal() term.Env {
	if o.Terminal == nil {
		panic("mainmenu: the main menu has no terminal (Options.Terminal)")
	}
	return o.Terminal()
}

// out is stdout (Options.Out).
func (o Options) out() io.Writer {
	if o.Out == nil {
		panic("mainmenu: the main menu has no output (Options.Out)")
	}
	return o.Out
}

// topRows is how many blank rows every screen keeps at the top of the
// terminal after a clear: two, so that one shows as a gap under a bar a
// terminal draws over its own top row, which hides what is written there
// (the maintainer, 2026-11-06; docs/engine/menu-layout.md §5).
const topRows = 2

// clearTop clears the screen on the terminal as it is now
// (term.Env.Clear), then writes the blank lines that leave topRows of them
// above what comes next, less own, the blank lines that writes itself
// (navGap, or none before the shell's prompt). Off a terminal it writes
// nothing.
func (o Options) clearTop(own int) {
	env := o.terminal()
	if env.Plain {
		return
	}
	_ = env.Clear(o.out())
	_, _ = io.WriteString(o.out(), strings.Repeat("\n", max(0, topRows-own)))
}

// belowWarning leaves theme.SectionGap blank lines between hardn.yml's
// warning and the main menu drawn under it, less own, the blank lines nav
// writes itself (navGap), on a terminal alone, where the warning on stderr
// and the main menu on stdout share the screen. Without it the header sat
// on the warning's last line (the maintainer's LXC, 2026-11-07).
func (o Options) belowWarning(own int) {
	if o.terminal().Plain {
		return
	}
	_, _ = io.WriteString(o.out(), strings.Repeat("\n", max(0, theme.SectionGap-own)))
}

// navGap is how many blank lines nav.Prompter.Run writes above the next
// drawing, the main menu's or an item's screen's: theme.SectionGap once a
// prompt has been answered (prompt.Reader.Asked), which every drawing but
// the main menu's first follows, and that one too after hardn.yml's
// first-run question; none before any.
func (o Options) navGap() int {
	if o.Input != nil && o.Input().Asked() {
		return theme.SectionGap
	}
	return 0
}

// readStatus reads the status for one pass (Options.Status): the main
// menu's view-model with its status blocks filled. The checks only read,
// and run again on every pass, so each pass shows the host as it is then.
// Ctrl-C while they run: no signal handler is registered for them, so Go's
// default ends hardn by SIGINT at once; it does not reach the hook through
// ctx. A hook that returns a cancelled context's error, of a context of its
// own, ends the main menu as Ctrl-C does (prompt.ErrInterrupted).
func (o Options) readStatus(ctx context.Context) (screen.MainMenu, error) {
	if o.Status == nil {
		panic("mainmenu: the main menu has no status (Options.Status)")
	}
	vm, err := o.Status(ctx)
	if errors.Is(err, context.Canceled) {
		return vm, fmt.Errorf("%w: %w", prompt.ErrInterrupted, err)
	}
	return vm, err
}

// show runs an item's screen from a cleared screen on a terminal (clearTop),
// as every item's screen starts, and panics with unset, a programming
// error, when none is set. Return, or a back from it, clears the screen
// again and comes back here (errAgain), so that the main menu is drawn on
// a cleared screen, not under the item's: on a terminal whose height was
// measured the item's screen scrolls into the scrollback; where it was not,
// the clear is ED 2, and whether the terminal keeps the screen is the
// terminal's (term.Env.Clear). Either way no result goes unseen: an item's
// screen returns only on the user's 0 at its own prompt, drawn again under
// any result its action wrote. q quits from the screen as
// from the main menu (nav.ErrQuit); the end of the input, Ctrl-C and any
// other error end the main menu (Run).
func (o Options) show(run func() error, unset string) func() error {
	return func() error {
		if run == nil {
			panic(unset)
		}
		o.clearTop(o.navGap()) // the screen's nav.Prompter.Run writes its gap
		err := run()
		if err == nil || errors.Is(err, nav.ErrBack) {
			o.clearTop(o.navGap()) // the main menu's nav.Prompter.Run writes its gap
			return errAgain
		}
		return err
	}
}

// mode is the mode indicator for the session as it is now (slice 2b,
// decisions 2 and 3): plan only or live, from the configuration every
// screen reads (DryRun), and what set plan only (DryRunSource): -n or
// hardn.yml as hardn started, and nothing for live.
func (o Options) mode() component.ModeIndicator {
	mode := component.ModeIndicator{DryRun: o.Config.DryRun}
	switch o.Config.DryRunSource() {
	case config.DryRunFlag:
		mode.Source = component.ModeSourceFlag
	case config.DryRunFile:
		mode.Source = component.ModeSourceFile
	}
	return mode
}

// firewallDescription is the Firewall item's description: origin/main's,
// or on Proxmox VE what the screen does there (gallery-firewall.md §5,
// decision 7), or on Rocky Linux and AlmaLinux firewalld's
// (el-firewalld.md §1.6).
func (o Options) firewallDescription() string {
	switch {
	case o.FirewallPVE != nil && o.FirewallPVE():
		return "View pve-firewall"
	case o.FirewallEL != nil && o.FirewallEL():
		return "Configure firewalld"
	}
	return "Configure ufw rules"
}

// items is the main menu's items, each bound to the screen cmd/hardn sets
// for it, and its Exit item "0) Exit", "q to quit" (DEV-030); the hint,
// generated from the items, says "[0-10 or q]". The order is the
// maintainer's (2026-11-07): User Management, SSH Login, Firewall, DNS,
// Auto Updates, Run All, Backup, Environment, System Details, Logs. Item
// 1's parenthesis is its aside. Item 2 says what the SSH Login screen
// changes, "Root login, passwords, port" (legacy-left.md §3, decision 7
// (a); P-07 (a), P-12); item 3 on Proxmox VE "View pve-firewall" (slice
// 4, decision 7), on Rocky Linux and AlmaLinux "Configure firewalld"; Auto Updates (slice 7, unit (d)) took the key
// origin/main's Dry-Run left; item 8 "Keep HARDN_CONFIG" (slice 9,
// decision 7 (a), P-07 (c)). Every
// item opens under plan only as live: its screen stops each change at its
// plan.
func (o Options) items() nav.Menu {
	item := func(key, label, description string) component.MenuItem {
		return component.MenuItem{Key: key, Label: label, Description: description}
	}
	return nav.Menu{
		Options: []nav.Option{
			{Item: component.MenuItem{Key: "1", Label: "User Management", Description: "Create, configure", Aside: "sudo, SSH keys"},
				Action: o.show(o.Users, "mainmenu: the User Management item has no screen (Options.Users)")},
			{Item: item("2", "SSH Login", "Root login, passwords, port"), Action: o.show(o.SSH, "mainmenu: the SSH Login item has no screen (Options.SSH)")},
			{Item: item("3", "Firewall", o.firewallDescription()), Action: o.show(o.Firewall, "mainmenu: the Firewall item has no screen (Options.Firewall)")},
			{Item: item("4", "DNS", "Configure nameservers"), Action: o.show(o.DNS, "mainmenu: the DNS item has no screen (Options.DNS)")},
			{Item: item("5", "Auto Updates", "Configure automatic updates"), Action: o.show(o.Updates, "mainmenu: the Auto Updates item has no screen (Options.Updates)")},
			{Item: item("6", "Run All", "Execute hardening operations"), Action: o.show(o.RunAll, "mainmenu: the Run All item has no screen (Options.RunAll)")},
			{Item: item("7", "Backup", "Configure hardn backup"), Action: o.show(o.Backup, "mainmenu: the Backup item has no screen (Options.Backup)")},
			{Item: item("8", "Environment", "Keep HARDN_CONFIG"), Action: o.show(o.Environment, "mainmenu: the Environment item has no screen (Options.Environment)")},
			{Item: item("9", "System Details", "View system information"), Action: o.show(o.SystemDetails, "mainmenu: the System Details item has no screen (Options.SystemDetails)")},
			{Item: item("10", "Logs", "View log file"), Action: o.show(o.Logs, "mainmenu: the Logs item has no screen (Options.Logs)")},
		},
		Exit: component.MenuItem{Key: "0", Label: "Exit", Description: keys.Set{keys.Quit()}.Help()},
	}
}

// FirewallOf is what the Firewall item's description turns on, from
// detect's facts f: whether the host is Proxmox VE (Options.FirewallPVE)
// and whether it is Rocky Linux or AlmaLinux (Options.FirewallEL).
// cmd/hardn's hooks answer with it. It is pure.
func FirewallOf(f detect.Facts) (pve, el bool) {
	return f.Proxmox.Installed == detect.Yes, f.Platform.Distro.EL()
}

// MenuOf is the main menu's items as Run draws them on a host with facts
// f (items, its Firewall hooks answered by FirewallOf), for a drawing of
// the main menu that runs no item: internal/tools/p4status draws a host's
// main menu with it. It is pure.
func MenuOf(f detect.Facts) component.Menu {
	pve, el := FirewallOf(f)
	return Options{FirewallPVE: func() bool { return pve }, FirewallEL: func() bool { return el }}.items().View()
}
