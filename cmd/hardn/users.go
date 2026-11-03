package main

// User Management on the plan/apply engine (refactor plan slice 5, User
// Management, unit (d), next/s5-replace; the maintainer, 2026-11-03: a
// hard cutover, the legacy User Management deleted, not frozen): the main
// menu's User Management item runs the User Management screen
// (pkg/screens/users), which pkg/menu reaches through the hook set here, as
// it reaches the Firewall screen (firewall.go), so that pkg/menu builds no
// engine. The engine's options are engine.go's.

import (
	"context"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// usersMenu is what withUsersScreen sets the User Management item of:
// pkg/menu's main menu, and the tests' record of what was set.
type usersMenu interface{ SetUsers(run func() error) }

// The main menu is a usersMenu.
var _ usersMenu = (*menu.MainMenu)(nil)

// withUsersScreen returns m with its User Management item set to the User
// Management screen on c, the configuration m was given
// (menu.MainMenu.SetUsers).
func withUsersScreen[M usersMenu](m M, c *config.Config) M {
	m.SetUsers(func() error { return runUsersScreen(context.Background(), c) })
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
// chosen, every change then stopping at its plan. No audit record is
// written: the audit log's first writer is next/p3-startup's (Options.Ran
// left nil). Each change's apply, and nothing else, runs under
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
	})
}
