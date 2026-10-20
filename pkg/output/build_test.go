package output_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
)

// TestRollbackNotFinished: when the rollback in this run did not finish,
// or another process had the change, the changes this run applied are
// unknown, never applied (nor rolled back): whether they stand is not
// established. The next step says what to do in each case.
func TestRollbackNotFinished(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rerr     error
		rr       revert.RollbackResult
		step     string
		outcome  output.Outcome
		finished bool
		next     string
	}{
		{"incomplete", fmt.Errorf("revert: %w", revert.ErrIncomplete), revert.RollbackResult{}, engine.StepApply,
			output.OutcomeRollbackIncomplete, false, "The rollback did not finish: run sudo hardn rollback " + fixtureID + " to finish it."},
		{"interrupted, incomplete", fmt.Errorf("revert: %w", revert.ErrIncomplete), revert.RollbackResult{}, engine.StepStart,
			output.OutcomeCancelled, false, "The rollback did not finish: run sudo hardn rollback " + fixtureID + " to finish it."},
		{"another process's rollback runs", fmt.Errorf("revert: %w", revert.ErrRollingBack), revert.RollbackResult{}, engine.StepApply,
			output.OutcomeRollbackIncomplete, false, "Another hardn process is rolling the change back; wait for it to end."},
		{"the journal gone", fmt.Errorf("engine: nothing rolled back here: it is not journalled: confirmed, or rolled back already: %w", revert.ErrRollingBack),
			revert.RollbackResult{Nothing: true, Why: "it is not journalled: confirmed, or rolled back already"}, engine.StepApply,
			output.OutcomeRollbackIncomplete, false,
			"Another hardn process had taken the change, so this run undid nothing: it is not journalled: confirmed, or rolled back already."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.New("x")
			if tc.step == engine.StepStart {
				err = context.Canceled
			}
			ae := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/reload", Step: tc.step, Err: err, Rollback: tc.rr, RollbackErr: tc.rerr}
			run := output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{ID: fixtureID, Applied: applied()[:2], Failed: ae}, Err: ae}
			d := output.Build(run)
			if d.Result.Outcome != tc.outcome || d.Result.Rollback.Finished != tc.finished {
				t.Errorf("outcome %s, rollback %+v", d.Result.Outcome, d.Result.Rollback)
			}
			for _, c := range d.Changes[1:3] {
				if c.Status != output.StatusUnknown {
					t.Errorf("%s: %s, want unknown", c.ID, c.Status)
				}
			}
			if h := squash(humanText(t, run)); !strings.Contains(h, squash(tc.next)) {
				t.Errorf("no %q in\n%s", tc.next, humanText(t, run))
			}
		})
	}
}

// TestWrappedApplyError: an ApplyError the caller wraps in the run's
// error is still reported once, as the apply's.
func TestWrappedApplyError(t *testing.T) {
	ae := &engine.ApplyError{ID: fixtureID, Change: "ssh.port/reload", Step: engine.StepApply, Err: fmt.Errorf("x: %w", ufw.ErrVerify)}
	run := output.Run{Mode: output.ModeApply, Plan: portPlan(), Result: &engine.Result{ID: fixtureID, Failed: ae}, Err: fmt.Errorf("hardn -w: %w", ae)}
	if d := output.Build(run); len(d.Errors) != 1 || d.Errors[0].Code != "UFW_VERIFY_FAILED" || d.Errors[0].Change != "ssh.port/reload" {
		t.Errorf("errors %+v, want the apply's once", d.Errors)
	}
}

// holder is an error of a struct type holding an error, which Go deems
// comparable whatever the error inside is.
type holder struct{ inner error }

func (h holder) Error() string { return "holder: " + h.inner.Error() }

// listErr is an error of an uncomparable type.
type listErr struct{ errs []error }

func (listErr) Error() string { return "list" }

// TestBuildNeverPanics: two errors of a comparable struct type holding an
// uncomparable value would panic if compared; Build does not compare them,
// and reports each.
func TestBuildNeverPanics(t *testing.T) {
	bad := holder{inner: listErr{errs: []error{nil}}}
	run := output.Run{Mode: output.ModeCheck, Reports: []engine.Report{{Control: "a", Err: bad}, {Control: "b", Err: bad}}}
	if d := output.Build(run); len(d.Errors) != 2 {
		t.Errorf("errors %+v, want both", d.Errors)
	}
}
