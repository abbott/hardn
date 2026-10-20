package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The steps of an apply an ApplyError names.
const (
	StepStart        = "start"         // cancelled (Ctrl-C, SIGTERM, a hangup) before the change started
	StepWindow       = "window"        // the confirm window's last Options.Margin reached (ErrWindow)
	StepBackup       = "backup"        // a backup of one of the change's files
	StepJournal      = "journal"       // the write-ahead journal of the change's undo
	StepApply        = "apply"         // the change's Apply
	StepJournalAfter = "journal after" // the journal of what the step that ran needs undone
	StepMarkApplied  = "mark applied"  // revert.MarkApplied, a lockout-risk plan's last write
	StepDone         = "done"          // revert.Done, any other plan's last write
)

// ApplyError is an apply that stopped after the revert timer was armed,
// and what rolling it back did. Is(ErrRolledBack) holds when the rollback
// finished and the journal held every started change's undo; otherwise
// RollbackErr says why the rollback did not finish (revert.ErrIncomplete,
// naming `hardn rollback <id>`, which takes it up; revert.ErrRollingBack,
// another process has it), or Unjournalled names the change it may not
// have undone (Is(ErrUnjournalled)).
type ApplyError struct {
	ID     string // the change's journal id
	Change string // the change that failed; "" for the apply's last write
	Step   string // StepStart … StepDone
	Err    error
	// Rollback is revert.Rollback's result; RollbackErr its error.
	Rollback    revert.RollbackResult
	RollbackErr error
	// Unjournalled, when set, is a change that ran but whose undo the
	// journal did not hold once it had run, so the rollback did not run
	// that undo.
	Unjournalled *Unjournalled
}

// Unjournalled is what a change that ran needs undone, which the journal
// did not hold once it had run: its journal after the step failed
// (Ran.Rollback, ufw's Report.Undo or the reload without IfUp, was not
// written, so the rollback ran the undo journalled before the step), or
// another process's rollback (the timer's, the user's `hardn rollback
// <id>`) took the journal while the step ran, so what the step did may
// have landed after that rollback had restored the files. What it did may
// be left on the host; this is what would undo it, for the caller to show.
type Unjournalled struct {
	Change string
	Undo   Undo
	// Backups are the change's backups, in the run directory Dir, which
	// restore the files it wrote.
	Backups []backup.Entry
	Dir     string
	Err     error // why the journal does not hold it
}

func (e *ApplyError) Error() string {
	what := "change " + e.ID
	if e.Change != "" {
		what += ", " + e.Change
	}
	var b strings.Builder
	fmt.Fprintf(&b, "engine: %s: %s: %v", what, e.Step, e.Err)
	if e.RollbackErr != nil {
		fmt.Fprintf(&b, "; the rollback did not finish: %v", e.RollbackErr)
	} else {
		b.WriteString(" (rolled back from the journal)")
	}
	if u := e.Unjournalled; u != nil {
		fmt.Fprintf(&b, "; %s ran, and the journal does not hold what undoes it (%v), so what it did may be left: %s", u.Change, u.Err, u.words())
	}
	return b.String()
}

// words names what would undo the change.
func (u *Unjournalled) words() string {
	var parts []string
	for _, s := range u.Undo.Steps {
		switch {
		case s.Op == revert.OpRun:
			parts = append(parts, host.FormatArgv(s.Argv))
		case s.Op == revert.OpPlan:
			parts = append(parts, "the "+s.Unit+" plan's rollback")
		default:
			parts = append(parts, string(s.Op)+" "+s.Path)
		}
	}
	for _, r := range u.Undo.Reloads {
		parts = append(parts, "reload "+string(r.Service))
	}
	for _, e := range u.Backups {
		parts = append(parts, fmt.Sprintf("restore %s from backup %d in %s", e.Path, e.Seq, u.Dir))
	}
	if len(parts) == 0 {
		return "nothing journalled undoes it"
	}
	return strings.Join(parts, "; ")
}

// Unwrap returns the step's error and the rollback's.
func (e *ApplyError) Unwrap() []error {
	if e.RollbackErr == nil {
		return []error{e.Err}
	}
	return []error{e.Err, e.RollbackErr}
}

// Is makes errors.Is(err, ErrRolledBack) true when the rollback finished
// and covered every change started, and errors.Is(err, ErrUnjournalled)
// when a change was not covered.
func (e *ApplyError) Is(target error) bool {
	switch target {
	case ErrRolledBack:
		return e.RollbackErr == nil && e.Unjournalled == nil
	case ErrUnjournalled:
		return e.Unjournalled != nil
	}
	return false
}

// Applied is one change whose Apply returned nil.
type Applied struct {
	Change string
	Detail string
}

// Confirm is how a lockout-risk change is kept: `hardn confirm` from a new
// SSH connection, made from another machine, to one of Via when set,
// before Deadline; or undone at once with `hardn rollback <ID>`.
type Confirm struct {
	ID string
	// Deadline is when the revert timer fires: its own trigger time where
	// it can be read (systemd: list-timers' "next"); otherwise the
	// journal's (created, to the second, plus the window), and
	// Approximate: the timer was started a little after it, so it fires
	// up to a few seconds later (vmtest.md §4.9: 1-2 s on Ubuntu 26.04).
	// A confirmation in that gap is still taken: the claim decides.
	Deadline    time.Time
	Approximate bool
	Via         []revert.Listener
}

// Result is what Apply did, as data.
type Result struct {
	ID   string // the change's journal id ("" when nothing was armed)
	Risk RiskClass
	// Nothing: the plan had no changes, so nothing was locked, armed or
	// journalled.
	Nothing bool
	// Applied are the changes whose Apply returned nil, in order. With
	// Failed set they were rolled back after, as Failed says.
	Applied []Applied
	// Confirm is set for a lockout-risk plan applied and waiting for
	// confirmation.
	Confirm *Confirm
	// Failed is set when the apply stopped and was rolled back; Apply
	// returns it as its error too.
	Failed *ApplyError
}

// Apply carries plan p out, p being Engine.Plan's. It refuses a plan with
// refusals (ErrRefused) and applies nothing for an empty one. Otherwise:
//
//  1. the run lock (another apply on the host: host.ErrLocked); from here
//     the plan is spent, and a second Apply of it is ErrPlanUsed;
//  2. the plan made again under the lock, with the same id, which must
//     have no refusals and equal p (ErrPlanChanged): what the user was
//     shown is what runs;
//  3. revert.Arm: the journal, and for a lockout-risk plan the revert
//     timer, before any change (brief 2.4);
//  4. each change in the plan's order: ctx checked, and for a lockout-risk
//     plan the time left of the window (ErrWindow, StepWindow); its files
//     backed up (RollbackRequired); its undo journalled with the backups
//     (revert.Record, write-ahead); Apply, under a context that ends
//     Options.Margin before the window does; what the step that ran needs
//     undone journalled under a context without the cancellation, with a
//     bound (each time: it also proves the journal was still this
//     process's once the step had run);
//  5. revert.MarkApplied for a lockout-risk plan (the window checked
//     first), revert.Done for any other: the apply's last write.
//
// From step 3 on, the first failure, or ctx done (Ctrl-C, SIGTERM, a
// hangup: the caller cancels ctx, from signal.NotifyContext, and keeps it
// registered until Apply returns), rolls back every change started, in
// reverse, from the journal (revert.Rollback, ReasonFailed), with signals
// held (Options.Hold); Apply then returns Result.Failed, an *ApplyError. A
// rollback that did not finish stops the timer all the same (revert.md
// §2.14 step 4): its error names `hardn rollback <id>`, and the next run's
// notice says so. Options.Progress hears of each change as it runs, and of
// the rollback.
func (e *Engine) Apply(ctx context.Context, p *Plan) (Result, error) {
	res := Result{Risk: p.Risk}
	switch {
	case p.controls == nil && !p.Empty():
		return res, fmt.Errorf("engine: a plan not made by Engine.Plan: %w", ErrRefused)
	case p.Err() != nil:
		return res, p.Err()
	case p.Empty():
		res.Nothing = true
		return res, nil
	case p.used.Load():
		return res, fmt.Errorf("engine: change %s: %w", p.ID, ErrPlanUsed)
	}
	unlock, err := e.lock(ctx, p.ID)
	if err != nil {
		return res, err
	}
	defer unlock()
	if p.used.Swap(true) {
		return res, fmt.Errorf("engine: change %s: %w", p.ID, ErrPlanUsed)
	}
	defer e.removeRunDir(ctx, p.ID)
	fresh, err := e.plan(ctx, p.ID, p.controls, p.cfg)
	switch {
	case err != nil:
		return res, err
	case fresh.Err() != nil:
		return res, fresh.Err()
	case !samePlan(p, fresh):
		return res, fmt.Errorf("engine: change %s: %w", p.ID, ErrPlanChanged)
	}
	return e.apply(ctx, fresh)
}

// applier is one apply's state after Arm.
type applier struct {
	e      *Engine
	p      *Plan
	j      revert.Journal
	ro     revert.Options
	run    *backup.Run
	cutoff time.Time // a lockout-risk plan's: the deadline less the margin
	seqs   []int     // every backup the engine took, in order
	undos  []Undo    // every change started, in order
	// done are the indexes of the changes done (EventDone), in order;
	// current is the one running, -1 between changes.
	done    []int
	current int
	unj     *Unjournalled
	res     Result
}

func (e *Engine) apply(ctx context.Context, p *Plan) (Result, error) {
	a := &applier{e: e, p: p, ro: e.opts.Revert, current: -1, res: Result{ID: p.ID, Risk: p.Risk}}
	run, err := backup.Open(e.sys, e.opts.Backups, p.ID)
	if err != nil {
		return Result{Risk: p.Risk}, fmt.Errorf("engine: not applied: %w", err)
	}
	a.run = run
	j, err := revert.Arm(ctx, e.sys, e.journal(p), p.Facts.Init, a.ro)
	if err != nil {
		return Result{Risk: p.Risk}, fmt.Errorf("engine: not applied: %w", err)
	}
	a.j = j
	if p.Risk == RiskLockout {
		a.cutoff = j.Deadline().Add(-e.opts.Margin)
	}
	e.logf("change %s armed (%s, risk %v): %d changes", p.ID, j.Risk, p.Risk, len(p.Changes))
	for i, c := range p.Changes {
		if step, err := a.change(ctx, i, c); err != nil {
			return a.fail(ctx, c.ID, step, err)
		}
	}
	if p.Risk != RiskLockout {
		if err := revert.Done(ctx, e.sys, p.ID, a.ro); err != nil {
			return a.fail(ctx, "", StepDone, err)
		}
		e.logf("change %s applied and done", p.ID)
		return a.res, nil
	}
	if err := a.window(); err != nil {
		return a.fail(ctx, "", StepWindow, err)
	}
	if a.j, err = revert.MarkApplied(ctx, e.sys, p.ID, a.ro); err != nil {
		return a.fail(ctx, "", StepMarkApplied, err)
	}
	c := e.confirm(ctx, a.j)
	a.res.Confirm = &c
	e.logf("change %s applied; it is rolled back at %s%s unless confirmed", p.ID, c.Deadline.Format(time.RFC3339), approx(c.Approximate))
	return a.res, nil
}

// window refuses to go on in the last Options.Margin of a lockout-risk
// plan's window (ErrWindow): the rollback here must claim the change
// before the timer's does.
func (a *applier) window() error {
	if a.cutoff.IsZero() {
		return nil
	}
	if now := a.e.now(); !now.Before(a.cutoff) {
		return fmt.Errorf("engine: %s left until the revert timer fires at %s, under the %s kept to roll back here: %w",
			max(a.j.Deadline().Sub(now), 0).Round(time.Second), a.j.Deadline().Format(time.RFC3339), a.e.opts.Margin, ErrWindow)
	}
	return nil
}

// stepContext is ctx for a change's Apply: in a lockout-risk plan it ends
// at the cutoff, by the engine's clock, with ErrWindow as its cause.
func (a *applier) stepContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if a.cutoff.IsZero() {
		return context.WithCancel(ctx)
	}
	return context.WithTimeoutCause(ctx, a.cutoff.Sub(a.e.now()), ErrWindow)
}

// change runs change i: backups, the write-ahead journal, Apply, the
// journal of what ran. It returns the step that failed.
func (a *applier) change(ctx context.Context, i int, c Change) (string, error) {
	if err := ctx.Err(); err != nil {
		return StepStart, err
	}
	if err := a.window(); err != nil {
		return StepWindow, err
	}
	a.current = i
	a.progress(Event{Index: i, Change: c.ID, State: EventRunning})
	run := Run{ID: a.p.ID, Dir: filepath.Join(a.e.runRoot(), a.p.ID), Facts: a.p.Facts, Backups: map[string]backup.Entry{}, Store: a.run}
	var entries []backup.Entry
	for _, path := range c.Backups {
		r, err := a.run.Backup(ctx, path, backup.RollbackRequired)
		if err != nil {
			return StepBackup, err
		}
		a.seqs = append(a.seqs, r.Entry.Seq)
		run.Backups[path] = r.Entry
		entries = append(entries, r.Entry)
	}
	a.undos = append(a.undos, c.Rollback)
	if err := a.record(ctx); err != nil {
		return StepJournal, err
	}
	sctx, cancel := a.stepContext(ctx)
	ran, err := applyOne(sctx, a.e.sys, c, run)
	if err != nil && ctx.Err() == nil && errors.Is(context.Cause(sctx), ErrWindow) {
		err = fmt.Errorf("%w (the step was stopped %s before the revert timer fires): %w", ErrWindow, a.e.opts.Margin, err)
	}
	cancel()
	undo := c.Rollback
	if ran.Rollback != nil {
		undo = *ran.Rollback
		a.undos[len(a.undos)-1] = undo
	}
	rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), a.e.opts.JournalTimeout)
	rerr := a.record(rctx)
	rcancel()
	if rerr != nil {
		taken := errors.Is(rerr, revert.ErrRollingBack) || errors.Is(rerr, revert.ErrNothingPending)
		if ran.Rollback != nil || taken {
			a.unj = &Unjournalled{Change: c.ID, Undo: undo, Backups: entries, Dir: a.run.Dir(), Err: rerr}
		}
		return StepJournalAfter, errors.Join(err, rerr)
	}
	if err != nil {
		return StepApply, err
	}
	a.res.Applied = append(a.res.Applied, Applied{Change: c.ID, Detail: ran.Detail})
	a.e.logf("change %s: %s done%s", a.p.ID, c.ID, detailWords(ran.Detail))
	a.progress(Event{Index: i, Change: c.ID, State: EventDone, Detail: ran.Detail})
	a.done, a.current = append(a.done, i), -1
	return "", nil
}

// progress sends ev to Options.Progress, when set.
func (a *applier) progress(ev Event) {
	if f := a.e.opts.Progress; f != nil {
		f(ev)
	}
}

// applyOne runs c's Apply. A panic in it is a failed step, so the changes
// made so far are rolled back while the session is open, rather than left
// to the timer.
func applyOne(ctx context.Context, sys host.System, c Change, run Run) (ran Ran, err error) {
	defer func() {
		if r := recover(); r != nil {
			ran, err = Ran{}, fmt.Errorf("engine: change %s panicked: %v", c.ID, r)
		}
	}()
	return c.Apply(ctx, sys, run)
}

// record writes the journal as it now stands: every backup taken, and
// every started change's undo, the last change's first (the order to undo
// in), each change's steps in their own order.
func (a *applier) record(ctx context.Context) error {
	j := a.j
	j.Backup, j.Undo, j.Reload = nil, nil, nil
	if a.run != nil && len(a.seqs) > 0 {
		j.Backup = &revert.Backups{Dir: a.run.Dir(), Seqs: slices.Clone(a.seqs)}
	}
	for i := len(a.undos) - 1; i >= 0; i-- {
		j.Undo = append(j.Undo, a.undos[i].Steps...)
		j.Reload = append(j.Reload, a.undos[i].Reloads...)
	}
	return revert.Record(ctx, a.e.sys, j, a.ro)
}

// fail rolls the change back at once, from the journal, while this session
// is open (ReasonFailed), with signals held; revert.Rollback runs without
// ctx's cancellation. A rollback another process has started already (the
// timer's, the user's `hardn rollback`) is left to it: Rollback then
// reports Nothing, and the ApplyError is not ErrRolledBack but
// revert.ErrRollingBack.
func (a *applier) fail(ctx context.Context, change, step string, err error) (Result, error) {
	ev := Event{Index: a.current, State: EventFailed, Step: step, Err: err}
	if a.current >= 0 {
		ev.Change = change
	}
	a.progress(ev)
	a.e.logf("change %s: %s: %s failed: %v; rolling back while this session is open", a.p.ID, change, step, err)
	ro := a.ro
	ro.Reason = revert.ReasonFailed
	release := a.e.opts.Hold()
	rr, rerr := revert.Rollback(context.WithoutCancel(ctx), a.e.sys, a.p.ID, ro)
	release()
	if rerr == nil && rr.Nothing {
		// Not this process's to report as rolled back: another has the
		// rollback, or the journal is gone.
		rerr = fmt.Errorf("engine: nothing rolled back here: %s: %w", rr.Why, revert.ErrRollingBack)
	}
	fe := &ApplyError{ID: a.p.ID, Change: change, Step: step, Err: err, Rollback: rr, RollbackErr: rerr, Unjournalled: a.unj}
	if rerr == nil {
		for k := len(a.done) - 1; k >= 0; k-- {
			if c := a.p.Changes[a.done[k]]; a.unj == nil || c.ID != a.unj.Change {
				a.progress(Event{Index: a.done[k], Change: c.ID, State: EventRolledBack})
			}
		}
	}
	a.res.Failed = fe
	return a.res, fe
}

func approx(a bool) string {
	if a {
		return " (about)"
	}
	return ""
}

func detailWords(d string) string {
	if d == "" {
		return ""
	}
	return ": " + d
}
