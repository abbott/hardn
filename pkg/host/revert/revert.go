// Package revert is confirm-or-revert (brief 2.4): the persisted journal of
// a change that could lock the user out, the timer that rolls the change
// back unless it is confirmed from a new connection, the confirmation, and
// an idempotent, logged rollback.
//
// The Phase 3 engine writes the journal: Arm before its first step, which
// writes /var/lib/hardn/pending.json and, for a lockout-risk change, starts
// the timer (systemd-run on systemd, a setsid-detached sleeper on OpenRC);
// Record before each step takes effect (write-ahead), and MarkApplied once
// the last has. A later process reads it: the timer's `hardn rollback <id>`
// (Rollback), or the user's `hardn confirm` from an SSH connection made
// after the apply (Confirm). One change is journalled at a time, so confirm
// needs no token, and a new change is refused while one is (PendingError).
//
// Everything reaches the host through host.System. The decisions are pure
// functions: the confirm window (ResolveTimeout), the timer's command
// (TimerFor), whether a session may confirm (CheckConfirm), what the next
// run says (Notices).
//
// docs/safety/revert.md describes the guarantees, the platform facts they
// rely on and what is written but not yet run on a Linux host.
package revert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/reload"
)

// Where the journal lives, and the names in it.
const (
	DefaultDir    = "/var/lib/hardn" // the journal and the last outcome, root's, 0700
	DefaultRunDir = "/run/hardn"     // the OpenRC sleeper's pidfile; gone at reboot, as the sleeper is
	PendingName   = "pending.json"   // the journalled change
	RollbackName  = "rollback.json"  // a rollback that started and has not finished
	ConfirmedName = "confirmed.json" // the last change confirmed
	RevertedName  = "reverted.json"  // the last change the timer undid, until the next run has said so
)

// SchemaVersion is the journal's format version. OutcomeVersion is the
// outcome's (reverted.json), on its own: the outcome is only a notice, so a
// change to the journal's format does not make a leftover one unreadable.
const (
	SchemaVersion  = 1
	OutcomeVersion = 1
)

// The confirm window (decision §3.14: --confirm-timeout, confirmTimeout).
const (
	// DefaultTimeout is brief 2.4's 180 seconds.
	DefaultTimeout = 180 * time.Second
	// MinTimeout is the floor. In the window the user opens a terminal,
	// connects (on the new port), unlocks a key and types a sudo password
	// (brief 2.4), and the window starts before the change is applied, so
	// the apply itself (validation, reload, the listener check) comes out
	// of it too. Under a minute, the revert races a practised user;
	// iptables-apply's 10 s is what brief 2.4 calls too short.
	MinTimeout = 60 * time.Second
)

// RollbackTimeout bounds a rollback: it runs under its own deadline, not
// its caller's context, so a cancelled context does not stop it half-way.
// A signal is the caller's to hold off (cmd/hardn does for `hardn
// rollback`).
const RollbackTimeout = 5 * time.Minute

// cleanupTimeout bounds the clean-up after a failed Arm, which runs under
// its own deadline for the same reason.
const cleanupTimeout = 30 * time.Second

// Errors, testable with errors.Is.
var (
	ErrTimeout        = errors.New("confirm timeout not accepted")
	ErrID             = errors.New("not a change id")
	ErrJournal        = errors.New("journal not valid")
	ErrUnsafe         = errors.New("a user other than root can change it")
	ErrPending        = errors.New("a change is already journalled")
	ErrNothingPending = errors.New("no change is waiting for confirmation")
	ErrNotConfirmable = errors.New("the journalled change is not waiting for confirmation")
	ErrRollingBack    = errors.New("a rollback of the change has started")
	ErrSameSession    = errors.New("this is the SSH connection that applied the change")
	ErrOldConnection  = errors.New("this SSH connection was open before the change was applied")
	ErrLocalConn      = errors.New("this SSH connection comes from the host itself")
	ErrSessionUnknown = errors.New("this session's SSH connection is not established")
	ErrWrongListener  = errors.New("this connection did not reach a listener the change added")
	ErrApplied        = errors.New("the change is applied already")
	ErrNoTimer        = errors.New("no revert timer for this init system")
	ErrExecutable     = errors.New("not safe to run as root from the timer")
	ErrClaim          = errors.New("the journal changed while it was being claimed")
	ErrIncomplete     = errors.New("the rollback did not finish")
	ErrNoPlanRollback = errors.New("no rollback is registered for the unit of this plan")
)

// PlanRollback undoes a unit's kept plan, an OpPlan step: it decodes plan
// and runs the unit's own checked rollback (stack B's sudoers, DNS and
// Alpine units each keep a Plan for p2-revert). cmd/hardn registers one per
// unit in Options.Plans, so this package imports none of them.
type PlanRollback func(ctx context.Context, sys host.System, plan json.RawMessage) error

// Options are the paths and hooks the functions here share. The zero value
// is production's.
type Options struct {
	Dir    string           // DefaultDir when empty
	RunDir string           // DefaultRunDir when empty
	Now    func() time.Time // time.Now when nil
	// Log receives one line per decision and step of Arm, Confirm and
	// Rollback; nil discards. Commands go to the System's run log.
	Log io.Writer
	// Hardn is the absolute path of the hardn binary the timer runs (Arm):
	// os.Executable, resolved. It is outside host.SearchPath, so it is
	// named by path, and checked (CheckExecutable). For Boot, it is the
	// running hardn, which the journal's timer must run.
	Hardn string
	// Reason, when set, is the rollback's reason; otherwise Rollback works
	// it out from the deadline (Reason).
	Reason Reason
	// DetectSSH tells Rollback how sshd is started, for an ssh reload; nil
	// is detect.DetectSSH over detect.DetectInit.
	DetectSSH func(ctx context.Context, sys host.System) detect.SSH
	// Reload is passed to reload.Execute.
	Reload reload.Options
	// NoStart: Rollback starts no unit. Every reload runs as one journalled
	// with IfUp: a unit that is up is reloaded, and one that is down is left
	// so, to read the restored files when it starts. `hardn rollback --boot`
	// sets it (Boot): it runs before ssh.service and ssh.socket start, and a
	// start from it would wait on the boot unit itself.
	NoStart bool
	// Plans are the rollbacks of OpPlan steps, by unit. Record refuses a
	// step whose unit has none (ErrNoPlanRollback), and so does Rollback.
	Plans map[string]PlanRollback
}

// checkPlans refuses an OpPlan step of j whose unit has no rollback in o.
func (o Options) checkPlans(j Journal) error {
	for i, s := range j.Undo {
		if s.Op == OpPlan && o.Plans[s.Unit] == nil {
			return fmt.Errorf("revert: journal %q: undo step %d: unit %q: %w", j.ID, i+1, s.Unit, ErrNoPlanRollback)
		}
	}
	return nil
}

func (o Options) dir() string {
	if o.Dir == "" {
		return DefaultDir
	}
	return o.Dir
}

func (o Options) runDir() string {
	if o.RunDir == "" {
		return DefaultRunDir
	}
	return o.RunDir
}

func (o Options) now() time.Time {
	if o.Now == nil {
		return time.Now()
	}
	return o.Now()
}

// cleanupContext is ctx without its cancellation, under cleanupTimeout: a
// clean-up after Ctrl-C must still run, and every host.System method
// refuses a done context.
func cleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
}

func (o Options) logf(format string, args ...any) {
	if o.Log != nil {
		_, _ = fmt.Fprintf(o.Log, "revert: "+format+"\n", args...)
	}
}

// NewID returns a new change id, in backup.NewRunID's form: one id can name
// the change, its backup run directory and its timer unit.
func NewID(t time.Time) string { return backup.NewRunID(t) }

// ValidID reports whether id can name a change: 1 to 64 letters, digits,
// '-' or '_', starting with a letter or digit. It is then one path
// component and a valid systemd unit name part.
func ValidID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case (c == '-' || c == '_') && i > 0:
		default:
			return false
		}
	}
	return true
}

// ResolveTimeout returns the confirm window: the --confirm-timeout flag when
// it is set (non-nil), else the confirmTimeout key when set, else
// DefaultTimeout, rounded up to a whole second. A value below MinTimeout,
// an explicit 0 among them, is refused with ErrTimeout, naming where it came
// from, rather than raised without a word.
func ResolveTimeout(flag, key *time.Duration) (time.Duration, error) {
	d, from := DefaultTimeout, "the default"
	switch {
	case flag != nil:
		d, from = *flag, "--confirm-timeout"
	case key != nil:
		d, from = *key, "confirmTimeout"
	}
	if r := d % time.Second; r != 0 {
		d += time.Second - r
	}
	if d < MinTimeout {
		return 0, fmt.Errorf("revert: %s %s: at least %s: %w", from, d, MinTimeout, ErrTimeout)
	}
	return d, nil
}

// ParseTimeout reads a --confirm-timeout or confirmTimeout value: whole
// seconds ("180") or a Go duration ("3m", "180s"). It does not apply the
// floor; ResolveTimeout does.
func ParseTimeout(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseUint(s, 10, 32); err == nil {
		return time.Duration(n) * time.Second, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("revert: confirm timeout %q: seconds or a duration such as 3m: %w", s, ErrTimeout)
	}
	return d, nil
}
