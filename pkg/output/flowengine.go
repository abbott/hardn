package output

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// FlowEngine is pkg/ui/flow's Engine over the plan/apply engine
// (design-system README §9.10; engine.md §7), for one action: its controls
// and its configuration. Plan is Engine.Plan, each change a
// component.PlanChange in Plan.Changes' order; Apply is Engine.Apply of
// that plan, its progress reported from Progress, which Engine must have
// as its engine.Options.Progress:
//
//	a := &output.FlowEngine{Controls: cs, Config: cfg}
//	a.Engine = engine.New(sys, engine.Options{Progress: a.Progress, ...})
//
// Progress is the one source of the steps' states: Apply plans again under
// the run lock and runs the changes of that plan (engine.md §2.9), so the
// shown plan's Change.Apply functions are never wrapped. A plan is applied
// once (engine.ErrPlanUsed): Apply never retries; a caller that wants to
// try again runs the flow again, which plans again.
//
// After Flow.Run returns, the caller writes AfterFlow(env, a.Run(o, err),
// now) on the flow's writer: the flow ends an apply with "Operation
// Successful", and a lockout-risk change waiting for confirmation needs
// its countdown, its deadline and the confirm command shown after it.
type FlowEngine struct {
	Engine   *engine.Engine
	Controls []engine.Control
	Config   *config.Config
	// LogPath is the log the run log goes to, which an ErrorReport names;
	// "" names none.
	LogPath string
	// DryRun is the flow's (flow.Flow.DryRun), so that Run tells a dry
	// run that found nothing to change (ModePlan) from an apply that did
	// (ModeApply): the flow returns Unchanged for both.
	DryRun bool

	mu     sync.Mutex
	report flow.Report
	plan   *engine.Plan
	result *engine.Result
	err    error
}

var _ flow.Engine = (*FlowEngine)(nil)

// eventStates are the flow's step states of the engine's events.
var eventStates = map[engine.EventState]component.StepState{engine.EventRunning: component.StepRunning,
	engine.EventDone: component.StepDone, engine.EventFailed: component.StepFailed, engine.EventRolledBack: component.StepRolledBack}

// Progress reports ev to the Apply under way as the flow's Report: the
// change by its index in the plan, the event's state as a step's, and for
// a failure its code. An event of the apply as a whole (Index -1), or one
// outside an Apply, is not reported.
func (a *FlowEngine) Progress(ev engine.Event) {
	a.mu.Lock()
	report := a.report
	a.mu.Unlock()
	state, ok := eventStates[ev.State]
	if report == nil || ev.Index < 0 || !ok {
		return
	}
	detail := ""
	if ev.State == engine.EventFailed && ev.Err != nil {
		detail = CodeOf(ev.Err)
	}
	report(ev.Index, state, detail)
}

// Plan plans the action. A plan the engine refuses is an error, a
// *flow.Failure whose text is the first refusal's Why (or message) with
// its code, and whose next step is its Fix.
func (a *FlowEngine) Plan(ctx context.Context) (flow.Plan, error) {
	p, err := a.Engine.Plan(ctx, a.Controls, a.Config)
	a.mu.Lock()
	a.plan, a.result, a.err = p, nil, err
	a.mu.Unlock()
	if err == nil {
		err = p.Err()
	}
	if err != nil {
		return flow.Plan{}, a.failure(err)
	}
	fp := flow.Plan{View: planView(p)}
	if p.Risk == engine.RiskLockout {
		fp.Risk = flow.RiskLockout
	}
	return fp, nil
}

// Apply applies the plan Plan last returned, reporting each step through
// report as the engine's events arrive. A failure is a *flow.Failure: what
// failed with its code, the command and its output when a command failed,
// the log, and the next step (finishing a rollback that did not finish).
func (a *FlowEngine) Apply(ctx context.Context, report flow.Report) error {
	a.mu.Lock()
	a.report = report
	p := a.plan
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.report = nil
		a.mu.Unlock()
	}()
	res, err := a.Engine.Apply(ctx, p)
	a.mu.Lock()
	a.result, a.err = &res, err
	a.mu.Unlock()
	if err != nil {
		return a.failure(err)
	}
	return nil
}

// Run is what the flow did, for Build, ExitCode, AfterFlow and the audit
// log: the plan, the result and the error, with the flow's outcome and
// error (flow.Flow.Run's). A dry run is ModePlan (Planned, or Unchanged
// with DryRun); a declined confirmation is Declined; a flow that ended
// before a result has its error: prompt.ErrAborted is CANCELLED (130), and
// nav.ErrQuit and nav.ErrBack, q or 0 at a prompt, are declining (3, not
// an error).
func (a *FlowEngine) Run(o flow.Outcome, flowErr error) Run {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := Run{Mode: ModeApply, Plan: a.plan, Result: a.result, Err: a.err}
	switch {
	case o == flow.Planned || (o == flow.Unchanged && a.DryRun):
		r.Mode = ModePlan
	case o == flow.Cancelled:
		r.Declined = true
	case o == flow.NotRun && r.Err == nil:
		r.Err = flowErr
	}
	if a.Config != nil {
		r.ConfigPath, r.Deprecations = a.Config.Path(), a.Config.Deprecations()
	}
	return r
}

// failure is err as the flow's ErrorReport shows it.
func (a *FlowEngine) failure(err error) *flow.Failure {
	r := Run{Mode: ModeApply, Plan: a.plan, Result: a.result, Err: err}
	if a.result == nil {
		r.Mode = ModePlan
	}
	d := Build(r)
	f := &flow.Failure{Err: err, LogPath: a.LogPath}
	if len(d.Errors) > 0 {
		first := d.Errors[0]
		var failed error
		if a.result != nil && a.result.Failed != nil {
			failed = a.result.Failed.Err
		}
		if text, next, ok := failureWords(first, failed); ok {
			f.Err, f.Next = &codedError{text: text, err: err}, next
		} else {
			f.Err = &codedError{text: errorText(d, first), err: err}
			if first.Fix != "" {
				f.Next = toGoOn(first.Fix)
			}
		}
	}
	if rb := d.Result.Rollback; rb != nil {
		f.Next = strings.TrimSpace(f.Next + " " + rollbackNext(*rb, a.result.Failed))
	}
	var xe *host.ExitError
	if errors.As(err, &xe) {
		f.Command, f.Output = host.FormatArgv(xe.Argv), strings.Join(xe.Tail, "\n")
	}
	return f
}

// codedError is an error in the words the ErrorReport shows, which still
// unwraps to the engine's.
type codedError struct {
	text string
	err  error
}

func (e *codedError) Error() string { return e.text }
func (e *codedError) Unwrap() error { return e.err }
