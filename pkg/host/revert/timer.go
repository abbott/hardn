package revert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// TimerKind is how the revert timer runs.
type TimerKind string

const (
	// TimerSystemd: a transient timer and service in the system manager,
	// from systemd-run.
	TimerSystemd TimerKind = "systemd-run"
	// TimerSetsid: a shell that sleeps, then runs hardn rollback, started
	// in a session of its own (host.System.StartDetached), with a pidfile.
	TimerSetsid TimerKind = "setsid"
)

// UnitPrefix names the systemd units: hardn-revert-<id>.timer and .service.
const UnitPrefix = "hardn-revert-"

// SleeperScript is the setsid timer's shell program: $0 is hardn's path, $1
// the seconds, $2 the id. They are positional parameters, never part of the
// program text. It does not exec, so the pid in the pidfile stays the
// shell's: cancelling it never kills a rollback that has started.
const SleeperScript = `sleep "$1" && "$0" rollback "$2"`

// Timer is the armed revert timer, in the journal.
type Timer struct {
	Kind     TimerKind `json:"kind"`
	Unit     string    `json:"unit,omitempty"`    // systemd-run: hardn-revert-<id>
	PIDFile  string    `json:"pidFile,omitempty"` // setsid: <RunDir>/revert-<id>.pid
	Argv     []string  `json:"argv"`              // what Arm started
	Deadline time.Time `json:"deadline"`
}

// TimerFor returns, with no I/O, the timer that runs `<hardn> rollback <id>`
// timeout after it starts:
//
//   - systemd (detect's Init Systemd, Running Yes): systemd-run
//     --unit=hardn-revert-<id> --on-active=<N>s, with the timer's
//     AccuracySec at 1s (its default, a minute, would let it fire up to a
//     minute late);
//   - OpenRC (Init OpenRC, Running Yes): /bin/sh -c SleeperScript, to be
//     started detached, with its pid in <runDir>/revert-<id>.pid.
//
// Anything else is ErrNoTimer: a lockout-risk change is not applied with no
// way to undo it.
func TimerFor(in detect.Init, id, hardn string, timeout time.Duration, runDir string) (Timer, error) {
	switch {
	case !ValidID(id):
		return Timer{}, fmt.Errorf("revert: timer: %q: %w", id, ErrID)
	case !clean(hardn):
		return Timer{}, fmt.Errorf("revert: timer: hardn's path %q: %w", hardn, ErrExecutable)
	case timeout < MinTimeout || timeout%time.Second != 0:
		return Timer{}, fmt.Errorf("revert: timer: %s: %w", timeout, ErrTimeout)
	case in.Running != detect.Yes:
		return Timer{}, fmt.Errorf("revert: timer: init %s, running %s: %w", in.Kind, in.Running, ErrNoTimer)
	}
	secs := strconv.Itoa(int(timeout / time.Second))
	switch in.Kind {
	case detect.Systemd:
		unit := UnitPrefix + id
		return Timer{Kind: TimerSystemd, Unit: unit, Argv: []string{
			"systemd-run", "--unit=" + unit, "--description=hardn: roll back change " + id + " unless confirmed",
			"--on-active=" + secs + "s", "--timer-property=AccuracySec=1s", hardn, "rollback", id,
		}}, nil
	case detect.OpenRC:
		if !clean(runDir) {
			return Timer{}, fmt.Errorf("revert: timer: run directory %q: %w", runDir, fs.ErrInvalid)
		}
		return Timer{Kind: TimerSetsid, PIDFile: runDir + "/revert-" + id + ".pid",
			Argv: []string{"/bin/sh", "-c", SleeperScript, hardn, secs, id}}, nil
	}
	return Timer{}, fmt.Errorf("revert: timer: init %s: %w", in.Kind, ErrNoTimer)
}

func (t Timer) check(j Journal) error {
	switch {
	case t.Deadline.IsZero() || len(t.Argv) == 0:
		return errors.New("no deadline or argv")
	case t.Kind == TimerSystemd && t.Unit != UnitPrefix+j.ID:
		return fmt.Errorf("unit %q for change %q", t.Unit, j.ID)
	case t.Kind == TimerSetsid && !clean(t.PIDFile):
		return fmt.Errorf("pidfile %q", t.PIDFile)
	case t.Kind != TimerSystemd && t.Kind != TimerSetsid:
		return fmt.Errorf("kind %q", t.Kind)
	}
	return nil
}

// CheckExecutable refuses, with ErrExecutable, a hardn binary the timer
// could not run safely as root once hardn has exited: path must be
// absolute and clean (os.Executable, resolved with filepath.EvalSymlinks),
// a regular file with an execute bit, and it and every directory above it
// owned by root, not symlinks, and writable by neither group nor others.
// Otherwise a user who can replace the file, or rename a directory on the
// way, chooses what root runs when the timer fires.
func CheckExecutable(ctx context.Context, sys host.System, path string) error {
	if !clean(path) {
		return fmt.Errorf("revert: hardn's path %q: absolute and clean: %w", path, ErrExecutable)
	}
	for p := path; ; p = filepath.Dir(p) {
		fi, err := sys.Lstat(ctx, p)
		if err != nil {
			return fmt.Errorf("revert: hardn's path: %w", err)
		}
		uid, _, ok := host.Owner(fi)
		switch {
		case p == path && !fi.Mode().IsRegular():
			return fmt.Errorf("revert: %s: not a regular file (resolve symlinks first): %w", p, ErrExecutable)
		case p == path && fi.Mode().Perm()&0o111 == 0:
			return fmt.Errorf("revert: %s: not executable: %w", p, ErrExecutable)
		case p != path && !fi.IsDir():
			return fmt.Errorf("revert: %s: not a directory: %w", p, ErrExecutable)
		case !ok || uid != 0 || fi.Mode().Perm()&0o022 != 0:
			return fmt.Errorf("revert: %s: %w: %w", p, ErrUnsafe, ErrExecutable)
		}
		if p == "/" {
			return nil
		}
	}
}

// startTimer starts t; for the setsid timer it writes the pidfile.
func startTimer(ctx context.Context, sys host.System, t Timer, opts Options) error {
	if t.Kind == TimerSystemd {
		_, err := sys.Run(ctx, t.Argv[0], t.Argv[1:]...)
		return err
	}
	if err := stateDir(ctx, sys, filepath.Dir(t.PIDFile), true); err != nil {
		return err
	}
	pid, err := sys.StartDetached(ctx, t.Argv[0], t.Argv[1:]...)
	if err != nil {
		return err
	}
	if err := sys.WriteFileAtomic(ctx, t.PIDFile, []byte(strconv.Itoa(pid)+"\n"), 0o600, -1, -1); err != nil {
		// The sleeper's rollback would find nothing pending once Arm
		// removes the journal; stopping it is tidiness.
		_, killErr := sys.Run(ctx, "kill", "-s", "TERM", strconv.Itoa(pid))
		return errors.Join(err, killErr)
	}
	opts.logf("timer: sleeper pid %d, pidfile %s", pid, t.PIDFile)
	return nil
}

// cancelTimer stops t, so that it does not fire. Its failure is harmless:
// the rollback the timer runs finds the change no longer pending.
func cancelTimer(ctx context.Context, sys host.System, t Timer) error {
	if t.Kind == TimerSystemd {
		_, err := sys.Run(ctx, "systemctl", "stop", t.Unit+".timer")
		return err
	}
	data, _, err := sys.ReadFileNoFollow(ctx, t.PIDFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return fmt.Errorf("revert: %s: pid %q", t.PIDFile, data)
	}
	// Signal it only if that pid is still the sleeper: its command line is
	// exactly the timer's argv, so a pid reused since is left alone.
	cmdline, err := sys.ReadFile(ctx, ProcDir+"/"+strconv.Itoa(pid)+"/cmdline")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// It has ended.
	case err != nil:
		return err
	case bytes.Equal(cmdline, []byte(strings.Join(t.Argv, "\x00")+"\x00")):
		if _, err := sys.Run(ctx, "kill", "-s", "TERM", strconv.Itoa(pid)); err != nil {
			return err
		}
	}
	return sys.Remove(ctx, t.PIDFile)
}
