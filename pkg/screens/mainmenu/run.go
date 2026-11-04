package mainmenu

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
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
	// DNS is the DNS item (3): pkg/screens/dns, which edits hardn.yml's
	// list in Config.
	DNS func() error
	// Firewall is the Firewall item (4): pkg/screens/firewall.
	Firewall func() error
	// FirewallPVE, asked as the main menu is drawn, says whether the host
	// is Proxmox VE, where item 4 reads "View pve-firewall"
	// (gallery-firewall.md §5, decision 7); nil is never.
	FirewallPVE func() bool
	// Backup is the Backup item (5): pkg/screens/backup.
	Backup func() error
	// Updates is the Auto Updates item (6): pkg/screens/updates.
	Updates func() error
	// RunAll is the Run All item (7): pkg/screens/runall on the planner. No
	// change it makes saves hardn.yml.
	RunAll func() error
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
// returned, so that the next pass reads the status again and draws the
// main menu under what the screen printed.
var errAgain = errors.New("mainmenu: the main menu again")

// Run runs the main menu until the user leaves it. The main menu is
// pkg/ui/screen's main-menu layout in its full form (refactor plan slice
// 2b): the status blocks (Options.Status), "Select an option" with the mode
// indicator, the items (items), and "Enter selection" with its hint
// generated from them, asked through Options.Input. The screen is cleared
// once, as the main menu starts, on a terminal alone (term.Env.Clear), and
// not when hardn.yml warned (Options.Warned); each pass then reads the
// status again and draws the main menu on the terminal as it is then,
// under what the item before it printed, not cleared (brief 1C), each
// item's screen clearing as it opens (show). A line no item takes is
// refused under the prompt, "Invalid option. Please try again.", and asked
// again, the screen above kept (nav.Ask).
//
// 0 (Exit) and q clear the screen and return nil. Any other end is the
// error returned, for the caller to end hardn with: the end of the input or
// Ctrl-C at the prompt or in a screen (prompt.ErrAborted, prompt.ExitCode:
// 130; prompt.ErrInterrupted, prompt.Reraise), a status not read, a write
// that failed. Nothing bounds a pass: ctx is the caller's, and a status
// command that blocks holds the screen until it returns or Ctrl-C ends
// hardn (docs/engine/menu-live.md §5).
func Run(ctx context.Context, o Options) error {
	if !o.Warned {
		o.clear()
	}
	for {
		switch err := o.pass(ctx); {
		case errors.Is(err, errAgain):
		case err == nil, errors.Is(err, nav.ErrQuit):
			o.clear()
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

// clear clears the screen on the terminal as it is now, and writes nothing
// off a terminal (term.Env.Clear).
func (o Options) clear() {
	_ = o.terminal().Clear(o.out())
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

// show runs an item's screen from a cleared screen on a terminal (clear),
// as every item's screen starts, and panics with unset, a programming
// error, when none is set. Return, or a back from it, comes back here
// (errAgain); q quits from the screen as from the main menu (nav.ErrQuit);
// the end of the input, Ctrl-C and any other error end the main menu
// (Run).
func (o Options) show(run func() error, unset string) func() error {
	return func() error {
		if run == nil {
			panic(unset)
		}
		o.clear()
		err := run()
		if err == nil || errors.Is(err, nav.ErrBack) {
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
// decision 7).
func (o Options) firewallDescription() string {
	if o.FirewallPVE != nil && o.FirewallPVE() {
		return "View pve-firewall"
	}
	return "Configure UFW rules"
}

// items is the main menu's items, each bound to the screen cmd/hardn sets
// for it, and its Exit item "0) Exit", "q to quit" (DEV-030); the hint,
// generated from the items, says "[0-10 or q]". Item 1's parenthesis is
// its aside. Item 2 says what the SSH Login screen changes, "Root login,
// passwords, port" (legacy-left.md §3, decision 7 (a); P-07 (a), P-12);
// item 4 on Proxmox VE "View pve-firewall" (slice 4, decision 7); item 6
// is Auto Updates (slice 7, unit (d)), where origin/main's Dry-Run was;
// item 8 "Keep HARDN_CONFIG" (slice 9, decision 7 (a), P-07 (c)). Every
// item opens under plan only as live: its screen stops each change at its
// plan.
func (o Options) items() nav.Menu {
	item := func(key, label, description string) component.MenuItem {
		return component.MenuItem{Key: key, Label: label, Description: description}
	}
	return nav.Menu{
		Options: []nav.Option{
			{Item: component.MenuItem{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"},
				Action: o.show(o.Users, "mainmenu: the User Management item has no screen (Options.Users)")},
			{Item: item("2", "SSH Login", "Root login, passwords, port"), Action: o.show(o.SSH, "mainmenu: the SSH Login item has no screen (Options.SSH)")},
			{Item: item("3", "DNS", "Configure Nameservers"), Action: o.show(o.DNS, "mainmenu: the DNS item has no screen (Options.DNS)")},
			{Item: item("4", "Firewall", o.firewallDescription()), Action: o.show(o.Firewall, "mainmenu: the Firewall item has no screen (Options.Firewall)")},
			{Item: item("5", "Backup", "Configure Hardn backup settings"), Action: o.show(o.Backup, "mainmenu: the Backup item has no screen (Options.Backup)")},
			{Item: item("6", "Auto Updates", "Configure automatic updates"), Action: o.show(o.Updates, "mainmenu: the Auto Updates item has no screen (Options.Updates)")},
			{Item: item("7", "Run All", "Execute hardening operations"), Action: o.show(o.RunAll, "mainmenu: the Run All item has no screen (Options.RunAll)")},
			{Item: item("8", "Environment", "Keep HARDN_CONFIG"), Action: o.show(o.Environment, "mainmenu: the Environment item has no screen (Options.Environment)")},
			{Item: item("9", "System Details", "View system information"), Action: o.show(o.SystemDetails, "mainmenu: the System Details item has no screen (Options.SystemDetails)")},
			{Item: item("10", "Logs", "View log file"), Action: o.show(o.Logs, "mainmenu: the Logs item has no screen (Options.Logs)")},
		},
		Exit: component.MenuItem{Key: "0", Label: "Exit", Description: keys.Set{keys.Quit()}.Help()},
	}
}
