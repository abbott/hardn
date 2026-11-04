package main

// Auto Updates on the plan/apply engine (refactor plan slice 7, Auto
// Updates, unit (d)): the main menu's Auto Updates item (6) runs the Auto
// Updates screen (pkg/screens/updates), which pkg/menu reaches through the
// hook set here, as it reaches the SSH Login screen (ssh.go), so that
// pkg/menu builds no engine. The engine's options are engine.go's. No flag
// routes to Auto Updates' off (gallery-updates.md §4, decision 2).

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	updatesscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// updatesMenu is what withUpdatesScreen sets the Auto Updates item of:
// pkg/menu's main menu, and the tests' record of what was set.
type updatesMenu interface{ SetUpdates(run func() error) }

// The main menu is an updatesMenu.
var _ updatesMenu = (*menu.MainMenu)(nil)

// withUpdatesScreen returns m with its Auto Updates item set to the Auto
// Updates screen on c, the configuration m was given, and hardn's version
// (menu.MainMenu.SetUpdates).
func withUpdatesScreen[M updatesMenu](m M, c *config.Config, version string) M {
	m.SetUpdates(func() error { return runUpdatesScreen(context.Background(), c, version) })
	return m
}

// runUpdatesScreen is the main menu's Auto Updates item: the Auto Updates
// screen until the user goes back (nil) or quits (nav.ErrQuit), or the
// input ends (prompt.ErrAborted), as runSSHScreen runs the SSH Login
// screen: on the host hardn builds, with its run log in hardn's log, the
// facts detected once per visit, through the one line reader every prompt
// shares and stdout's render environment; each change on an engine with
// engineOptions, assembled for this visit from c as it is now; c, the
// configuration the main menu holds, whose enableUnattendedUpgrades an
// off saves false where it says true, the screen adopting the save into c
// as runBackupScreen's does, so that the main menu, and the next screens'
// saves, take hardn.yml as saved; a dry run when c says so as the item is
// chosen, every change then stopping at its plan. The control's record
// carries version, hardn's, and is kept where hardn keeps its records
// (Records zero: /var/lib/hardn, time.Now). No audit record is written:
// the audit log's first writer is next/p3-startup's (Options.Ran left
// nil). Each change's apply, and nothing else, runs under engineSignals,
// hardn ending after a hangup (screenApplyContext; runDNSScreen says why).
func runUpdatesScreen(ctx context.Context, c *config.Config, version string) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return updatesscreen.Run(ctx, updatesscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Config:       c,
		NewEngine:    newEngine(sys, engineOptions(ctx, sys, c)),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
		Version:      version,
	})
}
