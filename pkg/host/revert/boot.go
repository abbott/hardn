package revert

// At boot (refactor plan P3, next/p3-revert-boot; P-34 (c), decided by the
// maintainer 2026-10-19): the revert timer is runtime state, which a reboot
// loses. The boot unit (bootunit.go) runs `hardn rollback --boot`, Boot,
// which re-arms it for what remains of the window, or rolls the change back
// at once. docs/safety/revert-boot.md has the guarantees.

import (
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
	// which, a rollback left to `hardn rollback <id>`); BootResult.Why says
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
// one whose state cannot be read is BootLeft. It acts on a lockout-risk
// change journalled in pending.json only when the journal's timer runs
// this hardn, by path and SHA-256 (Timer.Hardn, HardnSHA256), and records
// the boot it was started in; a journal that lacks either, or names
// another binary, is BootLeft: reported, not acted on. Then:
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
// claimed it meanwhile wins, and the timer is not started.
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
	res, err := boot(dctx, sys, in, opts)
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
		removeBootUnit(rctx, sys, in, opts, &res)
		cancel()
	}
	return res, err
}

func boot(ctx context.Context, sys host.System, in detect.Init, opts Options) (BootResult, error) {
	var res BootResult
	done := func(a BootAction, r BootReason, format string, args ...any) (BootResult, error) {
		res.Action, res.Reason, res.Why = a, r, fmt.Sprintf(format, args...)
		return res, nil
	}
	st, err := ReadState(ctx, sys, opts)
	if err != nil {
		res.Action, res.Reason, res.Why = BootLeft, BootReasonJournalUnreadable, "the change journal cannot be read, so hardn does not act on it"
		return res, err
	}
	if j := st.RollingBack; j != nil {
		res.Change = *j
		if st.Running {
			return done(BootNothing, BootReasonRollbackRuns, "its rollback runs (process %d), and is left to finish", j.Rollback.Runner.PID)
		}
		return done(BootLeft, BootReasonRollbackUnfinished, "its rollback did not finish, and is left to its own path: run `sudo hardn rollback %s` to finish it, or, if it cannot finish, give it up with `sudo hardn rollback --abandon %s`", j.ID, j.ID)
	}
	p := st.Pending
	if p == nil {
		return done(BootNothing, BootReasonNoChange, "no change is journalled%s", lastConfirmed(ctx, sys, opts))
	}
	res.Change = *p
	if p.Risk != RiskLockout {
		return done(BootNothing, BootReasonNoTimer, "it has no timer, and did not finish: the next run's notice says how to undo it (`sudo hardn rollback %s`)", p.ID)
	}
	t := *p.Timer
	thisBoot, bootErr := bootID(ctx, sys)
	leave := "confirm it with `sudo hardn confirm` from a new SSH connection, or undo it with `sudo hardn rollback " + p.ID + "`"
	if bootErr == nil && t.Boot == thisBoot {
		// The journal says the timer runs; it may not: a re-arm stopped
		// between its journal write and the timer's start, or a systemd
		// soft-reboot, which stops the timer and keeps the boot's id.
		switch runs, err := timerRuns(ctx, sys, t); {
		case err != nil:
			return done(BootLeft, BootReasonTimerUnknown, "its timer was started in this boot, and whether it is still there cannot be told (%v); %s", err, leave)
		case runs:
			return done(BootNothing, BootReasonTimerRuns, "its timer was started in this boot, and is there")
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
		return done(BootLeft, BootReasonOtherContent, "%s is not the binary that wrote the journal (sha256 %s, then %s): another hardn's journal is not acted on at boot; %s", opts.Hardn, sum, t.HardnSHA256, leave)
	}
	now := opts.now()
	switch {
	case p.Applied == nil:
		return rollBackAtBoot(ctx, sys, res, in, BootReasonApplyInterrupted, "its apply did not finish before the reboot, so it cannot be confirmed, and its files, which no check after the apply has passed, are what sshd is about to read", opts)
	case now.Before(p.Created):
		res.ClockUntrusted = true
		return rearm(ctx, sys, res, in, thisBoot, p.TimeoutSeconds, now.UTC().Truncate(time.Second).Add(p.Timeout()), opts)
	case !now.Before(p.Deadline()):
		return rollBackAtBoot(ctx, sys, res, in, BootReasonDeadlinePassed, "its deadline, "+stamp(p.Deadline())+", has passed", opts)
	}
	left := p.Deadline().Sub(now)
	secs := int((left + time.Second - 1) / time.Second)
	secs = min(max(secs, 1), p.TimeoutSeconds)
	return rearm(ctx, sys, res, in, thisBoot, secs, p.Deadline(), opts)
}

// rearm starts the timer of res.Change again, for secs seconds, with
// deadline, in boot thisBoot; when it cannot, it rolls the change back at
// once.
func rearm(ctx context.Context, sys host.System, res BootResult, in detect.Init, thisBoot string, secs int, deadline time.Time, opts Options) (BootResult, error) {
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
	// Written before the timer starts, as Arm writes it, then read again:
	// a confirmation or a rollback that claimed the change since ReadState
	// wins, and its pending.json, written again here, is removed.
	switch err := rewritePending(ctx, sys, j, opts); {
	case errors.Is(err, ErrRollingBack):
		res.Action, res.Reason, res.Why = BootNothing, BootReasonClaimedMeanwhile, "a rollback claimed it while hardn re-armed its timer, and has it"
		return res, nil
	case err != nil:
		return rollBackAtBoot(ctx, sys, res, in, BootReasonRearmFailed, fmt.Sprintf("its re-armed journal could not be written (%v)", err), opts)
	}
	if confirmedMeanwhile(ctx, sys, j.ID, opts) {
		res.Action, res.Reason, res.Why = BootNothing, BootReasonConfirmedMeanwhile, "it was confirmed while hardn re-armed its timer"
		return res, removeIfThere(ctx, sys, filepath.Join(opts.dir(), PendingName))
	}
	if err := startTimer(ctx, sys, t, opts); err != nil {
		return rollBackAtBoot(ctx, sys, res, in, BootReasonRearmFailed, fmt.Sprintf("its timer did not start (%v)", err), opts)
	}
	res.Change, res.Timer, res.For = j, &t, time.Duration(secs)*time.Second
	res.Action, res.Reason = BootRearmed, BootReasonInWindow
	how := fmt.Sprintf("it is rolled back at %s unless confirmed with `sudo hardn confirm` from %s, made from another machine", stamp(t.Deadline), confirmWhere(j))
	res.Why = fmt.Sprintf("its timer is re-armed for what remains of its window, %s: %s", res.For, how)
	if res.ClockUntrusted {
		res.Reason = BootReasonClockUntrusted
		res.Why = fmt.Sprintf("the clock reads %s, before the change was made (%s), so what remains of its window cannot be told: its timer is re-armed for the whole window, %s: %s, by this clock",
			stamp(opts.now()), stamp(j.Created), res.For, how)
	}
	return res, nil
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

// removeBootUnit removes the boot unit once nothing is journalled; a change
// journalled since keeps it.
func removeBootUnit(ctx context.Context, sys host.System, in detect.Init, opts Options, res *BootResult) {
	u, err := PlanBootUnit(in, opts.Hardn)
	if err == nil {
		res.Removed, err = RemoveBootUnit(ctx, sys, u, opts)
	}
	switch {
	case errors.Is(err, ErrPending):
		res.Removed = false
		opts.logf("boot: the boot unit is kept: %v", err)
	case err != nil:
		res.Removed, res.RemoveErr = false, err // part of it may be gone: not removed as planned
		opts.logf("boot: the boot unit was not removed: %v", err)
	}
}
