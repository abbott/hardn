package revert

// The boot unit (refactor plan P3, next/p3-revert-boot; P-34 (c), decided
// by the maintainer 2026-10-19): a unit hardn installs, before it arms the
// revert timer of a lockout-risk change, that runs `hardn rollback --boot`
// at boot (Boot). docs/safety/revert-boot.md has the guarantees.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/backup"
	"github.com/abbott/hardn/pkg/host/detect"
)

// BootUnitName names the boot unit: hardn-boot.service on systemd, the
// hardn-boot init script on OpenRC. It is outside UnitPrefix, so that no
// change id names it.
const BootUnitName = "hardn-boot"

// Where the boot unit goes, and the links that enable it.
const (
	SystemdUnitDir = "/etc/systemd/system"
	SystemdWants   = SystemdUnitDir + "/multi-user.target.wants" // systemctl enable's link, for WantedBy=multi-user.target
	OpenRCInitDir  = "/etc/init.d"
	OpenRCDefault  = detect.OpenRCRunlevels + "/default" // rc-update add's link, in the default runlevel
)

// ErrBootUnit: the boot unit could not be installed as planned (its copy
// refused by its check, its link missing after the enable), or not removed.
var ErrBootUnit = errors.New("the boot unit is not as planned")

// BootUnit is the boot unit for one init system, as PlanBootUnit plans it.
type BootUnit struct {
	Init detect.InitKind
	Path string      // the unit file or init script
	Data []byte      // its content
	Mode fs.FileMode // 0644 for the unit file, 0755 for the script; root's
	// Link is the link the enable command makes: in multi-user.target.wants
	// on systemd, in the default runlevel on OpenRC.
	Link string
	// Check validates a copy of Data, whose path is appended:
	// systemd-analyze verify, failing on a warning about this unit alone
	// (--recursive-errors=no; without it verify exits 0 whatever it
	// warns), or sh -n (the script's syntax).
	Check   []string
	Enable  []string
	Disable []string
}

// systemdBootUnit is hardn-boot.service; %s is hardn's path. It runs early,
// before sshd reads its files, so that a rollback at boot restores them
// first: ordered before ssh.service and ssh.socket, with no default
// dependencies (they would order it after basic.target, so after
// sockets.target and ssh.socket: a cycle with Before=ssh.socket), after the
// local file systems, sysinit.target (the transient timer a re-arm makes
// requires it) and ufw.service (the rollback's ufw commands find its rules
// loaded). Ordering only: sshd starts whatever it does. TimeoutStartSec is
// above Boot's own bounds together (bootStepTimeout, RollbackTimeout and
// cleanupTimeout, 6m30s), as the backstop for a hardn that hangs outside a
// command: sshd waits for this unit (TestBootUnitTimeout).
const systemdBootUnit = `# hardn's boot unit (P-34 (c); docs/safety/revert-boot.md). hardn installs
# it before it arms the revert timer of a change that could lock you out, and
# removes it at a boot that finds no change journalled.
#
# At boot, before sshd reads its files: re-arm the revert timer of a change
# waiting for "hardn confirm", for what remains of its window, or roll the
# change back when its deadline has passed. Ordering only: sshd starts
# whatever this unit does.
[Unit]
Description=hardn: re-arm or run the revert timer of an unconfirmed change
DefaultDependencies=no
After=local-fs.target sysinit.target ufw.service
Before=ssh.service ssh.socket shutdown.target
Conflicts=shutdown.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=%s rollback --boot
TimeoutStartSec=7min

[Install]
WantedBy=multi-user.target
`

// openRCBootUnit is the hardn-boot init script; %s is hardn's path. In the
// default runlevel, before sshd, after the local file systems and the
// firewall; ordering only, as on systemd.
const openRCBootUnit = `#!/sbin/openrc-run
# hardn's boot service (P-34 (c); docs/safety/revert-boot.md). hardn installs
# it before it arms the revert timer of a change that could lock you out, and
# removes it at a boot that finds no change journalled.
#
# At boot, before sshd starts: re-arm the revert timer of a change waiting
# for "hardn confirm", for what remains of its window, or roll the change
# back when its deadline has passed. Ordering only: sshd starts whatever
# this service does.

description="hardn: re-arm or run the revert timer of an unconfirmed change"

depend() {
	need localmount
	after bootmisc firewall
	before sshd
}

start() {
	ebegin "hardn: checking the change journal"
	%s rollback --boot
	eend $?
}
`

// unquoted reports whether p can stand in a unit file's ExecStart and in a
// shell script without quoting or escaping: letters, digits and "/._+-",
// so no space, no systemd specifier (%) and nothing the shell reads.
func unquoted(p string) bool {
	for _, c := range p {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '/' || c == '.' || c == '_' || c == '+' || c == '-') {
			return false
		}
	}
	return p != ""
}

// PlanBootUnit returns, with no I/O, the boot unit that runs `<hardn>
// rollback --boot` at boot on init system in: hardn-boot.service in
// /etc/systemd/system, enabled for multi-user.target with `systemctl
// --no-reload enable` (no daemon-reload now: systemd reads it at the next
// boot), checked with systemd-analyze verify --recursive-errors=no; or
// /etc/init.d/hardn-boot in
// the default runlevel (rc-update add), checked with sh -n. hardn is the
// binary the revert timer runs (Options.Hardn). An init system that is not
// running is ErrNoTimer, as for the timer; a path that the unit or the
// script could not name unquoted is ErrExecutable.
func PlanBootUnit(in detect.Init, hardn string) (BootUnit, error) {
	if !clean(hardn) || !unquoted(hardn) {
		return BootUnit{}, fmt.Errorf("revert: boot unit: hardn's path %q: absolute, clean, and only letters, digits and /._+-: %w", hardn, ErrExecutable)
	}
	if in.Running != detect.Yes {
		return BootUnit{}, fmt.Errorf("revert: boot unit: init %s, running %s: %w", in.Kind, in.Running, ErrNoTimer)
	}
	switch in.Kind {
	case detect.Systemd:
		name := BootUnitName + ".service"
		return BootUnit{Init: detect.Systemd, Path: SystemdUnitDir + "/" + name, Data: fmt.Appendf(nil, systemdBootUnit, hardn), Mode: 0o644,
			Link: SystemdWants + "/" + name, Check: []string{"systemd-analyze", "verify", "--recursive-errors=no"},
			Enable:  []string{"systemctl", "--no-reload", "enable", name},
			Disable: []string{"systemctl", "--no-reload", "disable", name}}, nil
	case detect.OpenRC:
		return BootUnit{Init: detect.OpenRC, Path: OpenRCInitDir + "/" + BootUnitName, Data: fmt.Appendf(nil, openRCBootUnit, hardn), Mode: 0o755,
			Link: OpenRCDefault + "/" + BootUnitName, Check: []string{"sh", "-n"},
			Enable:  []string{"rc-update", "add", BootUnitName, "default"},
			Disable: []string{"rc-update", "del", BootUnitName, "default"}}, nil
	}
	return BootUnit{}, fmt.Errorf("revert: boot unit: init %s: %w", in.Kind, ErrNoTimer)
}

// BootUnitState is what the host has of a boot unit.
type BootUnitState struct {
	File    bool // a file at Path
	Current bool // it holds Data, with Mode, a regular file root's
	Linked  bool // Link is a link to Path
	// Data, Mode and UID are the file's, when File, so that a plan can show
	// what a write replaces (pkg/diff against BootUnit.Data); UID is -1
	// when the owner cannot be read.
	Data []byte
	Mode fs.FileMode
	UID  int
}

// Installed reports whether the unit is installed as planned and enabled.
func (s BootUnitState) Installed() bool { return s.Current && s.Linked }

// ReadBootUnit reads what the host has of u, changing nothing.
func ReadBootUnit(ctx context.Context, sys host.System, u BootUnit) (BootUnitState, error) {
	s := BootUnitState{UID: -1}
	data, fi, err := sys.ReadFileNoFollow(ctx, u.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return s, fmt.Errorf("revert: boot unit: %w", err)
	default:
		uid, _, ok := host.Owner(fi)
		s.File, s.Data, s.Mode = true, data, fi.Mode()
		if ok {
			s.UID = uid
		}
		s.Current = bytes.Equal(data, u.Data) && fi.Mode().Perm() == u.Mode.Perm() && ok && uid == 0
	}
	lfi, err := sys.Lstat(ctx, u.Link)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return s, fmt.Errorf("revert: boot unit: %w", err)
	case lfi.Mode()&fs.ModeSymlink == 0:
		return s, nil // not a link: the enable command says what it is
	}
	target, err := sys.Readlink(ctx, u.Link)
	if err != nil {
		return s, fmt.Errorf("revert: boot unit: %w", err)
	}
	s.Linked = target == u.Path
	return s, nil
}

// BootUnitResult is what EnsureBootUnit did.
type BootUnitResult struct {
	Unit BootUnit
	// Already: installed and enabled as planned; nothing written or run.
	Already bool
	Wrote   bool // the file was written, after its copy passed Check
	Enabled bool // Enable ran
	// Backup is the backup of the file Wrote replaced, when there was one
	// and a backup run was given.
	Backup *backup.Result
}

// EnsureBootUnit installs and enables boot unit u (PlanBootUnit), the step
// before the first Arm of a lockout-risk change on a host. It refuses while
// a change is journalled (a *PendingError, Is(ErrPending), as Arm's), before
// it writes or runs anything: the unit is that change's boot-time recovery,
// and one rewritten for another hardn binary would leave it alone at boot.
// Idempotent: a
// unit installed and enabled as planned is left as it is (Already). A file
// that is missing or differs is replaced: a copy in work (a root-only
// directory of the run, as sshd.Validate's) is checked first (u.Check), a
// file already there is backed up into run when run is not nil (Optional:
// nothing rolls it back; the boot unit outlives the change), and the file is
// written atomically, root's, with u.Mode. Then u.Enable runs unless the
// link is there, and the link must then be there. An error means the unit
// may not run at the next boot: the caller does not arm the change
// (ErrBootUnit, or the step's own).
func EnsureBootUnit(ctx context.Context, sys host.System, u BootUnit, work string, run *backup.Run, opts Options) (BootUnitResult, error) {
	res := BootUnitResult{Unit: u}
	js, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return res, err
	case js.Busy():
		return res, fmt.Errorf("revert: boot unit %s not installed: %w", u.Path, &PendingError{State: js})
	}
	st, err := ReadBootUnit(ctx, sys, u)
	if err != nil {
		return res, err
	}
	if st.Installed() {
		res.Already = true
		opts.logf("boot unit %s: installed and enabled already", u.Path)
		return res, nil
	}
	if !st.Current {
		if err := checkBootUnit(ctx, sys, u, work, opts); err != nil {
			return res, err
		}
		if st.File && run != nil {
			b, err := run.Backup(ctx, u.Path, backup.Optional)
			if err != nil {
				return res, fmt.Errorf("revert: boot unit: %w", err)
			}
			res.Backup = &b
		}
		if err := sys.WriteFileAtomic(ctx, u.Path, u.Data, u.Mode, 0, 0); err != nil {
			return res, fmt.Errorf("revert: boot unit: %w", err)
		}
		res.Wrote = true
		opts.logf("boot unit %s: written (%04o, root's)", u.Path, uint32(u.Mode.Perm()))
	}
	if !st.Linked {
		if _, err := sys.Run(ctx, u.Enable[0], u.Enable[1:]...); err != nil {
			return res, fmt.Errorf("revert: boot unit: %s: %w", host.FormatArgv(u.Enable), err)
		}
		res.Enabled = true
		if st, err = ReadBootUnit(ctx, sys, u); err != nil {
			return res, err
		} else if !st.Linked {
			return res, fmt.Errorf("revert: boot unit: %s ran, and %s is not a link to %s: %w", host.FormatArgv(u.Enable), u.Link, u.Path, ErrBootUnit)
		}
		opts.logf("boot unit %s: enabled (%s)", u.Path, host.FormatArgv(u.Enable))
	}
	return res, nil
}

// checkBootUnit runs u.Check over a copy of u.Data in work, under its own
// file name (systemd names the unit after it), and removes the copy. A
// checker that is not installed (host.ErrNotFound) checks nothing: the
// unit is written unchecked, and the log says so; any other failure
// refuses it.
func checkBootUnit(ctx context.Context, sys host.System, u BootUnit, work string, opts Options) error {
	if !clean(work) {
		return fmt.Errorf("revert: boot unit: work directory %q: %w", work, fs.ErrInvalid)
	}
	copyName := filepath.Join(work, filepath.Base(u.Path))
	if err := sys.WriteFileAtomic(ctx, copyName, u.Data, u.Mode, 0, 0); err != nil {
		return fmt.Errorf("revert: boot unit: %w", err)
	}
	argv := append(append([]string(nil), u.Check...), copyName)
	_, err := sys.Run(ctx, argv[0], argv[1:]...)
	cctx, cancel := cleanupContext(ctx)
	defer cancel()
	rerr := removeIfThere(cctx, sys, copyName)
	switch {
	case errors.Is(err, host.ErrNotFound):
		opts.logf("boot unit %s: %s is not installed, so it is written unchecked", u.Path, u.Check[0])
	case err != nil:
		return errors.Join(fmt.Errorf("revert: boot unit: %s refused it: %w: %w", host.FormatArgv(argv), ErrBootUnit, err), rerr)
	}
	return rerr
}

// RemoveBootUnit disables and removes boot unit u. It refuses while a
// change is journalled (a *PendingError, Is(ErrPending)): the unit is what
// re-arms its timer after a reboot. `hardn rollback --boot` calls it at a
// boot that finds nothing journalled; an uninstall would too. A file at
// u.Path that does not hold u.Data (hand-edited, another hardn's, or not
// hardn's at all) is disabled but kept, and reported (ErrBootUnit): nothing
// backs it up here. It reports whether the unit is gone because of it; a
// unit that is not there is no error.
func RemoveBootUnit(ctx context.Context, sys host.System, u BootUnit, opts Options) (bool, error) {
	st, err := ReadState(ctx, sys, opts)
	switch {
	case err != nil:
		return false, err
	case st.Busy():
		return false, fmt.Errorf("revert: boot unit %s kept: %w", u.Path, &PendingError{State: st})
	}
	bs, err := ReadBootUnit(ctx, sys, u)
	if err != nil {
		return false, err
	}
	removed := false
	if bs.Linked {
		if _, err := sys.Run(ctx, u.Disable[0], u.Disable[1:]...); err != nil {
			return false, fmt.Errorf("revert: boot unit: %s: %w", host.FormatArgv(u.Disable), err)
		}
		removed = true
	}
	keep := bs.File && !bytes.Equal(bs.Data, u.Data)
	if bs.File && !keep {
		if err := removeIfThere(ctx, sys, u.Path); err != nil {
			return removed, fmt.Errorf("revert: boot unit: %w", err)
		}
		removed = true
	}
	if bs, err = ReadBootUnit(ctx, sys, u); err == nil && (bs.Linked || bs.File && !keep) {
		err = fmt.Errorf("revert: boot unit %s: still there after %s: %w", u.Path, host.FormatArgv(u.Disable), ErrBootUnit)
	}
	if err != nil {
		return removed, err
	}
	if keep {
		opts.logf("boot unit %s: disabled, and kept: it does not hold the unit hardn installs", u.Path)
		return false, fmt.Errorf("revert: boot unit %s does not hold the unit hardn installs (edited, or not hardn's), so it is kept, disabled: check it, then remove it by hand: %w", u.Path, ErrBootUnit)
	}
	if removed {
		opts.logf("boot unit %s: disabled and removed: no change is journalled", u.Path)
	}
	return removed, nil
}
