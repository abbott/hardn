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
	return runControls(c, dnsctl.Controls(c), nil, nil)
}

// runControls runs an operation flag's controls on the plan/apply engine:
// planned over the configuration c with engineOptions, then applied
// without a question, unless the plan is refused or c.DryRun (-n) asks
// for the plan alone; its report on stdout (output.Human). refuse, when
// not nil, is the flag's own refusal of a plan the engine made (-e's of a
// lockout-risk plan, environment.go): an error from it is the run's, the
// plan shown with it and not applied, under -n too, so that the plan
// alone says what the flag would do. A failed apply is rolled back. The apply alone runs under engineSignals: Ctrl-C,
// SIGTERM and a hangup during it cancel it, rolled back, and end hardn
// at once with 130, once reported; before it they end hardn as they
// always have, a plan having written nothing. One that reaches the apply
// without cancelling it, SIGTERM or a hangup during the rollback of a
// failure (the engine's hold ignores Ctrl-C), or any of the three after
// the apply's last step or while the report is written, ends hardn too,
// once reported, the next flags not run: with the failure's status, or
// 130 for a change applied, so that a script never reads 0 for a run
// whose later flags did not run (since next/s3-hangup; the maintainer,
// 2026-10-28: "Yes, catch it"). A hangup's watch (watchHangup), registered
// first, around the same window, marks hardn's terminal gone as the
// hangup comes, so that the report is dropped rather than left blocking
// on a terminal nothing reads (sudo-rs's, once the session has dropped);
// a hangup it alone catches, before engineSignals' registration, ends
// hardn as one that reached the apply does.
// confirm, when not nil, is the flag's confirmation of a lockout-risk plan
// (typedConfirm, typed.go: -w's), or of any plan with changes where it
// says always (-r's). A plain plan is otherwise applied as any flag's,
// unasked, on a terminal or off. Live on a terminal, a lockout-risk plan,
// planned here first and not refused, goes to the canonical flow instead
// (runTyped), which plans again and confirms by the hostname typed; live
// off a terminal it is refused, after refuse. Under plan only confirm
// plays no part.
// It returns the exit status (output.ExitCode, Phase 3's): 0 for applied
// or nothing to change, 3 for a dry run with changes to make (or, with
// confirm, a plan declined), 1 for a refusal or a failure.
func runControls(c *config.Config, controls []engine.Control, refuse func(*engine.Plan) error, confirm *typedConfirm) int {
	live := confirm != nil && !c.DryRun
	typed := live && onTerminal()
	if live && !typed {
		if refuse == nil {
			refuse = confirm.refuse
		} else {
			own := refuse
			refuse = func(p *engine.Plan) error {
				if err := own(p); err != nil {
					return err
				}
				return confirm.refuse(p)
			}
		}
	}
	ctx := context.Background()
	sys := engineHost()
	e := engine.New(sys, engineOptions(ctx, sys, c))
	run := output.Run{Mode: output.ModeApply}
	if c.DryRun {
		run.Mode = output.ModePlan
	}
	run.Plan, run.Err = e.Plan(ctx, controls, c)
	if refuse != nil && run.Err == nil && run.Plan != nil {
		run.Err = refuse(run.Plan)
	}
	if typed && run.Err == nil && run.Plan != nil && run.Plan.Err() == nil && confirm.asks(run.Plan) {
		return runTyped(c, controls, *confirm)
	}
	actx := ctx // a signal's, around the apply and its report alone
	hungUp := func() bool { return false }
	if run.Mode == output.ModeApply && run.Err == nil && run.Plan.Err() == nil {
		hup := watchHangup() // first: no hangup engineSignals catches goes unmarked
		defer hup.stop()
		hungUp = hup.seen
		var stop context.CancelFunc
		actx, stop = engineSignals(ctx)
		defer stop()
		res, err := e.Apply(actx, run.Plan)
		run.Result, run.Err = &res, err
	}
	if err := output.Emit(output.Streams{Stdout: engineStdout, Out: stdoutEnv()}, output.FormatHuman, run, revertNow()); err != nil {
		sayError("%v", err)
	}
	return endControls(run, actx, hungUp)
}

// endControls is the exit status of a flag's run, output.ExitCode's, and
// ends hardn at once when it was interrupted, or when a signal reached its
// apply's registration, actx done, or a hangup's watch (hungUp): with 130
// for a change applied, the flags after it not run (runControls).
func endControls(run output.Run, actx context.Context, hungUp func() bool) int {
	code := output.ExitCode(run)
	signalled := actx.Err() != nil || hungUp()
	if signalled && code == output.ExitOK {
		code = output.ExitInterrupted // applied, but the flags after it are not run
	}
	if code == output.ExitInterrupted || signalled {
		exit(code)
	}
	return code
}

// dnsMenu is what withDNSScreen sets the DNS item of: pkg/menu's main
// menu, and the tests' record of what was set.
type dnsMenu interface{ SetDNS(run func() error) }

// The main menu is a dnsMenu.
var _ dnsMenu = (*menu.MainMenu)(nil)

// withDNSScreen returns m with its DNS item set to the DNS screen on c,
// the configuration m was given (menu.MainMenu.SetDNS).
func withDNSScreen[M dnsMenu](m M, c *config.Config) M {
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
// (dnsscreen.Options.ApplyContext, screenApplyContext; engine.md §2.14):
// Ctrl-C, SIGTERM and a hangup during it cancel it, and the change is
// rolled back here rather than left half applied; the screen then goes
// on, but after a hangup, during the apply or its rollback, hardn ends
// once the apply has returned. They are registered again for each apply,
// since the hold around a rollback ends a registration of Ctrl-C made
// before it. Elsewhere, at a prompt among them, they
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
		ApplyContext: screenApplyContext,
		DryRun:       c.DryRun,
		LogPath:      output.LegacyLogPath,
		Now:          revertNow,
	})
}
