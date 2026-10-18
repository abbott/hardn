package revert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// StepResult is one inverse step a rollback ran.
type StepResult struct {
	Step   Step
	Result host.Result // OpRun's
	Note   string      // "already gone": nothing left to put back
	Err    error
}

// RollbackResult is what Rollback did.
type RollbackResult struct {
	Journal Journal // the change, as claimed; zero when Nothing
	Reason  Reason
	// Nothing: there was nothing to roll back, which Why explains. Not an
	// error: the timer of a confirmed change ends here.
	Nothing bool
	Why     string
	// Reverted is the timer's outcome when Nothing is because the timer
	// rolled id back: once the caller has said so, it may Acknowledge it.
	Reverted *Outcome
	Resumed  bool // a rollback that had not finished was taken up again
	// Restored is backup.Restore's report; Undone the inverse steps run,
	// up to a failed one; Reloads the reloads run, up to a failed one.
	Restored []backup.Restored
	Undone   []StepResult
	Reloads  []reload.Report
	// Skipped says why the reloads did not run.
	Skipped string
	// CancelErr: the timer could not be stopped (harmless: the rollback it
	// runs finds nothing pending).
	CancelErr error
}

// Rollback undoes change id, for `hardn rollback <id>`: the timer's, the
// user's, or the engine's after a failed step (Options.Reason
// ReasonFailed). It runs under its own deadline (RollbackTimeout), not
// ctx's cancellation: a cancelled context does not stop it half-way. A
// signal is the caller's to hold off (cmd/hardn does).
//
// The change is claimed first, by renaming pending.json to rollback.json,
// so a confirm and a rollback never both act on it. Then, in order:
//
//  1. the backups the journal names are restored (backup.Restore: last
//     taken first, idempotent, past a failed entry);
//  2. the inverse steps run in the journal's order, stopping at the first
//     that fails (ufw's rule: an undo left out never locks anyone out, a
//     later one run out of order may);
//  3. the services are reloaded (an ssh reload is p2-reload's plan with
//     Rollback set), unless a backup was not restored: the files on disk
//     are then a mix no check has passed, which a reload would make sshd
//     act on;
//  4. a timer that has not fired is stopped.
//
// Progress is written to rollback.json after each part, with the process
// running it (Progress.Runner, from /proc/self), so a rollback that stops
// (an error, a crash, the deadline) is taken up again by the next `hardn
// rollback <id>` after what it had done, and until then a new change is
// refused. One that is still running is not taken up by another process:
// that is Nothing, "being rolled back". A rollback by the timer leaves
// reverted.json for the next run's notice. Every decision and step goes to
// Options.Log; commands to the System's run log.
//
// Nothing journalled under id is RollbackResult.Nothing, with no error:
// confirmed, rolled back already, another change, or a rollback under way.
// One that cannot ever finish is given up with Abandon.
func Rollback(ctx context.Context, sys host.System, id string, opts Options) (RollbackResult, error) {
	if !ValidID(id) {
		return RollbackResult{}, fmt.Errorf("revert: rollback %q: %w", id, ErrID)
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), RollbackTimeout)
	defer cancel()
	var res RollbackResult
	st, err := ReadState(ctx, sys, opts)
	if err != nil {
		opts.logf("rollback %s: the journal cannot be read: %v", id, err)
		return res, err
	}
	dir := opts.dir()
	rb := filepath.Join(dir, RollbackName)
	self, serr := selfProc(ctx, sys)
	if serr != nil {
		opts.logf("rollback %s: its own process cannot be named, so another rollback may take it up while it runs: %v", id, serr)
	}
	var j Journal
	switch {
	case st.RollingBack != nil && st.RollingBack.ID == id && st.Running && !st.RollingBack.Rollback.Runner.same(self):
		res.Nothing, res.Why = true, fmt.Sprintf("it is being rolled back by process %d, started %s", st.RollingBack.Rollback.Runner.PID, stamp(st.RollingBack.Rollback.Started))
		opts.logf("rollback %s: nothing to do: %s", id, res.Why)
		return res, nil
	case st.RollingBack != nil && st.RollingBack.ID == id:
		j, res.Resumed = *st.RollingBack, true
	case st.Pending != nil && st.Pending.ID == id:
		j, err = claim(ctx, sys, filepath.Join(dir, PendingName), rb, id)
		if errors.Is(err, fs.ErrNotExist) {
			res.Nothing, res.Why = true, "a confirmation or another rollback claimed it first"
			opts.logf("rollback %s: nothing to do: %s", id, res.Why)
			return res, nil
		} else if err != nil {
			return res, err
		}
	default:
		res.Nothing, res.Why = true, nothingWhy(st, id)
		if st.Reverted != nil && st.Reverted.ID == id {
			res.Reverted = st.Reverted
		}
		opts.logf("rollback %s: nothing to do: %s", id, res.Why)
		return res, nil
	}
	var errs []error
	save := func() {
		if err := write(ctx, sys, rb, j); err != nil {
			errs = append(errs, err)
		}
	}
	if j.Rollback == nil {
		j.Rollback = &Progress{Started: opts.now().UTC(), Reason: reasonFor(j, opts)}
	}
	j.Rollback.Runner = self
	save()
	res.Journal, res.Reason = j, j.Rollback.Reason
	resumed := ""
	if res.Resumed {
		resumed = ", resumed"
	}
	opts.logf("rollback %s (%s): %s%s", id, j.Summary, reasonWords(j), resumed)

	restored := true
	if j.Backup != nil && !j.Rollback.Restored {
		rs, err := restoreBackups(ctx, sys, *j.Backup)
		res.Restored = rs
		for _, r := range rs {
			opts.logf("rollback %s: restore %s: %s%s", id, r.Path, r.Action, errWords(r.Err))
		}
		if err != nil {
			restored = false
			errs = append(errs, err)
			opts.logf("rollback %s: backups not all restored: %v", id, err)
		} else {
			j.Rollback.Restored = true
			save()
		}
	}
	for j.Rollback.UndoDone < len(j.Undo) {
		n := j.Rollback.UndoDone
		r := runStep(ctx, sys, j.Undo[n], opts.Plans)
		res.Undone = append(res.Undone, r)
		opts.logf("rollback %s: undo %d of %d: %s%s%s", id, n+1, len(j.Undo), stepWords(r.Step), noteWords(r.Note), errWords(r.Err))
		if r.Err != nil {
			errs = append(errs, fmt.Errorf("undo step %d: %w", n+1, r.Err))
			break
		}
		j.Rollback.UndoDone++
		save()
	}
	if !restored && j.Rollback.ReloadDone < len(j.Reload) {
		res.Skipped = "a backup was not restored, so the files on disk are a mix no check has passed"
		opts.logf("rollback %s: reloads not run: %s", id, res.Skipped)
	}
	for restored && j.Rollback.ReloadDone < len(j.Reload) {
		r := j.Reload[j.Rollback.ReloadDone]
		rep, err := runReload(ctx, sys, r, opts)
		res.Reloads = append(res.Reloads, rep)
		opts.logf("rollback %s: reload %s (%v)%s", id, r.Service, r.Keywords, errWords(err))
		if err != nil {
			errs = append(errs, fmt.Errorf("reload %s: %w", r.Service, err))
			break
		}
		j.Rollback.ReloadDone++
		save()
	}
	if t := j.Timer; t != nil {
		if j.Rollback.Reason != ReasonTimeout {
			res.CancelErr = cancelTimer(ctx, sys, *t)
		} else if t.Kind == TimerSetsid {
			res.CancelErr = removeIfThere(ctx, sys, t.PIDFile) // the sleeper ran this rollback
		}
		if res.CancelErr != nil {
			opts.logf("rollback %s: the timer was not stopped (its rollback will find nothing pending): %v", id, res.CancelErr)
		}
	}
	if j.Rollback.Reason == ReasonTimeout {
		o := Outcome{SchemaVersion: SchemaVersion, ID: j.ID, Summary: j.Summary, Created: j.Created,
			TimeoutSeconds: j.TimeoutSeconds, Reason: ReasonTimeout, Finished: opts.now().UTC()}
		for _, e := range errs {
			o.Errors = append(o.Errors, e.Error())
		}
		if err := write(ctx, sys, filepath.Join(dir, RevertedName), o); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		err := fmt.Errorf("revert: rollback %s: %w; run `sudo hardn rollback %s` to take it up again (if it cannot finish, fix what this names, or give it up, leaving the host as it is, by moving %s aside): %w",
			id, ErrIncomplete, id, rb, errors.Join(errs...))
		opts.logf("rollback %s: did not finish: %v", id, errors.Join(errs...))
		return res, err
	}
	if err := sys.Remove(ctx, rb); err != nil {
		return res, fmt.Errorf("revert: rollback %s: done, but %w", id, err)
	}
	opts.logf("rollback %s: done", id)
	return res, nil
}

// reasonFor is Options.Reason, or else timeout once a lockout-risk change's
// deadline has passed, requested before it and for any other change.
func reasonFor(j Journal, opts Options) Reason {
	switch {
	case opts.Reason != "":
		return opts.Reason
	case j.Risk == RiskLockout && !opts.now().Before(j.Deadline()):
		return ReasonTimeout
	}
	return ReasonRequested
}

func nothingWhy(st State, id string) string {
	switch {
	case st.Pending != nil:
		return fmt.Sprintf("the journalled change is %s, not this one", st.Pending.ID)
	case st.RollingBack != nil:
		return fmt.Sprintf("the rollback of change %s, not this one, has not finished", st.RollingBack.ID)
	case st.Reverted != nil && st.Reverted.ID == id:
		return "it was rolled back at " + stamp(st.Reverted.Finished)
	}
	return "it is not journalled: confirmed, or rolled back already"
}

// restoreBackups restores the entries of b, from its run directory.
func restoreBackups(ctx context.Context, sys host.System, b Backups) ([]backup.Restored, error) {
	m, err := backup.Load(ctx, sys, b.Dir)
	if err != nil {
		return nil, err
	}
	var keep []backup.Entry
	for _, seq := range b.Seqs {
		i := -1
		for k, e := range m.Entries {
			if e.Seq == seq {
				i = k
			}
		}
		if i < 0 {
			return nil, fmt.Errorf("revert: backup %d is not in %s: %w", seq, b.Dir, ErrJournal)
		}
		keep = append(keep, m.Entries[i])
	}
	m.Entries = keep
	return backup.Restore(ctx, sys, m)
}

// runStep runs one inverse step; an OpPlan step through plans. A path that
// is gone has nothing left to put back: a Note, not an error.
func runStep(ctx context.Context, sys host.System, s Step, plans map[string]PlanRollback) StepResult {
	r := StepResult{Step: s}
	switch s.Op {
	case OpRun:
		r.Result, r.Err = sys.Run(ctx, s.Argv[0], s.Argv[1:]...)
		return r
	case OpPlan:
		if fn := plans[s.Unit]; fn == nil {
			r.Err = fmt.Errorf("revert: unit %q: %w", s.Unit, ErrNoPlanRollback)
		} else {
			r.Err = fn(ctx, sys, s.Plan)
		}
		return r
	case OpChmod:
		r.Err = sys.Chmod(ctx, s.Path, s.Mode.FileMode())
	case OpChown:
		r.Err = sys.Chown(ctx, s.Path, s.Owner.UID, s.Owner.GID)
	case OpRemoveDir:
		fi, err := sys.Lstat(ctx, s.Path)
		switch {
		case err != nil:
			r.Err = err
		case !fi.IsDir():
			r.Err = fmt.Errorf("revert: %s: not a directory now, so not the one the change created", s.Path)
		default:
			r.Err = sys.Remove(ctx, s.Path)
		}
	}
	if errors.Is(r.Err, fs.ErrNotExist) {
		r.Err, r.Note = nil, "already gone"
	}
	return r
}

// runReload reloads r.Service once its files are restored. With IfUp, a
// unit that is down is left so (no Step.Start): the forward reload may not
// have run, and the unit may have been down before the change. The socket
// drop-ins r names as restored are expected on a socket host
// (reload.Change.RestoredDropIns); any other with ListenStream is refused.
func runReload(ctx context.Context, sys host.System, r Reload, opts Options) (reload.Report, error) {
	detectSSH := opts.DetectSSH
	if detectSSH == nil {
		detectSSH = func(ctx context.Context, sys host.System) detect.SSH {
			return detect.DetectSSH(ctx, sys, detect.DetectInit(ctx, sys))
		}
	}
	ssh := detectSSH(ctx, sys)
	var drops reload.SocketDropIns
	if ssh.Mode == detect.SSHSocket && movesListeners(r.Keywords) {
		var err error
		if drops, err = reload.ReadSocketDropIns(ctx, sys); err != nil {
			return reload.Report{}, err
		}
	}
	after := make([]sshd.Listener, len(r.After))
	for i, l := range r.After {
		after[i] = l.sshd()
	}
	p, err := reload.PlanFor(reload.Input{SSH: ssh, DropIns: drops,
		Change: reload.Change{Keywords: r.Keywords, After: after, Rollback: true, RestoredDropIns: r.SocketDropIns}})
	if err != nil {
		return reload.Report{}, err
	}
	if r.IfUp {
		for i := range p.Steps {
			p.Steps[i].Start = nil
		}
	}
	return reload.Execute(ctx, sys, p, opts.Reload)
}

// AbandonedName is where Abandon moves an unfinished rollback of change
// <id>: abandoned-<id>.json, kept for the record.
func AbandonedName(id string) string { return "abandoned-" + id + ".json" }

// Abandon gives up the unfinished rollback of change id, one that cannot
// ever finish (its backup run directory is gone, an undo command no longer
// exists): rollback.json is renamed to AbandonedName(id), so new changes
// can be applied again. The host stays as the rollback left it; what it
// did not do is in the log and in the file. A rollback still running is
// not abandoned (ErrRollingBack), nor anything but an unfinished rollback
// of id (ErrNothingPending). No command line reaches it yet (P-35).
func Abandon(ctx context.Context, sys host.System, id string, opts Options) (Journal, error) {
	if !ValidID(id) {
		return Journal{}, fmt.Errorf("revert: abandon %q: %w", id, ErrID)
	}
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return Journal{}, err
	case st.RollingBack == nil || st.RollingBack.ID != id:
		return Journal{}, fmt.Errorf("revert: abandon %s: no unfinished rollback of it: %w", id, ErrNothingPending)
	case st.Running:
		return *st.RollingBack, fmt.Errorf("revert: abandon %s: process %d is rolling it back: %w", id, st.RollingBack.Rollback.Runner.PID, ErrRollingBack)
	}
	j, err := claim(ctx, sys, filepath.Join(opts.dir(), RollbackName), filepath.Join(opts.dir(), AbandonedName(id)), id)
	if err != nil {
		return Journal{}, err
	}
	p := Progress{}
	if j.Rollback != nil {
		p = *j.Rollback
	}
	opts.logf("rollback %s (%s) abandoned: backups restored %v, %d of %d undo steps and %d of %d reloads done; kept in %s",
		id, j.Summary, p.Restored || j.Backup == nil, p.UndoDone, len(j.Undo), p.ReloadDone, len(j.Reload), AbandonedName(id))
	return j, nil
}

func removeIfThere(ctx context.Context, sys host.System, name string) error {
	if err := sys.Remove(ctx, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
