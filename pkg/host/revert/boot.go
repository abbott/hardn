package revert

// At boot (refactor plan P3, next/p3-revert-boot; P-34 (c), decided by the
// maintainer 2026-10-19): the revert timer is runtime state, which a reboot
// loses. The boot unit (bootunit.go) runs `hardn rollback --boot`, Boot,
// which re-arms it for what remains of the window, or rolls the change back
// at once. docs/safety/revert-boot.md has the guarantees.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// BootAction is what Boot did.
type BootAction string

const (
	// BootNothing: nothing to do. No change is journalled (and the boot
	// unit was removed), the change has no timer, its timer was started in
	// this boot, or a rollback of it runs.
	BootNothing BootAction = "nothing"
	// BootRearmed: the timer runs again, for what remains of the window
	// (BootResult.For).
	BootRearmed BootAction = "rearmed"
	// BootRolledBack: the change was rolled back at once (BootResult.Rollback).
	BootRolledBack BootAction = "rolled-back"
	// BootLeft: a lockout-risk change, or an unfinished rollback, Boot does
	// not act on (another hardn binary's journal, one that does not say
	// which, a rollback left to `hardn rollback <id>`, a change it does not
	// re-arm while another hardn holds the run lock); BootResult.Why says
	// what to do.
	BootLeft BootAction = "left"
)

// BootReason says, as a stable value, why Boot did what it did (BootResult.
// Why says it in words, which may change): what p3-output maps to its
// registry codes. Each goes with one BootAction, named beside it.
type BootReason string

const (
	BootReasonNoChange           BootReason = "no-change"           // nothing: nothing is journalled (also a change confirmed, late too)
	BootReasonNoTimer            BootReason = "no-timer"            // nothing: a change with no timer, left to the next run's notice
	BootReasonTimerRuns          BootReason = "timer-runs"          // nothing: its timer was started in this boot, and is there
	BootReasonRollbackRuns       BootReason = "rollback-runs"       // nothing: a rollback of it runs
	BootReasonConfirmedMeanwhile BootReason = "confirmed-meanwhile" // nothing: confirmed after Boot read the journal
	BootReasonClaimedMeanwhile   BootReason = "claimed-meanwhile"   // nothing: a confirmation or a rollback claimed it after Boot read the journal
	BootReasonApplyRuns          BootReason = "apply-runs"          // nothing: its timer, of this boot, is not there, it is not applied yet, and another hardn holds the run lock: taken as that hardn's apply (next/p3-engine-boot)
	BootReasonInWindow           BootReason = "in-window"           // rearmed: for what remains of the window
	BootReasonClockUntrusted     BootReason = "clock-untrusted"     // rearmed: for the whole window, the clock reading before the change
	BootReasonDeadlinePassed     BootReason = "deadline-passed"     // rolled back: past its deadline
	BootReasonApplyInterrupted   BootReason = "apply-interrupted"   // rolled back: its apply did not finish
	BootReasonRearmFailed        BootReason = "rearm-failed"        // rolled back: its timer could not be re-armed
	BootReasonJournalUnreadable  BootReason = "journal-unreadable"  // left: the journal cannot be read (Boot's error says why)
	BootReasonRollbackUnfinished BootReason = "rollback-unfinished" // left: a rollback of it did not finish
	BootReasonNoBinaryRecorded   BootReason = "no-binary-recorded"  // left: the journal does not name the binary its timer runs
	BootReasonOtherBinary        BootReason = "other-binary"        // left: its timer runs another path
	BootReasonOtherContent       BootReason = "other-content"       // left: the binary at the path is not the one that wrote the journal
	BootReasonBinaryUnreadable   BootReason = "binary-unreadable"   // left: this hardn's binary cannot be read
	BootReasonNoBootID           BootReason = "no-boot-id"          // left: no boot id recorded, or this boot's cannot be read
	BootReasonTimerUnknown       BootReason = "timer-unknown"       // left: started in this boot, and whether it is still there cannot be told
	BootReasonRunLockHeld        BootReason = "run-lock-held"       // left: its timer, of this boot, is not there, it is applied, and another hardn holds the run lock (next/p3-engine-boot)
)

// BootResult is what Boot found and did.
type BootResult struct {
	Action BootAction
	Reason BootReason
	Why    string // one plain-text line: why, and what to do when Left
	// Change is the journalled change as it is when Boot returns: as found,
	// or, when Rearmed, as re-armed (its new timer); zero when none.
	Change Journal
	// Timer is the re-armed timer; For how long it runs: what remains of
	// the window, or the whole window when the clock read before the change
	// was made (ClockUntrusted).
	Timer          *Timer
	For            time.Duration
	ClockUntrusted bool
	Rollback       *RollbackResult // the rollback run at once
	// Removed: nothing is journalled, and the boot unit was removed;
	// RemoveErr says why it was not, when it was to be.
	Removed   bool
	RemoveErr error
}

// bootStepTimeout bounds Boot's own steps before a rollback: the journal
// read, the hash, the timer's state, the re-arm. A rollback then runs under
// its own RollbackTimeout, and the boot unit's removal after it under
// cleanupTimeout, so Boot ends within the sum of the three, which the
// unit's TimeoutStartSec is above (bootunit.go): sshd waits for the unit.
const bootStepTimeout = time.Minute

// Boot is `hardn rollback --boot`, run by the boot unit before sshd starts.
// opts.Hardn is the running hardn's path (os.Executable, resolved), in the
// init system detected now. A timer started in this boot (Timer.Boot) that
// is still there (timerRuns) is left to run (BootNothing); one that is not
// there (a re-arm stopped before it started the timer, a systemd
// soft-reboot, which keeps the boot's id) is taken as after a reboot, and
// one whose state cannot be read is BootLeft. While another hardn holds the
// run lock, one that is not there is that hardn's apply's when the change
// is not applied yet (BootNothing, BootReasonApplyRuns), and BootLeft when
// it is (BootReasonRunLockHeld): never re-armed beside the holder. It acts
// on a lockout-risk change journalled in pending.json only when the
// journal's timer runs this hardn, by path and SHA-256 (Timer.Hardn,
// HardnSHA256), and records the boot it was started in; a journal that
// lacks either, or names another binary, is BootLeft: reported, not acted
// on. Then:
//
//   - a change whose apply did not finish (no Applied) is rolled back at
//     once: it cannot be confirmed, and its files, which no check after
//     the apply has passed, are what sshd is about to read;
//   - with a clock that reads before the change was created, the timer is
//     re-armed for the whole window (ClockUntrusted): what remains of it
//     cannot be told;
//   - past the deadline (Journal.Deadline), it is rolled back at once;
//   - otherwise the timer is re-armed for what remains, rounded up to the
//     second, never longer than the window.
//
// A rollback at boot is Rollback with ReasonTimeout (it leaves the next
// run's notice), and, run in the boot unit's start, Options.NoStart: no
// unit is started from the boot unit, which sshd's units wait for; they
// start after it and read the restored files. Run by hand at another time,
// a unit that is down is started, as `hardn rollback <id>` starts it
// (inBootUnitStart). When the timer cannot be re-armed (no timer for the init
// system, hardn no longer CheckExecutable's, the timer does not start), the
// change is rolled back at once: a change nothing would ever roll back is
// what P-34 is about. The re-armed journal is written before the timer
// starts (as Arm), and read again after: a confirmation or a rollback that
// claimed it meanwhile wins, and the timer is not started. Boot reads the
// journal, and writes the re-armed one, under the journal lock
// (JournalLockName), and writes it only over the journal it read
// (writeRearmed), so it never writes over a change armed, re-armed or
// claimed since; it holds the lock across no command, and never while it
// rolls back (Rollback takes the lock itself). A lock it cannot have for
// the re-arm is a timer it cannot re-arm: the change is rolled back.
//
// Nothing journalled: nothing to do (a change confirmed, also late, after a
// reboot, is in confirmed.json, not pending.json), and the boot unit is
// removed (RemoveBootUnit), as it is after a rollback here that finished. A
// rollback that runs or did not finish (rollback.json) is left to its own
// path, `hardn rollback <id>` or `--abandon`; a change with no timer, to the
// next run's notice. Every decision and step goes to Options.Log.
// BootResult.Reason says why, as a stable value. Boot's own steps run under
// bootStepTimeout, a rollback under its own RollbackTimeout, and the
// removal under cleanupTimeout.
func Boot(ctx context.Context, sys host.System, in detect.Init, opts Options) (BootResult, error) {
	dctx, cancel := context.WithTimeout(ctx, bootStepTimeout)
	// The run lock, tried once and never waited for, and held to the end
	// (lock.go): no apply installs the boot unit or arms a change while
	// Boot acts on the journal and removes the unit. Another hardn that
	// holds it already (applyErr is host.ErrLocked) may be an apply that
	// owns a change whose timer it has not started yet; another error is
	// logged, and Boot goes on.
	unlockRun, applyErr := LockRun(dctx, sys, opts, "hardn rollback --boot")
	switch {
	case applyErr == nil:
		defer func() { _ = unlockRun() }()
	case errors.Is(applyErr, host.ErrLocked):
		opts.logf("boot: another hardn holds the run lock: %v", applyErr)
	default:
		opts.logf("boot: the run lock cannot be taken, so the boot unit is not removed: %v", applyErr)
	}
	res, err := boot(dctx, sys, in, applyErr, opts)
	cancel()
	why := res.Why
	if err != nil {
		why += ": " + err.Error()
	}
	if id := res.Change.ID; id != "" {
		opts.logf("boot: change %s (%s): %s: %s", id, res.Change.Summary, res.Action, why)
	} else {
		opts.logf("boot: %s: %s", res.Action, why)
	}
	if err == nil && (res.Action == BootNothing || res.Action == BootRolledBack) {
		rctx, cancel := cleanupContext(ctx)
		removeBootUnit(rctx, sys, in, applyErr, opts, &res)
		cancel()
	}
	return res, err
}

// boot is Boot's decision and action; applyErr is why Boot does not hold
// the run lock (nil: it does).
func boot(ctx context.Context, sys host.System, in detect.Init, applyErr error, opts Options) (BootResult, error) {
	var res BootResult
	done := func(a BootAction, r BootReason, format string, args ...any) (BootResult, error) {
		res.Action, res.Reason, res.Why = a, r, fmt.Sprintf(format, args...)
		return res, nil
	}
	// The read under the journal lock (lock.go), so that it never sees a
	// rollback between its claim and its first progress write (which reads
	// as one that did not finish); released before anything is run. At
	// boot nothing else holds it; a lock that cannot be had is logged, and
	// the journal read as it is.
	unlock, lerr := lockJournal(ctx, sys, opts, "hardn rollback --boot")
	if lerr != nil {
		opts.logf("boot: the journal is read without the journal lock: %v", lerr)
		unlock = func() {}
	}
	st, err := ReadState(ctx, sys, opts)
	unlock()
	if err != nil {
		res.Action, res.Reason, res.Why = BootLeft, BootReasonJournalUnreadable, "the change journal cannot be read, so hardn does not act on it"
		return res, err
	}
	if j := st.RollingBack; j != nil {
		res.Change = *j
		if st.Running {
			return done(BootNothing, BootReasonRollbackRuns, "its rollback runs (process %d), and is left to finish", j.Rollback.Runner.PID)
		}
		return done(BootLeft, BootReasonRollbackUnfinished, "its rollback did not finish, and is left to its own path: run `%s rollback %s` to finish it, or, if it cannot finish, give it up with `%s rollback --abandon %s`", st.hardn(), j.ID, st.hardn(), j.ID)
	}
	p := st.Pending
	if p == nil {
		return done(BootNothing, BootReasonNoChange, "no change is journalled%s", lastConfirmed(ctx, sys, opts))
	}
	res.Change = *p
	if p.Risk != RiskLockout {
		return done(BootNothing, BootReasonNoTimer, "it has no timer, and did not finish: the next run's notice says how to undo it (`%s rollback %s`)", st.hardn(), p.ID)
	}
	t := *p.Timer
	thisBoot, bootErr := bootID(ctx, sys)
	leave := "confirm it with `" + st.hardn() + " confirm` from a new SSH connection, or undo it with `" + st.hardn() + " rollback " + p.ID + "`"
	if bootErr == nil && t.Boot == thisBoot {
		// The journal says the timer runs; it may not: a re-arm stopped
		// between its journal write and the timer's start, or a systemd
		// soft-reboot, which stops the timer and keeps the boot's id.
		switch runs, err := timerRuns(ctx, sys, t); {
		case err != nil:
			return done(BootLeft, BootReasonTimerUnknown, "its timer was started in this boot, and whether it is still there cannot be told (%v); %s", err, leave)
		case runs:
			return done(BootNothing, BootReasonTimerRuns, "its timer was started in this boot, and is there")
		case errors.Is(applyErr, host.ErrLocked) && p.Applied == nil:
			// Not after a reboot: a change not applied yet is taken as the
			// apply's of the hardn that holds the run lock, between Arm's
			// journal write and its timer's start, or removing a journal
			// whose timer did not start. Boot re-arms only an applied
			// change, so the holder is no other `hardn rollback --boot`.
			return done(BootNothing, BootReasonApplyRuns, "its timer was started in this boot and is not there, it is not applied yet, and another hardn holds the run lock (%v): it is taken as that hardn's apply, which starts its timer or rolls it back; if no apply runs, run `%s rollback --boot` again once that hardn has finished", applyErr, st.hardn())
		case errors.Is(applyErr, host.ErrLocked):
			// Applied: its apply is over (MarkApplied is an apply's last
			// write), so the holder is something else: another `hardn
			// rollback --boot` that re-arms it (a second re-arm would fail
			// on its timer's unit, and roll it back), an uninstall, an apply
			// about to be refused. Left, never re-armed beside it.
			return done(BootLeft, BootReasonRunLockHeld, "its timer was started in this boot and is not there, and another hardn holds the run lock (%v), so hardn does not re-arm it beside that one: run `%s rollback --boot` again once that hardn has finished, or %s", applyErr, st.hardn(), leave)
		}
		opts.logf("boot: change %s (%s): its timer was started in this boot and is not there (a re-arm stopped before it started the timer, or a soft-reboot), so it is taken as after a reboot", p.ID, p.Summary)
	}
	switch {
	case t.Hardn == "":
		return done(BootLeft, BootReasonNoBinaryRecorded, "its journal does not say which hardn binary its timer runs (a hardn older than this one wrote it), so hardn does not act on it at boot; %s", leave)
	case t.Hardn != opts.Hardn:
		return done(BootLeft, BootReasonOtherBinary, "its timer runs %s, not this hardn (%s): another hardn's journal is not acted on at boot; %s", t.Hardn, opts.Hardn, leave)
	case t.Boot == "":
		return done(BootLeft, BootReasonNoBootID, "its journal does not say in which boot its timer was started, so whether that timer still runs cannot be told; %s", leave)
	case bootErr != nil:
		return done(BootLeft, BootReasonNoBootID, "this boot's id cannot be read (%v), so whether its timer still runs cannot be told; %s", bootErr, leave)
	}
	sum, err := hashFile(ctx, sys, opts.Hardn)
	switch {
	case err != nil:
		return done(BootLeft, BootReasonBinaryUnreadable, "this hardn's binary cannot be read (%v), so whether it wrote the journal cannot be told; %s", err, leave)
	case sum != t.HardnSHA256:
		return done(BootLeft, BootReasonOtherContent, "%s is not the binary that wrote the journal: its sha256 is %s, the journal's binary's was %s; another hardn's journal is not acted on at boot; %s", opts.Hardn, sum, t.HardnSHA256, leave)
	}
	now := opts.now()
	switch {
	case p.Applied == nil:
		return rollBackAtBoot(ctx, sys, res, in, BootReasonApplyInterrupted, "its apply did not finish before the reboot, so it cannot be confirmed, and its files, which no check after the apply has passed, are what sshd is about to read", opts)
	case now.Before(p.Created):
		res.ClockUntrusted = true
		return rearm(ctx, sys, res, st, in, thisBoot, p.TimeoutSeconds, now.UTC().Truncate(time.Second).Add(p.Timeout()), opts)
	case !now.Before(p.Deadline()):
		return rollBackAtBoot(ctx, sys, res, in, BootReasonDeadlinePassed, "its deadline, "+stamp(p.Deadline())+", has passed", opts)
	}
	left := p.Deadline().Sub(now)
	secs := int((left + time.Second - 1) / time.Second)
	secs = min(max(secs, 1), p.TimeoutSeconds)
	return rearm(ctx, sys, res, st, in, thisBoot, secs, p.Deadline(), opts)
}

// rearm starts the timer of res.Change again, for secs seconds, with
// deadline, in boot thisBoot; when it cannot, it rolls the change back at
// once.
func rearm(ctx context.Context, sys host.System, res BootResult, st State, in detect.Init, thisBoot string, secs int, deadline time.Time, opts Options) (BootResult, error) {
	j := res.Change
	old := *j.Timer
	t, err := timerFor(in, j.ID, old.Hardn, secs, opts.runDir())
	if err == nil {
		err = CheckExecutable(ctx, sys, old.Hardn)
	}
	if err != nil {
		return rollBackAtBoot(ctx, sys, res, in, BootReasonRearmFailed, fmt.Sprintf("its timer cannot be re-armed (%v), and a change no timer rolls back could lock you out", err), opts)
	}
	t.Deadline, t.Hardn, t.HardnSHA256, t.Boot = deadline, old.Hardn, old.HardnSHA256, thisBoot
	j.Timer = &t
	if err := j.Check(); err != nil {
		return rollBackAtBoot(ctx, sys, res, in, BootReasonRearmFailed, fmt.Sprintf("its re-armed journal is not valid (%v)", err), opts)
	}
	// Written before the timer starts, as Arm writes it, under the journal
	// lock, and only over the journal Boot read (writeRearmed). Released
	// before the timer starts and before any rollback, which takes it
	// itself.
	unlock, err := lockJournal(ctx, sys, opts, "hardn rollback --boot")
	if err != nil {
		return rollBackAtBoot(ctx, sys, res, in, BootReasonRearmFailed, fmt.Sprintf("its re-armed journal could not be written (%v)", err), opts)
	}
	reason, why, err := writeRearmed(ctx, sys, res.Change, j, opts)
	unlock()
	switch {
	case reason != "":
		res.Action, res.Reason, res.Why = BootNothing, reason, why
		return res, err
	case err != nil:
		return rollBackAtBoot(ctx, sys, res, in, BootReasonRearmFailed, fmt.Sprintf("its re-armed journal could not be written (%v)", err), opts)
	}
	if err := startTimer(ctx, sys, t, opts); err != nil {
		return rollBackAtBoot(ctx, sys, res, in, BootReasonRearmFailed, fmt.Sprintf("its timer did not start (%v)", err), opts)
	}
	res.Change, res.Timer, res.For = j, &t, time.Duration(secs)*time.Second
	res.Action, res.Reason = BootRearmed, BootReasonInWindow
	how := fmt.Sprintf("it is rolled back at %s unless confirmed with `%s confirm` from %s, made from another machine", stamp(t.Deadline), st.hardn(), confirmWhere(j))
	res.Why = fmt.Sprintf("its timer is re-armed for what remains of its window, %s: %s", res.For, how)
	if res.ClockUntrusted {
		res.Reason = BootReasonClockUntrusted
		res.Why = fmt.Sprintf("the clock reads %s, before the change was made (%s), so what remains of its window cannot be told: its timer is re-armed for the whole window, %s: %s, by this clock",
			stamp(opts.now()), stamp(j.Created), res.For, how)
	}
	return res, nil
}

// writeRearmed writes re-armed journal j over pending.json, which must
// still hold read, the journal Boot read: the caller holds the journal
// lock, so no Arm, rollback or other re-arm writes or claims the journal
// meanwhile. A journal that changed since (a rollback claimed it, another
// `hardn rollback --boot` re-armed it, a new change was armed after a
// confirmation) is left as it is now, and so is a change confirmed (a
// confirmation takes no lock: its claim is one rename, so confirmed.json
// is read again after the write, and the pending.json written is then
// removed, before the lock is released). It returns the reason, and why,
// when the change was not re-armed for one of these; err when the journal
// could not be read or written.
func writeRearmed(ctx context.Context, sys host.System, read, j Journal, opts Options) (BootReason, string, error) {
	confirmed := "it was confirmed while hardn re-armed its timer"
	st, err := ReadState(ctx, sys, opts)
	if err != nil {
		return "", "", err
	}
	if st.Pending == nil || !sameJournal(*st.Pending, read) {
		if confirmedMeanwhile(ctx, sys, read.ID, opts) {
			return BootReasonConfirmedMeanwhile, confirmed, nil
		}
		return BootReasonClaimedMeanwhile, "the journal changed while hardn re-armed its timer (a rollback claimed it, or another `hardn rollback --boot` re-armed it), and is left as it is now", nil
	}
	switch err := rewritePending(ctx, sys, j, opts); {
	case errors.Is(err, ErrRollingBack):
		return BootReasonClaimedMeanwhile, "a rollback claimed it while hardn re-armed its timer, and has it", nil
	case err != nil:
		return "", "", err
	}
	if confirmedMeanwhile(ctx, sys, j.ID, opts) {
		return BootReasonConfirmedMeanwhile, confirmed, removeIfThere(ctx, sys, filepath.Join(opts.dir(), PendingName))
	}
	return "", "", nil
}

// sameJournal reports whether a and b are the same journal, as written.
func sameJournal(a, b Journal) bool {
	da, ea := encode(a)
	db, eb := encode(b)
	return ea == nil && eb == nil && bytes.Equal(da, db)
}

// confirmedMeanwhile reports whether confirmed.json holds change id: it was
// confirmed after Boot read the journal. One that cannot be read says no.
func confirmedMeanwhile(ctx context.Context, sys host.System, id string, opts Options) bool {
	c, err := readJournal(ctx, sys, filepath.Join(opts.dir(), ConfirmedName))
	if err != nil {
		opts.logf("boot: %s cannot be read: %v", ConfirmedName, err)
		return false
	}
	return c != nil && c.ID == id
}

// rollBackAtBoot rolls res.Change back at once, for reason, why: as the
// timer would (ReasonTimeout, so the next run's notice says so), starting
// no unit (NoStart) when it runs in the boot unit's start.
func rollBackAtBoot(ctx context.Context, sys host.System, res BootResult, in detect.Init, reason BootReason, why string, opts Options) (BootResult, error) {
	opts.logf("boot: change %s (%s): rolled back now: %s", res.Change.ID, res.Change.Summary, why)
	o := opts
	o.Reason, o.NoStart = ReasonTimeout, inBootUnitStart(ctx, sys, in, opts)
	rr, err := Rollback(ctx, sys, res.Change.ID, o)
	res.Rollback, res.Reason = &rr, reason
	switch {
	case err != nil:
		res.Action, res.Why = BootRolledBack, why+"; the rollback did not finish"
	case rr.Nothing:
		res.Action, res.Reason, res.Why = BootNothing, BootReasonClaimedMeanwhile, rr.Why
	default:
		res.Action, res.Why = BootRolledBack, why+": rolled back"
	}
	return res, err
}

// openRCStarting holds a link for each service OpenRC is starting
// (RC_SVCDIR/starting; OpenRC's librc marks a service's state with a link
// to its script in the state's directory).
const openRCStarting = detect.OpenRCRunDir + "/starting"

// inBootUnitStart reports whether this command runs in the boot unit's
// start, before sshd's units: hardn-boot.service is activating (systemd),
// hardn-boot is starting (OpenRC). There no unit may be started
// (Options.NoStart): a start from it waits on the unit itself. Run by hand
// at another time (the unit active, failed or inactive; OpenRC: not
// starting), the rollback starts a unit that is down, as `hardn rollback
// <id>` does. When it cannot be told, it says yes and logs it: a start
// left out leaves a unit that is down so, where one made from the unit's
// start would wait on the unit.
func inBootUnitStart(ctx context.Context, sys host.System, in detect.Init, opts Options) bool {
	if in.Running == detect.Yes && in.Kind == detect.OpenRC {
		_, err := sys.Lstat(ctx, openRCStarting+"/"+BootUnitName)
		switch {
		case err == nil:
			return true
		case errors.Is(err, fs.ErrNotExist):
			opts.logf("boot: OpenRC is not starting %s, so this is not its start: a unit that is down is started, as `hardn rollback <id>` starts it", BootUnitName)
			return false
		}
		opts.logf("boot: whether this runs in %s's start cannot be told (%v), so no unit is started", BootUnitName, err)
		return true
	}
	if in.Running != detect.Yes || in.Kind != detect.Systemd {
		opts.logf("boot: init %s, running %s: whether this runs in the boot unit's start cannot be told, so no unit is started", in.Kind, in.Running)
		return true
	}
	unit := BootUnitName + ".service"
	res, err := sys.Run(ctx, "systemctl", "is-active", unit)
	state, _, _ := strings.Cut(string(res.Stdout), "\n")
	state = strings.TrimSpace(state)
	var xe *host.ExitError
	if err == nil || errors.As(err, &xe) {
		switch state {
		case "activating":
			return true
		case "active", "inactive", "failed":
			opts.logf("boot: %s is %s, so this is not its start: a unit that is down is started, as `hardn rollback <id>` starts it", unit, state)
			return false
		}
	}
	opts.logf("boot: whether this runs in %s's start cannot be told (%q, %v), so no unit is started", unit, state, err)
	return true
}

// lastConfirmed says which change confirmed.json keeps, if any.
func lastConfirmed(ctx context.Context, sys host.System, opts Options) string {
	c, err := readJournal(ctx, sys, filepath.Join(opts.dir(), ConfirmedName))
	if err != nil || c == nil {
		return ""
	}
	return fmt.Sprintf("; the last change confirmed is %s (%s)", c.ID, c.Summary)
}

// removeBootUnit removes the boot unit once nothing is journalled, under the
// run lock Boot holds (applyErr nil); a change journalled since keeps it,
// and so does an apply that holds the run lock (applyErr host.ErrLocked),
// which may have installed it and not yet armed its change. The next boot
// that finds nothing journalled removes it.
func removeBootUnit(ctx context.Context, sys host.System, in detect.Init, applyErr error, opts Options, res *BootResult) {
	u, err := PlanBootUnit(in, opts.Hardn)
	switch {
	case err != nil:
	case applyErr != nil:
		err = fmt.Errorf("revert: boot unit %s kept: %w", u.Path, applyErr)
	default:
		res.Removed, err = removeUnit(ctx, sys, u, opts)
	}
	switch {
	case errors.Is(err, ErrPending), errors.Is(err, host.ErrLocked):
		res.Removed = false
		opts.logf("boot: the boot unit is kept: %v", err)
	case err != nil:
		res.Removed, res.RemoveErr = false, err // part of it may be gone: not removed as planned
		opts.logf("boot: the boot unit was not removed: %v", err)
	}
}
