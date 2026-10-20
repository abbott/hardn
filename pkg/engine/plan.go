package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// Item is one control in a plan.
type Item struct {
	Control string
	Title   string
	// NotApplicable is why the control does not apply here; "" when it
	// does. A control that does not apply plans nothing.
	NotApplicable string
	Changes       []string // its changes' IDs, as it planned them
	Err           error    // its refusal
}

// Plan is every control's changes, in the order Apply runs them, and every
// refusal. It is data: the dry run renders it and stops (brief §3
// principle 4), and Apply carries it out.
type Plan struct {
	// ID is the change's id: its journal's, its backup run's and its
	// timer's (revert.NewID).
	ID      string
	Facts   detect.Facts
	Items   []Item   // one per control, in the order given
	Changes []Change // in the order Apply runs them (order)
	Risk    RiskClass
	// Refused are the refusals: a control's (also in its Item), the
	// order's, the platform's support level, a change already journalled,
	// the revert timer's preconditions. A plan with any is not applied.
	Refused []error

	controls []Control
	cfg      *config.Config
}

// Empty reports whether the plan changes nothing.
func (p *Plan) Empty() bool { return len(p.Changes) == 0 }

// Err is the plan's refusals as one error, ErrRefused; nil when there are
// none.
func (p *Plan) Err() error {
	if len(p.Refused) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrRefused, errors.Join(p.Refused...))
}

// Summary is the plan in one line, for the journal and the notices: the
// change's own summary when there is one, else how many and whose.
func (p *Plan) Summary() string {
	if len(p.Changes) == 1 {
		return p.Changes[0].Summary
	}
	var titles []string
	for _, it := range p.Items {
		if len(it.Changes) > 0 {
			titles = append(titles, it.Title)
		}
	}
	return fmt.Sprintf("%d changes: %s", len(p.Changes), strings.Join(titles, ", "))
}

// Plan asks each control in turn for its changes on this host and returns
// the plan, which changes nothing on the host but the plan's run
// directory, removed again before Plan returns. Detection failing (a done
// ctx) is an error; everything else that stops the plan from being applied
// is in Plan.Refused.
func (e *Engine) Plan(ctx context.Context, controls []Control, cfg *config.Config) (*Plan, error) {
	id := revert.NewID(e.now())
	defer e.removeRunDir(ctx, id)
	return e.plan(ctx, id, controls, cfg)
}

// plan is Plan under a given id, leaving the run directory for Apply.
func (e *Engine) plan(ctx context.Context, id string, controls []Control, cfg *config.Config) (*Plan, error) {
	facts, err := e.opts.Detect(ctx, e.sys)
	if err != nil {
		return nil, fmt.Errorf("engine: detect: %w", err)
	}
	p := &Plan{ID: id, Facts: facts, controls: controls, cfg: cfg}
	refuse := func(err error) { p.Refused = append(p.Refused, err) }
	runDir := filepath.Join(e.runRoot(), id)
	if err := e.sys.MkdirAll(ctx, runDir, 0o700); err != nil {
		return nil, fmt.Errorf("engine: run directory: %w", err)
	}
	if err := trustedDir(ctx, e.sys, runDir); err != nil {
		refuse(err)
		return p, nil
	}
	in := Input{ID: id, RunDir: runDir, Facts: facts, Config: cfg}
	var all []Change
	seen := map[string]bool{}
	for _, c := range controls {
		it := Item{Control: c.ID(), Title: c.Title()}
		switch ok, why := c.Applies(facts); {
		case !validName(it.Control) || seen[it.Control] || it.Title == "" || strings.ContainsAny(it.Title, "\r\n"):
			it.Err = fmt.Errorf("engine: control %q: an id, once, and a one-line title: %w", it.Control, ErrChange)
		case !ok:
			it.NotApplicable = notApplicable(why)
		default:
			cs, err := c.Plan(ctx, e.sys, in)
			if err != nil {
				it.Err = fmt.Errorf("%s: %w", it.Control, err)
				break
			}
			for _, ch := range cs {
				ch.Control = it.Control
				if err := ch.check(id, e.opts.Revert.Plans); err != nil {
					it.Err = errors.Join(it.Err, err)
				}
				it.Changes = append(it.Changes, ch.ID)
				all = append(all, ch)
				p.Risk = max(p.Risk, ch.Risk)
			}
		}
		seen[it.Control] = true
		if it.Err != nil {
			refuse(it.Err)
		}
		p.Items = append(p.Items, it)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("engine: plan: %w", err)
	}
	if p.Changes, err = order(all); err != nil {
		p.Changes = all
		refuse(err)
	}
	if !p.Empty() {
		e.refuseApply(ctx, p, refuse)
	}
	return p, nil
}

// refuseApply adds the refusals that Arm, or the platform, would make
// later: everything Apply refuses before it changes anything is refused
// here, so the dry run shows it.
func (e *Engine) refuseApply(ctx context.Context, p *Plan, refuse func(error)) {
	switch s := p.Facts.Platform; s.Support {
	case detect.Supported, detect.BestEffort:
	default:
		refuse(fmt.Errorf("engine: %v %s is %v: %w", s.Distro, s.Version, s.Support, ErrUnsupported))
	}
	switch st, err := revert.ReadState(ctx, e.sys, e.opts.Revert); {
	case err != nil:
		refuse(fmt.Errorf("engine: the change journal: %w", err))
	case st.Busy():
		refuse(&revert.PendingError{State: st})
	}
	if p.Risk != RiskLockout {
		return
	}
	if _, err := revert.TimerFor(p.Facts.Init, p.ID, e.opts.Revert.Hardn, e.opts.Timeout, e.runRoot()); err != nil {
		refuse(fmt.Errorf("engine: the revert timer: %w", err))
	} else if err := revert.CheckExecutable(ctx, e.sys, e.opts.Revert.Hardn); err != nil {
		refuse(fmt.Errorf("engine: the revert timer: %w", err))
	}
	if s := e.opts.Session; !s.Remote && s.Source == "" {
		refuse(fmt.Errorf("engine: the session applying the change is not established (revert.ReadSession): %w", revert.ErrSessionUnknown))
	}
}

// removeRunDir removes plan id's run directory, after Ctrl-C too.
func (e *Engine) removeRunDir(ctx context.Context, id string) {
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.opts.JournalTimeout)
	defer cancel()
	if err := e.sys.RemoveAll(cctx, filepath.Join(e.runRoot(), id)); err != nil {
		e.logf("run directory of %s not removed: %v", id, err)
	}
}

// samePlan reports whether fresh, made under the run lock, is the plan p
// that was shown: the same changes in the same order, field by field (Apply
// aside, a function), and the same risk.
func samePlan(p, fresh *Plan) bool {
	if len(p.Changes) != len(fresh.Changes) || p.Risk != fresh.Risk {
		return false
	}
	for i := range p.Changes {
		a, b := p.Changes[i], fresh.Changes[i]
		a.Apply, b.Apply = nil, nil
		if !reflect.DeepEqual(a, b) {
			return false
		}
	}
	return true
}

// confirmVia is the listeners a confirmation must reach: every lockout-risk
// change's, once each.
func confirmVia(cs []Change) []revert.Listener {
	var out []revert.Listener
	for _, c := range cs {
		for _, l := range c.ConfirmVia {
			if !slices.Contains(out, l) {
				out = append(out, l)
			}
		}
	}
	return out
}

// window is the timeout as revert journals it.
func (e *Engine) window() int { return int(e.opts.Timeout / time.Second) }
