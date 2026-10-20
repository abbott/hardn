package revert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

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
	// rolled id back, and finished: once the caller has said so, it may
	// Acknowledge it. It is nil when that rollback did not finish and was
	// given up, so the next run's notice still says so.
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
// confirmed, rolled back already, another change, a rollback under way, or
// one given up. One that cannot ever finish is given up with Abandon.
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
		kept, abandoned := abandonedRecord(ctx, sys, opts, id)
		res.Nothing, res.Why = true, nothingWhy(st, id, kept, abandoned)
		if o := st.Reverted; o != nil && o.ID == id && !abandoned && len(o.Errors) == 0 {
			res.Reverted = o
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
		rep, err := runReload(ctx, sys, r, j.Backup, opts)
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
		err := fmt.Errorf("revert: rollback %s: %w; run `sudo hardn rollback %s` to take it up again (if it cannot finish, fix what this names, or give it up, leaving the host as it is, with `sudo hardn rollback --abandon %s`): %w",
			id, ErrIncomplete, id, id, errors.Join(errs...))
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

// nothingWhy says why st holds nothing to roll back for change id; kept is
// where an abandoned rollback of id is kept, when abandoned.
func nothingWhy(st State, id, kept string, abandoned bool) string {
	switch {
	case st.Pending != nil:
		return fmt.Sprintf("the journalled change is %s, not this one", st.Pending.ID)
	case st.RollingBack != nil:
		return fmt.Sprintf("the rollback of change %s, not this one, has not finished", st.RollingBack.ID)
	case abandoned:
		return givenUpWords(kept)
	case st.Reverted != nil && st.Reverted.ID == id:
		return revertedWords(*st.Reverted)
	}
	return "it is not journalled: confirmed, or rolled back already"
}

// abandonedRecord is the full path of the record Abandon keeps for change
// id, and whether it is there: the rollback of id was given up.
func abandonedRecord(ctx context.Context, sys host.System, opts Options, id string) (string, bool) {
	kept := filepath.Join(opts.dir(), AbandonedName(id))
	_, err := sys.Lstat(ctx, kept)
	return kept, err == nil
}

// givenUpWords says that a change's rollback was given up by Abandon, which
// keeps its record in kept.
func givenUpWords(kept string) string {
	return "its rollback did not finish and was given up (`hardn rollback --abandon`), leaving the host as that rollback left it; its record is kept in " + kept
}

// revertedWords says what the timer's rollback of a change did, by its
// outcome o: rolled it back, or did not finish. A rollback that did not
// finish leaves rollback.json, so when that is gone and Abandon kept no
// record, it was given up by hand.
func revertedWords(o Outcome) string {
	if len(o.Errors) == 0 {
		return "it was rolled back at " + stamp(o.Finished)
	}
	return fmt.Sprintf("the timer's rollback of it did not finish (%s) and was given up, leaving the host as that rollback left it", strings.Join(o.Errors, "; "))
}

// restoreBackups restores the entries of b, from its run directory.
func restoreBackups(ctx context.Context, sys host.System, b Backups) ([]backup.Restored, error) {
	m, err := journalled(ctx, sys, b)
	if err != nil {
		return nil, err
	}
	return backup.Restore(ctx, sys, m)
}

// journalled is b's manifest with only the entries b names, in b's order.
func journalled(ctx context.Context, sys host.System, b Backups) (backup.Manifest, error) {
	m, err := backup.Load(ctx, sys, b.Dir)
	if err != nil {
		return backup.Manifest{}, err
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
			return backup.Manifest{}, fmt.Errorf("revert: backup %d is not in %s: %w", seq, b.Dir, ErrJournal)
		}
		keep = append(keep, m.Entries[i])
	}
	m.Entries = keep
	return m, nil
}

// restoredDropIns refuses (ErrJournal) a reload whose SocketDropIns are not
// all paths of b's journalled entries of a file (backup.KindFile), which
// the restore put back: only those are exempt from reload.ErrSocketDropIn,
// so that a damaged or edited entry cannot exempt another unit's drop-in,
// or an administrator's.
func restoredDropIns(ctx context.Context, sys host.System, r Reload, b *Backups) error {
	if len(r.SocketDropIns) == 0 {
		return nil
	}
	if b == nil {
		return fmt.Errorf("revert: socketDropIns %v with no backups: %w", r.SocketDropIns, ErrJournal)
	}
	m, err := journalled(ctx, sys, *b)
	if err != nil {
		return err
	}
	for _, d := range r.SocketDropIns {
		if !slices.ContainsFunc(m.Entries, func(e backup.Entry) bool { return e.Path == d && e.Kind == backup.KindFile }) {
			return fmt.Errorf("revert: socketDropIns %s is not a file the journalled backups restore: %w", d, ErrJournal)
		}
	}
	return nil
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

// runReload reloads r.Service once its files are restored. With IfUp, or
// Options.NoStart, a unit that is down is left so (no Step.Start): the
// forward reload may not have run, and the unit may have been down before
// the change; at boot, it has not started yet. The socket
// drop-ins r names as restored are expected on a socket host
// (reload.Change.RestoredDropIns) when b's backups restored them as files
// (restoredDropIns); any other with ListenStream is refused.
func runReload(ctx context.Context, sys host.System, r Reload, b *Backups, opts Options) (reload.Report, error) {
	if err := restoredDropIns(ctx, sys, r, b); err != nil {
		return reload.Report{}, err
	}
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
	if r.IfUp || opts.NoStart {
		for i := range p.Steps {
			p.Steps[i].Start = nil
		}
	}
	return reload.Execute(ctx, sys, p, opts.Reload)
}

// AbandonedName is where Abandon moves an unfinished rollback of change
// <id>: abandoned-<id>.json, kept for the record.
func AbandonedName(id string) string { return "abandoned-" + id + ".json" }

// AbandonResult is what Abandon gave up.
type AbandonResult struct {
	// Journal is the record as it was given up: the change and the
	// rollback's progress (Journal.Rollback, nil when none was recorded),
	// which Unfinished lists.
	Journal Journal
	// Kept is the record's full path: AbandonedName(id) in Options.Dir.
	Kept string
	// RunnerUnknown: the record names no process (no progress recorded, or
	// the rollback could not name its own), so whether a `hardn rollback
	// <id>` still runs it could not be told; it counted as gone.
	RunnerUnknown bool
	// SSHUnchecked: the backups were not all restored and an ssh reload was
	// left, so sshd's files on disk may be a mix of the change's and the
	// ones before it that no check has passed, which is why the rollback
	// skipped the reloads; sshd reads them the next time it starts.
	SSHUnchecked bool
}

// Abandon gives up the unfinished rollback of change id, one that cannot
// ever finish (its backup run directory is gone, an undo command no longer
// exists): rollback.json is renamed to AbandonedName(id), so new changes
// can be applied again. The host stays as the rollback left it; what it
// did and did not do is in the log, in the record and in Unfinished of the
// journal returned. A rollback still running is not abandoned
// (ErrRollingBack), nor one a `hardn rollback <id>` took up between the
// read and the rename (its record names a process that runs: it is put
// back), nor anything but an unfinished rollback of id (a
// *NoRollbackError, Is(ErrNothingPending)); each error says what to do
// instead. A process that cannot be named (AbandonResult.RunnerUnknown)
// counts as gone. `hardn rollback --abandon <id>` reaches it (P-35).
func Abandon(ctx context.Context, sys host.System, id string, opts Options) (AbandonResult, error) {
	if !ValidID(id) {
		return AbandonResult{}, fmt.Errorf("revert: abandon %q: %w", id, ErrID)
	}
	rb, kept := filepath.Join(opts.dir(), RollbackName), filepath.Join(opts.dir(), AbandonedName(id))
	st, err := ReadState(ctx, sys, opts)
	if err != nil {
		return AbandonResult{}, err
	}
	if err := abandonable(ctx, sys, st, id, kept); err != nil {
		return AbandonResult{}, err
	}
	j, err := claim(ctx, sys, rb, kept, id)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// Another hardn took it first: a second --abandon, or a rollback
		// that finished. Say what the journal holds now.
		if st, err = ReadState(ctx, sys, opts); err != nil {
			return AbandonResult{}, err
		}
		if err := abandonable(ctx, sys, st, id, kept); err != nil {
			return AbandonResult{}, err
		}
		return AbandonResult{}, fmt.Errorf("revert: abandon %s: %s changed while it was being given up; run `sudo hardn rollback --abandon %s` again: %w", id, rb, id, ErrClaim)
	case err != nil:
		return AbandonResult{}, err
	}
	if p := j.Rollback; p != nil && alive(ctx, sys, p.Runner) {
		// A `hardn rollback <id>` took it up after the read, and saved
		// before the rename: it runs on, so its record goes back.
		return AbandonResult{}, errors.Join(rollingBackError(id, *p, "took it up while it was being given up, and is rolling it back now; its record is put back"), sys.Rename(ctx, kept, rb))
	}
	res := AbandonResult{Journal: j, Kept: kept, RunnerUnknown: j.Rollback == nil || !j.Rollback.Runner.known(), SSHUnchecked: sshUnchecked(j)}
	p := Progress{}
	if j.Rollback != nil {
		p = *j.Rollback
	}
	opts.logf("rollback %s (%s) abandoned: backups restored %v, %d of %d undo steps and %d of %d reloads done; kept in %s",
		id, j.Summary, p.Restored || j.Backup == nil, p.UndoDone, len(j.Undo), p.ReloadDone, len(j.Reload), kept)
	return res, nil
}

// rollingBackError refuses to give up the rollback of change id while
// process p.Runner runs it; how says what the process is doing.
func rollingBackError(id string, p Progress, how string) error {
	return fmt.Errorf("revert: abandon %s: %w: process %d %s (started %s); wait for it to end, and if it does not finish, run `sudo hardn rollback %s` once more before giving it up",
		id, ErrRollingBack, p.Runner.PID, how, stamp(p.Started), id)
}

// sshUnchecked is AbandonResult.SSHUnchecked for the record j.
func sshUnchecked(j Journal) bool {
	if j.Backup == nil || j.Rollback == nil || j.Rollback.Restored {
		return false
	}
	for i, r := range j.Reload {
		if i >= j.Rollback.ReloadDone && r.Service == ReloadSSH {
			return true
		}
	}
	return false
}

// NoRollbackKind is why Abandon found no unfinished rollback of a change to
// give up (NoRollbackError.Kind).
type NoRollbackKind string

const (
	NoRollbackOtherRunning    NoRollbackKind = "other-running"    // another change's rollback runs now: wait for it
	NoRollbackOtherUnfinished NoRollbackKind = "other-unfinished" // another change's rollback has not finished
	NoRollbackNotStarted      NoRollbackKind = "not-started"      // the change is journalled; its rollback has not started
	NoRollbackOtherPending    NoRollbackKind = "other-pending"    // another change is journalled
	NoRollbackGivenUp         NoRollbackKind = "given-up"         // its rollback did not finish and was given up already
	NoRollbackRolledBack      NoRollbackKind = "rolled-back"      // the timer rolled it back
	NoRollbackNotJournalled   NoRollbackKind = "not-journalled"   // confirmed, rolled back, or not a change made here
)

// NoRollbackError is Abandon's refusal when there is no unfinished rollback
// of change ID to give up: Is(ErrNothingPending). Kind says why; Other is
// the change the journal holds instead (the other-* kinds); Kept is where
// Abandon kept the record of a rollback given up (given-up; empty when it
// was given up by hand). Its text says what to do instead, and leaves out
// ErrNothingPending's own words, which are about confirming.
type NoRollbackError struct {
	ID    string
	Kind  NoRollbackKind
	Other string
	Kept  string
	why   string
}

func (e *NoRollbackError) Error() string {
	return fmt.Sprintf("revert: abandon %s: there is no unfinished rollback of it to give up: %s", e.ID, e.why)
}

// Is makes errors.Is(err, ErrNothingPending) true.
func (e *NoRollbackError) Is(target error) bool { return target == ErrNothingPending }

// abandonable is nil when st holds an unfinished rollback of change id
// whose process has gone; otherwise it is the refusal, which says what to
// do instead. kept is where an abandoned rollback of id is kept.
func abandonable(ctx context.Context, sys host.System, st State, id, kept string) error {
	refuse := func(kind NoRollbackKind, other, why string) error {
		return &NoRollbackError{ID: id, Kind: kind, Other: other, why: why}
	}
	if j := st.RollingBack; j != nil {
		o := j.ID
		switch {
		case o == id && st.Running:
			return rollingBackError(id, *j.Rollback, "is rolling it back now")
		case o == id:
			return nil
		case st.Running:
			return refuse(NoRollbackOtherRunning, o, fmt.Sprintf("change %s, not this one, is being rolled back now by process %d (started %s); wait for that rollback to end",
				o, j.Rollback.Runner.PID, stamp(j.Rollback.Started)))
		}
		return refuse(NoRollbackOtherUnfinished, o, fmt.Sprintf("the unfinished rollback is of change %s, not this one; finish it with `sudo hardn rollback %s`, or, if it cannot finish, give it up with `sudo hardn rollback --abandon %s`", o, o, o))
	}
	switch p := st.Pending; {
	case p != nil && p.ID == id:
		return refuse(NoRollbackNotStarted, "", fmt.Sprintf("change %s is journalled and its rollback has not started; to undo it, run `sudo hardn rollback %s`", id, id))
	case p != nil:
		return refuse(NoRollbackOtherPending, p.ID, fmt.Sprintf("the journalled change is %s, not this one, and its rollback has not started", p.ID))
	}
	if _, err := sys.Lstat(ctx, kept); err == nil {
		return &NoRollbackError{ID: id, Kind: NoRollbackGivenUp, Kept: kept, why: "its rollback was abandoned already, and its record is kept in " + kept}
	}
	if o := st.Reverted; o != nil && o.ID == id {
		if len(o.Errors) > 0 {
			return refuse(NoRollbackGivenUp, "", revertedWords(*o))
		}
		return refuse(NoRollbackRolledBack, "", revertedWords(*o))
	}
	return refuse(NoRollbackNotJournalled, "", "it is not journalled: confirmed, rolled back already, or not a change made on this host")
}

// Unfinished is what the unfinished rollback of j had done by its record
// (j.Rollback), and what it had left, one plain-text line per part, in the
// order a rollback runs them: the backups restored, each inverse step, each
// reload. It is for `hardn rollback --abandon <id>`, which gives such a
// rollback up and leaves the host as it is. The record is written after
// each part, so a part left may have run in part (the inverse step that
// failed; a restore goes on past an entry that fails); with no progress
// recorded (j.Rollback nil), every part is left. Pure.
func Unfinished(j Journal) (done, left []string) {
	p := Progress{}
	if j.Rollback != nil {
		p = *j.Rollback
	}
	add := func(ok bool, line string) {
		if ok {
			done = append(done, line)
		} else {
			left = append(left, line)
		}
	}
	if b := j.Backup; b != nil {
		seqs := make([]string, len(b.Seqs))
		for i, s := range b.Seqs {
			seqs[i] = strconv.Itoa(s)
		}
		entries := "entries"
		if len(seqs) == 1 {
			entries = "entry"
		}
		add(p.Restored, fmt.Sprintf("restore the backups in %s (%s %s)", b.Dir, entries, strings.Join(seqs, ", ")))
	}
	for i, s := range j.Undo {
		add(i < p.UndoDone, fmt.Sprintf("undo %d of %d: %s", i+1, len(j.Undo), stepWords(s)))
	}
	for i, r := range j.Reload {
		line := "reload " + string(r.Service)
		if len(r.Keywords) > 0 {
			line += " (" + strings.Join(r.Keywords, ", ") + ")"
		}
		add(i < p.ReloadDone, line)
	}
	return done, left
}

func removeIfThere(ctx context.Context, sys host.System, name string) error {
	if err := sys.Remove(ctx, name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
