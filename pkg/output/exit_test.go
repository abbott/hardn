package output_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestExitCode: one function over results and errors gives each run its
// exit status and outcome (brief Phase 3: 0, 1, 2, 3, 130).
func TestExitCode(t *testing.T) {
	report := func(s engine.State) engine.Report {
		return engine.Report{Control: "c", Title: "C", Status: engine.Status{State: s}}
	}
	empty := &engine.Plan{ID: fixtureID}
	refused := &engine.Plan{ID: fixtureID, Refused: []error{fmt.Errorf("engine: %w", engine.ErrUnsupported)}}
	rolledBack := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/reload", Step: engine.StepApply, Err: fmt.Errorf("x: %w", ufw.ErrVerify)}
	incomplete := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/reload", Step: engine.StepApply, Err: errors.New("x"),
		RollbackErr: fmt.Errorf("revert: %w", revert.ErrIncomplete)}
	elsewhere := &engine.ApplyError{ID: fixtureID, Step: engine.StepJournalAfter, Err: errors.New("x"), RollbackErr: revert.ErrRollingBack}
	interrupted := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/files", Step: engine.StepStart, Err: context.Canceled}
	interruptedIncomplete := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/files", Step: engine.StepStart, Err: context.Canceled,
		RollbackErr: fmt.Errorf("revert: %w", revert.ErrIncomplete)}
	confirm := &engine.Confirm{ID: fixtureID}
	for _, tc := range []struct {
		name    string
		run     output.Run
		exit    int
		outcome output.Outcome
	}{
		{"check: all configured or not applicable", output.Run{Mode: output.ModeCheck, Reports: []engine.Report{report(engine.Configured), report(engine.NotApplicable)}},
			0, output.OutcomeNothingToDo},
		{"check: not configured", output.Run{Mode: output.ModeCheck, Reports: []engine.Report{report(engine.Configured), report(engine.NotConfigured)}},
			3, output.OutcomeChangesNeeded},
		{"check: partial", output.Run{Mode: output.ModeCheck, Reports: []engine.Report{report(engine.Partial)}}, 3, output.OutcomeChangesNeeded},
		{"check: unknown is not hardened", output.Run{Mode: output.ModeCheck, Reports: []engine.Report{report(engine.Unknown)}}, 3, output.OutcomeChangesNeeded},
		{"check: a check failed", output.Run{Mode: output.ModeCheck, Reports: []engine.Report{{Control: "c", Err: fmt.Errorf("x: %w", ufw.ErrStateUnknown)}}},
			1, output.OutcomeFailed},
		{"check: detection cancelled", output.Run{Mode: output.ModeCheck, Err: fmt.Errorf("engine: detect: %w", context.Canceled)}, 130, output.OutcomeCancelled},
		{"plan: nothing to change", output.Run{Mode: output.ModePlan, Plan: empty}, 0, output.OutcomeNothingToDo},
		{"plan: changes", output.Run{Mode: output.ModePlan, Plan: portPlan()}, 3, output.OutcomeChangesNeeded},
		{"plan: refused", output.Run{Mode: output.ModePlan, Plan: refused, Err: refused.Err()}, 1, output.OutcomeRefused},
		{"plan: refused, the error not passed", output.Run{Mode: output.ModePlan, Plan: refused}, 1, output.OutcomeRefused},
		{"plan: no plan", output.Run{Mode: output.ModePlan, Err: errors.New("engine: run directory: x")}, 1, output.OutcomeFailed},
		{"usage", output.Run{Mode: output.ModePlan, Err: fmt.Errorf("%w: --frobnicate", output.ErrUsage)}, 2, output.OutcomeUsage},
		{"usage: a bad confirm timeout", output.Run{Mode: output.ModeApply, Err: fmt.Errorf("engine: %w", revert.ErrTimeout)}, 2, output.OutcomeUsage},
		{"usage: prohibit-password not applied", output.Run{Mode: output.ModeApply, Err: fmt.Errorf("config: %w", config.ErrRootLoginNotApplied)}, 2, output.OutcomeUsage},
		{"usage: a change id that is not one", output.Run{Mode: output.ModeApply, Err: revert.ErrID}, 2, output.OutcomeUsage},
		{"no terminal and no yes: the plan, exit 2", output.Run{Mode: output.ModeApply, Plan: portPlan(), Err: confirmationRequired()}, 2, output.OutcomeUsage},
		{"apply: nothing", output.Run{Mode: output.ModeApply, Plan: empty, Result: &engine.Result{Nothing: true}}, 0, output.OutcomeNothingToDo},
		{"apply: applied", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{ID: fixtureID, Applied: applied()}}, 0, output.OutcomeApplied},
		{"apply: waiting for confirmation", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{ID: fixtureID, Applied: applied(), Confirm: confirm}},
			0, output.OutcomeAwaiting},
		{"apply: failed, rolled back", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{Failed: rolledBack}, Err: rolledBack},
			1, output.OutcomeRolledBack},
		{"apply: failed, the rollback incomplete", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{Failed: incomplete}, Err: incomplete},
			1, output.OutcomeRollbackIncomplete},
		{"apply: failed, rolled back elsewhere", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{Failed: elsewhere}, Err: elsewhere},
			1, output.OutcomeRollbackIncomplete},
		{"apply: Ctrl-C, rolled back", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{Failed: interrupted}, Err: interrupted},
			130, output.OutcomeCancelled},
		// The outcome says nothing of the rollback: result.rollback does
		// (TestCancelledRollback).
		{"apply: Ctrl-C, the rollback incomplete", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{Failed: interruptedIncomplete},
			Err: interruptedIncomplete}, 130, output.OutcomeCancelled},
		{"apply: refused at plan time", output.Run{Mode: output.ModeApply, Plan: refused, Result: &engine.Result{}, Err: refused.Err()}, 1, output.OutcomeRefused},
		{"apply: the plan changed under the lock", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{},
			Err: fmt.Errorf("engine: change %s: %w", fixtureID, engine.ErrPlanChanged)}, 1, output.OutcomeRefused},
		{"apply: another apply holds the run lock", output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{},
			Err: fmt.Errorf("engine: %w", &revert.LockedError{Run: true, Err: host.ErrLocked})}, 1, output.OutcomeFailed},
		{"apply: the boot unit not installed", output.Run{Mode: output.ModeApply, Plan: portPlan(),
			Result: &engine.Result{Boot: &engine.BootInstall{Err: engine.ErrBootUnit}}, Err: engine.ErrBootUnit}, 1, output.OutcomeFailed},
		{"apply: declined at the confirmation", output.Run{Mode: output.ModeApply, Plan: portPlan(), Declined: true}, 3, output.OutcomeDeclined},
		{"apply: quit (q) at the confirmation", output.Run{Mode: output.ModeApply, Plan: portPlan(), Err: nav.ErrQuit}, 3, output.OutcomeDeclined},
		{"apply: quit (q) before the plan", output.Run{Mode: output.ModeApply, Err: nav.ErrQuit}, 3, output.OutcomeDeclined},
		{"apply: back (0) from the form's first field", output.Run{Mode: output.ModeApply, Err: fmt.Errorf("form: %w", nav.ErrBack)}, 3, output.OutcomeDeclined},
		{"no terminal and no yes, nothing to change: 0", output.Run{Mode: output.ModeApply, Plan: empty, Err: confirmationRequired()}, 0, output.OutcomeNothingToDo},
		{"apply: a prompt aborted", output.Run{Mode: output.ModeApply, Plan: portPlan(), Err: prompt.ErrEndOfInput}, 130, output.OutcomeCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := output.ExitCode(tc.run); got != tc.exit {
				t.Errorf("ExitCode = %d, want %d", got, tc.exit)
			}
			d := output.Build(tc.run)
			if d.Result.Outcome != tc.outcome || d.Result.ExitCode != tc.exit || tc.outcome.Exit() != tc.exit {
				t.Errorf("outcome %s (exit %d), want %s; errors %+v", d.Result.Outcome, d.Result.ExitCode, tc.outcome, d.Errors)
			}
			if (tc.outcome == output.OutcomeDeclined || tc.outcome == output.OutcomeNothingToDo) && len(d.Errors) > 0 {
				t.Errorf("errors %+v for a run that is not an error", d.Errors)
			}
		})
	}
	for _, f := range fixtures() {
		if got := output.ExitCode(f.run); got != f.exit {
			t.Errorf("fixture %s: exit %d, want %d", f.name, got, f.exit)
		}
	}
}

func confirmationRequired() error {
	_, err := output.Confirmation(output.Interaction{})
	return err
}

// TestConfirmation is the non-interactive policy: never prompt without a
// terminal, nor for a JSON run; apply without asking on an explicit yes
// alone.
func TestConfirmation(t *testing.T) {
	for _, tc := range []struct {
		in   output.Interaction
		want output.Decision
		err  bool
	}{
		{output.Interaction{InputTTY: true}, output.Ask, false},
		{output.Interaction{}, output.PlanOnly, true},
		{output.Interaction{InputTTY: true, JSON: true}, output.PlanOnly, true},
		{output.Interaction{JSON: true}, output.PlanOnly, true},
		{output.Interaction{Yes: true}, output.Apply, false},
		{output.Interaction{InputTTY: true, Yes: true}, output.Apply, false},
		{output.Interaction{JSON: true, Yes: true}, output.Apply, false},
	} {
		got, err := output.Confirmation(tc.in)
		if got != tc.want || (err != nil) != tc.err {
			t.Errorf("Confirmation(%+v) = %v, %v; want %v (error %v)", tc.in, got, err, tc.want, tc.err)
		}
		if err != nil && (!errors.Is(err, output.ErrConfirmationRequired) || output.CodeOf(err) != "CONFIRMATION_REQUIRED") {
			t.Errorf("%+v: error %v, code %s", tc.in, err, output.CodeOf(err))
		}
	}
}

// TestHostAndConfirm: detect's facts in the schema's own words, and the
// commands in the host's elevation tool, the confirmation's and the one
// that finishes a rollback: doas on an Alpine host without sudo, sudo
// elsewhere (engine.md §7).
func TestHostAndConfirm(t *testing.T) {
	facts := alpine324()
	r := fixtures()[3].run
	r.Facts = &facts
	d := output.Build(r)
	if h := d.Host; h.Platform.Distro != "alpine" || h.Init != "openrc" || h.SSH != "openrc" || h.Elevation.Sudo != "no" || h.Elevation.Doas != "yes" ||
		h.Elevation.SudoFlavor != "unknown" || h.Platform.Support != "supported" {
		t.Errorf("host %+v", h)
	}
	if c := d.Result.Confirm; c == nil || c.Command != "doas hardn confirm" || c.Undo != "doas hardn rollback "+fixtureID ||
		!c.Approximate || len(c.Via) != 2 || c.Via[0] != "0.0.0.0:2230" || c.Via[1] != "[::]:2230" {
		t.Errorf("confirm %+v", c)
	}
	if d := output.Build(fixtures()[3].run); d.Result.Confirm.Command != "sudo hardn confirm" {
		t.Errorf("Debian: %s", d.Result.Confirm.Command)
	}
	incomplete := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/reload", Step: engine.StepApply, Err: errors.New("x"),
		RollbackErr: fmt.Errorf("revert: %w", revert.ErrIncomplete)}
	for _, tc := range []struct {
		facts detect.Facts
		want  string
	}{{alpine324(), "doas hardn rollback " + fixtureID}, {debian13(), "sudo hardn rollback " + fixtureID}} {
		r := output.Run{Mode: output.ModeApply, Facts: &tc.facts, Plan: portPlan(), Result: &engine.Result{ID: fixtureID, Failed: incomplete}, Err: incomplete}
		if rb := output.Build(r).Result.Rollback; rb == nil || rb.Command != tc.want {
			t.Errorf("%s: rollback %+v, want the command %q", tc.facts.OS.ID, rb, tc.want)
		}
	}
	if d := output.Build(output.Run{Mode: output.ModeCheck}); d.Host.Init != "unknown" || d.Host.Platform.Support != "unknown" ||
		d.Host.Proxmox.Installed != "unknown" || d.Changes == nil || d.Errors == nil || d.Findings == nil || d.Status == nil {
		t.Errorf("no facts: %+v", d)
	}
}
