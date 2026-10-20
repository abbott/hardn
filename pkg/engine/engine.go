// Package engine is hardn's plan/apply engine (refactor plan P3,
// next/p3-engine; brief Phase 3). Controls report a setting's state (Check)
// and plan the changes that bring it to what the configuration asks (Plan).
// The engine puts every control's changes into one plan, in one stable
// order computed from brief 2.3's dependency rule (Stage, Change.After),
// and refuses at plan time everything it would refuse later, so a dry run,
// which is the plan alone, shows every refusal. Apply carries a plan out
// under confirm-or-revert (pkg/host/revert): one apply per host at a time
// (a run lock), the plan made again under the lock and compared, the revert
// timer armed first for a lockout-risk plan and the end of its window kept
// for the rollback here, every change journalled write-ahead and again once
// it ran, and on the first failure, Ctrl-C, SIGTERM or a hangup every
// change started rolled back in reverse from the journal, with SIGHUP and
// SIGINT held (package hold). A plan with nothing to change applies
// nothing; a plan is applied once.
//
// The engine returns data and renders nothing: plans, statuses, results and
// the progress of an apply (Options.Progress) are structs that p3-output and
// the slices render and wire into cmd/hardn. It reaches the host only through host.System (depguard
// engine-through-host).
//
// docs/engine/engine.md describes the guarantees, the facts they rely on and
// what is written but not yet run on a Linux host.
package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/engine/hold"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/revert/plans"
)

// LockName is the run lock's file in the run root (revert's RunDir,
// /run/hardn): one engine apply per host at a time. It is revert's
// RunLockName, which `hardn rollback --boot` tries too, and never waits for
// (next/p3-engine-boot).
const LockName = revert.RunLockName

// DefaultJournalTimeout bounds a journal write made after a step that ran
// (Ran.Rollback), which runs without the apply's cancellation so that it is
// written after Ctrl-C too.
const DefaultJournalTimeout = 30 * time.Second

// DefaultMargin is the end of a lockout-risk plan's confirm window that its
// changes do not run into: a change's context ends that long before the
// journal's deadline, and no change, nor MarkApplied, starts after then
// (ErrWindow). It is what the rollback in this process needs to claim the
// change before the timer's `hardn rollback <id>` does: host.Real.Run
// waits up to 10 s for a cancelled command's pipes (system.md §2.5), then
// the journal of what ran and the claim, a rename, follow.
const DefaultMargin = 20 * time.Second

// Errors, testable with errors.Is.
var (
	// ErrRefused: the plan has refusals (Plan.Refused); nothing is applied.
	ErrRefused = errors.New("refused at plan time")
	// ErrUnsupported: the platform's support level (detect.Support) is not
	// Supported or BestEffort, so no change is made.
	ErrUnsupported = errors.New("no changes on a platform outside the support matrix")
	// ErrChange: a change is not one the engine can journal and order.
	ErrChange = errors.New("change not valid")
	// ErrOrder: the changes cannot be put in one order: a cycle, a
	// prerequisite no control planned, or one in a later stage.
	ErrOrder = errors.New("changes cannot be ordered")
	// ErrConflict: two changes of one plan act on the same thing: both
	// back up one file, or two controls' changes name one resource
	// (Change.Resources). Each was planned from the host as it is, not as
	// the other leaves it, so the second would fail once the revert timer
	// is armed, or the plan would end as neither planned it
	// (next/p3-steps).
	ErrConflict = errors.New("two changes of the plan act on the same thing")
	// ErrPlanChanged: the plan made again under the run lock is not the
	// plan given to Apply: the host changed since. Nothing is applied.
	ErrPlanChanged = errors.New("the host changed since the plan was made; plan again")
	// ErrRolledBack: an apply failed and every change it had made was
	// rolled back (ApplyError, when its rollback finished).
	ErrRolledBack = errors.New("rolled back")
	// ErrPlanUsed: Apply was given a plan it had taken before. A plan is
	// applied once (its id names one journal, one backup run and one
	// timer); plan again.
	ErrPlanUsed = errors.New("the plan was applied already; plan again")
	// ErrWindow: a lockout-risk plan reached the last Options.Margin of
	// its confirm window, so the engine stopped and rolled it back here,
	// before the timer's rollback could race a change.
	ErrWindow = errors.New("the confirm window is closing")
	// ErrUnjournalled: a change ran whose undo the journal did not hold
	// once it had run (ApplyError.Unjournalled), so the rollback may have
	// left what it did.
	ErrUnjournalled = errors.New("a change ran that the journal does not cover")
	// ErrBootUnit: the boot unit's install (StageBoot, StepBoot) failed,
	// before revert.Arm: the change is not applied, and nothing is armed
	// (Result.Boot says what the install did; next/p3-engine-boot).
	ErrBootUnit = errors.New("the boot unit was not installed, so nothing is armed")
)

// Options configure an Engine. The zero value is production's, except for
// what only the caller knows: Session, PID and revert's Hardn.
type Options struct {
	// Revert are the journal's options: its directories, clock, log,
	// the hardn binary the timer runs (Hardn: os.Executable, resolved)
	// and the plan rollbacks (Plans; nil is plans.Registry()). Reason is
	// the engine's to set.
	Revert revert.Options
	// Timeout is the confirm window of a lockout-risk plan
	// (revert.ResolveTimeout over --confirm-timeout and confirmTimeout);
	// 0 is revert.DefaultTimeout.
	Timeout time.Duration
	// Session is the session applying the change (revert.ReadSession,
	// whose environment and pid are the caller's).
	Session revert.Session
	// Backups are backupPath and enableBackups; an empty Path is
	// backup.DefaultPath. The engine's backups are rollback-required, so
	// they are taken whatever Enabled says (§3.16).
	Backups backup.Settings
	// Detect establishes the platform facts; nil is detect.Detect.
	Detect func(ctx context.Context, sys host.System) (detect.Facts, error)
	// Hold holds signals around an in-process rollback; nil is
	// hold.Signals with Revert.Log.
	Hold func() (release func())
	// PID is hardn's process id, for the run lock's holder line.
	PID int
	// JournalTimeout bounds a journal write after a step that ran; 0 is
	// DefaultJournalTimeout.
	JournalTimeout time.Duration
	// Margin is the end of the confirm window no change runs into; 0 is
	// DefaultMargin.
	Margin time.Duration
	// Progress, when set, receives each moment of an apply as it happens
	// (Event), on Apply's goroutine and in order, for a caller that shows
	// progress (pkg/ui/flow's Report). It must not block for long, nor
	// call the engine.
	Progress func(Event)
}

// Engine plans and applies controls' changes on one host.
type Engine struct {
	sys  host.System
	opts Options
}

// New returns an Engine for sys. It does no I/O.
func New(sys host.System, opts Options) *Engine {
	if opts.Revert.Plans == nil {
		opts.Revert.Plans = plans.Registry()
	}
	if opts.Backups.Path == "" {
		opts.Backups.Path = backup.DefaultPath
	}
	if opts.Timeout == 0 {
		opts.Timeout = revert.DefaultTimeout
	}
	if opts.JournalTimeout == 0 {
		opts.JournalTimeout = DefaultJournalTimeout
	}
	if opts.Margin == 0 {
		opts.Margin = DefaultMargin
	}
	if opts.Detect == nil {
		opts.Detect = detect.Detect
	}
	if opts.Hold == nil {
		log := opts.Revert.Log
		opts.Hold = func() func() { return hold.Signals(log) }
	}
	return &Engine{sys: sys, opts: opts}
}

func (e *Engine) now() time.Time {
	if e.opts.Revert.Now != nil {
		return e.opts.Revert.Now()
	}
	return time.Now()
}

// runRoot is revert's run directory, /run/hardn: the run lock, and each
// plan's run directory under it.
func (e *Engine) runRoot() string {
	if e.opts.Revert.RunDir != "" {
		return e.opts.Revert.RunDir
	}
	return revert.DefaultRunDir
}

func (e *Engine) logf(format string, args ...any) {
	if w := e.opts.Revert.Log; w != nil {
		_, _ = fmt.Fprintf(w, "engine: "+format+"\n", args...)
	}
}

// trustedDir refuses dir unless it and every directory above it is a
// directory, not a symlink, owned by root and writable by neither group nor
// others (revert.ErrUnsafe): the run lock and the plans' run directories
// live there, and a user who could write it could hold the lock or plant
// what a validation reads.
func trustedDir(ctx context.Context, sys host.System, dir string) error {
	for p := dir; ; p = filepath.Dir(p) {
		fi, err := sys.Lstat(ctx, p)
		if err != nil {
			return fmt.Errorf("engine: %w", err)
		}
		uid, _, ok := host.Owner(fi)
		if !fi.IsDir() || !ok || uid != 0 || fi.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("engine: %s: %w", p, revert.ErrUnsafe)
		}
		if p == "/" {
			return nil
		}
	}
}

// trustedAncestor is trustedDir for the deepest directory of dir's path
// that exists, so that what Arm creates under it is root's alone: revert's
// ReadState checks a journal directory that exists, and none that does
// not.
func trustedAncestor(ctx context.Context, sys host.System, dir string) error {
	for p := dir; ; p = filepath.Dir(p) {
		_, err := sys.Lstat(ctx, p)
		switch {
		case err == nil:
			return trustedDir(ctx, sys, p)
		case !errors.Is(err, fs.ErrNotExist) || p == "/":
			return fmt.Errorf("engine: %w", err)
		}
	}
}

// lock takes the run lock for change id, after making sure the run root is
// root's alone. Another hardn holding it is a *revert.LockedError (Run,
// host.ErrLocked), with its holder line, as revert.LockRun's. The release
// logs a failure and goes on: the kernel releases the lock at exit in any
// case.
//
// It is not revert.LockRun, though both take one file (LockName is
// revert.RunLockName, in the run root both read from Options.Revert.RunDir)
// with host.System.Lock, so they exclude each other whatever their words:
// the engine's holder line names Options.PID, which the caller passes,
// where LockRun reads /proc/self, and the engine checks the run root with
// trustedDir, whose refusals ("engine: run lock: ") TestRunLock pins.
func (e *Engine) lock(ctx context.Context, id string) (func(), error) {
	root := e.runRoot()
	if err := e.sys.MkdirAll(ctx, root, 0o700); err != nil {
		return nil, fmt.Errorf("engine: run lock: %w", err)
	}
	if err := trustedDir(ctx, e.sys, root); err != nil {
		return nil, fmt.Errorf("engine: run lock: %w", err)
	}
	name := filepath.Join(root, LockName)
	pid := "pid unknown"
	if e.opts.PID > 0 {
		pid = fmt.Sprintf("pid %d", e.opts.PID)
	}
	holder := fmt.Sprintf("%s, change %s, since %s\n", pid, id, e.now().UTC().Format(time.RFC3339))
	unlock, err := e.sys.Lock(ctx, name, []byte(holder))
	if errors.Is(err, host.ErrLocked) {
		data, _, _ := e.sys.ReadFileNoFollow(ctx, name)
		return nil, fmt.Errorf("engine: %w", &revert.LockedError{Run: true, Name: name, Holder: strings.TrimSpace(string(data)), Err: err})
	} else if err != nil {
		return nil, fmt.Errorf("engine: run lock: %w", err)
	}
	return func() {
		if err := unlock(); err != nil {
			e.logf("run lock %s not released cleanly (the kernel releases it at exit): %v", name, err)
		}
	}, nil
}
