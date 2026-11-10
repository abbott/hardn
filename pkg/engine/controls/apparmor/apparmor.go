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
// read, or a value it does not rank, is Unknown, "can't read", never a
// pass. docs/engine/check-apparmor.md has every line and the order in
// which they are decided.
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
	// SELinuxEnforce is selinuxfs's mode: "1" enforcing, "0" permissive;
	// absent when SELinux is disabled (Rocky Linux, AlmaLinux).
	SELinuxEnforce = "/sys/fs/selinux/enforce"
)

// The profile modes the kernel names (aa_profile_mode_names). enforce and
// kill deny what a profile does not allow (kill also kills the task);
// complain and user deny nothing and log, and the kernel does not mediate
// an unconfined-mode profile at all (check-apparmor.md §6 (j)).
var (
	enforcing = []string{"enforce", "kill"}
	logging   = []string{"complain", "unconfined", "user"}
)

// ID is the control's ID, stable once released.
const ID = "apparmor.enforce"

// The row's details, the wording agreed in P-43 (3); a Configured detail
// is the count of enforcing profiles (Enforced). Exported so that the risk
// level (next/p4-risk) and the gallery tell apart two lines of one state
// without a copy of the wording.
const (
	DetailLoggingOnly     = "logging only"       // Partial
	DetailOffAfterRestart = "off after restart"  // Partial
	DetailNoProfiles      = "no profiles loaded" // NotConfigured
	DetailOffInKernel     = "off in kernel"      // NotConfigured
	DetailByHost          = "set by the host"    // NotApplicable
	DetailAlpineDefault   = "Alpine default"     // NotApplicable
	DetailSELinux         = "uses SELinux"       // NotApplicable: Rocky Linux, AlmaLinux
	DetailCantRead        = "can't read"         // Unknown
)

// The finding codes of this package (pkg/output's registry holds them):
// a value the kernel or the init system documents, read, that this
// control does not rank, so the line is Unknown with the finding and no
// error (checks.md §1.5's second kind).
const (
	// CodeGlobalModeUnknown: AppArmor's global mode is kill or
	// unconfined, whose effect beside each profile's own mode is not
	// ranked.
	CodeGlobalModeUnknown = "APPARMOR_GLOBAL_MODE_UNKNOWN"
	// CodeBootUnknown: AppArmor's service is in a state that does not say
	// whether it loads the profiles at the next boot (a -runtime state,
	// static, indirect, ...; OpenRC's apparmor in another runlevel only).
	CodeBootUnknown = "APPARMOR_BOOT_UNKNOWN"
)

// nextBoot is what each is-enabled state systemctl(1) documents says of
// the next boot. enabled is an enabling link in /etc/systemd/system;
// disabled, masked, linked and not-found do not start it. The -runtime
// states are links in /run, gone at the next boot, which say nothing of
// what stays: systemd prints enabled-runtime even beside an enabling link
// that outlives the boot, a mask in /run hides the state under it, and
// linked-runtime can stand beside an enabling link in /usr/lib. Those, and
// static, indirect, generated, transient, alias and bad, are not ranked.
var nextBoot = map[string]detect.Tri{
	"enabled":  detect.Yes,
	"disabled": detect.No, "masked": detect.No, "linked": detect.No, "not-found": detect.No,
}

// Controls is the AppArmor row, the only one of its area.
func Controls() []engine.Control { return []engine.Control{Enforce{}} }

// Enforce is AppArmor: whether AppArmor confines processes on the host.
// Configured is one profile or more enforcing, in a kernel with AppArmor
// on and in the global mode enforce, with AppArmor's service loading the
// profiles again at the next boot.
type Enforce struct{ engine.CheckOnly }

// ID returns ID.
func (Enforce) ID() string { return ID }

// Title is Figma's row label.
func (Enforce) Title() string { return "AppArmor" }

// Applies is false only in a container, which detect establishes:
// AppArmor there is the host kernel's (brief 2.5). A container check that
// is not established applies, and Check reports it Unknown.
func (Enforce) Applies(f detect.Facts) (bool, string) {
	if f.Container.Detected == detect.Yes {
		return false, DetailByHost
	}
	return true, ""
}

// Check reads, in this order, whether the LSM is on, the loaded profiles,
// the global mode and the boot service, and stops at the first that
// decides the line: off in the kernel before anything about profiles, no
// profiles before their modes, nothing enforcing before the next boot. A
// read that fails, or a value in a form neither the kernel nor systemctl
// documents, is Unknown, "can't read", with the error; a documented value
// hardn does not rank is Unknown with a finding and no error. Alpine
// without AppArmor is NotApplicable here rather than through Applies,
// which reads no file (check-apparmor.md §2.2).
func (Enforce) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	switch f.Container.Detected {
	case detect.Yes: // Applies' line, for a caller that does not ask it first
		return status(engine.NotApplicable, DetailByHost, nil), nil
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
		return status(engine.NotConfigured, DetailNoProfiles, ev), nil
	}
	data, err := sys.ReadFile(ctx, modeParam)
	if err != nil {
		return unread(ev, fmt.Errorf("apparmor: %w", err))
	}
	ev = append(ev, fmt.Sprintf("%s: %q", modeParam, data))
	switch mode := strings.TrimSuffix(string(data), "\n"); {
	case mode == "complain":
		return status(engine.Partial, DetailLoggingOnly, ev), nil
	case mode == "enforce": // each profile's own mode decides
	case slices.Contains(enforcing, mode) || slices.Contains(logging, mode):
		return notRanked(ev, engine.Finding{Code: CodeGlobalModeUnknown, Detail: "global mode " + mode, Source: modeParam}), nil
	default:
		return unread(ev, fmt.Errorf("apparmor: %s: %q: %w", modeParam, mode, detect.ErrUnrecognised))
	}
	switch {
	case p.enforcing == 0 && len(p.other) > 0:
		return unread(ev, fmt.Errorf("apparmor: %s: modes %q: %w", profiles, p.other, detect.ErrUnrecognised))
	case p.enforcing == 0:
		return status(engine.Partial, DetailLoggingOnly, ev), nil
	}
	boot, bev, why, err := atBoot(ctx, sys, f.Init)
	ev = append(ev, bev...)
	switch {
	case err != nil:
		return unread(ev, err)
	case boot == detect.Unknown:
		return notRanked(ev, why), nil
	case boot == detect.No:
		return status(engine.Partial, DetailOffAfterRestart, ev), nil
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

// selinux reads whether SELinux enforces: SELinuxEnforce "1". "0"
// (permissive) and no selinuxfs (disabled) are not; anything else is not
// established. ev is the file as read.
func selinux(ctx context.Context, sys host.System) (on bool, ev string, err error) {
	data, err := sys.ReadFile(ctx, SELinuxEnforce)
	switch v := strings.TrimSpace(string(data)); {
	case errors.Is(err, fs.ErrNotExist):
		return false, SELinuxEnforce + ": absent", nil
	case err != nil:
		return false, SELinuxEnforce + ": not read", fmt.Errorf("apparmor: %w", err)
	case v == "1" || v == "0":
		return v == "1", fmt.Sprintf("%s: %q", SELinuxEnforce, data), nil
	default:
		return false, fmt.Sprintf("%s: %q", SELinuxEnforce, data), fmt.Errorf("apparmor: %s: %q: %w", SELinuxEnforce, data, detect.ErrUnrecognised)
	}
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
// Alpine, which hardn does not change there (brief §2); on Rocky Linux and
// AlmaLinux, whose kernels have SELinux and no AppArmor, Not Applicable
// while SELinux enforces (selinux), else off in kernel; and off in kernel
// elsewhere, with the command line's AppArmor parameters as evidence.
// Where whether the host is Alpine is not established, the two lines
// differ, so it is Unknown.
func off(ctx context.Context, sys host.System, f detect.Facts, ev []string) (engine.Status, error) {
	switch {
	case f.Platform.Distro == detect.DistroAlpine:
		return status(engine.NotApplicable, DetailAlpineDefault, ev), nil
	case f.Platform.Distro.EL():
		on, sev, err := selinux(ctx, sys)
		ev = append(ev, sev)
		switch {
		case err != nil:
			return unread(ev, err)
		case on:
			return status(engine.NotApplicable, DetailSELinux, ev), nil
		}
	case f.Platform.Distro == detect.DistroUnknown && (!f.OS.Known() || f.OS.ID == string(detect.DistroAlpine)):
		return unread(ev, fmt.Errorf("apparmor: distribution: %w", errOr(f.Platform.Err, detect.ErrNoSignal)))
	}
	return status(engine.NotConfigured, DetailOffInKernel, append(ev, bootParams(ctx, sys))), nil
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

// loaded counts the loaded profiles by mode, every namespace's, as
// aa-status counts them: a namespace below root's is not only a
// container's (on Ubuntu 26.04 glycin sandboxes its image loaders on the
// host in a namespace of its own, the Phase 4 VM run found), and the
// number on the status line is the one an administrator's aa-status
// shows. nested says how many of them are in other namespaces, for the
// evidence.
type loaded struct {
	total     int
	enforcing int
	modes     map[string]int
	other     []string // modes the kernel's list does not name, each once
	nested    int      // of total, the profiles of namespaces below root's
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
		if ns, ok := strings.CutPrefix(line, ":"); ok {
			if j := strings.Index(ns, ":"); j <= 0 || j+2 >= i { // ":ns:name": neither empty
				return l, fmt.Errorf("apparmor: %s: line %q: %w", profiles, line, detect.ErrUnrecognised)
			}
			l.nested++
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

// evidence is the count of the loaded profiles in each mode, the kernel's
// modes first, then how many of them are in other namespaces.
func (l loaded) evidence() string {
	var s []string
	for _, m := range slices.Concat([]string{"enforce", "complain", "kill", "unconfined", "user"}, l.other) {
		if n := l.modes[m]; n > 0 {
			s = append(s, strconv.Itoa(n)+" "+m)
		}
	}
	ev := profiles + ": " + strings.Join(s, ", ")
	if l.total == 0 {
		ev = profiles + ": none"
	}
	if l.nested > 0 {
		ev += "; " + strconv.Itoa(l.nested) + " of them in other namespaces"
	}
	return ev
}

// atBoot is whether AppArmor's own service loads the profiles at the next
// boot: apparmor.service enabled on systemd (nextBoot), OpenRC's apparmor
// in the sysinit, boot or default runlevel. A read that fails is the
// error; a state read that does not say either way is Unknown with the
// finding that says which.
func atBoot(ctx context.Context, sys host.System, in detect.Init) (detect.Tri, []string, engine.Finding, error) {
	var none engine.Finding
	switch {
	case in.Kind == detect.Systemd && in.Running == detect.Yes:
		st, err := detect.SystemctlIsEnabled(ctx, sys, unit)
		if err != nil {
			return detect.Unknown, nil, none, fmt.Errorf("apparmor: %w", err)
		}
		cmd := "systemctl is-enabled " + unit
		t, ok := nextBoot[st]
		if !ok {
			return detect.Unknown, []string{cmd + ": " + st}, engine.Finding{Code: CodeBootUnknown, Detail: unit + " " + st, Source: cmd}, nil
		}
		return t, []string{cmd + ": " + st}, none, nil
	case in.Kind == detect.OpenRC && in.Running == detect.Yes:
		levels, t, err := detect.ServiceRunlevels(ctx, sys, service)
		if t == detect.Unknown {
			return t, nil, none, fmt.Errorf("apparmor: %w", errOr(err, detect.ErrNoSignal))
		}
		where := "no runlevel"
		if len(levels) > 0 {
			where = strings.Join(levels, ",")
		}
		ev := []string{detect.OpenRCRunlevels + ": " + service + " in " + where}
		switch {
		case t == detect.No:
			return t, ev, none, nil
		case slices.ContainsFunc(levels, func(l string) bool { return l == "sysinit" || l == "boot" || l == "default" }):
			return detect.Yes, ev, none, nil
		case err != nil: // an entry not checked may hold it
			return detect.Unknown, ev, none, fmt.Errorf("apparmor: %w", err)
		}
		return detect.Unknown, ev, engine.Finding{Code: CodeBootUnknown, Detail: service + " in " + where + " only", Source: detect.OpenRCRunlevels}, nil
	}
	return detect.Unknown, nil, none, fmt.Errorf("apparmor: init system: %w", errOr(in.Err, detect.ErrNoSignal))
}

// status is a status with no error and no findings.
func status(s engine.State, detail string, ev []string) engine.Status {
	return engine.Status{State: s, Detail: detail, Evidence: ev}
}

// unread is the status of a check that could not establish the line:
// Unknown, "can't read", with what was read and the error, which
// Engine.Check reports (its code is pkg/output's).
func unread(ev []string, err error) (engine.Status, error) {
	return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: ev}, err
}

// notRanked is the status of a documented value read that the control
// does not rank: Unknown, "can't read", with the finding that says which,
// and no error (checks.md §1.5's second kind).
func notRanked(ev []string, why engine.Finding) engine.Status {
	return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: ev, Findings: []engine.Finding{why}}
}

func errOr(err, def error) error {
	if err != nil {
		return err
	}
	return def
}
