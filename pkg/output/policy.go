package output

import (
	"errors"
	"fmt"
)

// ErrUsage marks a usage error: bad flags or configuration (exit 2). Wrap
// it, with what was wrong: fmt.Errorf("%w: ...", ErrUsage); or, for words
// that must carry no "usage: " prefix, return an error type whose Unwrap
// gives it (pkg/cmd's refusals of plan only with an operation flag).
var ErrUsage = errors.New("usage")

// ErrConfirmationRequired: a run that would apply changes without a
// terminal to confirm them on, and without an explicit yes (exit 2, a
// usage error; refactor plan row p3-output).
var ErrConfirmationRequired = errors.New("applying changes without a terminal needs an explicit yes")

// Decision is how a run that would apply changes may confirm them.
type Decision uint8

// The decisions.
const (
	// Ask: confirm at the terminal, as pkg/ui/flow's Confirm step asks
	// (a y/N question, or a typed confirmation for lockout risk).
	Ask Decision = iota + 1
	// Apply: apply without asking; the user said yes in advance. A
	// lockout-risk change is still confirmed afterwards, from a new
	// connection, or rolled back at its deadline: yes is not that
	// confirmation.
	Apply
	// PlanOnly: show the plan and apply nothing; the run ends with
	// ErrConfirmationRequired (exit 2).
	PlanOnly
)

// Interaction is what decides whether a run may prompt.
type Interaction struct {
	// InputTTY: stdin is a terminal (term.Env.InputTTY), so a prompt can
	// be answered.
	InputTTY bool
	// JSON: the run writes the JSON document on stdout, for a script.
	JSON bool
	// Yes: the user said yes in advance (--yes, refactor plan §3.11,
	// approved 2026-11-04).
	Yes bool
}

// Confirmation is the non-interactive policy (brief Phase 3): never prompt
// without a terminal, and apply without asking only on an explicit yes.
// Without a terminal on stdin, or for a JSON run (whose stdout is the
// document, read by a script, so a prompt there would be answered by no
// one and would break the one document), a run without Yes is PlanOnly,
// with ErrConfirmationRequired, naming --yes. Yes is Apply, with or
// without a terminal. Otherwise Ask. cmd/hardn's operation flags consult
// it for a plan with changes and no lockout risk (runControls' gatePlan):
// a lockout-risk plan is confirmed by the hostname typed on a terminal and
// refused off one, Yes or not.
//
// It decides how changes are confirmed, so it applies to a plan with
// changes: a plan with nothing to change asks nothing, applies nothing and
// exits 0, and a run given ErrConfirmationRequired with such a plan is
// built as nothing to do, not as an error (so that a cron job on a hardened
// host exits 0).
func Confirmation(in Interaction) (Decision, error) {
	switch {
	case in.Yes:
		return Apply, nil
	case !in.InputTTY:
		return PlanOnly, fmt.Errorf("%w: stdin is not a terminal, so hardn does not ask: the plan is shown and nothing is applied; run it again with --yes to apply it", ErrConfirmationRequired)
	case in.JSON:
		return PlanOnly, fmt.Errorf("%w: with JSON output hardn does not ask: the plan is shown and nothing is applied; run it again with --yes to apply it", ErrConfirmationRequired)
	}
	return Ask, nil
}
