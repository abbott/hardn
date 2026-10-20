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
// and, while 00-hardn.conf still has its Port line, finishes the clean-up
// (Held.Finishing), from a session that could have confirmed that change.
// The claim of the next change renames its journal over confirmed.json, so
// Claim reads the record first: when that change held no port of its own,
// the clean-up left by the one before it is this run's (Held.Carried).
//
// The package reaches the host only through host.System (depguard
// confirm-through-host) and renders nothing: Held.Report says in words what
// the clean-up did and left, as the engine's summaries and errors are
// words, and cmd/hardn renders the run through pkg/output and the report
// through pkg/ui. docs/engine/confirm-cleanup.md describes the
// guarantees, the facts they rely on and what is written but not yet run on
// a Linux host.
package confirm

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// ControlID is the clean-up's control, and the prefix of its changes' ids
// (ssh.cleanup/firewall, /files, /reload, /verify).
const ControlID = "ssh.cleanup"

// Held is what `hardn confirm` found of the change it kept, or, with
// nothing pending, of the last change confirmed: what the change held
// during its confirm window, and whether this session cleans it up (Run).
type Held struct {
	// Confirmed is revert.Confirm's result when this run confirmed a
	// change; nil when nothing was pending (Finishing).
	Confirmed *revert.ConfirmResult
	// Change is the change whose clean-up this is: the one confirmed now;
	// Finishing, the last one confirmed (confirmed.json); Carried, the one
	// confirmed before Confirmed's.
	Change revert.Journal
	// Finishing: nothing was pending, and the last change confirmed still
	// has a port it held in 00-hardn.conf (a crash between the claim and
	// the clean-up, a clean-up that did not finish, or a console's
	// confirmation).
	Finishing bool
	// Carried: the change confirmed now held no port, and the one
	// confirmed before it, whose record in confirmed.json this claim
	// replaced, still has a port it held in 00-hardn.conf. No later
	// `hardn confirm` finds that port, so its clean-up is this one.
	Carried bool
	// Cleanup is what the change held (steps.CleanupOf); no Drop: nothing,
	// and confirm behaves as before the clean-up existed. Finishing or
	// Carried, Drop has only the ports 00-hardn.conf still has.
	Cleanup steps.Cleanup
	// Console: the session is a console, not SSH (the local-console
	// exception), so the clean-up is left for a new SSH connection to the
	// new port.
	Console bool
	// Refused is why this session may not finish the clean-up, Finishing
	// or Carried: revert.CheckConfirm's refusal over Change's journal.
	Refused error
}

// Holds reports whether the change held a port that the clean-up drops.
func (h Held) Holds() bool { return len(h.Cleanup.Drop) > 0 }

// Run reports whether the clean-up is to be run from this session.
func (h Held) Run() bool { return h.Holds() && !h.Console && h.Refused == nil }

// Claim is `hardn confirm`'s claim: revert.Confirm from session s, and what
// the change confirmed held (steps.CleanupOf). Its error is revert.Confirm's,
// the confirmation's. confirmed.json is read first, since the claim renames
// the journal over it: when the change confirmed held no port, and the one
// confirmed before it still has one in 00-hardn.conf, the clean-up is that
// change's (Carried); when it held ports, the ufw rules the one before it
// added are added to its clean-up's (they are deleted only for a port it
// drops). When nothing is pending (*revert.NotPendingError), and the last
// change confirmed still has a held port in 00-hardn.conf, Held says so
// (Finishing). Carried or Finishing, the session runs the clean-up only when
// it could have confirmed that change: revert.CheckConfirm over its
// journal, over SSH (Run; Refused or Console otherwise). Claim reads the
// host through revert and reads 00-hardn.conf; the clean-up is Cleanup's.
func Claim(ctx context.Context, sys host.System, s revert.Session, opts revert.Options) (Held, error) {
	before, berr := revert.ReadConfirmed(ctx, sys, opts)
	res, err := revert.Confirm(ctx, sys, s, opts)
	var np *revert.NotPendingError
	if err != nil && !errors.As(err, &np) {
		return Held{}, err
	}
	if berr != nil {
		logf(opts, "confirm: the last change confirmed cannot be read, so whether its clean-up is left cannot be told: %v", berr)
		before = nil
	}
	if err == nil {
		h := Held{Confirmed: &res, Change: res.Journal, Cleanup: steps.CleanupOf(res.Journal), Console: res.Local}
		if before != nil && before.ID != res.Journal.ID {
			h = h.carry(ctx, sys, *before, s)
		}
		return h, nil
	}
	if before == nil {
		return Held{}, err
	}
	h := Held{Change: *before, Finishing: true, Cleanup: steps.CleanupOf(*before)}
	if h.Cleanup.Drop = stillHeld(ctx, sys, h.Cleanup.Drop); !h.Holds() {
		return Held{}, err
	}
	h.Console, h.Refused = revert.CheckConfirm(*before, s)
	return h, err
}

// carry is h, the claim of a change, with what b, the change confirmed
// before it, left: b's record in confirmed.json is gone once h's journal is
// renamed over it. When h's change held ports, its Drop has every port sshd
// was configured for at its apply, b's held ones among them, so only b's
// ufw commands are added to h's (PlanCleanup deletes a rule only for a port
// it drops). When it held none, and 00-hardn.conf still has a port b held
// (none that h's change added), the clean-up is b's (Carried), run when s
// could have confirmed b, as Finishing's is.
func (h Held) carry(ctx context.Context, sys host.System, b revert.Journal, s revert.Session) Held {
	c := steps.CleanupOf(b)
	if h.Holds() {
		for _, a := range c.Firewall {
			if !slices.ContainsFunc(h.Cleanup.Firewall, func(f []string) bool { return slices.Equal(f, a) }) {
				h.Cleanup.Firewall = append(h.Cleanup.Firewall, a)
			}
		}
		return h
	}
	via := viaPorts(h.Change)
	if c.Drop = slices.DeleteFunc(stillHeld(ctx, sys, c.Drop), func(p uint16) bool { return slices.Contains(via, p) }); len(c.Drop) == 0 {
		return h
	}
	k := Held{Confirmed: h.Confirmed, Change: b, Carried: true, Cleanup: c, Console: h.Console}
	if !k.Console {
		_, k.Refused = revert.CheckConfirm(b, s)
	}
	return k
}

// stillHeld are the ports of ps that 00-hardn.conf still has a Port line
// for, in order: what is left of a clean-up, read from the one file it
// edits (PlanCleanup refuses a port configured elsewhere). None when the
// file is not there; ps itself when it cannot be read, since what is left
// is then not known (the clean-up's plan reads sshd -T).
func stillHeld(ctx context.Context, sys host.System, ps []uint16) []uint16 {
	data, err := sys.ReadFile(ctx, sshd.HardnDropIn)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return ps
	}
	var out []uint16
	for _, l := range sshd.ReadOrder(data, nil) {
		if l.Keyword != "port" || l.Match != nil {
			continue
		}
		for _, a := range l.Args {
			if n, err := strconv.ParseUint(a, 10, 16); err == nil && slices.Contains(ps, uint16(n)) && !slices.Contains(out, uint16(n)) {
				out = append(out, uint16(n))
			}
		}
	}
	slices.Sort(out)
	return out
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
	// Result is Apply's, once it was called (with nothing applied when the
	// run lock or the plan made again under it stopped it); nil for an
	// error before the plan, a refusal at plan time, or no clean-up run.
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
