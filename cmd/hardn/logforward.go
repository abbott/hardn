package main

// Log Forwarding on the plan/apply engine (docs/roadmap.md, "Log
// forwarding"; docs/engine/gallery-logforward.md, adopted 2026-11-13): the
// main menu's Log Forwarding item (6) runs the Log Forwarding screen
// (pkg/screens/logforward), which pkg/screens/mainmenu reaches through the
// hook set here, as it reaches the Auto Updates screen (updates.go). The
// engine's options are engine.go's. No flag routes to it.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	logforwardscreen "github.com/abbott/hardn/pkg/screens/logforward"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// withLogForwardScreen returns m with its Log Forwarding item set to the
// Log Forwarding screen on c, the configuration m was given
// (mainmenu.Options.LogForwarding).
func withLogForwardScreen(m mainmenu.Options, c *config.Config) mainmenu.Options {
	m.LogForwarding = func() error { return runLogForwardScreen(context.Background(), c) }
	return m
}

// logForwardRun is the Log Forwarding screen's Run, a seam for the tests,
// which record the options runLogForwardScreen gives it.
var logForwardRun = logforwardscreen.Run

// runLogForwardScreen is the main menu's Log Forwarding item: the screen
// until the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runUpdatesScreen runs the Auto Updates screen:
// on the host hardn builds, the facts detected once per visit, through the
// one line reader every prompt shares and stdout's render environment;
// each change on an engine with engineOptions; c, the configuration the
// main menu holds, whose logForwarding names the server and which no
// change saves; a dry run when c says so as the item is chosen. Each
// change's run is a record of the audit log (auditRan), and each change's
// apply, and nothing else, runs under engineSignals (screenApplyContext).
func runLogForwardScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return logForwardRun(ctx, logforwardscreen.Options{
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
