// Package apparmor holds the AppArmor control (refactor plan Phase 4,
// next/p4-check-apparmor): Figma's AppArmor row. AppArmor is status only
// in v0.4 (the maintainer, 2026-10-20; proposals P-16, P-43): Check reads
// the host, and Plan returns engine.ErrNotMigrated (engine.CheckOnly),
// since hardn neither turns AppArmor on nor off.
//
// Check reads the kernel's own answer, never a configuration file: whether
// the AppArmor LSM is on (/sys/module/apparmor/parameters/enabled, through
// alpine.CheckAppArmor on Alpine), each loaded profile's mode
// (/sys/kernel/security/apparmor/profiles), the global mode
// (/sys/module/apparmor/parameters/mode), and whether AppArmor's service
// loads the profiles at the next boot (apparmor.service, or OpenRC's
// apparmor). Inside a container AppArmor is the host's (detect). The row
// shows one of the eight lines agreed in P-43 (3); a fact hardn cannot
// read is Unknown, "can't read", never a pass. docs/engine/check-apparmor.md
// has every line and the order in which they are decided.
package apparmor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
)

// What the control reads besides alpine.AppArmorEnabled (linux v6.18,
// security/apparmor/lsm.c and apparmorfs.c; check-apparmor.md §3).
const (
	modeParam = "/sys/module/apparmor/parameters/mode"   // the global profile mode; root only
	profiles  = "/sys/kernel/security/apparmor/profiles" // one line per loaded profile: "name (mode)"
	cmdline   = "/proc/cmdline"
	unit      = "apparmor.service"
	service   = "apparmor" // OpenRC's, from Alpine's apparmor package
)

// The profile modes the kernel names (aa_profile_mode_names). enforce and
// kill deny what a profile does not allow (kill also kills the task);
// complain, user and unconfined deny nothing.
var (
	enforcing = []string{"enforce", "kill"}
	logging   = []string{"complain", "unconfined", "user"}
)

// Controls is the AppArmor row, the only one of its area.
func Controls() []engine.Control { return []engine.Control{Enforce{}} }

// Enforce is AppArmor: whether AppArmor confines processes on the host.
// Configured is one profile or more enforcing, in a kernel with AppArmor
// on and in the global mode enforce, with AppArmor's service loading the
// profiles again at the next boot.
type Enforce struct{ engine.CheckOnly }

// ID is "apparmor.enforce".
func (Enforce) ID() string { return "apparmor.enforce" }

// Title is Figma's row label.
func (Enforce) Title() string { return "AppArmor" }

// Applies is false only in a container, which detect establishes:
// AppArmor there is the host kernel's (brief 2.5). A container check that
// is not established applies, and Check reports it Unknown.
func (Enforce) Applies(f detect.Facts) (bool, string) {
	if f.Container.Detected == detect.Yes {
		return false, "set by the host"
	}
	return true, ""
}

// Check reads, in this order, whether the LSM is on, the loaded profiles,
// the global mode and the boot service, and stops at the first that
// decides the line: off in the kernel before anything about profiles, no
// profiles before their modes, nothing enforcing before the next boot. A
// read that fails, or a value hardn does not rank, is Unknown, "can't
// read", with the error. Alpine without AppArmor is NotApplicable here
// rather than through Applies, which reads no file (check-apparmor.md
// §2.2).
func (Enforce) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	switch f.Container.Detected {
	case detect.Yes: // Applies' line, for a caller that does not ask it first
		return status(engine.NotApplicable, "set by the host", nil), nil
	case detect.Unknown: // /sys would show the host's kernel
		return unread(nil, fmt.Errorf("apparmor: container: %w", errOr(f.Container.Err, detect.ErrNoSignal)))
	}
	on, ev, err := lsm(ctx, sys, f)
	switch {
	case err != nil:
		return unread(ev, err)
	case on == detect.No:
		return off(ctx, sys, f, ev)
	}
	p, err := readProfiles(ctx, sys)
	if err != nil {
		return unread(ev, err)
	}
	if ev = append(ev, p.evidence()); p.total == 0 {
		return status(engine.NotConfigured, "no profiles loaded", ev), nil
	}
	data, err := sys.ReadFile(ctx, modeParam)
	if err != nil {
		return unread(ev, fmt.Errorf("apparmor: %w", err))
	}
	ev = append(ev, fmt.Sprintf("%s: %q", modeParam, data))
	switch mode := strings.TrimSuffix(string(data), "\n"); {
	case mode == "complain":
		return status(engine.Partial, "logging only", ev), nil
	case mode != "enforce":
		return unread(ev, fmt.Errorf("apparmor: global mode %q: not ranked: %w", mode, detect.ErrUnrecognised))
	case p.enforcing == 0 && len(p.other) > 0:
		return unread(ev, fmt.Errorf("apparmor: profile modes %q: not ranked: %w", p.other, detect.ErrUnrecognised))
	case p.enforcing == 0:
		return status(engine.Partial, "logging only", ev), nil
	}
	boot, bev, err := atBoot(ctx, sys, f.Init)
	ev = append(ev, bev...)
	switch {
	case err != nil:
		return unread(ev, err)
	case boot == detect.No:
		return status(engine.Partial, "off after restart", ev), nil
	}
	return status(engine.Configured, enforced(p.enforcing), ev), nil
}

// enforced is a Configured detail: "34 profiles enforced", "1 profile
// enforced" (P-43 (3); the singular is the one adjustment).
func enforced(n int) string {
	if n == 1 {
		return "1 profile enforced"
	}
	return strconv.Itoa(n) + " profiles enforced"
}

// Enforced is the count of enforcing profiles in a Configured status of
// this control, read back from its detail, for the risk level
// (next/p4-risk); ok is false for any other status.
func Enforced(st engine.Status) (n int, ok bool) {
	if st.State != engine.Configured {
		return 0, false
	}
	if st.Detail == enforced(1) {
		return 1, true
	}
	s, found := strings.CutSuffix(st.Detail, " profiles enforced")
	n, err := strconv.Atoi(s)
	if !found || err != nil || n < 2 || enforced(n) != st.Detail {
		return 0, false
	}
	return n, true
}

// lsm reads whether the AppArmor LSM is on: on Alpine through
// alpine.CheckAppArmor, elsewhere from the same file by its rules
// (readLSM). The evidence is the file as read, or that the module is
// absent.
func lsm(ctx context.Context, sys host.System, f detect.Facts) (detect.Tri, []string, error) {
	if f.Platform.Distro != detect.DistroAlpine {
		return readLSM(ctx, sys)
	}
	a, err := alpine.CheckAppArmor(ctx, sys, f)
	var ev []string
	if i := slices.IndexFunc(a.Evidence, func(l string) bool { return strings.HasPrefix(l, alpine.AppArmorEnabled+":") }); i >= 0 {
		ev = a.Evidence[i : i+1] // the bootloader files after it are for enabling, not the state
	} else if a.Enabled == detect.No {
		ev = []string{alpine.SysModule + "/apparmor: absent"}
	}
	if err == nil && a.Enabled == detect.Unknown {
		err = errOr(a.Err, detect.ErrUnrecognised)
	}
	return a.Enabled, ev, err
}

// readLSM reads whether the kernel has the AppArmor LSM on, by
// alpine.CheckAppArmor's rules for the same file: "Y" is on; "N" is off
// (apparmor=0, an lsm= without it, or a kernel whose built-in order leaves
// it out); with no such file, a /sys/module without apparmor is a kernel
// built without it, off; anything else is not established.
func readLSM(ctx context.Context, sys host.System) (detect.Tri, []string, error) {
	data, err := sys.ReadFile(ctx, alpine.AppArmorEnabled)
	switch {
	case err == nil:
		ev := []string{fmt.Sprintf("%s: %q", alpine.AppArmorEnabled, data)}
		switch string(data) {
		case "Y\n":
			return detect.Yes, ev, nil
		case "N\n":
			return detect.No, ev, nil
		}
		return detect.Unknown, ev, fmt.Errorf("apparmor: %s: %q: %w", alpine.AppArmorEnabled, data, detect.ErrUnrecognised)
	case !errors.Is(err, fs.ErrNotExist):
		return detect.Unknown, nil, fmt.Errorf("apparmor: %w", err)
	}
	mod := alpine.SysModule + "/apparmor"
	switch _, err := sys.Lstat(ctx, mod); {
	case err == nil:
		return detect.Unknown, nil, fmt.Errorf("apparmor: %s without %s: %w", mod, alpine.AppArmorEnabled, detect.ErrUnrecognised)
	case !errors.Is(err, fs.ErrNotExist):
		return detect.Unknown, nil, fmt.Errorf("apparmor: %w", err)
	}
	if _, err := sys.Stat(ctx, alpine.SysModule); err != nil {
		return detect.Unknown, nil, fmt.Errorf("apparmor: %w", err)
	}
	return detect.No, []string{mod + ": absent"}, nil
}

// off is the line of a kernel with the LSM off: Alpine's default on
// Alpine, which hardn does not change there (brief §2), and off in kernel
// elsewhere, with the command line's AppArmor parameters as evidence.
// Where whether the host is Alpine is not established, the two lines
// differ, so it is Unknown.
func off(ctx context.Context, sys host.System, f detect.Facts, ev []string) (engine.Status, error) {
	switch {
	case f.Platform.Distro == detect.DistroAlpine:
		return status(engine.NotApplicable, "Alpine default", ev), nil
	case f.Platform.Distro == detect.DistroUnknown && (!f.OS.Known() || f.OS.ID == string(detect.DistroAlpine)):
		return unread(ev, fmt.Errorf("apparmor: distribution: %w", errOr(f.Platform.Err, detect.ErrNoSignal)))
	}
	return status(engine.NotConfigured, "off in kernel", append(ev, bootParams(ctx, sys))), nil
}

// bootParams is the evidence of the kernel command line's AppArmor
// parameters, which say why the kernel left AppArmor off when one did. It
// decides nothing: the kernel's own parameter is the state, so a command
// line that cannot be read is said, not an error.
func bootParams(ctx context.Context, sys host.System) string {
	data, err := sys.ReadFile(ctx, cmdline)
	if err != nil {
		return cmdline + ": not read"
	}
	var ps []string
	for _, w := range strings.Fields(string(data)) {
		if k, _, ok := strings.Cut(w, "="); ok && (k == "apparmor" || k == "lsm" || k == "security") {
			ps = append(ps, w)
		}
	}
	if len(ps) == 0 {
		return cmdline + ": no apparmor=, lsm= or security="
	}
	return cmdline + ": " + strings.Join(ps, " ")
}

// loaded counts the loaded profiles by mode.
type loaded struct {
	total     int
	enforcing int
	modes     map[string]int
	other     []string // modes the kernel's list does not name, each once
}

// readProfiles reads the kernel's list of loaded profiles, as root sees
// it: one line per profile, "name (mode)", or ":ns:name (mode)" for a
// namespace below root's. A line in another form is not established.
func readProfiles(ctx context.Context, sys host.System) (loaded, error) {
	l := loaded{modes: map[string]int{}}
	data, err := sys.ReadFile(ctx, profiles)
	if err != nil {
		return l, fmt.Errorf("apparmor: %w", err)
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return l, nil // no profile loaded
	}
	for _, line := range strings.Split(text, "\n") {
		i := strings.LastIndex(line, " (")
		if i <= 0 || !strings.HasSuffix(line, ")") {
			return l, fmt.Errorf("apparmor: %s: line %q: %w", profiles, line, detect.ErrUnrecognised)
		}
		mode := line[i+2 : len(line)-1]
		if mode == "" || strings.ContainsAny(mode, " ()") {
			return l, fmt.Errorf("apparmor: %s: line %q: %w", profiles, line, detect.ErrUnrecognised)
		}
		l.total++
		l.modes[mode]++
		switch {
		case slices.Contains(enforcing, mode):
			l.enforcing++
		case !slices.Contains(logging, mode) && !slices.Contains(l.other, mode):
			l.other = append(l.other, mode)
		}
	}
	return l, nil
}

// evidence is the count of profiles in each mode, the kernel's modes
// first.
func (l loaded) evidence() string {
	if l.total == 0 {
		return profiles + ": none"
	}
	var s []string
	for _, m := range slices.Concat([]string{"enforce", "complain", "kill", "unconfined", "user"}, l.other) {
		if n := l.modes[m]; n > 0 {
			s = append(s, strconv.Itoa(n)+" "+m)
		}
	}
	return profiles + ": " + strings.Join(s, ", ")
}

// atBoot is whether AppArmor's own service loads the profiles at the next
// boot: apparmor.service enabled on systemd, OpenRC's apparmor in the
// sysinit, boot or default runlevel. Anything that does not say so either
// way is not established, with the error.
func atBoot(ctx context.Context, sys host.System, in detect.Init) (detect.Tri, []string, error) {
	switch {
	case in.Kind == detect.Systemd && in.Running == detect.Yes:
		st, err := detect.SystemctlState(ctx, sys, "is-enabled", unit)
		if err != nil {
			return detect.Unknown, nil, fmt.Errorf("apparmor: %w", err)
		}
		ev := []string{"systemctl is-enabled " + unit + ": " + st}
		t := detect.UnitState{Enabled: st}.IsEnabled()
		if t == detect.Unknown {
			return t, ev, fmt.Errorf("apparmor: %s is %s, which does not say whether it starts at boot: %w", unit, st, detect.ErrUnrecognised)
		}
		return t, ev, nil
	case in.Kind == detect.OpenRC && in.Running == detect.Yes:
		levels, t, err := detect.ServiceRunlevels(ctx, sys, service)
		if t == detect.Unknown {
			return t, nil, fmt.Errorf("apparmor: %w", errOr(err, detect.ErrNoSignal))
		}
		where := "none"
		if len(levels) > 0 {
			where = strings.Join(levels, ",")
		}
		ev := []string{detect.OpenRCRunlevels + ": " + service + " in " + where}
		booted := slices.ContainsFunc(levels, func(l string) bool { return l == "sysinit" || l == "boot" || l == "default" })
		switch {
		case t == detect.No:
			return t, ev, nil
		case !booted:
			return detect.Unknown, ev, fmt.Errorf("apparmor: %s only in runlevels %s, which a boot may not reach: %w", service, where, detect.ErrUnrecognised)
		}
		return detect.Yes, ev, nil
	}
	return detect.Unknown, nil, fmt.Errorf("apparmor: init system: %w", errOr(in.Err, detect.ErrNoSignal))
}

func status(s engine.State, detail string, ev []string) engine.Status {
	return engine.Status{State: s, Detail: detail, Evidence: ev}
}

// unread is the status of a check that could not establish the line:
// Unknown, "can't read", with what was read and the error, which
// Engine.Check reports (its code is pkg/output's).
func unread(ev []string, err error) (engine.Status, error) {
	return engine.Status{State: engine.Unknown, Detail: "can't read", Evidence: ev}, err
}

func errOr(err, def error) error {
	if err != nil {
		return err
	}
	return def
}
