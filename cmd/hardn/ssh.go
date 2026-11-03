package main

// SSH Login on the plan/apply engine (refactor plan slice 6, SSH Login,
// unit (d), next/s6-replace; the maintainer, 2026-11-03: a hard cutover,
// the legacy SSH Login deleted, not frozen): the main menu's SSH Login item
// runs the SSH Login screen (pkg/screens/ssh), which pkg/menu reaches
// through the hook set here, as it reaches the User Management screen
// (users.go), so that pkg/menu builds no engine. The engine's options are
// engine.go's.

import (
	"context"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// sshMenu is what withSSHScreen sets the SSH Login item of: pkg/menu's
// main menu, and the tests' record of what was set.
type sshMenu interface{ SetSSH(run func() error) }

// The main menu is an sshMenu.
var _ sshMenu = (*menu.MainMenu)(nil)

// withSSHScreen returns m with its SSH Login item set to the SSH Login
// screen on c, the configuration m was given (menu.MainMenu.SetSSH).
func withSSHScreen[M sshMenu](m M, c *config.Config) M {
	m.SetSSH(func() error { return runSSHScreen(context.Background(), c) })
	return m
}

// runSSHScreen is the main menu's SSH Login item: the SSH Login screen
// until the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runUsersScreen runs the User Management screen:
// on the host hardn builds, with its run log in hardn's log, the facts
// detected once per visit, hardn's environment (os.Getenv), from which the
// control takes the session user, SUDO_USER or DOAS_USER (without it every
// run would read as a root login, and Disallow root login would refuse as
// at one), through the one line reader every prompt shares and stdout's
// render environment; each change on an engine with engineOptions,
// assembled for this visit from c as it is now, and the session the
// screen reads (its line in sshd's log, or the console) the one those
// options hold, as runFirewallScreen gives it, so that the screen and the
// engine never disagree on how hardn's user logged in; c, the
// configuration the main menu holds, whose sshPort is the port form's
// default and sshConfigFile Move old settings' file alone, and which no
// change saves (gallery-ssh.md §5, decision 9 (a)); a dry run when c says
// so as the item is chosen, every change then stopping at its plan. No
// audit record is written: the audit log's first writer is
// next/p3-startup's (Options.Ran left nil). Each change's apply, and
// nothing else, runs under engineSignals, hardn ending after a hangup
// (screenApplyContext; runDNSScreen says why).
func runSSHScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	o := engineOptions(ctx, sys, c)
	env := stdoutEnv()
	return sshscreen.Run(ctx, sshscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Session:      o.Session,
		Getenv:       os.Getenv,
		Config:       c,
		NewEngine:    newEngine(sys, o),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
	})
}
