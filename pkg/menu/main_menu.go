package menu

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// MainMenu is the main menu of the application
type MainMenu struct {
	config *config.Config

	// users runs the User Management item (SetUsers).
	users func() error

	// ssh runs the SSH Login item (SetSSH).
	ssh func() error

	// dns runs the DNS item (SetDNS).
	dns func() error

	// backup runs the Backup item (SetBackup).
	backup func() error

	// updates runs the Auto Updates item (SetUpdates).
	updates func() error

	// environment runs the Environment item (SetEnvironment).
	environment func() error

	// firewall runs the Firewall item, and firewallPVE says whether the host
	// is Proxmox VE, where the item reads as pve-firewall's (SetFirewall).
	firewall    func() error
	firewallPVE func() bool

	// systemDetails runs the System Details item (SetSystemDetails).
	systemDetails func() error

	// logs runs the Logs item (SetLogs).
	logs func() error

	// runAll runs the Run All item (SetRunAll).
	runAll func() error

	// warned: warnings above the first draw stay on screen (SetWarnedAtLoad).
	warned bool

	// status reads the host block, risk level and status rows (SetStatus).
	status func(ctx context.Context) (screen.MainMenu, error)

	// terminal is the terminal the main menu is drawn on (SetTerminal).
	terminal func() term.Env
}

// The main menu's words on its layout: origin/main's heading and prompt
// (pkg/style's Menu, style.go:668), unchanged (refactor plan slice 2b,
// decision 5; P-29 (a) still open).
const (
	mainHeading = "Select an option"
	mainPrompt  = "Enter selection"
)

// errAgain ends a pass of the main menu once an item's screen has
// returned, so that the next pass reads the status again and draws the
// main menu under what the screen printed.
var errAgain = errors.New("menu: the main menu again")

// NewMainMenu returns the main menu on config, the configuration every
// screen reads.
func NewMainMenu(config *config.Config) *MainMenu {
	return &MainMenu{config: config}
}

// SetUsers sets what the User Management item (1) runs: the User
// Management screen (pkg/screens/users) on the plan/apply engine, which
// cmd/hardn assembles, as SetFirewall sets Firewall's (refactor plan slice
// 5, unit (d); the legacy User Management deleted, not frozen: the
// maintainer, 2026-11-03, hard cutover). It returns as SetDNS's does. No
// change the screen makes saves hardn.yml: the configuration this menu was
// given is Create's defaults alone (gallery-users.md §5, decision 7 (a)).
// A main menu whose User Management item is chosen without one set panics:
// cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetUsers(run func() error) { m.users = run }

// showUsers is the User Management item's action: the User Management
// screen (showScreen).
func (m *MainMenu) showUsers() error {
	return m.showScreen(m.users, "menu: the User Management item has no screen (SetUsers)")
}

// SetSSH sets what the SSH Login item (2) runs: the SSH Login screen
// (pkg/screens/ssh) on the plan/apply engine, which cmd/hardn assembles,
// as SetUsers sets User Management's (refactor plan slice 6, unit (d); the
// legacy SSH Login deleted, not frozen: the maintainer, 2026-11-03, hard
// cutover). It returns as SetDNS's does. No change the screen makes saves
// hardn.yml: the configuration this menu was given is the port form's
// default and Move old settings' file alone (gallery-ssh.md §5, decision
// 9 (a)). A main menu whose SSH Login item is chosen without one set
// panics: cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetSSH(run func() error) { m.ssh = run }

// showSSH is the SSH Login item's action: the SSH Login screen
// (showScreen).
func (m *MainMenu) showSSH() error {
	return m.showScreen(m.ssh, "menu: the SSH Login item has no screen (SetSSH)")
}

// SetDNS sets what the DNS item (3) runs: the DNS screen
// (pkg/screens/dns) on the plan/apply engine, which cmd/hardn assembles,
// so that pkg/menu builds no engine (refactor plan slice 2, unit (d)). It
// returns nil when the user goes back to the main menu, nav.ErrQuit when
// they quit with q, and an error wrapping prompt.ErrAborted at the end of
// the input or on Ctrl-C. The screen edits hardn.yml's list in the
// configuration this menu was given, so the main menu shows the list as
// saved when it returns. A main menu whose DNS item is chosen without one
// set panics: cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetDNS(run func() error) { m.dns = run }

// showDNS is the DNS item's action: the DNS screen (showScreen).
func (m *MainMenu) showDNS() error {
	return m.showScreen(m.dns, "menu: the DNS item has no screen (SetDNS)")
}

// SetBackup sets what the Backup item (5) runs: the Backup screen
// (pkg/screens/backup) on the plan/apply engine, which cmd/hardn
// assembles, as SetDNS sets the DNS item's (refactor plan slice 3, unit
// (d)). It returns as SetDNS's does. The screen adopts hardn.yml as each
// change saved it into the configuration this menu was given, so the main
// menu, and the DNS screen's next changes, take the backup settings as
// saved. A main menu whose Backup item is chosen without one set panics:
// cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetBackup(run func() error) { m.backup = run }

// showBackup is the Backup item's action: the Backup screen (showScreen).
func (m *MainMenu) showBackup() error {
	return m.showScreen(m.backup, "menu: the Backup item has no screen (SetBackup)")
}

// SetUpdates sets what the Auto Updates item (6) runs: the Auto Updates
// screen (pkg/screens/updates) on the plan/apply engine, which cmd/hardn
// assembles, as SetSSH sets SSH Login's (refactor plan slice 7, unit (d);
// gallery-updates.md §4, decision 1). It returns as SetDNS's does. Off
// saves enableUnattendedUpgrades false where the configuration this menu
// was given says true, and the screen adopts the save into it, so the
// main menu, and the next screens' changes, take hardn.yml as saved. A
// main menu whose Auto Updates item is chosen without one set panics:
// cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetUpdates(run func() error) { m.updates = run }

// showUpdates is the Auto Updates item's action: the Auto Updates screen
// (showScreen).
func (m *MainMenu) showUpdates() error {
	return m.showScreen(m.updates, "menu: the Auto Updates item has no screen (SetUpdates)")
}

// SetEnvironment sets what the Environment item (8) runs: the Environment
// screen (pkg/screens/environment) on the plan/apply engine, which
// cmd/hardn assembles, as SetDNS and SetBackup set theirs (refactor plan
// slice 9, unit (d)). It returns as SetDNS's does. The screen reads and
// saves no setting of hardn.yml, so the main menu's configuration is as it
// was. A main menu whose Environment item is chosen without one set
// panics: cmd/hardn sets it on the one main menu it builds.
func (m *MainMenu) SetEnvironment(run func() error) { m.environment = run }

// showEnvironment is the Environment item's action: the Environment screen
// (showScreen).
func (m *MainMenu) showEnvironment() error {
	return m.showScreen(m.environment, "menu: the Environment item has no screen (SetEnvironment)")
}

// SetFirewall sets what the Firewall item (4) runs: the Firewall screen
// (pkg/screens/firewall) on the plan/apply engine, which cmd/hardn
// assembles, as SetEnvironment sets Environment's (refactor plan slice 4,
// unit (d)), and onPVE, asked as the main menu is drawn, which reports
// whether the host is Proxmox VE, where the item reads "View pve-firewall"
// (gallery-firewall.md §5, decision 7, adopted 2026-11-01); nil is never.
// It returns as SetDNS's does. The screen adopts hardn.yml as each change
// saved it into the configuration this menu was given. A main menu whose
// Firewall item is chosen without one set panics: cmd/hardn sets it on the
// one main menu it builds.
func (m *MainMenu) SetFirewall(run func() error, onPVE func() bool) {
	m.firewall, m.firewallPVE = run, onPVE
}

// showFirewall is the Firewall item's action: the Firewall screen
// (showScreen).
func (m *MainMenu) showFirewall() error {
	return m.showScreen(m.firewall, "menu: the Firewall item has no screen (SetFirewall)")
}

// firewallDescription is the Firewall item's description: origin/main's,
// or on Proxmox VE what the screen does there (decision 7).
func (m *MainMenu) firewallDescription() string {
	if m.firewallPVE != nil && m.firewallPVE() {
		return "View pve-firewall"
	}
	return "Configure UFW rules"
}

// SetSystemDetails sets what the System Details item (9) runs: the screen
// (pkg/screens/systemdetails), which cmd/hardn assembles, as SetEnvironment
// sets Environment's (refactor plan slice 1, unit (d)). It reads and
// changes nothing else, under plan only as live. A main menu whose item is
// chosen without one set panics: cmd/hardn sets it on the one main menu it
// builds.
func (m *MainMenu) SetSystemDetails(run func() error) { m.systemDetails = run }

// showSystemDetails is the System Details item's action (showScreen).
func (m *MainMenu) showSystemDetails() error {
	return m.showScreen(m.systemDetails, "menu: the System Details item has no screen (SetSystemDetails)")
}

// SetLogs sets what the Logs item (10) runs: the screen (pkg/screens/logs),
// as SetSystemDetails sets 9's.
func (m *MainMenu) SetLogs(run func() error) { m.logs = run }

// showLogs is the Logs item's action (showScreen).
func (m *MainMenu) showLogs() error {
	return m.showScreen(m.logs, "menu: the Logs item has no screen (SetLogs)")
}

// SetRunAll sets what the Run All item (7) runs: the Run All screen
// (pkg/screens/runall) on Run All's planner and the plan/apply engine,
// which cmd/hardn assembles, as SetSSH sets SSH Login's (refactor plan
// slice 8, unit (d); gallery-runall.md §5, decision 7 (a): the item's
// label and description kept). It returns as SetDNS's does. No change the
// screen makes saves hardn.yml. A main menu whose Run All item is chosen
// without one set panics: cmd/hardn sets it on the one main menu it
// builds.
func (m *MainMenu) SetRunAll(run func() error) { m.runAll = run }

// showRunAll is the Run All item's action: the Run All screen
// (showScreen).
func (m *MainMenu) showRunAll() error {
	return m.showScreen(m.runAll, "menu: the Run All item has no screen (SetRunAll)")
}

// showScreen runs an item's screen that cmd/hardn sets (SetUsers, SetSSH,
// SetDNS, SetBackup, SetUpdates, SetRunAll, SetEnvironment, SetFirewall,
// SetSystemDetails, SetLogs) from a cleared screen on a terminal (clear), as every item's screen starts, and
// panics with unset, a programming error, when none is set. Return, or a
// back from it, comes back here (errAgain); q quits from the screen as
// from this menu (nav.ErrQuit); the end of the input, Ctrl-C and any
// other error end hardn as at a legacy prompt (ShowMainMenu, endHardn).
func (m *MainMenu) showScreen(run func() error, unset string) error {
	if run == nil {
		panic(unset)
	}
	m.clear()
	err := run()
	if err == nil || errors.Is(err, nav.ErrBack) {
		return errAgain
	}
	return err
}

// clear clears the screen on the terminal as it is now (SetTerminal), and
// writes nothing off a terminal (term.Env.Clear).
func (m *MainMenu) clear() {
	if m.terminal == nil {
		panic("menu: the main menu has no terminal (SetTerminal)")
	}
	_ = m.terminal().Clear(os.Stdout)
}

// SetWarnedAtLoad says that loading hardn.yml warned on stderr, as cmd/hardn does
// (warnDeprecations): the main menu then draws below the warning, where it would clear
// the screen and lose it.
func (m *MainMenu) SetWarnedAtLoad(warned bool) { m.warned = warned }

// SetStatus sets what reads the main menu's host block, risk level and
// status rows: the status model (the controls' checks, the header probe
// and the risk level), which cmd/hardn reads from the host it builds, so
// that pkg/menu runs no check and builds no engine, as with SetDNS
// (refactor plan slice 2, next/s2-menu-live). It is called on every pass
// of the main menu and returns the main menu's view-model with those
// blocks filled (pkg/screens/mainmenu.Status); the main menu fills in its
// heading, mode indicator, items and prompt and draws the whole layout
// (slice 2b, next/s2b-menu). An error ends hardn as at a legacy prompt: a
// cancelled context (Ctrl-C) by SIGINT, any other with exit status 1 once
// printed. A main menu drawn without one set panics: cmd/hardn sets it on
// the one main menu it builds.
func (m *MainMenu) SetStatus(read func(ctx context.Context) (screen.MainMenu, error)) {
	m.status = read
}

// SetTerminal sets the terminal the main menu is drawn on: stdout's, as
// cmd/hardn detects it (its width, colours and glyphs), asked again on
// every pass, so that each pass draws for the terminal as it is then. A
// main menu drawn without one set panics, as with SetStatus.
func (m *MainMenu) SetTerminal(env func() term.Env) { m.terminal = env }

// readStatus reads the status for one pass (SetStatus): the main menu's
// view-model with its status blocks filled. The checks only read, and run again on every pass, so each pass shows the
// host as it is then. Nothing bounds a pass: ctx is context.Background(),
// never done, so a command that blocks holds the screen until it returns
// or Ctrl-C ends hardn (docs/engine/menu-live.md §5). Ctrl-C while they
// run: no signal handler is registered for them, so Go's default ends
// hardn by SIGINT at once, as prompt.Reraise ends it at the menu's prompt;
// it does not reach the hook through ctx. A hook that returns a cancelled
// context's error, of a context of its own, ends hardn the same way: the
// error is returned as Ctrl-C's (prompt.ErrInterrupted).
func (m *MainMenu) readStatus(ctx context.Context) (screen.MainMenu, error) {
	if m.status == nil {
		panic("menu: the main menu has no status (SetStatus)")
	}
	vm, err := m.status(ctx)
	if errors.Is(err, context.Canceled) {
		return vm, fmt.Errorf("%w: %w", prompt.ErrInterrupted, err)
	}
	return vm, err
}

// refresh any configuration values that might have been
// changed by sub-menus
func (m *MainMenu) refreshConfig() {
	// add ways for sub-menus to notify the main menu of changes,

	// For now, we're using a shared config pointer, so changes are automatically visible
}

// ShowMainMenu displays the main menu and handles user input. The main
// menu is pkg/ui/screen's main-menu layout in its full form (refactor plan
// slice 2b, next/s2b-menu; decision 5 as adopted): the status blocks
// (SetStatus), "Select an option" with the mode indicator, origin/main's
// items (mainMenu), and "Enter selection" with its hint generated from
// them, asked through the one line reader every screen reads
// (prompt.Stdin, the input seam). The screen is cleared once, as hardn
// starts, on a terminal alone (clear; off one nothing clears); each pass then reads the status again and draws the main menu
// on the terminal as it is then, under what the item before it printed,
// not cleared (brief 1C), each item's screen clearing as it opens
// (showScreen).
// A line no item takes is refused under the prompt, "Invalid option.
// Please try again.", and asked again, the screen above kept (nav.Ask).
// 0 (Exit) and q clear the screen and return, as they did; the end of the
// input exits 130 and Ctrl-C ends hardn by SIGINT (endHardn).
func (m *MainMenu) ShowMainMenu() {
	if !m.warned {
		m.clear()
	}
	for {
		// Refresh any configuration that might have been changed
		m.refreshConfig()

		switch err := m.pass(context.Background()); {
		case errors.Is(err, errAgain):
		case err == nil, errors.Is(err, nav.ErrQuit):
			m.clear()
			return
		default:
			endHardn(err)
			return
		}
	}
}

// pass draws the main menu once, its status read again (readStatus) on
// the terminal as it is now (SetTerminal), and runs what the user
// chooses, through nav.Run, which draws the layout's Screen, asks, refuses
// a line no item takes under the prompt and asks again, and runs the
// item's action. It returns errAgain once an item's screen has returned,
// so that the next pass draws the main menu again; nil for 0, the Exit
// item; nav.ErrQuit for q; and the error that ends hardn otherwise: the
// end of the input or Ctrl-C at its prompt (prompt.ErrAborted), a status
// not read, a write that failed.
func (m *MainMenu) pass(ctx context.Context) error {
	if m.terminal == nil {
		panic("menu: the main menu has no terminal (SetTerminal)")
	}
	env := m.terminal()
	vm, err := m.readStatus(ctx)
	if err != nil {
		return err
	}
	vm.Heading, vm.Mode, vm.Prompt = mainHeading, m.mode(), mainPrompt
	p := nav.Prompter{Ctx: component.NewCtx(env), In: input(), Out: env.Writer(os.Stdout)}
	menu := m.mainMenu()
	drawn := vm.Screen(p.Ctx, menu)
	return p.Run(func() nav.Screen { return drawn })
}

// mode is the mode indicator for the session as it is now (slice 2b,
// decisions 2 and 3): plan only or live, from the configuration every
// screen reads (DryRun), and what set plan only (DryRunSource):
// -n or hardn.yml as hardn started, and nothing for live. Plan only with
// no source, which item 6 and Run All's option 2 set inside the session
// until unit (d) removed them, is still drawn so, "Mode Plan only", should
// anything write DryRun again; nothing in hardn does.
func (m *MainMenu) mode() component.ModeIndicator {
	mode := component.ModeIndicator{DryRun: m.config.DryRun}
	switch m.config.DryRunSource() {
	case config.DryRunFlag:
		mode.Source = component.ModeSourceFlag
	case config.DryRunFile:
		mode.Source = component.ModeSourceFile
	}
	return mode
}

// mainMenu is the main menu's items, origin/main's unchanged in content,
// labels, keys and descriptions, but item 8's description, "Keep
// HARDN_CONFIG" where origin/main's said "Configure environment variable"
// (refactor plan slice 9, decision 7 (a), P-07 (c)), and item 4's on
// Proxmox VE, "View pve-firewall" (slice 4, decision 7); item 1's
// parenthesis its aside; each bound to the screen cmd/hardn sets for it
// (showUsers, showSSH, showDNS, showFirewall, showBackup, showUpdates,
// showRunAll, showEnvironment, showSystemDetails and showLogs); its Exit
// item "0) Exit", "q to quit" (DEV-030). Item 6 is Auto Updates,
// "Configure automatic updates" (slice 7, unit (d); gallery-updates.md
// §4, decision 1), where origin/main's Dry-Run was until slice 2b's unit
// (d) removed it; item 7, Run All, keeps its label and description and
// runs the Run All screen on the planner (slice 8, unit (d);
// gallery-runall.md §5, decision 7 (a)); the hint, generated from the
// items, says "[0-10 or q]". Each screen ends the pass when it returns
// (errAgain). User Management (1), SSH Login (2), Firewall (4), Backup
// (5), Auto Updates (6), Run All (7) and Environment (8) open under plan
// only as live: their screens stop each change at its plan (slice 8,
// unit (d), Options.DryRun of pkg/screens/runall; slice 7, unit (d),
// Options.DryRun of pkg/screens/updates; slice 6, unit (d),
// Options.DryRun of pkg/screens/ssh; slice 5, unit (d), which lifts
// slice 2b's "Not available in plan only." from item 1 as its screen goes
// on the flow; slice 4, screen-firewall.md; slice 3, decision 10; slice
// 9, gallery-environment.md §5, Made here).
func (m *MainMenu) mainMenu() nav.Menu {
	item := func(key, label, description string) component.MenuItem {
		return component.MenuItem{Key: key, Label: label, Description: description}
	}
	return nav.Menu{
		Options: []nav.Option{
			{Item: component.MenuItem{Key: "1", Label: "User Management", Description: "Create, Configure", Aside: "sudo, SSH keys"}, Action: m.showUsers},
			{Item: item("2", "SSH Login", "Toggle SSH root access"), Action: m.showSSH},
			{Item: item("3", "DNS", "Configure Nameservers"), Action: m.showDNS},
			{Item: item("4", "Firewall", m.firewallDescription()), Action: m.showFirewall},
			{Item: item("5", "Backup", "Configure Hardn backup settings"), Action: m.showBackup},
			{Item: item("6", "Auto Updates", "Configure automatic updates"), Action: m.showUpdates},
			{Item: item("7", "Run All", "Execute hardening operations"), Action: m.showRunAll},
			{Item: item("8", "Environment", "Keep HARDN_CONFIG"), Action: m.showEnvironment},
			{Item: item("9", "System Details", "View system information"), Action: m.showSystemDetails},
			{Item: item("10", "Logs", "View log file"), Action: m.showLogs},
		},
		Exit: component.MenuItem{Key: "0", Label: "Exit", Description: keys.Set{keys.Quit()}.Help()},
	}
}
