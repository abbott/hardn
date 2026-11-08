package alpine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// The openssh upgrade (the maintainer's decisions of 2026-11-08;
// docs/safety/alpine.md §7.1): every
// installed package of Alpine's openssh build upgraded together, as their
// exact-version dependencies need (aports main/openssh/APKBUILD:
// openssh-server, -pam and -krb5 depend on openssh-keygen=$pkgver-r$pkgrel
// and openssh-server-common=$pkgver-r$pkgrel, the clients on keygen and
// client-common likewise; `apk upgrade openssh` alone upgrades only the
// meta package, alpine.md §7), once apk's own simulation lists nothing but
// those packages' upgrades and the server's to its branch's fix or later.
// There is no undo: a stable branch's repository keeps only its newest
// pkgrel, so the old packages are gone once replaced.

// Errors of the upgrade, testable with errors.Is.
var (
	// ErrNotOpenSSH: apk upgrade --simulate lists a change that is not
	// the upgrade of an installed openssh package (a library's upgrade
	// the new packages need, a world file edited by hand, say); hardn
	// upgrades nothing then (*OtherChangesError names them).
	ErrNotOpenSSH = errors.New("apk would change packages other than openssh's")
	// ErrNoFixOffered: apk's package lists, once updated, offer no
	// openssh-server with the fix: no upgrade listed for it, or one
	// to a version before the branch's first fixed one.
	ErrNoFixOffered = errors.New("apk's package lists offer no openssh-server with the fix")
	// ErrUpdate: apk update failed, so the package lists may be old.
	ErrUpdate = errors.New("apk could not update its package lists")
	// ErrPackagesChanged: the installed openssh packages are not those
	// the plan read.
	ErrPackagesChanged = errors.New("the openssh packages changed since they were read")
)

// OtherChangesError is ErrNotOpenSSH with the changes apk listed that are
// not openssh's, as apk lists them ("Upgrading libcrypto3 (3.5.4-r0 ->
// 3.5.5-r0)").
type OtherChangesError struct{ Changes []string }

func (e *OtherChangesError) Error() string {
	return ErrNotOpenSSH.Error() + ": " + strings.Join(e.Changes, "; ")
}

// Is makes errors.Is(e, ErrNotOpenSSH) true.
func (e *OtherChangesError) Is(target error) bool { return target == ErrNotOpenSSH }

// Names are the packages of the changes, each once: the second word of
// "Upgrading x (ver)", or the change itself when it has one word.
func (e *OtherChangesError) Names() []string {
	var out []string
	for _, c := range e.Changes {
		f := strings.Fields(c)
		name := c
		if len(f) > 1 {
			name = f[1]
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// OpenSSHBuild reports whether name is a package of Alpine's openssh build:
// the openssh meta package or one of its subpackages ("openssh-server",
// "openssh-keygen"; aports main/openssh/APKBUILD's subpackages).
func OpenSSHBuild(name string) bool { return name == "openssh" || strings.HasPrefix(name, "openssh-") }

// Upgrade is the plan of the openssh upgrade, as PlanUpgrade read it.
type Upgrade struct {
	// From is the server package's fix as read: Fixed No where there is
	// anything to upgrade.
	From Fix
	// Versions are the installed packages of the openssh build
	// (OpenSSHBuild) and their versions, as read; nil when there is
	// nothing to upgrade.
	Versions map[string]string
}

// Empty reports whether the plan upgrades nothing: the server has the fix.
func (u Upgrade) Empty() bool { return len(u.Versions) == 0 }

// Packages are the packages the upgrade names, sorted.
func (u Upgrade) Packages() []string {
	out := make([]string, 0, len(u.Versions))
	for n := range u.Versions {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// Commands are what Apply runs, in order, as argv: apk update, then apk
// upgrade naming every package (the simulation before it, Simulate, is a
// read).
func (u Upgrade) Commands() [][]string {
	if u.Empty() {
		return nil
	}
	return [][]string{{"apk", "update"}, append([]string{"apk", "upgrade"}, u.Packages()...)}
}

// Simulate is apk upgrade --simulate with the same packages, which changes
// nothing (apk(8)).
func (u Upgrade) Simulate() []string {
	return append([]string{"apk", "upgrade", "--simulate"}, u.Packages()...)
}

// PlanUpgrade plans the upgrade of server (OpenSSHServer, or the -pam or
// -krb5 build the init script runs) on the branch of versionID, detect's
// OS.VersionID. It reads InstalledDB and changes nothing. A server with the
// fix plans nothing; one whose fix is not established is ErrUnknown,
// wrapping why (Fix.Why), never a guess.
func PlanUpgrade(ctx context.Context, sys host.System, server, versionID string) (Upgrade, error) {
	f := OpenSSHFix(ctx, sys, server, versionID)
	switch f.Fixed {
	case detect.Yes:
		return Upgrade{From: f}, nil
	case detect.Unknown:
		return Upgrade{From: f}, fmt.Errorf("alpine: %s: %s: %w", server, f.Why, ErrUnknown)
	}
	db, err := ReadInstalled(ctx, sys)
	if err != nil {
		return Upgrade{From: f}, fmt.Errorf("%w: %w", err, ErrUnknown)
	}
	return Upgrade{From: f, Versions: build(db)}, nil
}

// build is db's packages of the openssh build, with their versions.
func build(db map[string]string) map[string]string {
	out := map[string]string{}
	for n, v := range db {
		if OpenSSHBuild(n) {
			out[n] = v
		}
	}
	return out
}

// UpgradeResult is what ApplyUpgrade did.
type UpgradeResult struct {
	// Updated: apk update ran and succeeded.
	Updated bool
	// Changes are the simulation's changes, as apk listed them, once
	// accepted.
	Changes []string
	// Upgraded: apk upgrade ran (whatever its exit status).
	Upgraded bool
	// Fix is the server's fix read after the upgrade.
	Fix Fix
}

// upgradeLine is one change of the simulation that ApplyUpgrade accepts:
// "Upgrading <name> (<old> -> <new>)" (apk-tools v3.0.8 src/commit.c,
// print_change; alpine.md §3).
var upgradeLine = regexp.MustCompile(`^Upgrading (\S+) \((\S+) -> (\S+)\)$`)

// ApplyUpgrade runs u on the branch of versionID: it reads InstalledDB
// again and refuses a host that changed since u (ErrPackagesChanged); runs apk
// update (ErrUpdate); runs Simulate, and refuses, upgrading nothing, unless
// every change it lists is the upgrade of a package of u from the version
// read (*OtherChangesError otherwise, detect.ErrUnrecognised for output
// that lists no change and does not say all is up to date) and the
// server's is to its branch's first fixed version or later
// (ErrNoFixOffered); then runs apk upgrade, and checks that the server
// then has the fix (ErrNotEffective). Nothing is undone: an upgrade has no
// undo (the old packages are gone from the repository). An empty u runs
// nothing.
func ApplyUpgrade(ctx context.Context, sys host.System, u Upgrade, versionID string) (UpgradeResult, error) {
	var res UpgradeResult
	if err := ctx.Err(); err != nil || u.Empty() {
		return res, err
	}
	db, err := ReadInstalled(ctx, sys)
	if err != nil {
		return res, err
	}
	if now := build(db); !mapsEqual(now, u.Versions) {
		return res, fmt.Errorf("alpine: the openssh packages are %s, not %s as read: %w", versionWords(now), versionWords(u.Versions), ErrPackagesChanged)
	}
	if _, err := sys.Run(ctx, "apk", "update"); err != nil {
		return res, fmt.Errorf("alpine: %w: %w", ErrUpdate, err)
	}
	res.Updated = true
	sim := u.Simulate()
	out, err := sys.Run(ctx, sim[0], sim[1:]...)
	if err != nil {
		return res, fmt.Errorf("alpine: %w", err)
	}
	cs := Changes(out.Combined)
	if err := u.accept(cs, out.Combined); err != nil {
		return res, fmt.Errorf("alpine: %s: %w", host.FormatArgv(sim), err)
	}
	res.Changes = cs
	cmds := u.Commands()
	up := cmds[len(cmds)-1]
	res.Upgraded = true
	if _, err := sys.Run(ctx, up[0], up[1:]...); err != nil {
		res.Fix = OpenSSHFix(ctx, sys, u.From.Package, versionID)
		return res, fmt.Errorf("alpine: %w", err)
	}
	res.Fix = OpenSSHFix(ctx, sys, u.From.Package, versionID)
	if res.Fix.Fixed != detect.Yes {
		why := res.Fix.Why
		if why == "" {
			why = res.Fix.Package + " " + res.Fix.Installed + ", fixed in " + res.Fix.Wanted
		}
		return res, fmt.Errorf("alpine: after %s: %s: %w", host.FormatArgv(up), why, ErrNotEffective)
	}
	return res, nil
}

// accept checks the simulation's changes cs (out, its output): each the
// upgrade of a package of u from the version read, the server's among
// them, to its fix or later. No change listed is ErrNoFixOffered where apk
// printed its summary ("OK: ..."), which it prints with nothing to do, and
// detect.ErrUnrecognised otherwise.
func (u Upgrade) accept(cs []string, out []byte) error {
	var other []string
	server := ""
	for _, c := range cs {
		m := upgradeLine.FindStringSubmatch(c)
		if m == nil || u.Versions[m[1]] == "" || u.Versions[m[1]] != m[2] {
			other = append(other, c)
			continue
		}
		if m[1] == u.From.Package {
			server = m[3]
		}
	}
	switch {
	case len(other) > 0:
		return &OtherChangesError{Changes: other}
	case len(cs) == 0 && !summary(out):
		return fmt.Errorf("no package change listed: %w", detect.ErrUnrecognised)
	case server == "":
		return fmt.Errorf("no upgrade of %s %s listed: %w", u.From.Package, u.From.Installed, ErrNoFixOffered)
	}
	c, err := CompareVersion(server, u.From.Wanted)
	switch {
	case err != nil:
		return fmt.Errorf("%s %s: %w: %w", u.From.Package, server, err, ErrNoFixOffered)
	case c < 0:
		return fmt.Errorf("the newest %s is %s, and the fix is in %s: %w", u.From.Package, server, u.From.Wanted, ErrNoFixOffered)
	}
	return nil
}

// summary reports whether apk printed its closing "OK: <size> in <n>
// packages" line, as it does after a commit, a simulated one included,
// with nothing to change (the 2026-11-08 probe, apk-tools 3.0.8).
func summary(out []byte) bool {
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "OK: ") {
			return true
		}
	}
	return false
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// versionWords is m as "name version, …", sorted.
func versionWords(m map[string]string) string {
	var out []string
	for n, v := range m {
		out = append(out, n+" "+v)
	}
	slices.Sort(out)
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// SSHDPidFile is the listener's pid file under OpenRC (aports
// main/openssh/sshd.initd: pidfile="${SSHD_PIDFILE:-"/run/$RC_SVCNAME.pid"}";
// SSHD_PIDFILE is deprecated there).
const SSHDPidFile = "/run/sshd.pid"

// deletedSuffix is what the kernel appends to /proc/<pid>/exe once the
// file the process runs was removed or replaced (proc_pid_exe(5)).
const deletedSuffix = " (deleted)"

// ListenerBinary is the binary sshd's listener runs: /proc/<pid>/exe of
// SSHDPidFile's pid, and whether the kernel marks it deleted (Stale): the
// file it started from was replaced, by an upgrade, and the listener still
// runs the old one. It changes nothing. A pid file that does not hold a
// pid, and a process that is gone, are errors: not established.
func ListenerBinary(ctx context.Context, sys host.System) (path string, stale bool, err error) {
	data, err := sys.ReadFile(ctx, SSHDPidFile)
	if err != nil {
		return "", false, fmt.Errorf("alpine: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return "", false, fmt.Errorf("alpine: %s: %q is not a pid: %w", SSHDPidFile, strings.TrimSpace(string(data)), fs.ErrInvalid)
	}
	exe, err := sys.Readlink(ctx, "/proc/"+strconv.Itoa(pid)+"/exe")
	if err != nil {
		return "", false, fmt.Errorf("alpine: sshd's listener, pid %d: %w", pid, err)
	}
	if p, ok := strings.CutSuffix(exe, deletedSuffix); ok {
		return p, true, nil
	}
	return exe, false, nil
}
