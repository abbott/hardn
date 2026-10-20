// Package confirm is what `hardn confirm` does (refactor plan P3,
// next/p3-confirm-cleanup; P-40, decided by the maintainer on 2026-10-19).
// Claim keeps the change waiting for confirmation (revert.Confirm: the
// journal claimed by rename, the revert timer stopped) and says what the
// change held during its confirm window: the old port beside the new one,
// and the ufw rule the change added for it (steps.CleanupOf). Cleanup then
// removes them through the engine, from the new connection that confirmed
// the change: the held port dropped from 00-hardn.conf, the reload, the ufw
// rule deleted (steps.PlanCleanup), applied under the run lock, journalled
// write-ahead and rolled back in this process if a step fails. The
// confirmation stands either way.
//
// The clean-up has no revert timer of its own (steps.Cleanup.Proven): it
// runs only from a session that revert.CheckConfirm accepted over the
// change's journal, a new SSH connection, made from another machine, to a
// listener the change added, and it only takes away (a reload that may only
// drop listeners, ufw rules deleted for the dropped ports alone), so it
// cannot cut that connection off; the check after its reload requires the
// kept port still listened on. A console session confirms, as before, but
// cleans nothing up: it shows nothing about SSH (Held.Console).
//
// A crash between the claim and the clean-up leaves the held port recorded
// in confirmed.json alone. Claim, finding nothing pending, reads it there
// and finishes the clean-up (Held.Finishing), from a session that could
// have confirmed that change; once the port is gone the clean-up plans
// nothing.
//
// The package reaches the host only through host.System (depguard
// confirm-through-host) and renders nothing: cmd/hardn renders its results
// through pkg/output. docs/engine/confirm-cleanup.md describes the
// guarantees, the facts they rely on and what is written but not yet run on
// a Linux host.
package confirm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// ControlID is the clean-up's control, and the prefix of its changes' ids
// (ssh.cleanup/firewall, /files, /reload, /verify).
const ControlID = "ssh.cleanup"

// Held is what `hardn confirm` found of the change it kept, or, with
// nothing pending, of the last change confirmed: what the change held
// during its confirm window, and whether this session cleans it up (Run).
type Held struct {
	// Confirmed is revert.Confirm's result when this run confirmed the
	// change; nil when nothing was pending (Finishing).
	Confirmed *revert.ConfirmResult
	// Change is the change whose clean-up this is: the one confirmed now,
	// or, Finishing, the last one confirmed (confirmed.json).
	Change revert.Journal
	// Finishing: nothing was pending, and the last change confirmed still
	// records a port it held (a crash between the claim and the clean-up,
	// or a clean-up that did not finish).
	Finishing bool
	// Cleanup is what the change held (steps.CleanupOf); no Drop: nothing,
	// and confirm behaves as before the clean-up existed.
	Cleanup steps.Cleanup
	// Console: the session is a console, not SSH (the local-console
	// exception), so the clean-up is left for a new SSH connection to the
	// new port.
	Console bool
	// Refused is why this session may not finish the clean-up, Finishing:
	// revert.CheckConfirm's refusal over the confirmed change's journal.
	Refused error
}

// Holds reports whether the change held a port that the clean-up drops.
func (h Held) Holds() bool { return len(h.Cleanup.Drop) > 0 }

// Run reports whether the clean-up is to be run from this session.
func (h Held) Run() bool { return h.Holds() && !h.Console && h.Refused == nil }

// Claim is `hardn confirm`'s claim: revert.Confirm from session s, and what
// the change confirmed held (steps.CleanupOf). Its error is revert.Confirm's,
// the confirmation's: when it is that nothing is pending
// (*revert.NotPendingError), and the last change confirmed (confirmed.json)
// still records a held port, Held says so (Finishing), and whether this
// session may finish its clean-up: revert.CheckConfirm over that journal,
// over SSH (Run; Refused or Console otherwise). Claim reads the host only
// through revert; the clean-up is Cleanup's.
func Claim(ctx context.Context, sys host.System, s revert.Session, opts revert.Options) (Held, error) {
	res, err := revert.Confirm(ctx, sys, s, opts)
	if err == nil {
		return Held{Confirmed: &res, Change: res.Journal, Cleanup: steps.CleanupOf(res.Journal), Console: res.Local}, nil
	}
	var np *revert.NotPendingError
	if !errors.As(err, &np) {
		return Held{}, err
	}
	j, rerr := revert.ReadConfirmed(ctx, sys, opts)
	if rerr != nil {
		logf(opts, "confirm: the last change confirmed cannot be read, so whether its clean-up is left cannot be told: %v", rerr)
		return Held{}, err
	}
	if j == nil {
		return Held{}, err
	}
	h := Held{Change: *j, Finishing: true, Cleanup: steps.CleanupOf(*j)}
	if !h.Holds() {
		return Held{}, err
	}
	h.Console, h.Refused = revert.CheckConfirm(*j, s)
	if h.Run() {
		logf(opts, "confirm: nothing is pending; change %s (%s), the last confirmed, records port %v held: finishing its clean-up", j.ID, j.Summary, h.Cleanup.Drop)
	}
	return h, err
}

// Options configure Cleanup's engine.
type Options struct {
	// Revert are the journal's options: its log, clock and directories,
	// and the plan rollbacks (nil is plans.Registry()).
	Revert revert.Options
	// Detect establishes the platform facts; nil is detect.Detect.
	Detect func(ctx context.Context, sys host.System) (detect.Facts, error)
	// Hold holds signals around an in-process rollback; nil is the
	// engine's (hold.Signals).
	Hold func() (release func())
	// PID is hardn's process id, for the run lock's holder line.
	PID int
	// Progress, when set, hears of each change as it runs (engine.Event).
	Progress func(engine.Event)
}

// Outcome is what Cleanup did.
type Outcome struct {
	// Plan is the clean-up's plan; nil when planning failed before one
	// was made (detection).
	Plan *engine.Plan
	// Result is Apply's; nil when nothing was applied: an error before
	// the plan, a refusal, or no clean-up run.
	Result *engine.Result
	// Err is the clean-up's error: planning's, the plan's refusals
	// (engine.ErrRefused, with each one), or Apply's (an
	// *engine.ApplyError for a step that failed, rolled back here).
	Err error
}

// Done reports whether nothing is left of the clean-up: it was applied, or
// there was nothing to do (the held port gone already).
func (o Outcome) Done() bool { return o.Err == nil && o.Result != nil && o.Result.Failed == nil }

// Cleanup plans and applies the clean-up of h, from session s, the one
// Claim was given, through the engine: one control, steps.PlanCleanup with
// h.Cleanup, Proven (no revert timer of its own: the journal is risk
// "other", ended by revert.Done, and a failure rolls back here), under the
// run lock. Every refusal PlanCleanup has is kept and returned (Err, with
// the plan): a port another file keeps, a clean-up that would leave sshd no
// port, Proxmox VE's port 22, a session not on a port it keeps. The
// backups go where the change's own went (the parent of its journal's
// backup directory, <backupPath>/<id>), backup.DefaultPath when it took
// none. Nothing is done unless h.Run().
func Cleanup(ctx context.Context, sys host.System, h Held, s revert.Session, opts Options) Outcome {
	if !h.Run() {
		return Outcome{}
	}
	c := h.Cleanup
	c.Proven = true
	e := engine.New(sys, engine.Options{Revert: opts.Revert, Session: s, Backups: backup.Settings{Path: backupRoot(h.Change), Enabled: true},
		Detect: opts.Detect, Hold: opts.Hold, PID: opts.PID, Progress: opts.Progress})
	p, err := e.Plan(ctx, []engine.Control{control{c}}, nil)
	switch {
	case err != nil:
		return Outcome{Plan: p, Err: err}
	case p.Err() != nil:
		return Outcome{Plan: p, Err: p.Err()}
	case p.Empty():
		return Outcome{Plan: p, Result: &engine.Result{ID: p.ID, Nothing: true}}
	}
	res, err := e.Apply(ctx, p)
	return Outcome{Plan: p, Result: &res, Err: err}
}

// backupRoot is the backupPath the change's own backups went under: its
// journal names their run directory, <backupPath>/<id>. hardn confirm reads
// no configuration (its first-run prompt is p3-startup's), so this keeps
// the clean-up's backups beside the change's; backup.DefaultPath when the
// change took none.
func backupRoot(j revert.Journal) string {
	if b := j.Backup; b != nil && filepath.Base(b.Dir) == j.ID && filepath.IsAbs(b.Dir) {
		return filepath.Dir(b.Dir)
	}
	return backup.DefaultPath
}

// control is the clean-up as the engine's one control.
type control struct{ c steps.Cleanup }

func (control) ID() string                          { return ControlID }
func (control) Title() string                       { return "SSH clean-up after the confirmation" }
func (control) Applies(detect.Facts) (bool, string) { return true, "" }

// Check is not used: the clean-up is planned, never checked.
func (control) Check(context.Context, host.System, detect.Facts) (engine.Status, error) {
	return engine.Status{}, errors.New("confirm: the clean-up has no check")
}

func (c control) Plan(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	return steps.PlanCleanup(ctx, sys, in, ControlID, c.c)
}

// viaPorts are the ports of the listeners the change added (ConfirmVia),
// once each, in order: where a new connection confirms it.
func viaPorts(j revert.Journal) []uint16 {
	var out []uint16
	for _, l := range j.ConfirmVia {
		if !slices.Contains(out, l.Port) {
			out = append(out, l.Port)
		}
	}
	return out
}

func logf(opts revert.Options, format string, args ...any) {
	if opts.Log != nil {
		_, _ = fmt.Fprintf(opts.Log, "revert: "+format+"\n", args...)
	}
}
