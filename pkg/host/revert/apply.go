package revert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Arm journals change j before its first step (brief 2.4: schedule the
// revert before applying), and returns it as written. It sets
// SchemaVersion, Created (Options.Now) and, for RiskLockout, Timer, from
// detect's init facts and Options.Hardn (CheckExecutable); the timer
// records that binary's path and SHA-256, and the boot's id, for `hardn
// rollback --boot` (Timer.Hardn, HardnSHA256, Boot; a boot id that cannot
// be read is left out and logged, and the boot command then leaves the
// change alone). It refuses, and writes nothing but the journal
// directory, while a change is journalled
// (*PendingError), and for a journal Check refuses. Then it writes
// pending.json and starts the timer; when the timer does not start, it
// removes pending.json again, even after Ctrl-C (under a context of its
// own), and returns the error: the change must not be applied. The engine
// adds the backups, inverse steps and reloads with Record, write-ahead, and
// ends a lockout-risk apply with MarkApplied.
func Arm(ctx context.Context, sys host.System, j Journal, in detect.Init, opts Options) (Journal, error) {
	j.SchemaVersion = SchemaVersion
	j.Created = opts.now().UTC().Truncate(time.Second)
	j.Timer, j.Applied, j.Rollback = nil, nil, nil
	if j.Risk == RiskLockout {
		t, err := TimerFor(in, j.ID, opts.Hardn, j.Timeout(), opts.runDir())
		if err != nil {
			return j, err
		}
		t.Deadline = j.Deadline()
		j.Timer = &t
	}
	if err := j.Check(); err != nil {
		return j, err
	}
	if err := opts.checkPlans(j); err != nil {
		return j, err
	}
	if j.Timer != nil {
		if err := CheckExecutable(ctx, sys, opts.Hardn); err != nil {
			return j, err
		}
		sum, err := hashFile(ctx, sys, opts.Hardn)
		if err != nil {
			return j, fmt.Errorf("revert: arm %s: hardn's binary: %w", j.ID, err)
		}
		j.Timer.Hardn, j.Timer.HardnSHA256 = opts.Hardn, sum
		if j.Timer.Boot, err = bootID(ctx, sys); err != nil {
			opts.logf("arm %s: the boot's id cannot be read, so after a reboot `hardn rollback --boot` leaves this change to the next run's notice: %v", j.ID, err)
		}
	}
	if err := stateDir(ctx, sys, opts.dir(), true); err != nil {
		return j, err
	}
	st, err := ReadState(ctx, sys, opts)
	if err != nil {
		return j, err
	}
	if st.Busy() {
		return j, &PendingError{State: st}
	}
	pending := filepath.Join(opts.dir(), PendingName)
	if err := write(ctx, sys, pending, j); err != nil {
		return j, err
	}
	if j.Timer == nil {
		opts.logf("journalled change %s (%s), risk %s, no timer", j.ID, j.Summary, j.Risk)
		return j, nil
	}
	if err := startTimer(ctx, sys, *j.Timer, opts); err != nil {
		cctx, cancel := cleanupContext(ctx)
		defer cancel()
		return j, errors.Join(fmt.Errorf("revert: arm %s: the timer did not start: %w", j.ID, err), removeIfThere(cctx, sys, pending))
	}
	opts.logf("armed change %s (%s): %s rolls it back at %s unless confirmed", j.ID, j.Summary, j.Timer.Kind, stamp(j.Deadline()))
	return j, nil
}

// Record rewrites the journal of change j before each of its steps takes
// effect (write-ahead, see Journal): the backups taken, the inverse steps,
// the reloads. What Arm set (Created, the timeout, the risk, the timer) is
// kept as written; an OpPlan step needs a rollback in Options.Plans
// (ErrNoPlanRollback). j must still be the pending change, not yet applied
// (ErrApplied). When a rollback has claimed it, Record returns
// ErrRollingBack, and the engine must stop before the step: the rollback
// has the change.
func Record(ctx context.Context, sys host.System, j Journal, opts Options) error {
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return err
	case st.RollingBack != nil && st.RollingBack.ID == j.ID:
		return fmt.Errorf("revert: record %s: %w", j.ID, ErrRollingBack)
	case st.Pending == nil || st.Pending.ID != j.ID:
		return fmt.Errorf("revert: record %s: not the pending change: %w", j.ID, ErrNothingPending)
	case st.Pending.Applied != nil:
		return fmt.Errorf("revert: record %s: applied at %s; a later step is a change of its own: %w", j.ID, stamp(st.Pending.Applied.At), ErrApplied)
	}
	p := st.Pending
	j.SchemaVersion, j.Created, j.TimeoutSeconds, j.Risk, j.Timer, j.Applied, j.Rollback = p.SchemaVersion, p.Created, p.TimeoutSeconds, p.Risk, p.Timer, nil, nil
	if err := j.Check(); err != nil {
		return err
	}
	if err := opts.checkPlans(j); err != nil {
		return err
	}
	return rewritePending(ctx, sys, j, opts)
}

// MarkApplied records that the apply of lockout-risk change id has
// finished, its last step and reload done (Journal.Applied), and returns
// the journal. It notes the sshd processes running at that moment, the
// server ends of the connections made before the change took effect, which
// cannot confirm it (CheckConfirm). Until it is called the change cannot be
// confirmed (ErrNotConfirmable) and the next run says it is being applied;
// a rollback is unaffected. When the processes cannot be listed it fails,
// and the change cannot be confirmed: the engine rolls it back
// (ReasonFailed). It is the apply's last write: Record refuses after it. A
// second call changes nothing.
func MarkApplied(ctx context.Context, sys host.System, id string, opts Options) (Journal, error) {
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return Journal{}, err
	case st.RollingBack != nil && st.RollingBack.ID == id:
		return *st.RollingBack, fmt.Errorf("revert: mark %s applied: %w", id, ErrRollingBack)
	case st.Pending == nil || st.Pending.ID != id:
		return Journal{}, fmt.Errorf("revert: mark %s applied: %w", id, ErrNothingPending)
	case st.Pending.Risk != RiskLockout:
		return *st.Pending, fmt.Errorf("revert: mark %s applied: a change with no timer ends with Done: %w", id, ErrNotConfirmable)
	case st.Pending.Applied != nil:
		return *st.Pending, nil
	}
	j := *st.Pending
	procs, err := sshdProcesses(ctx, sys)
	if err != nil {
		return j, fmt.Errorf("revert: mark %s applied: the SSH connections open now cannot be told from new ones: %w", id, err)
	}
	j.Applied = &Applied{At: opts.now().UTC(), SSHD: procs}
	if err := rewritePending(ctx, sys, j, opts); err != nil {
		return j, err
	}
	opts.logf("change %s (%s) applied; the %d sshd processes running now cannot confirm it", id, j.Summary, len(procs))
	return j, nil
}

// rewritePending writes j over pending.json, for Record and MarkApplied,
// which have read the state. A rollback that claimed the journal in between
// (renamed pending.json to rollback.json) would find pending.json written
// again. So rollback.json is read after the write: when it holds j's
// change, the pending.json just written is removed (no other change can be
// armed while rollback.json is there) and ErrRollingBack returned.
func rewritePending(ctx context.Context, sys host.System, j Journal, opts Options) error {
	pending := filepath.Join(opts.dir(), PendingName)
	if err := write(ctx, sys, pending, j); err != nil {
		return err
	}
	rb, err := readJournal(ctx, sys, filepath.Join(opts.dir(), RollbackName))
	switch {
	case err != nil:
		return err
	case rb != nil && rb.ID == j.ID:
		return errors.Join(fmt.Errorf("revert: record %s: claimed while it was written: %w", j.ID, ErrRollingBack), removeIfThere(ctx, sys, pending))
	}
	return nil
}

// Done ends the journal of a RiskOther change whose apply succeeded. A
// lockout-risk change ends with Confirm or Rollback (ErrNotConfirmable).
func Done(ctx context.Context, sys host.System, id string, opts Options) error {
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return err
	case st.Pending == nil || st.Pending.ID != id:
		return fmt.Errorf("revert: done %s: %w", id, ErrNothingPending)
	case st.Pending.Risk == RiskLockout:
		return fmt.Errorf("revert: done %s: a lockout-risk change ends with hardn confirm or a rollback: %w", id, ErrNotConfirmable)
	}
	if err := sys.Remove(ctx, filepath.Join(opts.dir(), PendingName)); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("revert: done %s: a rollback claimed it: %w", id, ErrRollingBack)
	} else if err != nil {
		return fmt.Errorf("revert: %w", err)
	}
	opts.logf("change %s (%s) done", id, st.Pending.Summary)
	return nil
}

// ConfirmResult is what Confirm did.
type ConfirmResult struct {
	Journal Journal
	// Local: confirmed from a session that is not over SSH (the local
	// console exception): it proves nothing about SSH.
	Local bool
	// CancelErr: the timer could not be stopped. Harmless: the rollback it
	// runs finds nothing pending.
	CancelErr error
}

// NotPendingError is ErrNothingPending with what happened to the last
// change, when the timer undid it. Abandoned, since next/p3-rollback-abandon,
// is where Abandon kept the record when that rollback did not finish and
// was given up.
type NotPendingError struct {
	Reverted  *Outcome
	Abandoned string
}

func (e *NotPendingError) Error() string {
	o := e.Reverted
	switch {
	case o == nil:
		return "revert: confirm: " + ErrNothingPending.Error()
	case e.Abandoned != "":
		return fmt.Sprintf("revert: confirm: %s: change %s (%s) was not confirmed within %ds, and %s",
			ErrNothingPending, o.ID, o.Summary, o.TimeoutSeconds, givenUpWords(e.Abandoned))
	case len(o.Errors) > 0:
		return fmt.Sprintf("revert: confirm: %s: change %s (%s) was not confirmed within %ds, and %s",
			ErrNothingPending, o.ID, o.Summary, o.TimeoutSeconds, revertedWords(*o))
	}
	return fmt.Sprintf("revert: confirm: %s: change %s (%s) was rolled back at %s, because it was not confirmed within %ds",
		ErrNothingPending, o.ID, o.Summary, stamp(o.Finished), o.TimeoutSeconds)
}

// Is makes errors.Is(err, ErrNothingPending) true.
func (e *NotPendingError) Is(target error) bool { return target == ErrNothingPending }

// RolledBack reports whether the error says that the timer rolled the last
// change back and finished: once it has been shown, the caller may
// Acknowledge the outcome. A rollback that did not finish and was given up
// is left for the next run's notice, which says so.
func (e *NotPendingError) RolledBack() bool {
	return e.Reverted != nil && e.Abandoned == "" && len(e.Reverted.Errors) == 0
}

// Confirm keeps the pending lockout-risk change, for `hardn confirm` from
// session s (ReadSession). CheckConfirm decides whether s may: the apply
// has finished (MarkApplied), and s is a console or an SSH connection made
// after it, from another machine. Then the journal is claimed (renamed to
// confirmed.json, which keeps the last confirmed change), so a rollback
// that starts now finds nothing, and the timer is stopped. A rollback that
// has started is ErrRollingBack: too late. Nothing pending is a
// *NotPendingError.
func Confirm(ctx context.Context, sys host.System, s Session, opts Options) (ConfirmResult, error) {
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return ConfirmResult{}, err
	case st.RollingBack != nil:
		return ConfirmResult{Journal: *st.RollingBack}, fmt.Errorf("revert: confirm %s: %w", st.RollingBack.ID, ErrRollingBack)
	case st.Pending == nil:
		e := &NotPendingError{Reverted: st.Reverted}
		if o := st.Reverted; o != nil {
			if kept, ok := abandonedRecord(ctx, sys, opts, o.ID); ok {
				e.Abandoned = kept
			}
		}
		return ConfirmResult{}, e
	case st.Pending.Risk != RiskLockout:
		return ConfirmResult{Journal: *st.Pending}, fmt.Errorf("revert: confirm %s: it did not finish; undo it with `sudo hardn rollback %s`: %w", st.Pending.ID, st.Pending.ID, ErrNotConfirmable)
	}
	j := *st.Pending
	local, err := CheckConfirm(j, s)
	switch {
	case errors.Is(err, ErrNotConfirmable):
		return ConfirmResult{Journal: j}, fmt.Errorf("%w; unless its apply finishes and it is confirmed, it is rolled back at %s", err, stamp(j.Deadline()))
	case err != nil:
		return ConfirmResult{Journal: j}, fmt.Errorf("%w; %s", err, confirmHint(j))
	}
	dir := opts.dir()
	if _, err := claim(ctx, sys, filepath.Join(dir, PendingName), filepath.Join(dir, ConfirmedName), j.ID); errors.Is(err, fs.ErrNotExist) {
		return ConfirmResult{Journal: j}, fmt.Errorf("revert: confirm %s: a rollback claimed it first: %w", j.ID, ErrRollingBack)
	} else if err != nil {
		return ConfirmResult{Journal: j}, err
	}
	res := ConfirmResult{Journal: j, Local: local}
	if res.CancelErr = cancelTimer(ctx, sys, *j.Timer); res.CancelErr != nil {
		opts.logf("confirm %s: the timer was not stopped (its rollback will find nothing pending): %v", j.ID, res.CancelErr)
	}
	opts.logf("confirmed change %s (%s), from %s", j.ID, j.Summary, sessionWords(s))
	return res, nil
}

// confirmHint says how to confirm j.
func confirmHint(j Journal) string {
	return fmt.Sprintf("run `sudo hardn confirm` from %s, made from another machine, before %s", confirmWhere(j), stamp(j.Deadline()))
}

// confirmWhere is the connection a confirmation of j must come from.
func confirmWhere(j Journal) string {
	var ports []uint16
	for _, l := range j.ConfirmVia {
		if !slices.Contains(ports, l.Port) {
			ports = append(ports, l.Port)
		}
	}
	if len(ports) == 0 {
		return "a new SSH connection"
	}
	return "a new SSH connection to port " + joinPorts(ports)
}

func joinPorts(ps []uint16) string {
	s := ""
	for i, p := range ps {
		if i > 0 {
			s += " or "
		}
		s += fmt.Sprint(p)
	}
	return s
}

func sessionWords(s Session) string {
	switch {
	case !s.Remote:
		return "a session not over SSH (" + s.Source + ")"
	case s.Conn != "":
		return "SSH connection " + s.Conn
	}
	return "an SSH session"
}
