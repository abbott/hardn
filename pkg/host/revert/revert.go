// Package revert is confirm-or-revert (brief 2.4): the persisted journal of
// a change that could lock the user out, the timer that rolls the change
// back unless it is confirmed from a new connection, the confirmation, and
// an idempotent, logged rollback.
//
// The Phase 3 engine writes the journal: Arm before its first step, which
// writes /var/lib/hardn/pending.json and, for a lockout-risk change, starts
// the timer (systemd-run on systemd, a setsid-detached sleeper on OpenRC);
// Record as its steps complete. A later process reads it: the timer's
// `hardn rollback <id>` (Rollback), or the user's `hardn confirm` from a new
// SSH session (Confirm). One change is journalled at a time, so confirm
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

// SchemaVersion is the journal's and the outcome's format version.
const SchemaVersion = 1

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
// its caller's context, so Ctrl-C cannot stop it half-way.
const RollbackTimeout = 5 * time.Minute

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
	ErrSessionUnknown = errors.New("this session's SSH connection is not established")
	ErrWrongListener  = errors.New("this connection did not reach a listener the change added")
	ErrNoTimer        = errors.New("no revert timer for this init system")
	ErrExecutable     = errors.New("not safe to run as root from the timer")
	ErrClaim          = errors.New("the journal changed while it was being claimed")
	ErrIncomplete     = errors.New("the rollback did not finish")
)

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
	// named by path, and checked (CheckExecutable).
	Hardn string
	// Reason, when set, is the rollback's reason; otherwise Rollback works
	// it out from the deadline (Reason).
	Reason Reason
	// DetectSSH tells Rollback how sshd is started, for an ssh reload; nil
	// is detect.DetectSSH over detect.DetectInit.
	DetectSSH func(ctx context.Context, sys host.System) detect.SSH
	// Reload is passed to reload.Execute.
	Reload reload.Options
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
// it is set (non-zero), else the confirmTimeout key when set, else
// DefaultTimeout, rounded up to a whole second. A value below MinTimeout is
// refused with ErrTimeout, naming where it came from, rather than raised
// without a word.
func ResolveTimeout(flag, key time.Duration) (time.Duration, error) {
	d, from := DefaultTimeout, "the default"
	switch {
	case flag != 0:
		d, from = flag, "--confirm-timeout"
	case key != 0:
		d, from = key, "confirmTimeout"
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
