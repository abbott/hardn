package main

// Environment on the plan/apply engine (refactor plan slice 9,
// Environment, unit (d), next/s9-replace; plan §3.14: the legacy path
// replaced, not gated): the main menu's Environment item runs the
// Environment screen (pkg/screens/environment), which pkg/menu reaches
// through the hook set here, as it reaches the DNS and Backup screens
// (dns.go, backup.go), so that pkg/menu builds no engine. The engine's
// options are engine.go's.

import (
	"context"
	"os"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	envscreen "github.com/abbott/hardn/pkg/screens/environment"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// environmentGetenv is hardn's environment as the Environment screen reads
// it: who ran hardn through sudo or doas (SUDO_USER, DOAS_USER) and
// HARDN_CONFIG as the tool passed it on. A seam for the tests.
var environmentGetenv = os.Getenv

// environmentMenu is what withEnvironmentScreen sets the Environment item
// of: pkg/menu's main menu, and the tests' record of what was set.
type environmentMenu interface{ SetEnvironment(run func() error) }

// The main menu is an environmentMenu.
var _ environmentMenu = (*menu.MainMenu)(nil)

// withEnvironmentScreen returns m with its Environment item set to the
// Environment screen on c, the configuration m was given
// (menu.MainMenu.SetEnvironment).
func withEnvironmentScreen[M environmentMenu](m M, c *config.Config) M {
	m.SetEnvironment(func() error { return runEnvironmentScreen(context.Background(), c) })
	return m
}

// runEnvironmentScreen is the main menu's Environment item: the
// Environment screen until the user goes back (nil) or quits
// (nav.ErrQuit), or the input ends (prompt.ErrAborted), as runDNSScreen
// runs the DNS screen: on the host hardn builds, with its run log in
// hardn's log, the facts detected once per visit, hardn's environment
// (environmentGetenv), through the one line reader every prompt shares
// and stdout's render environment; each change on an engine with
// engineOptions, assembled for this visit from c as it is now (its backup
// settings, the session a lockout-risk change's journal names, hardn's
// path for the revert timer); a dry run when c says so as the item is
// chosen, item 1 then stopping at its plan. No Config: the screen reads
// and saves no setting of hardn.yml. No audit record is written: the
// audit log's first writer is next/p3-startup's (Options.Ran left nil).
// Each change's apply, and nothing else, runs under engineSignals,
// hardn ending after a hangup (screenApplyContext; runDNSScreen says
// why).
func runEnvironmentScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return envscreen.Run(ctx, envscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Getenv:       environmentGetenv,
		NewEngine:    newEngine(sys, engineOptions(ctx, sys, c)),
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
	})
}
