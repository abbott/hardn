package reload

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	Step   Step
	Ran    bool        // the command ran
	Result host.Result // its result, when it ran
	// NotUp is the unit's state when a StepIfUp found it not up: the
	// command did not run, or failed because the unit stopped meanwhile.
	// sshd then reads the files when it next starts.
	NotUp string
}

// Report is what Execute did.
type Report struct {
	Steps []StepReport // in order, up to and including a failed step
	// Bound is ss's last answer; Verified: every listener of Plan.Verify is
	// bound.
	Bound    []Bound
	Verified bool
	// Unverified says why the listeners were not checked: none changed, a
	// unit was not up, or ss is not installed. "" when Verified, or when
	// Execute failed.
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
// checks that Plan.Verify's listeners are bound, polling. It refuses a done
// ctx; a rollback after Ctrl-C must pass context.WithoutCancel. Errors: a
// command's (*host.ExitError, host.ErrNotFound) wrapped with its argv,
// ErrUnitState, a *ListenerError, fs.ErrInvalid for a step PlanFor does not
// make (no argv, no kind), and ctx's.
func Execute(ctx context.Context, sys host.System, p Plan, opts Options) (Report, error) {
	var rep Report
	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("reload: %w", err)
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
	case len(p.Verify) == 0:
		rep.Unverified = "no listener changed"
	case len(notUp) > 0:
		rep.Unverified = strings.Join(notUp, "; ") + ": nothing listens on the new configuration until it starts"
	default:
		return verify(ctx, sys, p.Verify, opts, rep)
	}
	return rep, nil
}

func runStep(ctx context.Context, sys host.System, st Step, want []sshd.Listener) (StepReport, error) {
	sr := StepReport{Step: st}
	if len(st.Argv) == 0 || st.Kind < StepRun || st.Kind > StepCheckSocket {
		return sr, fmt.Errorf("reload: step %+v: %w", st, fs.ErrInvalid)
	}
	if st.Kind == StepIfUp {
		up, state, err := isUp(ctx, sys, st)
		if err != nil {
			return sr, err
		}
		if !up {
			sr.NotUp = state
			return sr, nil
		}
	}
	var err error
	sr.Ran = true
	sr.Result, err = sys.Run(ctx, st.Argv[0], st.Argv[1:]...)
	if err != nil {
		if st.Kind == StepIfUp {
			if up, state, uerr := isUp(ctx, sys, st); uerr == nil && !up {
				sr.NotUp = state + " (it stopped while " + host.FormatArgv(st.Argv) + " ran)"
				return sr, nil
			}
		}
		return sr, fmt.Errorf("reload: %s: %w", host.FormatArgv(st.Argv), err)
	}
	if st.Kind == StepCheckSocket {
		ls, bad := sshd.ParseSocketListen(sr.Result.Stdout)
		if missing := uncovered(want, ls, true); len(missing) > 0 {
			return sr, &ListenerError{Source: host.FormatArgv(st.Argv), Missing: missing, Bad: bad, Err: ErrSocketConfig}
		}
	}
	return sr, nil
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
	return false, "", fmt.Errorf("reload: systemctl is-active %s printed %q (exit %d): %w", st.Unit, state, res.ExitCode, ErrUnitState)
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
			rep.Unverified = "ss is not installed (iproute2), so the bound listeners were not read"
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
