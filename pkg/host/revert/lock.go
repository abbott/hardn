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
// refused with host.ErrLocked, naming the holder; a done ctx ends the
// wait. unlock releases it and logs a failure to: the kernel releases the
// lock when hardn exits in any case.
func lockJournal(ctx context.Context, sys host.System, opts Options, what string) (unlock func(), err error) {
	dir := opts.dir()
	switch err := stateDir(ctx, sys, dir, false); {
	case errors.Is(err, fs.ErrNotExist):
		return func() {}, nil
	case err != nil:
		return nil, fmt.Errorf("revert: the journal lock: %w", err)
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
		case !errors.Is(err, host.ErrLocked):
			return nil, fmt.Errorf("revert: the journal lock: %w", err)
		case !time.Now().Before(give):
			data, _, _ := sys.ReadFileNoFollow(ctx, name)
			return nil, fmt.Errorf("revert: another hardn has held the journal lock %s for %s or more (%s): %w",
				name, lockWait, strings.TrimSpace(string(data)), err)
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
// without waiting: another hardn holding it is host.ErrLocked, naming its
// holder. The run directory is created when missing and must be root's
// alone. pkg/engine holds the lock for a whole apply (its own code, which
// names the change); internal/tools/p2drive holds it for a lockout-risk
// change; RemoveBootUnit while it removes the boot unit.
func LockRun(ctx context.Context, sys host.System, opts Options, what string) (unlock func() error, err error) {
	dir := opts.runDir()
	if err := stateDir(ctx, sys, dir, true); err != nil {
		return nil, fmt.Errorf("revert: the run lock: %w", err)
	}
	name := filepath.Join(dir, RunLockName)
	unlock, err = sys.Lock(ctx, name, holderLine(ctx, sys, opts, what))
	if errors.Is(err, host.ErrLocked) {
		data, _, _ := sys.ReadFileNoFollow(ctx, name)
		return nil, fmt.Errorf("revert: another hardn holds the run lock %s (%s): %w", name, strings.TrimSpace(string(data)), err)
	} else if err != nil {
		return nil, fmt.Errorf("revert: the run lock: %w", err)
	}
	return unlock, nil
}
