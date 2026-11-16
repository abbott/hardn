package main

// Syslog Settings on the plan/apply engine (docs/roadmap.md, "Log
// forwarding"; docs/engine/gallery-logforward.md, adopted 2026-11-13): the
// Configuration group's Syslog Settings item (main menu 8, then 4) runs the
// Syslog Settings screen (pkg/screens/logforward), which the group reaches
// through readers.go's runConfigGroup. The engine's options are engine.go's.
// No flag routes to it.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	logforwardscreen "github.com/abbott/hardn/pkg/screens/logforward"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// logForwardRun is the Syslog Settings screen's Run, a seam for the tests,
// which record the options runLogForwardScreen gives it.
var logForwardRun = logforwardscreen.Run

// runLogForwardScreen is the Configuration group's Syslog Settings item: the screen
// until the user goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted), as runUpdatesScreen runs the Auto Updates screen:
// on the host hardn builds, the facts detected once per visit, through the
// one line reader every prompt shares and stdout's render environment;
// each change on an engine with engineOptions; c, the configuration the
// main menu holds, whose syslogServers names the servers and which no
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
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout), Terminal: stdoutEnv},
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
