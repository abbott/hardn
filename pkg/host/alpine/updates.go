package alpine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// ScriptState is what is at Script. The zero value is Unknown.
type ScriptState uint8

const (
	ScriptUnknown   ScriptState = iota
	ScriptAbsent                // nothing there
	ScriptInstalled             // a regular file with an execute bit, as apk-cron installs it
	ScriptOther                 // anything else (no execute bit, a symlink, a directory)
)

// CrondState is crond's state, as rc-service crond status gives it. The zero
// value is Unknown.
type CrondState uint8

const (
	CrondUnknown CrondState = iota // not established, or starting, stopping, inactive
	CrondMissing                   // no InitScript
	CrondStopped                   // status exit 3
	CrondStarted                   // status exit 0
	CrondCrashed                   // status exit 32: marked started, its daemon gone
)

func (s CrondState) String() string {
	return word(uint8(s), "unknown", "missing", "stopped", "started", "crashed")
}

// word is words[i], or words[0] ("unknown") for a value out of range, such
// as one read back from a journal.
func word(i uint8, words ...string) string {
	if int(i) < len(words) {
		return words[i]
	}
	return words[0]
}

// Legacy is what is at LegacyScript. The zero value is Unknown.
type Legacy uint8

const (
	LegacyUnknown Legacy = iota
	LegacyAbsent
	LegacyHardn // a regular file holding exactly LegacyContent that only root can change
	LegacyOther // anything else, left alone
)

// File is a regular file as read: content, mode and owner.
type File struct {
	Data     []byte
	Mode     fs.FileMode
	UID, GID int
}

// Updates is what ReadUpdates found. Branch on the fields: Err explains
// whichever is Unknown, joined.
type Updates struct {
	Package    detect.Tri // apk-cron installed (apk info -e)
	Script     ScriptState
	Runlevel   detect.Tri // RunlevelLink resolves to a file, as OpenRC tests it
	Crond      CrondState
	Legacy     Legacy
	LegacyFile File // LegacyHardn: as read, for the rollback
	Evidence   []string
	Err        error
}

// ReadUpdates reads what Status and PlanUpdates need, and only reads: apk
// info -e apk-cron, rc-service crond status, and Script, InitScript,
// RunlevelLink and LegacyScript. It refuses a host that is not Alpine
// (ErrNotAlpine, or ErrUnknown when the distribution is not established),
// one whose init system is not established (ErrUnknown, wrapping detect's
// error), and one that OpenRC did not boot (ErrNoOpenRC: an Alpine
// container image has OpenRC installed but not running, and no crond). A
// fact it cannot establish is Unknown, with Updates.Err; its error is
// otherwise only ctx's.
func ReadUpdates(ctx context.Context, sys host.System, f detect.Facts) (Updates, error) {
	if err := alpineOnly(f); err != nil {
		return Updates{}, err
	}
	switch in := f.Init; {
	case in.Kind == detect.InitUnknown || in.Running == detect.Unknown:
		return Updates{}, fmt.Errorf("alpine: init system: %w: %w", ErrUnknown, errOr(in.Err, detect.ErrNoSignal))
	case in.Kind != detect.OpenRC || in.Running != detect.Yes:
		return Updates{}, fmt.Errorf("alpine: init %s, running %s (%s): %w", in.Kind, in.Running, in.Source, ErrNoOpenRC)
	}
	u := read(ctx, sys)
	if err := ctx.Err(); err != nil {
		return u, fmt.Errorf("alpine: %w", err)
	}
	return u, nil
}

func read(ctx context.Context, sys host.System) Updates {
	var u Updates
	var errs []error
	note := func(format string, a ...any) { u.Evidence = append(u.Evidence, fmt.Sprintf(format, a...)) }

	var err error
	if u.Package, err = installed(ctx, sys); err != nil {
		errs = append(errs, err)
	}
	note("apk info -e %s: installed %s", Package, u.Package)

	if u.Script, err = script(ctx, sys); err != nil {
		errs = append(errs, err)
	}
	note("%s: %s", Script, word(uint8(u.Script), "unknown", "absent", "executable file", "not an executable file"))

	u.Crond = CrondMissing
	switch fi, err := sys.Lstat(ctx, InitScript); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		u.Crond = CrondUnknown
		errs = append(errs, err)
	case !fi.Mode().IsRegular():
		u.Crond = CrondUnknown
		errs = append(errs, fmt.Errorf("%s: not a regular file: %w", InitScript, detect.ErrUnrecognised))
	default:
		if u.Crond, err = status(ctx, sys); err != nil {
			errs = append(errs, err)
		}
	}
	note("rc-service %s status: %s", Service, u.Crond)

	switch _, err := sys.Stat(ctx, RunlevelLink); {
	case err == nil:
		u.Runlevel = detect.Yes
	case errors.Is(err, fs.ErrNotExist):
		u.Runlevel = detect.No
	default:
		errs = append(errs, err)
	}
	note("%s: in runlevel %s", RunlevelLink, u.Runlevel)

	if u.Legacy, u.LegacyFile, err = legacy(ctx, sys); err != nil {
		errs = append(errs, err)
	}
	note("%s: %s", LegacyScript, word(uint8(u.Legacy), "unknown", "absent", "hardn's (origin/main)", "not hardn's"))
	u.Err = errors.Join(errs...)
	return u
}

// installed reads apk info -e: the exit status is the number of names not
// installed (apk-info(8), info_exists), and an installed name is printed
// as itself, as name-version at a higher verbosity and not at all at a
// lower one (info_print_pkg_oneline; /etc/apk/config can set the
// verbosity, apk(8)). Exit 0 with one of those outputs is Yes, exit 1 with
// no output on either stream is No; anything else is Unknown.
func installed(ctx context.Context, sys host.System) (detect.Tri, error) {
	res, err := sys.Run(ctx, "apk", "info", "-e", Package)
	var exit *host.ExitError
	out := strings.TrimSuffix(string(res.Stdout), "\n")
	switch {
	case err == nil && (out == "" || out == Package || versioned(out)):
		return detect.Yes, nil
	case errors.As(err, &exit) && exit.ExitCode == 1 && len(res.Stdout) == 0 && len(res.Stderr) == 0:
		return detect.No, nil
	case err == nil:
		err = fmt.Errorf("output %q: %w", res.Stdout, detect.ErrUnrecognised)
	}
	return detect.Unknown, fmt.Errorf("apk info -e %s: %w", Package, err)
}

// versioned: s is Package-<version>, the version starting with a digit,
// on one line.
func versioned(s string) bool {
	v, ok := strings.CutPrefix(s, Package+"-")
	return ok && v != "" && v[0] >= '0' && v[0] <= '9' && !strings.ContainsAny(v, " \n")
}

// script reads Script: run-parts runs a regular file only when it is
// executable.
func script(ctx context.Context, sys host.System) (ScriptState, error) {
	switch fi, err := sys.Lstat(ctx, Script); {
	case errors.Is(err, fs.ErrNotExist):
		return ScriptAbsent, nil
	case err != nil:
		return ScriptUnknown, err
	case fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0:
		return ScriptInstalled, nil
	}
	return ScriptOther, nil
}

// isLegacy: f is origin/main's file as hardn removes it and a rollback
// writes it back: exactly LegacyContent, permission bits only, owned by
// root and writable by neither group nor others, so that only root can
// have changed it, or change what a rollback restores. A Plan whose From
// says LegacyHardn with any other File is refused (fs.ErrInvalid).
func isLegacy(f File) bool {
	return bytes.Equal(f.Data, []byte(LegacyContent)) && f.Mode == f.Mode.Perm() && f.Mode&0o022 == 0 && f.UID == 0
}

// legacy reads LegacyScript: hardn's when it is a regular file for which
// isLegacy holds, read with ReadFileNoFollow, so that a link planted
// at it is never followed.
func legacy(ctx context.Context, sys host.System) (Legacy, File, error) {
	switch fi, err := sys.Lstat(ctx, LegacyScript); {
	case errors.Is(err, fs.ErrNotExist):
		return LegacyAbsent, File{}, nil
	case err != nil:
		return LegacyUnknown, File{}, err
	case !fi.Mode().IsRegular():
		return LegacyOther, File{}, nil
	}
	data, fi, err := sys.ReadFileNoFollow(ctx, LegacyScript)
	switch {
	case errors.Is(err, host.ErrSymlinkParent):
		return LegacyUnknown, File{}, err
	case errors.Is(err, host.ErrSymlink), errors.Is(err, fs.ErrInvalid):
		return LegacyOther, File{}, nil // replaced since the Lstat
	case errors.Is(err, fs.ErrNotExist):
		return LegacyAbsent, File{}, nil
	case err != nil:
		return LegacyUnknown, File{}, err
	case !bytes.Equal(data, []byte(LegacyContent)):
		return LegacyOther, File{}, nil
	}
	uid, gid, ok := host.Owner(fi)
	if !ok {
		return LegacyUnknown, File{}, fmt.Errorf("%s: no owner: %w", LegacyScript, detect.ErrUnrecognised)
	}
	f := File{Data: data, Mode: fi.Mode().Perm(), UID: uid, GID: gid}
	if !isLegacy(f) {
		return LegacyOther, File{}, nil // hardn's bytes, but a user other than root can change them
	}
	return LegacyHardn, f, nil
}

// status maps rc-service crond status's exit code as OpenRC's _status
// sets it: 0 started, 3 stopped, 32 crashed; 4, 8 and 16 (stopping,
// starting, inactive) and anything else are Unknown.
func status(ctx context.Context, sys host.System) (CrondState, error) {
	res, err := sys.Run(ctx, "rc-service", Service, "status")
	var exit *host.ExitError
	switch {
	case err == nil:
		return CrondStarted, nil
	case !errors.As(err, &exit):
		return CrondUnknown, fmt.Errorf("rc-service %s status: %w", Service, err)
	case exit.ExitCode == 3:
		return CrondStopped, nil
	case exit.ExitCode == 32:
		return CrondCrashed, nil
	}
	return CrondUnknown, fmt.Errorf("rc-service %s status: exit %d: %w", Service, res.ExitCode, detect.ErrUnrecognised)
}

// Status reports automatic updates: Configured when apk-cron is installed
// with its daily script, crond is in the default runlevel and crond is
// running; NotConfigured when apk-cron is not installed (whatever else
// runs: origin/main's script is a finding); Partial when apk-cron is
// installed and another condition fails; Unknown when a fact that decides
// is.
func (u Updates) Status() Status {
	s := Status{Evidence: u.Evidence, Findings: u.findings()}
	var missing []string
	switch u.Script {
	case ScriptAbsent, ScriptOther:
		missing = append(missing, Script+" is missing or not executable")
	}
	if u.Runlevel == detect.No {
		missing = append(missing, "crond is not in the default runlevel")
	}
	switch u.Crond {
	case CrondMissing:
		missing = append(missing, "crond is not installed")
	case CrondStopped:
		missing = append(missing, "crond is not running")
	case CrondCrashed:
		missing = append(missing, "crond has crashed")
	}
	unknown := u.Script == ScriptUnknown || u.Runlevel == detect.Unknown || u.Crond == CrondUnknown
	switch {
	case u.Package == detect.No:
		s.State, s.Detail = StateNotConfigured, Package+" is not installed"
	case u.Package == detect.Unknown, len(missing) == 0 && unknown:
		s.State, s.Err = StateUnknown, fmt.Errorf("alpine: updates: %w: %w", ErrUnknown, errOr(u.Err, detect.ErrNoSignal))
	case len(missing) > 0:
		s.State, s.Detail = StatePartial, strings.Join(missing, "; ")
	default:
		s.State, s.Detail = StateConfigured, Package+" daily, crond running and in the default runlevel"
	}
	return s
}

func (u Updates) findings() []Finding {
	switch u.Legacy {
	case LegacyHardn:
		return []Finding{{Kind: FindingLegacyScript, Path: LegacyScript,
			Detail: "written by an earlier hardn; removed, after a backup, once " + Package + " is installed"}}
	case LegacyOther:
		return []Finding{{Kind: FindingForeignScript, Path: LegacyScript,
			Detail: "not the file hardn wrote (other content, not a regular file, or an owner or mode that lets a user other than root change it); " +
				"left alone (if it upgrades packages, two daily jobs do)"}}
	}
	return nil
}
