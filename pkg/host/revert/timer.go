package revert

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
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
// program text. A shell may exec the last command of -c (dash does), so
// once the sleep is over the pidfile's pid may be the rollback itself. What
// keeps a cancel from killing a rollback that has started is cancelTimer's
// check that the pid's command line is still this one.
const SleeperScript = `sleep "$1" && "$0" rollback "$2"`

// Timer is the armed revert timer, in the journal.
type Timer struct {
	Kind     TimerKind `json:"kind"`
	Unit     string    `json:"unit,omitempty"`    // systemd-run: hardn-revert-<id>
	PIDFile  string    `json:"pidFile,omitempty"` // setsid: <RunDir>/revert-<id>.pid
	Argv     []string  `json:"argv"`              // what Arm started
	Deadline time.Time `json:"deadline"`
	// Hardn is the hardn binary the timer runs, HardnSHA256 the SHA-256 of
	// its content when the timer was started, and Boot the boot's id then
	// (/proc/sys/kernel/random/boot_id). Since next/p3-revert-boot (P-34
	// (c)): `hardn rollback --boot` acts only on a journal whose timer runs
	// the hardn it is, by path and content, and was started in an earlier
	// boot (Boot). A journal written before has none of them, and the boot
	// command leaves it to the next run's notice (Boot).
	Hardn       string `json:"hardn,omitempty"`
	HardnSHA256 string `json:"hardnSHA256,omitempty"`
	Boot        string `json:"boot,omitempty"`
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
	}
	return timerFor(in, id, hardn, int(timeout/time.Second), runDir)
}

// timerFor is TimerFor for a timer of secs seconds, which may be under
// MinTimeout: the re-armed timer of a change, at boot (Boot), runs for
// what remains of its window.
func timerFor(in detect.Init, id, hardn string, secs int, runDir string) (Timer, error) {
	switch {
	case !ValidID(id):
		return Timer{}, fmt.Errorf("revert: timer: %q: %w", id, ErrID)
	case !clean(hardn):
		return Timer{}, fmt.Errorf("revert: timer: hardn's path %q: %w", hardn, ErrExecutable)
	case secs < 1:
		return Timer{}, fmt.Errorf("revert: timer: %ds: %w", secs, ErrTimeout)
	case in.Running != detect.Yes:
		return Timer{}, fmt.Errorf("revert: timer: init %s, running %s: %w", in.Kind, in.Running, ErrNoTimer)
	}
	n := strconv.Itoa(secs)
	switch in.Kind {
	case detect.Systemd:
		unit := UnitPrefix + id
		return Timer{Kind: TimerSystemd, Unit: unit, Argv: []string{
			"systemd-run", "--unit=" + unit, "--description=hardn: roll back change " + id + " unless confirmed",
			"--on-active=" + n + "s", "--timer-property=AccuracySec=1s", hardn, "rollback", id,
		}}, nil
	case detect.OpenRC:
		if !clean(runDir) {
			return Timer{}, fmt.Errorf("revert: timer: run directory %q: %w", runDir, fs.ErrInvalid)
		}
		return Timer{Kind: TimerSetsid, PIDFile: runDir + "/revert-" + id + ".pid",
			Argv: []string{"/bin/sh", "-c", SleeperScript, hardn, n, id}}, nil
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
	case (t.Hardn == "") != (t.HardnSHA256 == ""):
		return errors.New("hardn and hardnSHA256 go together")
	case t.Hardn != "" && (!clean(t.Hardn) || !slices.Contains(t.Argv, t.Hardn)):
		return fmt.Errorf("hardn %q is not the binary of argv %q", t.Hardn, t.Argv)
	case t.HardnSHA256 != "" && !isSHA256(t.HardnSHA256):
		return fmt.Errorf("hardnSHA256 %q", t.HardnSHA256)
	case len(t.Boot) > 64 || strings.ContainsAny(t.Boot, " \t\r\n"):
		return fmt.Errorf("boot %q", t.Boot)
	}
	return nil
}

// isSHA256 reports whether s is a SHA-256 in lower-case hex.
func isSHA256(s string) bool {
	if len(s) != 2*sha256.Size {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// hashFile is the SHA-256, in hex, of the regular file name, read without
// following a symlink at name: what Timer.HardnSHA256 records of the hardn
// binary the timer runs.
func hashFile(ctx context.Context, sys host.System, name string) (string, error) {
	data, _, err := sys.ReadFileNoFollow(ctx, name)
	if err != nil {
		return "", fmt.Errorf("revert: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// bootID is this boot's id (random(4): generated once per boot).
func bootID(ctx context.Context, sys host.System) (string, error) {
	data, err := sys.ReadFile(ctx, bootIDFile)
	if err != nil {
		return "", fmt.Errorf("revert: %w", err)
	}
	id := strings.TrimSpace(string(data))
	if id == "" || len(id) > 64 || strings.ContainsAny(id, " \t\r\n") {
		return "", fmt.Errorf("revert: %s: %q is not a boot id: %w", bootIDFile, data, fs.ErrInvalid)
	}
	return id, nil
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
		// removes the journal; stopping it is tidiness, after Ctrl-C too.
		cctx, cancel := cleanupContext(ctx)
		defer cancel()
		_, killErr := sys.Run(cctx, "kill", "-s", "TERM", strconv.Itoa(pid))
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
	// exactly the timer's argv, so a pid reused since, and a shell that has
	// exec'd the rollback (SleeperScript), are left alone.
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
