package output

import (
	"errors"

	"github.com/abbott/hardn/pkg/engine"
)

// hardn's exit statuses (brief Phase 3, decided in brief §2): a public
// interface that does not change once released.
const (
	// ExitOK: success; for a check or a plan, nothing needs changing. An
	// apply waiting for confirmation is a success too.
	ExitOK = 0
	// ExitError: an error. What this run applied, it rolled back
	// (Result.Rollback says whether that finished). A refusal is 1 too:
	// the run did not do what was asked, and changed nothing.
	ExitError = 1
	// ExitUsage: bad flags or configuration (pkg/cmd's ExitUsage).
	ExitUsage = 2
	// ExitChanges: a check or a plan found changes to make, so a script
	// can tell when a host has drifted from its hardened state.
	ExitChanges = 3
	// ExitInterrupted: interrupted, by Ctrl-C (the shell's 128 + SIGINT;
	// pkg/ui/prompt's ExitInterrupted), or by SIGTERM or a hangup, which
	// the engine's caller treats as Ctrl-C (engine.md §2.14).
	ExitInterrupted = 130
)

// Exit is the exit status of a run that ended with o.
func (o Outcome) Exit() int {
	switch o {
	case OutcomeNothingToDo, OutcomeApplied, OutcomeAwaiting:
		return ExitOK
	case OutcomeChangesNeeded:
		return ExitChanges
	case OutcomeUsage:
		return ExitUsage
	case OutcomeCancelled:
		return ExitInterrupted
	}
	return ExitError
}

// ExitCode is run r's exit status: one function over the run's results
// and errors, the same that Build writes as result.exitCode.
//
//   - 130 when anything in the run was interrupted (CANCELLED), whatever
//     else failed;
//   - 2 for a usage error (a code whose Entry.Exit is ExitUsage);
//   - 1 for any other error: an apply that failed (rolled back or not), a
//     refused plan, an error before anything was armed, a check that
//     failed;
//   - 3 when a check found a control not as asked (not configured,
//     partial, or unknown: a state not established is not taken as
//     hardened), when a plan has changes, and when the user declined them;
//   - 0 otherwise: nothing to change, or every change applied (waiting
//     for confirmation included).
func ExitCode(r Run) int { return outcomeOf(r).Exit() }

// outcomeOf is how run r ended.
func outcomeOf(r Run) Outcome {
	exit := ExitOK // the highest of the errors': 130, then 2, then 1
	for _, e := range errorsOf(r) {
		exit = max(exit, exitOf(e.Code))
	}
	switch exit {
	case ExitInterrupted:
		return OutcomeCancelled
	case ExitUsage:
		return OutcomeUsage
	}
	switch r.Mode {
	case ModeCheck:
		if exit != ExitOK {
			return OutcomeFailed
		}
		for _, rep := range r.Reports {
			if s := rep.Status.State; s != engine.Configured && s != engine.NotApplicable {
				return OutcomeChangesNeeded
			}
		}
		return OutcomeNothingToDo
	case ModePlan:
		return planOutcome(r, exit)
	}
	res := r.Result
	switch {
	case res != nil && res.Failed != nil && errors.Is(res.Failed, engine.ErrRolledBack):
		return OutcomeRolledBack
	case res != nil && res.Failed != nil:
		return OutcomeRollbackIncomplete
	case res == nil || r.Declined:
		return planOutcome(r, exit)
	case exit != ExitOK && refused(r):
		return OutcomeRefused
	case exit != ExitOK:
		return OutcomeFailed
	case res.Nothing:
		return OutcomeNothingToDo
	case res.Confirm != nil:
		return OutcomeAwaiting
	}
	return OutcomeApplied
}

// planOutcome is the outcome of a plan that was not applied: refused, an
// error, no changes, or changes to make.
func planOutcome(r Run, exit int) Outcome {
	switch {
	case refused(r):
		return OutcomeRefused
	case exit != ExitOK || r.Plan == nil:
		return OutcomeFailed
	case r.Plan.Empty():
		return OutcomeNothingToDo
	}
	return OutcomeChangesNeeded
}

// refused reports whether the run's plan was refused, when it was shown or
// when it was made again under the run lock.
func refused(r Run) bool {
	return (r.Plan != nil && len(r.Plan.Refused) > 0) || errors.Is(r.Err, engine.ErrRefused) || errors.Is(r.Err, engine.ErrPlanChanged)
}

// exitOf is the exit status an error of code gives.
func exitOf(code string) int {
	if e, ok := byCode[code]; ok && e.Kind == KindError {
		return e.Exit
	}
	return ExitError
}
