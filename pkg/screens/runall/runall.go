// Package runall runs hardn's Run All screen on a host (refactor plan
// slice 8, unit (c), next/s8-screen): the mock the maintainer adopted on
// 2026-11-04 (docs/engine/gallery-runall.md §5 and §6), drawn by
// pkg/ui/screen from what Run All's planner (pkg/engine/runall) reads of
// the host, writing nothing; Apply all the planner's one plan through the
// plan/apply engine in pkg/ui's canonical flow: the refusals said first,
// the review grouped by control, the hostname typed whatever the plan's
// risk (decision 6 (a)), the apply, confirmed from a new connection or
// reverted where the plan is lockout-risk, then the screen drawn again from
// a fresh reading. Run is the one entry point, which cmd/hardn's main menu
// item 6 is to call (unit (d)); docs/engine/screen-runall.md has its API
// and what it decided.
//
// The change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the planner and the engine (depguard rule
// screens-through-host), and renders only through pkg/ui and pkg/output.
package runall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	planner "github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// skippedWords follow a refused row's label before the review: "<Row>
// skipped: <the control's refusal>" (gallery-runall.md §6).
const skippedWords = " skipped: "

// Options is what the Run All screen runs with: what cmd/hardn has when its
// Run All menu item is chosen. It has the SSH Login screen's, and what Run
// All's planner reads besides (Version, Revert).
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the planner reads it, and the engine changes it.
	System host.System
	// Facts are detect's, which the planner's rows read; the engine
	// establishes its own when it plans.
	Facts detect.Facts
	// Session is the session hardn runs in, as the caller reads it
	// (revert.ReadSession): the applying session, as the engine's.
	Session revert.Session
	// Getenv looks up hardn's environment (os.Getenv in hardn), from which
	// the controls take the session user (SUDO_USER, DOAS_USER). Required.
	Getenv func(string) string
	// Config is hardn.yml as loaded: what it asks of each control
	// (runall.Asked). Nil asks nothing. No change saves it.
	Config *config.Config
	// NewEngine returns the engine the planner reads with and the change
	// plans and applies with, built as the caller builds it (engine.New
	// over System with its options: hardn.yml's backups, its session,
	// hardn's path for the revert timer), progress as its Options.Progress.
	// Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context the apply runs under,
	// made from the screen's, and its stop, called once the apply has
	// returned (screenkit.Planning.ApplyContext). Nil applies under the
	// screen's context.
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops Apply all at its plan: nothing applied.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what Apply all did once its flow has ended,
	// for a plan made: for the audit log.
	Ran func(output.Run)
	// Version is hardn's, in Auto Updates' record (runall.Input.Version).
	Version string
	// Revert is the revert options the planner reads the change journal
	// with (runall.Input.Revert): zero in hardn.
	Revert revert.Options
}

// Run shows the Run All screen and runs Apply all when chosen, until the
// user goes back with 0 (nil) or quits with q (nav.ErrQuit); the end of the
// input and Ctrl-C abort (prompt.ErrAborted), from the screen and the
// question. Each round reads the host with the planner (runall.Read),
// writing nothing, under plan only too, and draws each control's row and
// the item from what it read.
//
// Apply all reads the host again and says, before asking anything, a
// change pending or the one plan's refusal (and goes back), then each
// refused row's words whole as one warning; where nothing is planned it
// goes back there. Then the combined plan: its review, one line per
// control; the plan from the engine; the hostname typed whatever its risk;
// the apply, confirmed or reverted where it is lockout-risk. A live
// lockout-risk plan whose hostname can't be read says so before asking;
// plan only stops at the plan. Run returns an error wrapping fs.ErrInvalid,
// writing nothing, without a Prompter's In and Out, a System, Getenv or
// NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Getenv == nil || o.NewEngine == nil {
		return fmt.Errorf("runall screen: %w: it needs a Prompter with In and Out, a System, Getenv and NewEngine", fs.ErrInvalid)
	}
	r := newRunner(o)
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// newRunner is the run of the screen with o, which Run has checked: its
// clock time.Now where o has none, a nil Config asking nothing.
func newRunner(o Options) *runner {
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{} // asks nothing; no change saves it
	}
	r := &runner{o: o, cfg: cfg, s: screenkit.Session{Name: "runall screen", Prompter: o.Prompter, System: o.System, Config: cfg,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: o.Ran}}
	r.reader = r.read
	return r
}

// runner is one run of the Run All screen: its options, hardn.yml as the
// planner reads it, the session its change runs with, and how it reads
// the host: read, which a test in the package replaces with a reading no
// host reaches.
type runner struct {
	o      Options
	cfg    *config.Config
	s      screenkit.Session
	reader func(ctx context.Context) reading
}

// reading is one reading of the host for Run All: the planner's, its error,
// and each row as the screen draws it.
type reading struct {
	planner.Reading
	err  error
	rows []screen.RunAllRow
}

// read reads the host with the planner, writing nothing.
func (r *runner) read(ctx context.Context) reading {
	rd, err := planner.Read(ctx, planner.Input{System: r.o.System, Facts: r.o.Facts, Config: r.cfg, Session: r.o.Session,
		Getenv: r.o.Getenv, Revert: r.o.Revert, Version: r.o.Version, NewEngine: r.o.NewEngine})
	h := reading{Reading: rd, err: err}
	if err != nil {
		return h
	}
	for _, rw := range rd.Rows {
		// The row's check's title, which is the main menu's label too
		// (output.StatusViewOf labels each row with its report's Title).
		row := screen.RunAllRow{Label: rw.Title, Outcome: rw.Words}
		if rw.Kind == planner.Refused {
			row.Refusal = rw.Refusal
		}
		h.rows = append(h.rows, row)
	}
	return h
}

// refusal is what refuses Apply all before asking, "" for nothing: the
// reading's error, in its words; a change pending, or the one plan's
// refusal, in the planner's.
func (h reading) refusal() string {
	if h.err != nil {
		return screenkit.FullStop(h.err.Error())
	}
	return h.Refusal
}

// view is h as the screen draws it. A reading that failed draws one row,
// Run All refused, in the error's words, and the item refused.
func (h reading) view() screen.RunAll {
	v := screen.RunAll{Rows: h.rows, Lockout: h.Lockout()}
	switch {
	case h.err != nil:
		v.Rows = []screen.RunAllRow{{Label: planner.Title, Outcome: planner.WordsRefused, Refusal: h.refusal()}}
		v.State = screen.RunAllRefused
	case h.Refusal != "" && h.Pending():
		v.State = screen.RunAllPending
	case h.Refusal != "":
		v.State = screen.RunAllRefused
	case len(h.Planned()) > 0:
		v.State = screen.RunAllPlanned
	case len(h.Refused()) > 0:
		v.State = screen.RunAllSkipped
	default:
		v.State = screen.RunAllNothing
	}
	return v
}

// planned are the planned rows, as the screen draws them.
func (h reading) planned() []screen.RunAllRow {
	var out []screen.RunAllRow
	for i, rw := range h.Rows {
		if rw.Kind == planner.Planned {
			out = append(out, h.rows[i])
		}
	}
	return out
}

// skipped are the refused rows' words, each after its label, whole.
func (h reading) skipped() []string {
	var out []string
	for i, rw := range h.Rows {
		if rw.Kind == planner.Refused {
			out = append(out, h.rows[i].Label+skippedWords+rw.Refusal)
		}
	}
	return out
}

// draw reads the host and returns the screen, its item bound.
func (r *runner) draw(ctx context.Context) nav.Screen {
	sub := r.reader(ctx).view().SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: func() error { return r.applyAll(ctx) }})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// applyAll is item 1, from a fresh reading of the host, not the draw's:
// refused before asking where the reading failed, a change is pending or
// the one plan refuses; each refused row said; then, where something is
// planned, the planned rows' controls through the flow (screenkit
// Session.Change, Typed): the review by control, the plan, the hostname
// typed, the apply. A live lockout-risk plan whose hostname can't be read
// says so before asking; a hostname that can't be read at the question
// says so in its place (nav.ErrPhrase, as CantConfirm).
func (r *runner) applyAll(ctx context.Context) error {
	h := r.reader(ctx)
	switch {
	case h.err != nil && ctx.Err() != nil:
		return h.err
	case h.refusal() != "":
		return r.s.Say(screenkit.Warnings(h.refusal()))()
	}
	skipped := h.skipped()
	switch {
	case len(h.Planned()) == 0 && len(skipped) > 0:
		return r.s.Say(screenkit.Warnings(skipped...))()
	case len(h.Planned()) == 0:
		return nil // nothing to change since the screen was drawn: drawn again
	case len(skipped) > 0:
		if err := r.s.Write(screenkit.Apart(r.o.Prompter.Ctx, screenkit.Warnings(skipped...))); err != nil {
			return err
		}
	}
	if h.Lockout() && !r.o.DryRun {
		if _, _, err := screenkit.Hostname(ctx, r.o.System); err != nil {
			return r.s.CantConfirm(err)
		}
	}
	c := screen.RunAllApplyAll(h.planned())
	controls := h.Controls()
	var phraseErr error
	confirm := func(p flow.Plan) flow.Confirm {
		cf := flow.Confirm{Question: c.Question}
		if p.Typed() {
			cf.Label = screenkit.TypeTheHostname
			cf.Phrase, cf.Short, phraseErr = screenkit.Hostname(ctx, r.o.System)
		}
		return cf
	}
	err := r.s.Change(ctx, screenkit.Change{
		Step:     c.Step,
		Flow:     flow.Flow{Review: func() flow.Review { return c.Review }, Confirm: confirm},
		Controls: func(*config.Config) []engine.Control { return controls },
		Typed:    true,
	})
	if phraseErr != nil && errors.Is(err, nav.ErrPhrase) {
		return r.s.CantConfirm(phraseErr)
	}
	return err
}
