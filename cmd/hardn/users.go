package main

// User Management on the plan/apply engine (refactor plan slice 5, User
// Management, unit (d), next/s5-replace; the maintainer, 2026-11-03: a
// hard cutover, the legacy User Management deleted, not frozen): the main
// menu's User Management item runs the User Management screen
// (pkg/screens/users), which pkg/screens/mainmenu reaches through the hook set here, as
// it reaches the Firewall screen (firewall.go), so that pkg/screens/mainmenu builds no
// engine; and -c plans and applies the User control's Create through
// runControls, as -g runs the DNS control (gallery-users.md §5, decision 6
// (a), adopted 2026-11-03). The engine's options are engine.go's.

import (
	"context"
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// createAdmin is -c: the User control's Create of c.Username, -u's when
// given, else hardn.yml's username (Create's default, decision 7 (a); the
// root command refuses -c with neither), with admin rights, hardn's grant
// asking the user's password, or none where hardn.yml's adminNoPassword
// is true (the maintainer, 2026-11-11), no password, and every key in hardn.yml's sshKeys
// (the maintainer, 2026-11-03: "(a) Add sshKeys", replacing decision 6
// (a)'s no key; with none, the plan's own finding, USERS_NO_KEY, says the
// user can't log in until one is added); planned and applied as runControls
// runs a flag's controls, without a question, as -g's; its report
// output.Human's on stdout; under -n, or hardn.yml's dryRun: true, the
// plan alone, nothing written. The control's refusals, said before
// anything is written (a name useradd would refuse, a name a user or
// group has, /home/<name> already there, no elevation tool, a key that is
// not one, ...), are reported with their code and how to go on, and not
// applied. It sets no password: the plan's finding, USERS_PASSWORD_NOT_SET,
// says the user can't use sudo until one is set. It no longer configures sshd after the user, as
// origin/main's -c did (SSHManager.ConfigureSSH): sshd is slice 6's. It
// returns the exit status: 0 applied or nothing to change, 3 a plan with
// changes to make, 1 refused or failed.
func createAdmin(c *config.Config) int {
	create := usersctl.Admin{Action: usersctl.ActionCreate, New: createNew(c), Getenv: os.Getenv}
	return runControls(c, []engine.Control{create}, createRefuse, nil)
}

// createNew is the user -c creates on c: c.Username, an admin, with
// c.SshKeys as loaded, every one (the maintainer, 2026-11-03: "(a) Add
// sshKeys"), an empty entry refused by the control as not a key.
func createNew(c *config.Config) usersctl.New {
	return usersctl.New{Name: c.Username, Admin: true, NoPassword: c.AdminNoPassword, Keys: c.SshKeys}
}

// createRefuse is -c's refusal of a plan, refuseCreateLockoutRisk; a seam,
// so that a test can show -c hands it to runControls, Create's plan never
// being lockout-risk.
var createRefuse = refuseCreateLockoutRisk

// errCreateLockoutRisk is -c's refusal of a lockout-risk plan (proposed
// words, refactor plan slice 5, unit (d), on -e's pattern): its code the
// registry's PLAN_REFUSED (engine.ErrRefused, a refusal with no code of
// its own), exit status 1.
var errCreateLockoutRisk = &steps.Refusal{
	Code: "PLAN_REFUSED",
	Why:  "Creating this user here risks a lockout",
	Fix:  "create it from main menu 1, User Management, which asks you to confirm",
	Err:  fmt.Errorf("%w: a lockout-risk plan, which -c does not confirm", engine.ErrRefused),
}

// refuseCreateLockoutRisk refuses p when it is lockout-risk
// (engine.RiskLockout): such a plan is
// confirmed by the hostname typed and armed with the revert timer, which a
// flag that applies without asking must not arm unasked. Create's plan is
// never one (its grant is a new user's, plain: gallery-users.md §5,
// decision 4, y/N); a guard, so that one that ever is is shown and refused,
// not applied. Any other plan it leaves to the engine.
func refuseCreateLockoutRisk(p *engine.Plan) error {
	if p.Risk == engine.RiskLockout {
		return errCreateLockoutRisk
	}
	return nil
}

// withUsersScreen returns m with its User Management item set to the User
// Management screen on c, the configuration m was given
// (mainmenu.Options.Users).
func withUsersScreen(m mainmenu.Options, c *config.Config) mainmenu.Options {
	m.Users = func() error { return runUsersScreen(context.Background(), c) }
	return m
}

// runUsersScreen is the main menu's User Management item: the User
// Management screen until the user goes back (nil) or quits (nav.ErrQuit),
// or the input ends (prompt.ErrAborted), as runFirewallScreen runs the
// Firewall screen: on the host hardn builds, with its run log in hardn's
// log, the facts detected once per visit, hardn's environment (os.Getenv),
// from which the control takes the session user, SUDO_USER or DOAS_USER
// (without it every run would read as a root login, the session user not
// known, and each lockout rule of gallery-users.md §5, decision 4, would
// apply as at one), through the one line reader every prompt shares and
// stdout's render environment; each change on an engine with
// engineOptions, assembled for this visit from c as it is now; c, the
// configuration the main menu holds, whose username and sshKeys are
// Create's defaults alone, never the user the rows are about, and which no
// change saves (decision 7 (a)); a dry run when c says so as the item is
// chosen, every change then stopping at its plan. Each change's run is a
// record of the audit log (Options.Ran, auditRan). Each change's apply, and nothing else, runs under
// engineSignals, hardn ending after a hangup (screenApplyContext;
// runDNSScreen says why).
func runUsersScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return usrscreen.Run(ctx, usrscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Getenv:       os.Getenv,
		Config:       c,
		NewEngine:    newEngine(sys, engineOptions(ctx, sys, c)),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
		Ran:          auditRan(sys, c),
	})
}
