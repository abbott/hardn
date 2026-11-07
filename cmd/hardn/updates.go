package main

// Auto Updates on the plan/apply engine (refactor plan slice 7, Auto
// Updates, unit (d)): the main menu's Auto Updates item (5) runs the Auto
// Updates screen (pkg/screens/updates), which pkg/screens/mainmenu reaches through the
// hook set here, as it reaches the SSH Login screen (ssh.go), so that
// pkg/screens/mainmenu builds no engine. The engine's options are engine.go's. No flag
// routes to Auto Updates' off (gallery-updates.md §4, decision 2).

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	updatesscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// withUpdatesScreen returns m with its Auto Updates item set to the Auto
// Updates screen on c, the configuration m was given, and hardn's version
// (mainmenu.Options.Updates).
func withUpdatesScreen(m mainmenu.Options, c *config.Config, version string) mainmenu.Options {
	m.Updates = func() error { return runUpdatesScreen(context.Background(), c, version) }
	return m
}

// updatesRun is the Auto Updates screen's Run, a seam for the tests, which
// record the options runUpdatesScreen gives it.
var updatesRun = updatesscreen.Run

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
// (Records zero: /var/lib/hardn, time.Now). Each change's run is a record
// of the audit log (Options.Ran, auditRan). Each change's apply, and nothing else, runs under engineSignals,
// hardn ending after a hangup (screenApplyContext; runDNSScreen says why).
func runUpdatesScreen(ctx context.Context, c *config.Config, version string) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return updatesRun(ctx, updatesscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Config:       c,
		NewEngine:    newEngine(sys, engineOptions(ctx, sys, c)),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
		Ran:          auditRan(sys, c),
		Version:      version,
	})
}
