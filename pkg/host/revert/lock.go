package revert

// The journal lock and the run lock (refactor plan P3, next/p3-engine-boot;
// docs/engine/engine-boot.md §2): a short lock around each read and claim
// of the journal, which closes the races of revert.md §8.1 item 8 and
// §2.16, and the engine's run lock, which RemoveBootUnit tries.

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// JournalLockName is the journal lock's file, in the journal's directory
// (Options.Dir, /var/lib/hardn), under the same root-only checks as the
// journal (stateDir). Arm holds it around its check that nothing
// is journalled and its write of pending.json; Rollback around its read of
// the journal, its claim (or its take-up of an unfinished rollback) and its
// first progress write; Abandon around its read, its rename and its check;
// Boot around its read and around the re-arm's write. Each holds it for
// file operations alone, never across a command, a reload or a wait for
// the user, so whoever waits for it waits for a few of them.
const JournalLockName = "journal.lock"

// RunLockName is the engine's run lock's file in the run directory
// (pkg/engine's LockName): an apply holds it from its plan made again under
// the lock to its last write, the boot unit's install and Arm included.
// RemoveBootUnit tries it, and never waits for it: an apply that holds it
// may be between the boot unit's install and its Arm. Nothing that holds
// the journal lock takes the run lock, and the run lock is never waited
// for, so the two cannot deadlock.
const RunLockName = "engine.lock"

// ErrNoLock: a lock could not be taken for a reason other than another
// hardn holding it (that is a *LockedError) or a done context: its
// directory is not root's alone (ErrUnsafe too), its file is not a regular
// file, or the file system is full or read-only (host.System.Lock creates
// the file and writes the holder line).
var ErrNoLock = errors.New("the lock cannot be taken")

// LockedError is a lock another hardn holds; errors.Is(err, host.ErrLocked)
// holds for it. Run tells the two locks apart: the run lock (RunLockName),
// tried once and never waited for (LockRun: Boot, RemoveBootUnit, p2drive;
// pkg/engine's apply returns it too), or the journal lock (JournalLockName)
// held past its wait (Arm, Abandon; a rollback goes on without it).
type LockedError struct {
	Run    bool          // the run lock; false: the journal lock
	Name   string        // the lock's file
	Holder string        // the holder's line ("pid <n>, <what>, since <time>"); "" when it cannot be read, or the holder writes none
	Waited time.Duration // how long it was waited for: 0 for the run lock
	Err    error         // host.System.Lock's, which wraps host.ErrLocked
}

func (e *LockedError) Error() string {
	holder := e.Holder
	if holder == "" {
		holder = "its holder writes no line"
	}
	if e.Run {
		return fmt.Sprintf("revert: another hardn holds the run lock %s (%s); try again once it has finished: %v", e.Name, holder, e.Err)
	}
	return fmt.Sprintf("revert: another hardn has held the journal lock %s for %s or more (%s): %v", e.Name, e.Waited, holder, e.Err)
}

// Unwrap returns host.System.Lock's error.
func (e *LockedError) Unwrap() error { return e.Err }

// lockedError is the *LockedError for lock name, which Lock found held
// (err): its holder line read as the holder wrote it.
func lockedError(ctx context.Context, sys host.System, run bool, name string, waited time.Duration, err error) *LockedError {
	data, _, _ := sys.ReadFileNoFollow(ctx, name)
	return &LockedError{Run: run, Name: name, Holder: strings.TrimSpace(string(data)), Waited: waited, Err: err}
}

// The journal lock's wait. Test seams.
var (
	// lockWait bounds the wait for the journal lock. Every holder holds it
	// for a few file operations, milliseconds, so one that still holds it
	// after this long is stopped (Ctrl-Z, SIGSTOP) or stuck in the kernel,
	// and waiting longer would not help. A rollback then goes on without
	// it (Rollback); Arm, Abandon and a re-arm at boot do not.
	lockWait = 10 * time.Second
	// lockPoll is how often the lock is tried again while it is held:
	// host.System.Lock does not wait (flock's LOCK_NB).
	lockPoll = 50 * time.Millisecond
)

// lockJournal takes the journal lock for what (the command holding it, for
// the holder line another hardn names when it finds the lock held). The
// journal's directory must be root's alone (stateDir); when there is none,
// nothing is journalled and there is nothing to lock against, so unlock is
// a no-op (Arm creates the directory before it locks). A lock held by
// another process is tried again every lockPoll, up to lockWait, then
// refused with a *LockedError (host.ErrLocked), naming the holder; a done
// ctx ends the wait, with its cause. Any other failure to take it, a
// journal directory that is not root's alone included, is ErrNoLock.
// unlock releases it and logs a failure to: the kernel releases the lock
// when hardn exits in any case.
func lockJournal(ctx context.Context, sys host.System, opts Options, what string) (unlock func(), err error) {
	dir := opts.dir()
	switch err := stateDir(ctx, sys, dir, false); {
	case errors.Is(err, fs.ErrNotExist):
		return func() {}, nil
	case err != nil:
		return nil, noLock(ctx, "the journal lock in "+dir, err)
	}
	name := filepath.Join(dir, JournalLockName)
	holder := holderLine(ctx, sys, opts, what)
	// The wait is by the real clock: Options.Now is the change's clock,
	// which may be stopped or wrong.
	give := time.Now().Add(lockWait)
	for {
		release, err := sys.Lock(ctx, name, holder)
		switch {
		case err == nil:
			return func() {
				if err := release(); err != nil {
					opts.logf("the journal lock %s was not released cleanly (the kernel releases it at exit): %v", name, err)
				}
			}, nil
		case ctx.Err() != nil:
			return nil, fmt.Errorf("revert: waiting for the journal lock %s: %w", name, context.Cause(ctx))
		case !errors.Is(err, host.ErrLocked):
			return nil, noLock(ctx, "the journal lock "+name, err)
		case !time.Now().Before(give):
			return nil, lockedError(ctx, sys, false, name, lockWait, err)
		}
		t := time.NewTimer(lockPoll)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, fmt.Errorf("revert: waiting for the journal lock %s: %w", name, context.Cause(ctx))
		case <-t.C:
		}
	}
}

// holderLine is a lock's holder line: this process (when /proc/self can
// be read), what holds it, and since when.
func holderLine(ctx context.Context, sys host.System, opts Options, what string) []byte {
	pid := "pid unknown"
	if p, err := selfProc(ctx, sys); err == nil {
		pid = fmt.Sprintf("pid %d", p.PID)
	}
	return fmt.Appendf(nil, "%s, %s, since %s\n", pid, what, opts.now().UTC().Format(time.RFC3339))
}

// LockRun takes the run lock (RunLockName in Options.RunDir) for what,
// without waiting: another hardn holding it is a *LockedError (Run,
// host.ErrLocked), naming its holder. The run directory is created when
// missing and must be root's alone; any other failure to take the lock is
// ErrNoLock. pkg/engine holds the lock for a whole apply (its own code,
// which names the change); internal/tools/p2drive holds it for a
// lockout-risk change; RemoveBootUnit while it removes the boot unit;
// Boot for its whole run.
func LockRun(ctx context.Context, sys host.System, opts Options, what string) (unlock func() error, err error) {
	dir := opts.runDir()
	if err := stateDir(ctx, sys, dir, true); err != nil {
		return nil, noLock(ctx, "the run lock in "+dir, err)
	}
	name := filepath.Join(dir, RunLockName)
	unlock, err = sys.Lock(ctx, name, holderLine(ctx, sys, opts, what))
	if errors.Is(err, host.ErrLocked) {
		return nil, lockedError(ctx, sys, true, name, 0, err)
	} else if err != nil {
		return nil, noLock(ctx, "the run lock "+name, err)
	}
	return unlock, nil
}

// noLock is err, a failure to take lock (its words) that is not another
// hardn holding it: ErrNoLock, or, with ctx done, ctx's cause.
func noLock(ctx context.Context, lock string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("revert: %s: %w", lock, context.Cause(ctx))
	}
	return fmt.Errorf("revert: %s: %w: %w", lock, ErrNoLock, err)
}
