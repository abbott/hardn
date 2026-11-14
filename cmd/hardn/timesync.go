package main

// Time Sync on the plan/apply engine (docs/roadmap.md, "Time sync";
// docs/engine/gallery-timesync.md, adopted 2026-11-14): the main menu's Time Sync item (7) runs the Time Sync screen
// (pkg/screens/timesync), which pkg/screens/mainmenu reaches through the
// hook set here, as it reaches the Log Forwarding screen (logforward.go). The
// engine's options are engine.go's. No flag routes to it.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	timesyncscreen "github.com/abbott/hardn/pkg/screens/timesync"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// withTimeSyncScreen returns m with its Time Sync item set to the
// Time Sync screen on c, the configuration m was given
// (mainmenu.Options.TimeSync).
func withTimeSyncScreen(m mainmenu.Options, c *config.Config) mainmenu.Options {
	m.TimeSync = func() error { return runTimeSyncScreen(context.Background(), c) }
	return m
}

// timeSyncRun is the Time Sync screen's Run, a seam for the tests,
// which record the options runTimeSyncScreen gives it.
var timeSyncRun = timesyncscreen.Run

// runTimeSyncScreen is the main menu's Time Sync item: the screen
// until the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runUpdatesScreen runs the Auto Updates screen:
// on the host hardn builds, the facts detected once per visit, through the
// one line reader every prompt shares and stdout's render environment;
// each change on an engine with engineOptions; c, the configuration the
// main menu holds, whose ntpServers names the servers and which no
// change saves; a dry run when c says so as the item is chosen. Each
// change's run is a record of the audit log (auditRan), and each change's
// apply, and nothing else, runs under engineSignals (screenApplyContext).
func runTimeSyncScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return timeSyncRun(ctx, timesyncscreen.Options{
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
	})
}
