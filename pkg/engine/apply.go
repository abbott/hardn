package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The steps of an apply an ApplyError names.
const (
	StepStart        = "start"         // cancelled (Ctrl-C, SIGTERM) before the change started
	StepBackup       = "backup"        // a backup of one of the change's files
	StepJournal      = "journal"       // the write-ahead journal of the change's undo
	StepApply        = "apply"         // the change's Apply
	StepJournalAfter = "journal after" // the journal of what the step that ran needs undone
	StepMarkApplied  = "mark applied"  // revert.MarkApplied, a lockout-risk plan's last write
	StepDone         = "done"          // revert.Done, any other plan's last write
)

// ApplyError is an apply that stopped after the revert timer was armed,
// and what rolling it back did. Is(ErrRolledBack) holds when the rollback
// finished; otherwise RollbackErr says why not (revert.ErrIncomplete,
// naming `hardn rollback <id>`, which takes it up).
type ApplyError struct {
	ID     string // the change's journal id
	Change string // the change that failed; "" for the apply's last write
	Step   string // StepStart … StepDone
	Err    error
	// Rollback is revert.Rollback's result; RollbackErr its error.
	Rollback    revert.RollbackResult
	RollbackErr error
}

func (e *ApplyError) Error() string {
	what := "change " + e.ID
	if e.Change != "" {
		what += ", " + e.Change
	}
	if e.RollbackErr != nil {
		return fmt.Sprintf("engine: %s: %s: %v; the rollback did not finish: %v", what, e.Step, e.Err, e.RollbackErr)
	}
	return fmt.Sprintf("engine: %s: %s: %v (rolled back)", what, e.Step, e.Err)
}

// Unwrap returns the step's error and the rollback's.
func (e *ApplyError) Unwrap() []error {
	if e.RollbackErr == nil {
		return []error{e.Err}
	}
	return []error{e.Err, e.RollbackErr}
}

// Is makes errors.Is(err, ErrRolledBack) true when the rollback finished.
func (e *ApplyError) Is(target error) bool { return target == ErrRolledBack && e.RollbackErr == nil }

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
	Applied []Applied // the changes whose Apply returned nil, in order
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
//  1. the run lock (another apply on the host: host.ErrLocked);
//  2. the plan made again under the lock, with the same id, which must
//     have no refusals and equal p (ErrPlanChanged): what the user was
//     shown is what runs;
//  3. revert.Arm: the journal, and for a lockout-risk plan the revert
//     timer, before any change (brief 2.4);
//  4. each change in the plan's order: ctx checked; its files backed up
//     (RollbackRequired); its undo journalled with the backups
//     (revert.Record, write-ahead); Apply; what the step that ran needs
//     undone journalled under a context without the cancellation, with a
//     bound;
//  5. revert.MarkApplied for a lockout-risk plan, revert.Done for any
//     other: the apply's last write.
//
// From step 3 on, the first failure, or ctx done (Ctrl-C, SIGTERM: the
// caller cancels ctx, from signal.NotifyContext, and keeps it registered
// until Apply returns), rolls back every change started, in reverse, from
// the journal (revert.Rollback, ReasonFailed), with signals held
// (Options.Hold); Apply then returns Result.Failed, an *ApplyError. A
// rollback that did not finish stops the timer all the same (revert.md
// §2.14 step 4): its error names `hardn rollback <id>`, and the next run's
// notice says so.
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
	}
	unlock, err := e.lock(ctx, p.ID)
	if err != nil {
		return res, err
	}
	defer unlock()
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
	e     *Engine
	p     *Plan
	j     revert.Journal
	ro    revert.Options
	run   *backup.Run
	seqs  []int  // every backup taken, in order
	undos []Undo // every change started, in order
	res   Result
}

func (e *Engine) apply(ctx context.Context, p *Plan) (Result, error) {
	a := &applier{e: e, p: p, ro: e.opts.Revert, res: Result{ID: p.ID, Risk: p.Risk}}
	j := revert.Journal{ID: p.ID, Risk: revert.RiskOther, Summary: p.Summary(), Session: e.opts.Session}
	if p.Risk == RiskLockout {
		j.Risk, j.TimeoutSeconds, j.ConfirmVia = revert.RiskLockout, e.window(), confirmVia(p.Changes)
	}
	j, err := revert.Arm(ctx, e.sys, j, p.Facts.Init, a.ro)
	if err != nil {
		return Result{Risk: p.Risk}, fmt.Errorf("engine: not applied: %w", err)
	}
	a.j = j
	e.logf("change %s armed (%s, risk %v): %d changes", p.ID, j.Risk, p.Risk, len(p.Changes))
	for _, c := range p.Changes {
		if step, err := a.change(ctx, c); err != nil {
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
	if a.j, err = revert.MarkApplied(ctx, e.sys, p.ID, a.ro); err != nil {
		return a.fail(ctx, "", StepMarkApplied, err)
	}
	c := e.confirm(ctx, a.j)
	a.res.Confirm = &c
	e.logf("change %s applied; it is rolled back at %s%s unless confirmed", p.ID, c.Deadline.Format(time.RFC3339), approx(c.Approximate))
	return a.res, nil
}

// change runs one change: backups, the write-ahead journal, Apply, the
// journal of what ran. It returns the step that failed.
func (a *applier) change(ctx context.Context, c Change) (string, error) {
	if err := ctx.Err(); err != nil {
		return StepStart, err
	}
	run := Run{ID: a.p.ID, Dir: filepath.Join(a.e.runRoot(), a.p.ID), Facts: a.p.Facts, Backups: map[string]backup.Entry{}}
	for _, path := range c.Backups {
		if a.run == nil {
			r, err := backup.Open(a.e.sys, a.e.opts.Backups, a.p.ID)
			if err != nil {
				return StepBackup, err
			}
			a.run = r
		}
		r, err := a.run.Backup(ctx, path, backup.RollbackRequired)
		if err != nil {
			return StepBackup, err
		}
		a.seqs = append(a.seqs, r.Entry.Seq)
		run.Backups[path] = r.Entry
	}
	a.undos = append(a.undos, c.Rollback)
	if err := a.record(ctx); err != nil {
		return StepJournal, err
	}
	ran, err := applyOne(ctx, a.e.sys, c, run)
	if ran.Rollback != nil {
		a.undos[len(a.undos)-1] = *ran.Rollback
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), a.e.opts.JournalTimeout)
		rerr := a.record(rctx)
		cancel()
		if rerr != nil {
			return StepJournalAfter, errors.Join(err, rerr)
		}
	}
	if err != nil {
		return StepApply, err
	}
	a.res.Applied = append(a.res.Applied, Applied{Change: c.ID, Detail: ran.Detail})
	a.e.logf("change %s: %s done%s", a.p.ID, c.ID, detailWords(ran.Detail))
	return "", nil
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
	fe := &ApplyError{ID: a.p.ID, Change: change, Step: step, Err: err, Rollback: rr, RollbackErr: rerr}
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
