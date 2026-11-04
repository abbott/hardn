package revert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// State is what the journal directory holds.
type State struct {
	Pending     *Journal // pending.json: the journalled change
	RollingBack *Journal // rollback.json: a rollback that started and has not finished
	// Running: the process running RollingBack's rollback (its Runner)
	// still runs, so it is under way, not stopped.
	Running  bool
	Reverted *Outcome // reverted.json: the timer undid a change, and no run has said so yet
	// OutcomeErr is why reverted.json could not be read; Reverted is then
	// nil. It is only a notice, so it fails nothing else.
	OutcomeErr error
	// Elevate is the command that runs hardn as root on this host
	// (detect.Elevation.Command), for the lines that say what to run: set by
	// ReadState when a change is journalled. Empty reads as sudo; NoElevation
	// where neither sudo nor doas is installed (Proxmox VE as installed), so
	// root runs hardn itself.
	Elevate string
}

// NoElevation is State.Elevate where neither sudo nor doas is installed:
// the lines then say "hardn", as root, whose own session it is, runs it.
const NoElevation = "none"

// Elevate is the State.Elevate of a host with elevation tools e:
// NoElevation where neither sudo nor doas is found (not where either is
// unknown), else e.Command().
func Elevate(e detect.Elevation) string {
	if e.Sudo.Present == detect.No && e.Doas.Present == detect.No {
		return NoElevation
	}
	return e.Command()
}

// Hardn is how the lines say to run hardn as root on a host with
// elevation tools e: "sudo hardn", "doas hardn", or "hardn" where neither
// is installed (Proxmox VE as installed: root's own session, and `sudo
// hardn confirm` would fail there).
func Hardn(e detect.Elevation) string { return State{Elevate: Elevate(e)}.hardn() }

// Busy reports whether a change is journalled: a new one is then refused.
func (s State) Busy() bool { return s.Pending != nil || s.RollingBack != nil }

// hardn is how the lines of s say to run hardn as root: "doas hardn" on a
// doas host, "hardn" on a host with neither sudo nor doas.
func (s State) hardn() string {
	switch s.Elevate {
	case "":
		return "sudo hardn"
	case NoElevation:
		return "hardn"
	}
	return s.Elevate + " hardn"
}

// PendingError refuses a new change while one is journalled (one at a
// time, brief 2.4). Is(ErrPending). Its text says how to go on.
type PendingError struct{ State State }

func (e *PendingError) Error() string {
	hardn := e.State.hardn()
	if j := e.State.RollingBack; j != nil {
		if e.State.Running {
			return fmt.Sprintf("revert: %s: change %s (%s) is being rolled back (process %d); try again once it has finished", ErrPending, j.ID, j.Summary, j.Rollback.Runner.PID)
		}
		return fmt.Sprintf("revert: %s: the rollback of change %s (%s) has not finished; run `%s rollback %s` to finish it", ErrPending, j.ID, j.Summary, hardn, j.ID)
	}
	j := e.State.Pending
	switch {
	case j.Risk != RiskLockout:
		return fmt.Sprintf("revert: %s: change %s (%s) did not finish; undo it with `%s rollback %s`", ErrPending, j.ID, j.Summary, hardn, j.ID)
	case j.Applied == nil:
		return fmt.Sprintf("revert: %s: change %s (%s) is being applied, or its apply stopped; unless it finishes and is confirmed, it is rolled back at %s; undo it now with `%s rollback %s`",
			ErrPending, j.ID, j.Summary, stamp(j.Deadline()), hardn, j.ID)
	}
	return fmt.Sprintf("revert: %s: change %s (%s) is waiting for confirmation until %s; confirm it with `%s confirm` from %s, or undo it with `%s rollback %s`",
		ErrPending, j.ID, j.Summary, stamp(j.Deadline()), hardn, confirmWhere(*j), hardn, j.ID)
}

// Is makes errors.Is(err, ErrPending) true.
func (e *PendingError) Is(target error) bool { return target == ErrPending }

// Test seams: directories above trustedTop are not checked, and trustedUID
// owns the state besides root (-1: none). Production checks every
// directory from "/" and trusts root alone.
var (
	trustedTop = "/"
	trustedUID = -1
)

// aboveTop reports whether p is above trustedTop, so not checked.
func aboveTop(p string) bool {
	return trustedTop != "/" && p != trustedTop && (p == "/" || strings.HasPrefix(trustedTop, p+"/"))
}

func trusted(fi fs.FileInfo) bool {
	uid, _, ok := host.Owner(fi)
	return ok && (uid == 0 || uid == trustedUID) && fi.Mode().Perm()&0o022 == 0
}

// stateDir checks that dir, and every directory above it, is a directory,
// not a symlink, owned by root and writable by neither group nor others:
// the journal holds commands a rollback runs as root. With create, a
// missing dir is created first, 0700. A missing dir is fs.ErrNotExist.
func stateDir(ctx context.Context, sys host.System, dir string, create bool) error {
	if !clean(dir) {
		return fmt.Errorf("revert: state directory %q: %w", dir, fs.ErrInvalid)
	}
	if create {
		if err := sys.MkdirAll(ctx, dir, 0o700); err != nil {
			return fmt.Errorf("revert: %w", err)
		}
	}
	for p := dir; ; p = filepath.Dir(p) {
		if aboveTop(p) {
			return nil
		}
		fi, err := sys.Lstat(ctx, p)
		switch {
		case err != nil:
			return fmt.Errorf("revert: %w", err)
		case !fi.IsDir():
			return fmt.Errorf("revert: %s: not a directory: %w", p, ErrUnsafe)
		case !trusted(fi):
			return fmt.Errorf("revert: %s: %w", p, ErrUnsafe)
		}
		if p == "/" {
			return nil
		}
	}
}

// readState reads one file of the state directory: nil data when absent.
func readState(ctx context.Context, sys host.System, name string) ([]byte, error) {
	data, fi, err := sys.ReadFileNoFollow(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("revert: %w", err)
	case !trusted(fi):
		return nil, fmt.Errorf("revert: %s: %w", name, ErrUnsafe)
	}
	return data, nil
}

func readJournal(ctx context.Context, sys host.System, name string) (*Journal, error) {
	data, err := readState(ctx, sys, name)
	if data == nil || err != nil {
		return nil, err
	}
	j, err := decodeJournal(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return &j, nil
}

// ReadState reads the journal directory (Options.Dir), refusing it, and
// pending.json or rollback.json, when another user can change it
// (ErrUnsafe), and a journal it cannot act on (ErrJournal). reverted.json is
// only a notice: what keeps it from being read is State.OutcomeErr, never
// an error, so a bad notice never stops a rollback or a confirmation. No
// directory is an empty State.
func ReadState(ctx context.Context, sys host.System, opts Options) (State, error) {
	dir := opts.dir()
	if err := stateDir(ctx, sys, dir, false); errors.Is(err, fs.ErrNotExist) {
		return State{}, nil
	} else if err != nil {
		return State{}, err
	}
	var st State
	var err error
	if st.Pending, err = readJournal(ctx, sys, filepath.Join(dir, PendingName)); err != nil {
		return State{}, err
	}
	if st.RollingBack, err = readJournal(ctx, sys, filepath.Join(dir, RollbackName)); err != nil {
		return State{}, err
	}
	if j := st.RollingBack; j != nil && j.Rollback != nil {
		st.Running = alive(ctx, sys, j.Rollback.Runner)
	}
	data, err := readState(ctx, sys, filepath.Join(dir, RevertedName))
	if err == nil && data != nil {
		var o Outcome
		if o, err = decodeOutcome(data); err == nil {
			st.Reverted = &o
		}
	}
	if err != nil {
		st.OutcomeErr = fmt.Errorf("%s: %w", RevertedName, err)
	}
	if st.Busy() {
		st.Elevate = Elevate(detect.FindElevation(ctx, sys))
	}
	return st, nil
}

// ReadConfirmed reads confirmed.json, the last change confirmed (§2.5 of
// revert.md): nil when there is none. It is checked as ReadState checks
// the journal (the directory and the file root's alone, ErrUnsafe; the
// file read without following a link and decoded strictly, ErrJournal).
// `hardn confirm` reads it to finish the clean-up of a change whose
// confirmation was claimed and whose clean-up did not run (a crash between
// the two; P-40, next/p3-confirm-cleanup).
func ReadConfirmed(ctx context.Context, sys host.System, opts Options) (*Journal, error) {
	dir := opts.dir()
	if err := stateDir(ctx, sys, dir, false); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return readJournal(ctx, sys, filepath.Join(dir, ConfirmedName))
}

// write writes v to name in the state directory: atomically, 0600, owned by
// hardn's user (root).
func write(ctx context.Context, sys host.System, name string, v any) error {
	data, err := encode(v)
	if err != nil {
		return err
	}
	if err := sys.WriteFileAtomic(ctx, name, data, 0o600, -1, -1); err != nil {
		return fmt.Errorf("revert: %w", err)
	}
	return nil
}

// claim renames from to to and checks that what it took is change id: a
// rename succeeds for one of two processes claiming one file, so a confirm
// and a rollback cannot both act on the change. When another journal was
// put in its place since it was read, it is put back and ErrClaim
// returned. A missing from is fs.ErrNotExist: the other process won.
func claim(ctx context.Context, sys host.System, from, to, id string) (Journal, error) {
	if err := sys.Rename(ctx, from, to); err != nil {
		return Journal{}, err
	}
	j, err := readJournal(ctx, sys, to)
	switch {
	case err != nil:
		return Journal{}, err
	case j == nil:
		return Journal{}, fmt.Errorf("revert: %s: gone after the rename: %w", to, ErrClaim)
	case j.ID != id:
		return Journal{}, errors.Join(fmt.Errorf("revert: claimed change %s, not %s: %w", j.ID, id, ErrClaim), sys.Rename(ctx, to, from))
	}
	return *j, nil
}

// Acknowledge removes the outcome of the timer's last rollback, once a run
// has shown it (Notices), or one that could not be read (OutcomeErr).
func Acknowledge(ctx context.Context, sys host.System, opts Options) error {
	err := sys.Remove(ctx, filepath.Join(opts.dir(), RevertedName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
