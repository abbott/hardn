package reload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// The listener check's defaults: a reload signals sshd and returns, and sshd
// rebinds after it re-executes, so the check polls.
const (
	DefaultAttempts = 10
	DefaultInterval = 500 * time.Millisecond
)

// Options tunes Execute.
type Options struct {
	Attempts int           // listener checks before ErrNotBound; 0 is DefaultAttempts
	Interval time.Duration // between them; 0 is DefaultInterval
	// Wait waits d or until ctx is done; nil sleeps. Tests replace it.
	Wait func(ctx context.Context, d time.Duration) error
}

// StepReport is what one step did.
type StepReport struct {
	Step    Step
	Ran     bool        // a command ran: Argv, or Start when Started
	Started bool        // Unit was not up, so Step.Start ran instead of Argv
	Result  host.Result // its result, when it ran
	// NotUp is the unit's state when a StepIfUp found it not up and did not
	// run, or, for an Activated unit, found it down after it ran. sshd then
	// reads the installed files when it next starts: on a socket host, at
	// the next connection.
	NotUp string
}

// Reason says, as a code, why a Report is not Verified.
type Reason uint8

const (
	ReasonNone             Reason = iota // Verified, or Execute failed
	ReasonNotUp                          // a unit was not up (StepReport.NotUp)
	ReasonNoListenerChange               // no listener changed, so none was checked
	ReasonNoSS                           // ss is not installed, so none was read
)

func (r Reason) String() string {
	switch r {
	case ReasonNone:
		return ""
	case ReasonNotUp:
		return "not-up"
	case ReasonNoListenerChange:
		return "no-listener-change"
	case ReasonNoSS:
		return "no-ss"
	}
	return "unknown"
}

// Report is what Execute did.
type Report struct {
	Steps []StepReport // in order, up to and including a failed step
	// Bound is ss's last answer; Verified: every listener of Plan.Verify is
	// bound.
	Bound    []Bound
	Verified bool
	// Reason and Unverified say why the listeners were not checked, as a
	// code and in words; a unit that was not up comes first. Zero when
	// Verified, or when Execute failed.
	Reason     Reason
	Unverified string
}

// ListenerError is a listener check that failed: ErrSocketConfig (the
// socket's configuration, after daemon-reload) or ErrNotBound (ss, after
// the steps).
type ListenerError struct {
	Source  string // the command whose answer lacks them
	Missing []sshd.Listener
	Bad     []string // lines of its answer hardn could not read
	Err     error
}

func (e *ListenerError) Error() string {
	s := fmt.Sprintf("reload: %s lacks %v: %s", e.Source, e.Missing, e.Err)
	if len(e.Bad) > 0 {
		s += fmt.Sprintf(" (unread lines %q)", e.Bad)
	}
	return s
}

func (e *ListenerError) Unwrap() error { return e.Err }

// Execute runs p's steps in order, stopping at the first that fails, then
// checks that Plan.Verify's listeners are bound, polling. When the plan adds
// listeners, it first reads ss and refuses, before any step, one that a
// socket other than Hold's may already hold (ErrInUse). It refuses a done
// ctx; a rollback after Ctrl-C must pass context.WithoutCancel. Errors: a
// command's (*host.ExitError, host.ErrNotFound) wrapped with its argv, with
// ErrUnitDown too when the unit is down after it; ErrUnitDown; ErrUnitState;
// ErrInUse; a *ListenerError; fs.ErrInvalid for a step PlanFor does not make
// (no argv, no kind); and ctx's.
func Execute(ctx context.Context, sys host.System, p Plan, opts Options) (Report, error) {
	var rep Report
	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("reload: %w", err)
	}
	if len(p.Adds) > 0 {
		if err := free(ctx, sys, p, &rep); err != nil {
			return rep, err
		}
	}
	var notUp []string
	for _, st := range p.Steps {
		sr, err := runStep(ctx, sys, st, p.Verify)
		rep.Steps = append(rep.Steps, sr)
		if err != nil {
			return rep, err
		}
		if sr.NotUp != "" {
			notUp = append(notUp, sr.NotUp)
		}
	}
	switch {
	case len(notUp) > 0:
		rep.Reason = ReasonNotUp
		rep.Unverified = strings.Join(notUp, "; ") + ": the installed files take effect when it next starts"
	case len(p.Verify) == 0:
		rep.Reason, rep.Unverified = ReasonNoListenerChange, "no listener changed"
	default:
		return verify(ctx, sys, p.Verify, opts, rep)
	}
	return rep, nil
}

// CheckFree is Execute's check before its first step, on its own: with
// p.Adds empty it reads nothing; otherwise it refuses, as Execute would, a
// port of Adds that a socket other than Hold's may already hold (ErrInUse;
// on a socket host also ss missing or failing). The engine runs it at plan
// time, so that the refusal comes before the revert timer is armed and the
// firewall changed, not from Execute after them (next/p3-steps); Execute
// still runs it before its first step. A done ctx is refused.
func CheckFree(ctx context.Context, sys host.System, p Plan) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	if len(p.Adds) == 0 {
		return nil
	}
	return free(ctx, sys, p, &Report{})
}

// free refuses a change whose added listeners may already be taken: a
// socket ss shows on one of their ports, at an equal or unspecified address
// on either side, whatever the family (fail closed), unless it is one of
// Hold's, which the running sshd or ssh.socket holds. On a socket host a
// port that cannot be bound fails the restart and closes every listener of
// the socket; elsewhere sshd skips it. So ss missing refuses only there.
func free(ctx context.Context, sys host.System, p Plan, rep *Report) error {
	bound, bad, err := ReadBound(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound) && p.Mode != detect.SSHSocket:
		return nil // the check after the steps cannot read ss either
	case err != nil:
		return fmt.Errorf("%w: on a socket host the ports a change adds must be shown free, since a port ssh.socket cannot bind fails its restart and closes every listener", err)
	}
	rep.Bound = bound
	var taken []string
	for _, b := range bound {
		own := slices.ContainsFunc(p.Hold, func(h sshd.Listener) bool {
			return b.Port == h.Port && (b.Addr.Unmap() == h.Addr.Unmap() || b.Addr.IsUnspecified() && h.Addr.IsUnspecified())
		})
		if !own && slices.ContainsFunc(p.Adds, func(w sshd.Listener) bool {
			return b.Port == w.Port && (b.Addr.Unmap() == w.Addr.Unmap() || b.Addr.IsUnspecified() || w.Addr.IsUnspecified())
		}) {
			taken = append(taken, b.String())
		}
	}
	if len(taken) == 0 && len(bad) == 0 {
		return nil
	}
	msg := fmt.Sprintf("reload: %s, before the change, shows %v bound on a port of %v", host.FormatArgv(ssArgv), taken, p.Adds)
	if len(bad) > 0 {
		msg += fmt.Sprintf(" (unread lines %q may be)", bad)
	}
	return fmt.Errorf("%s: %w", msg, ErrInUse)
}

func runStep(ctx context.Context, sys host.System, st Step, want []sshd.Listener) (StepReport, error) {
	sr := StepReport{Step: st}
	if len(st.Argv) == 0 || st.Kind < StepRun || st.Kind > StepCheckSocket {
		return sr, fmt.Errorf("reload: step %+v: %w", st, fs.ErrInvalid)
	}
	argv := st.Argv
	if st.Kind == StepIfUp {
		up, state, err := isUp(ctx, sys, st)
		switch {
		case err != nil:
			return sr, err
		case !up && len(st.Start) == 0:
			sr.NotUp = state
			return sr, nil
		case !up:
			argv, sr.Started = st.Start, true
		}
	}
	var err error
	sr.Ran = true
	sr.Result, err = sys.Run(ctx, argv[0], argv[1:]...)
	if err != nil {
		err = fmt.Errorf("reload: %s: %w", host.FormatArgv(argv), err)
	}
	switch {
	case st.Kind == StepCheckSocket && err == nil:
		ls, bad := sshd.ParseSocketListen(sr.Result.Stdout)
		if missing := uncovered(want, ls, true); len(missing) > 0 {
			return sr, &ListenerError{Source: host.FormatArgv(st.Argv), Missing: missing, Bad: bad, Err: ErrSocketConfig}
		}
		return sr, nil
	case st.Kind != StepIfUp:
		return sr, err
	}
	// A gated step: the unit must still be up, or, Activated, the socket
	// starts it at the next connection.
	up, state, uerr := isUp(ctx, sys, st)
	switch {
	case uerr != nil && err != nil:
		return sr, err
	case uerr != nil:
		return sr, uerr
	case up:
		return sr, err
	case st.Activated:
		sr.NotUp = state + " after " + host.FormatArgv(argv)
		return sr, nil
	case err != nil:
		return sr, fmt.Errorf("%w; then %s: %w", err, state, ErrUnitDown)
	}
	return sr, fmt.Errorf("reload: %s ran, then %s: %w", host.FormatArgv(argv), state, ErrUnitDown)
}

// isUp asks, now, whether st's unit is up: systemctl is-active on systemd,
// OpenRC's started link on OpenRC (as detect reads it).
func isUp(ctx context.Context, sys host.System, st Step) (bool, string, error) {
	if st.OpenRC {
		_, err := sys.Stat(ctx, detect.OpenRCStarted+"/"+st.Unit)
		switch {
		case err == nil:
			return true, "", nil
		case errors.Is(err, fs.ErrNotExist):
			return false, "OpenRC has not started " + st.Unit, nil
		}
		return false, "", fmt.Errorf("reload: OpenRC state of %s: %w", st.Unit, err)
	}
	res, err := sys.Run(ctx, "systemctl", "is-active", st.Unit)
	var xe *host.ExitError
	if err != nil && !errors.As(err, &xe) {
		return false, "", fmt.Errorf("reload: systemctl is-active %s: %w", st.Unit, err)
	}
	state, _, _ := strings.Cut(string(res.Stdout), "\n")
	state = strings.TrimSpace(state)
	switch (detect.UnitState{Active: state}).IsActive() {
	case detect.Yes:
		return true, "", nil
	case detect.No:
		return false, st.Unit + " is " + state, nil
	}
	if state == "activating" && waitingToRestart(ctx, sys, st.Unit) {
		// sshd exited and systemd restarts it after RestartSec (42 s on
		// Rocky Linux): down now. systemctl start runs it at once
		// (rocky10-test-110), so a rollback's Start brings it back.
		return false, st.Unit + " is waiting to restart", nil
	}
	return false, "", fmt.Errorf("reload: systemctl is-active %s printed %q (exit %d): %w", st.Unit, state, res.ExitCode, ErrUnitState)
}

// waitingToRestart: systemd shows unit's SubState auto-restart, the wait
// between its main process exiting and Restart= starting it again. Any
// other answer, or none, is false.
func waitingToRestart(ctx context.Context, sys host.System, unit string) bool {
	res, err := sys.Run(ctx, "systemctl", "show", "-p", "SubState", "--value", unit)
	return err == nil && strings.TrimSpace(string(res.Stdout)) == "auto-restart"
}

func verify(ctx context.Context, sys host.System, want []sshd.Listener, opts Options, rep Report) (Report, error) {
	if opts.Attempts <= 0 {
		opts.Attempts = DefaultAttempts
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultInterval
	}
	if opts.Wait == nil {
		opts.Wait = sleep
	}
	var missing []sshd.Listener
	var bad []string
	for i := range opts.Attempts {
		if i > 0 {
			if err := opts.Wait(ctx, opts.Interval); err != nil {
				return rep, fmt.Errorf("reload: %w", err)
			}
		}
		bound, b, err := ReadBound(ctx, sys)
		switch {
		case errors.Is(err, host.ErrNotFound):
			rep.Reason, rep.Unverified = ReasonNoSS, "ss is not installed (iproute2), so the bound listeners were not read"
			return rep, nil
		case err != nil:
			return rep, err
		}
		rep.Bound, bad = bound, b
		if missing = unbound(want, bound); len(missing) == 0 {
			rep.Verified = true
			return rep, nil
		}
	}
	return rep, &ListenerError{Source: host.FormatArgv(ssArgv), Missing: missing, Bad: bad, Err: ErrNotBound}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
