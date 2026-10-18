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
// detect's init facts and Options.Hardn (CheckExecutable). It refuses, and
// writes nothing but the journal directory, while a change is journalled
// (*PendingError), and for a journal Check refuses. Then it writes pending.json and starts the timer;
// when the timer does not start, it removes pending.json again and returns
// the error: the change must not be applied. The engine adds the backups,
// inverse steps and reloads with Record as its steps complete.
func Arm(ctx context.Context, sys host.System, j Journal, in detect.Init, opts Options) (Journal, error) {
	j.SchemaVersion = SchemaVersion
	j.Created = opts.now().UTC().Truncate(time.Second)
	j.Timer, j.Rollback = nil, nil
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
	if j.Timer != nil {
		if err := CheckExecutable(ctx, sys, opts.Hardn); err != nil {
			return j, err
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
		return j, errors.Join(fmt.Errorf("revert: arm %s: the timer did not start: %w", j.ID, err), sys.Remove(ctx, pending))
	}
	opts.logf("armed change %s (%s): %s rolls it back at %s unless confirmed", j.ID, j.Summary, j.Timer.Kind, stamp(j.Deadline()))
	return j, nil
}

// Record rewrites the journal of change j as its steps complete: the
// backups taken, the inverse steps, the reloads. What Arm set (Created, the
// timeout, the risk, the timer) is kept as written, and j must still be the
// pending change: when a rollback has claimed it, Record returns
// ErrRollingBack and the engine must stop.
func Record(ctx context.Context, sys host.System, j Journal, opts Options) error {
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return err
	case st.RollingBack != nil && st.RollingBack.ID == j.ID:
		return fmt.Errorf("revert: record %s: %w", j.ID, ErrRollingBack)
	case st.Pending == nil || st.Pending.ID != j.ID:
		return fmt.Errorf("revert: record %s: not the pending change: %w", j.ID, ErrNothingPending)
	}
	p := st.Pending
	j.SchemaVersion, j.Created, j.TimeoutSeconds, j.Risk, j.Timer, j.Rollback = p.SchemaVersion, p.Created, p.TimeoutSeconds, p.Risk, p.Timer, nil
	if err := j.Check(); err != nil {
		return err
	}
	return write(ctx, sys, filepath.Join(opts.dir(), PendingName), j)
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
// change, when the timer undid it.
type NotPendingError struct{ Reverted *Outcome }

func (e *NotPendingError) Error() string {
	if o := e.Reverted; o != nil {
		return fmt.Sprintf("revert: confirm: %s: change %s (%s) was rolled back at %s, because it was not confirmed within %ds",
			ErrNothingPending, o.ID, o.Summary, stamp(o.Finished), o.TimeoutSeconds)
	}
	return "revert: confirm: " + ErrNothingPending.Error()
}

// Is makes errors.Is(err, ErrNothingPending) true.
func (e *NotPendingError) Is(target error) bool { return target == ErrNothingPending }

// Confirm keeps the pending lockout-risk change, for `hardn confirm` from
// session s (ReadSession). CheckConfirm decides whether s may; then the
// journal is claimed (renamed to confirmed.json, which keeps the last
// confirmed change), so a rollback that starts now finds nothing, and the
// timer is stopped. A rollback that has started is ErrRollingBack: too late.
// Nothing pending is a *NotPendingError.
func Confirm(ctx context.Context, sys host.System, s Session, opts Options) (ConfirmResult, error) {
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return ConfirmResult{}, err
	case st.RollingBack != nil:
		return ConfirmResult{Journal: *st.RollingBack}, fmt.Errorf("revert: confirm %s: %w", st.RollingBack.ID, ErrRollingBack)
	case st.Pending == nil:
		return ConfirmResult{}, &NotPendingError{Reverted: st.Reverted}
	case st.Pending.Risk != RiskLockout:
		return ConfirmResult{Journal: *st.Pending}, fmt.Errorf("revert: confirm %s: it did not finish; undo it with `sudo hardn rollback %s`: %w", st.Pending.ID, st.Pending.ID, ErrNotConfirmable)
	}
	j := *st.Pending
	local, err := CheckConfirm(j, s)
	if err != nil {
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
	return fmt.Sprintf("run `sudo hardn confirm` from %s before %s", confirmWhere(j), stamp(j.Deadline()))
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
