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
// (dnsctl.Controls(c)) planned and applied as runControls runs a flag's
// controls, without a question (the maintainer, 2026-10-23: "apply
// without asking, -n -g shows the plan and writes nothing, and no udhcpc
// flag (the warning only)"). The list is the file's, so hardn.yml is not
// saved. udhcpc is not stopped (StopUDHCPC false): on Alpine the plan's
// finding says it may rewrite the file. A plan refused (a manager hardn
// only reports, a container, an empty list, a fact not established, ...)
// is reported with its code and how to go on, and not applied.
func configureDNS(c *config.Config) int {
	return runControls(c, dnsctl.Controls(c))
}

// runControls runs an operation flag's controls on the plan/apply engine:
// planned over the configuration c with engineOptions, then applied
// without a question, unless the plan is refused or c.DryRun (-n) asks
// for the plan alone; its report on stdout (output.Human). A failed apply
// is rolled back. The apply alone runs under engineSignals: Ctrl-C,
// SIGTERM and a hangup during it cancel it, rolled back, and end hardn
// at once with 130, once reported; before it they end hardn as they
// always have, a plan having written nothing. It returns the exit status
// (output.ExitCode, Phase 3's): 0 for applied or nothing to change, 3 for
// a dry run with changes to make, 1 for a refusal or a failure.
func runControls(c *config.Config, controls []engine.Control) int {
	ctx := context.Background()
	sys := engineHost()
	e := engine.New(sys, engineOptions(ctx, sys, c))
	run := output.Run{Mode: output.ModeApply}
	if c.DryRun {
		run.Mode = output.ModePlan
	}
	run.Plan, run.Err = e.Plan(ctx, controls, c)
	if run.Mode == output.ModeApply && run.Err == nil && run.Plan.Err() == nil {
		actx, stop := engineSignals(ctx)
		defer stop()
		res, err := e.Apply(actx, run.Plan)
		run.Result, run.Err = &res, err
	}
	if err := output.Emit(output.Streams{Stdout: engineStdout, Out: stdoutEnv()}, output.FormatHuman, run, revertNow()); err != nil {
		logging.LogError("%v", err)
	}
	code := output.ExitCode(run)
	if code == output.ExitInterrupted {
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
//
// Each change's apply, and nothing else, runs under engineSignals
// (dnsscreen.Options.ApplyContext; engine.md §2.14): Ctrl-C, SIGTERM and
// a hangup during it cancel it, and the change is rolled back here
// rather than left half applied; the screen then goes on. They are
// registered again for each apply, since the hold around a rollback ends
// a registration made before it. Elsewhere, at a prompt among them, they
// end hardn as they always have: Ctrl-C at a prompt as the prompt reports
// it (prompt.ErrInterrupted), SIGTERM and a hangup by Go's default.
func runDNSScreen(ctx context.Context, c *config.Config) error {
	sys := engineHost()
	facts, err := engineDetect(ctx, sys)
	if err != nil {
		return err
	}
	env := stdoutEnv()
	return dnsscreen.Run(ctx, dnsscreen.Options{
		Prompter:     nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)},
		System:       sys,
		Facts:        facts,
		Config:       c,
		NewEngine:    newEngine(sys, engineOptions(ctx, sys, c)),
		ApplyContext: engineSignals,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
	})
}
