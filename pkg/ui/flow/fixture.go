package flow

import (
	"context"

	"github.com/abbott/hardn/pkg/ui/component"
)

// Fixture is an Engine that touches nothing: its plan is fixed data, and
// applying it only reports each step as Engine.Apply describes, so that
// Phase 1's tests and gallery run the flow before the Phase 3 engine
// exists. A step can be made to fail, to show the rollback.
type Fixture struct {
	// Planned is the plan Plan returns.
	Planned Plan
	// FailAt is the step that fails, counted from 1; zero or less fails
	// none.
	FailAt int
	// Detail says why the step failed, beside it, such as "exit status 1".
	Detail string
	// Failure is what Apply returns when the step fails.
	Failure Failure
}

// Plan returns f.Planned, or ctx's error when it is done.
func (f Fixture) Plan(ctx context.Context) (Plan, error) {
	if err := ctx.Err(); err != nil {
		return Plan{}, err
	}
	return f.Planned, nil
}

// Apply reports each step of f.Planned running, then done, in order. At
// step FailAt it reports the step failed with f.Detail, reports the steps
// done before it rolled back, the last first, and returns a copy of
// f.Failure; when ctx is done before a step, it rolls back the same way
// and returns ctx's error.
func (f Fixture) Apply(ctx context.Context, report Report) error {
	rollBack := func(done int) {
		for i := done - 1; i >= 0; i-- {
			report(i, component.StepRolledBack, "")
		}
	}
	for i := range f.Planned.View.Changes {
		if err := ctx.Err(); err != nil {
			rollBack(i)
			return err
		}
		report(i, component.StepRunning, "")
		if i+1 == f.FailAt {
			report(i, component.StepFailed, f.Detail)
			rollBack(i)
			failure := f.Failure
			return &failure
		}
		report(i, component.StepDone, "")
	}
	return nil
}
