package main

// Run All on its planner (refactor plan slice 8, Run All, unit (d)): the
// main menu's Run All item (7) runs the Run All screen
// (pkg/screens/runall), which pkg/menu reaches through the hook set here,
// as it reaches the SSH Login screen (ssh.go), so that pkg/menu builds no
// engine (gallery-runall.md §5, decision 7 (a): the item's label and
// description kept). The engine's options are engine.go's.

import (
	"context"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// runAllMenu is what withRunAllScreen sets the Run All item of: pkg/menu's
// main menu, and the tests' record of what was set.
type runAllMenu interface{ SetRunAll(run func() error) }

// The main menu is a runAllMenu.
var _ runAllMenu = (*menu.MainMenu)(nil)

// withRunAllScreen returns m with its Run All item set to the Run All
// screen on c, the configuration m was given, and hardn's version
// (menu.MainMenu.SetRunAll).
func withRunAllScreen[M runAllMenu](m M, c *config.Config, version string) M {
	m.SetRunAll(func() error { return runRunAllScreen(context.Background(), c, version) })
	return m
}

// runAllRun is the Run All screen's Run, a seam for the tests, which
// record the options runRunAllScreen gives it.
var runAllRun = runallscreen.Run

// runRunAllScreen is the main menu's Run All item: the Run All screen until
// the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runSSHScreen runs the SSH Login screen: on the
// host hardn builds, with its run log in hardn's log, the facts detected
// once per visit, hardn's environment (os.Getenv), from which the
// controls take the session user, through the one line reader every
// prompt shares and stdout's render environment; the planner's reads and
// each change on an engine with engineOptions, assembled for this visit
// from c as it is now (hardn.yml's backups), and the session the planner
// reads the SSH Port row with the one those options hold, so that the
// planner and the engine never disagree on how hardn's user logged in; c,
// the configuration the main menu holds, what Run All is asked, which no
// change saves; hardn's version, in Auto Updates' record; a dry run when c
// says so as the item is chosen, Apply all then stopping at its plan.
// Revert is zero: the journal's options are hardn's own. No audit record
// is written (Options.Ran left nil, as the other screens'). Each change's
// apply, and nothing else, runs under engineSignals, hardn ending after a
// hangup (screenApplyContext; runDNSScreen says why).
func runRunAllScreen(ctx context.Context, c *config.Config, version string) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	o := engineOptions(ctx, sys, c)
	env := stdoutEnv()
	return runAllRun(ctx, runallscreen.Options{
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
		Version:      version,
	})
}
