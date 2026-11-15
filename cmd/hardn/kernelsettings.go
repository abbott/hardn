package main

// Kernel Settings on the plan/apply engine (docs/roadmap.md, "sysctl";
// docs/engine/gallery-sysctl.md, adopted 2026-11-14): the Configuration
// group's Kernel Settings item (main menu 8, then 4) runs the Kernel
// Settings screen (pkg/screens/kernelsettings), which the group reaches
// through readers.go's runConfigGroup. The engine's options are engine.go's.
// No flag routes to it.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/output"
	kernelscreen "github.com/abbott/hardn/pkg/screens/kernelsettings"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// kernelSettingsRun is the Kernel Settings screen's Run, a seam for the
// tests, which record the options runKernelSettingsScreen gives it.
var kernelSettingsRun = kernelscreen.Run

// runKernelSettingsScreen is the Configuration group's Kernel Settings
// item: the screen until the user goes back (nil) or quits (nav.ErrQuit),
// or the input ends (prompt.ErrAborted), as runTimeSyncScreen runs the NTP
// Settings screen: on the host hardn builds, the facts detected once per visit,
// through the one line reader every prompt shares and stdout's render
// environment; each change on an engine with engineOptions; c, the
// configuration the main menu holds, which no change reads a key of or
// saves; a dry run when c says so as the item is chosen. Each change's run
// is a record of the audit log (auditRan), and each change's apply, and
// nothing else, runs under engineSignals (screenApplyContext).
func runKernelSettingsScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return kernelSettingsRun(ctx, kernelscreen.Options{
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
