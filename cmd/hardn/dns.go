package main

// DNS on the plan/apply engine (refactor plan slice 2, the DNS pilot,
// unit (d), next/s2-replace; the maintainer, 2026-10-22: the legacy path
// replaced, not gated): -g plans and applies the DNS control, and the main
// menu's DNS item runs the DNS screen (pkg/screens/dns), which pkg/menu
// reaches through the hook set here, so that pkg/menu builds no engine.
// The engine's options are engine.go's.

import (
	"context"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/output"
	dnsscreen "github.com/abbott/hardn/pkg/screens/dns"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// configureDNS is -g: the DNS control over hardn.yml's own list
// (dnsctl.Controls(c)) planned and applied through the engine with
// engineOptions, without a question (the maintainer, 2026-10-23: "apply
// without asking, -n -g shows the plan and writes nothing, and no udhcpc
// flag (the warning only)"), its report on stdout (output.Human). The
// list is the file's, so hardn.yml is not saved. udhcpc is not stopped
// (StopUDHCPC false): on Alpine the plan's finding says it may rewrite
// the file. A dry run (c.DryRun, as -n sets it) is the plan alone. A plan
// refused (a manager hardn only reports, a container, an empty list, a
// fact not established, ...) is reported with its code and how to go on,
// and not applied; a failed apply is rolled back. Ctrl-C, SIGTERM and a
// hangup cancel it, an apply rolled back, and end hardn at once with
// 130. It returns the exit status (output.ExitCode, Phase 3's): 0 for
// applied or nothing to change, 3 for a dry run with changes to make, 1
// for a refusal or a failure.
func configureDNS(c *config.Config) int {
	ctx, stop := engineSignals(context.Background())
	defer stop()
	sys := engineHost()
	e := engine.New(sys, engineOptions(ctx, sys, c))
	run := output.Run{Mode: output.ModeApply}
	if c.DryRun {
		run.Mode = output.ModePlan
	}
	run.Plan, run.Err = e.Plan(ctx, dnsctl.Controls(c), c)
	if run.Mode == output.ModeApply && run.Err == nil && run.Plan.Err() == nil {
		res, err := e.Apply(ctx, run.Plan)
		run.Result, run.Err = &res, err
	}
	if err := output.Emit(output.Streams{Stdout: engineStdout, Out: stdoutEnv()}, output.FormatHuman, run, revertNow()); err != nil {
		logging.LogError("%v", err)
	}
	code := output.ExitCode(run)
	if code == output.ExitInterrupted {
		stop()
		exit(code)
	}
	return code
}

// withDNSScreen returns m with its DNS item set to the DNS screen on c,
// the configuration m was given (menu.MainMenu.SetDNS).
func withDNSScreen(m *menu.MainMenu, c *config.Config) *menu.MainMenu {
	m.SetDNS(func() error { return runDNSScreen(context.Background(), c) })
	return m
}

// runDNSScreen is the main menu's DNS item: the DNS screen until the user
// goes back (nil) or quits (nav.ErrQuit), or the input ends
// (prompt.ErrAborted). It runs on the host hardn builds, with its run log
// in hardn's log, the facts detected once, through the one line reader
// every prompt shares and stdout's render environment; each change on an
// engine with engineOptions, a dry run when c says so as the item is
// chosen (the dry-run setting as the legacy menus read it). The screen
// adopts hardn.yml as each change saved it into c (dnsscreen.Options), the
// configuration the main menu holds. No audit record is written: the
// audit log's first writer is next/p3-startup's (Options.Ran left nil).
func runDNSScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return dnsscreen.Run(ctx, dnsscreen.Options{
		Prompter:  nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:    sys,
		Facts:     facts,
		Config:    c,
		NewEngine: newEngine(sys, engineOptions(ctx, sys, c)),
		DryRun:    c.DryRun,
		LogPath:   output.LegacyLogPath,
		Now:       revertNow,
	})
}
