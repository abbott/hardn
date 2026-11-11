// Package packages installs one named package through apt-get, apk or dnf
// (refactor plan slice 4, decision 1; dnf for Rocky Linux and AlmaLinux,
// docs/engine/el-firewalld.md), for the firewall's Install UFW and Install
// firewalld, and for slice 7.
//
// PlanInstall only reads: a package already installed plans nothing, any other
// is simulated, and a simulation that would remove, upgrade or replace anything
// is refused, and so, from dnf, is one that brings a kernel. Execute runs the
// plan and checks the package is installed. Every
// command goes through host.System. Proxmox VE is the caller's refusal, and
// ToolFor refuses it too.
//
// docs/safety/packages.md describes the simulation rules, the logs they come
// from and what is written but not yet run on a Linux host.
package packages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Tool is the program that installs packages. The zero value is none.
type Tool uint8

const (
	Apt Tool = iota + 1 // apt-get, with dpkg-query to read
	Apk
	Dnf // dnf, with rpm to read (Rocky Linux, AlmaLinux)
)

// Errors the package returns, testable with errors.Is. A bad name or a plan
// that is not what PlanInstall makes wraps fs.ErrInvalid.
var (
	ErrNoTool = errors.New("hardn can't install packages on this system")
	// ErrNotInRepos: the tool cannot find the package. On Alpine the same
	// refusal covers a package no repository has, an index never fetched, a
	// community repository not enabled and no network: apk says "no such
	// package" for an empty index (alpine323-vm.log) and for no DNS
	// (alpine323-vm-2.log).
	ErrNotInRepos = errors.New("package not found in the repositories")
	// ErrNoRepos: dnf has no enabled repository to install from, as on an
	// unregistered RHEL host, whose repositories come with its
	// subscription (el-rhel.md §5).
	ErrNoRepos = errors.New("no enabled repositories to install from")
	// ErrOtherPackages: the simulation changes more than new packages
	// (*OtherPackagesError names the changes).
	ErrOtherPackages = errors.New("installing it would change other packages")
	// ErrVerify: the install ran, and the package does not read as installed.
	ErrVerify = errors.New("package not installed after the install")
	// ErrKernel: dnf's transaction installs a kernel of another release
	// than the running one beside the package (*KernelError names its
	// packages): an update the user didn't ask for, used at the next
	// restart (the maintainer, 2026-11-10: "Refuse"). On rocky10-test-110,
	// firewalld's weak dependencies ipset and iptables-nft needed
	// kernel-modules-extra, which the repository has only for recent
	// kernels, so the newest kernel came too (el-firewalld.md §1.1).
	ErrKernel = errors.New("installing it would also install a new kernel")
	// ErrLocked: another dnf holds its lock (exit 200, dnf's main.py).
	ErrLocked = errors.New("another package manager is running")
)

// KernelError is PlanInstall's refusal of a transaction that brings a
// kernel, with the kernel packages it lists. errors.Is finds ErrKernel.
type KernelError struct {
	Packages []string
}

func (e *KernelError) Error() string {
	return ErrKernel.Error() + ": " + strings.Join(e.Packages, ", ")
}

// Is reports target == ErrKernel.
func (e *KernelError) Is(target error) bool { return target == ErrKernel }

// OtherPackagesError is PlanInstall's refusal, with the changes as the tool's
// simulation lists them. errors.Is finds ErrOtherPackages.
type OtherPackagesError struct {
	Changes []string
}

func (e *OtherPackagesError) Error() string {
	return ErrOtherPackages.Error() + ": " + strings.Join(e.Changes, "; ")
}

// Is reports target == ErrOtherPackages.
func (e *OtherPackagesError) Is(target error) bool { return target == ErrOtherPackages }

// ToolFor returns the tool of f's distribution: apt-get on Debian and Ubuntu,
// apk on Alpine, dnf on Rocky Linux, AlmaLinux and RHEL. Anything else is
// ErrNoTool, Proxmox VE and a distribution not established among them, and
// one that is only like Debian: hardn never installs there.
func ToolFor(f detect.Facts) (Tool, error) {
	switch f.Platform.Distro {
	case detect.DistroDebian, detect.DistroUbuntu:
		return Apt, nil
	case detect.DistroAlpine:
		return Apk, nil
	case detect.DistroRocky, detect.DistroAlma, detect.DistroRHEL:
		return Dnf, nil
	}
	return 0, fmt.Errorf("packages: distribution %q: %w", f.Platform.Distro, ErrNoTool)
}

// dnfNoWeak keeps dnf from installing weak dependencies; dnfNoWait makes it
// exit 200 where another dnf holds its lock (ErrLocked) rather than wait
// for it (exit_on_lock, default False: dnf.conf(5)).
const (
	dnfNoWeak = "--setopt=install_weak_deps=False"
	dnfNoWait = "--setopt=exit_on_lock=True"
)

// validName is a package name hardn passes on. It cannot start with "-", so
// neither tool can read it as an option.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*$`)

// commands returns the install, its undo and its simulation; fs.ErrInvalid for
// a bad name or no tool.
func commands(t Tool, name string) (install, undo, simulate []string, err error) {
	if !validName.MatchString(name) {
		return nil, nil, nil, fmt.Errorf("packages: name %q: %w", name, fs.ErrInvalid)
	}
	switch t {
	case Apt:
		return []string{"apt-get", "install", "-y", name},
			[]string{"apt-get", "remove", "-y", name},
			[]string{"apt-get", "install", "-s", "-y", name}, nil
	case Apk:
		return []string{"apk", "add", name},
			[]string{"apk", "del", name},
			[]string{"apk", "add", "--simulate", name}, nil
	case Dnf:
		// No weak dependencies, in the simulation as in the install: two of
		// firewalld's bring a kernel (ErrKernel). --noautoremove: the undo
		// removes the package and leaves its dependencies, as apt-get
		// remove does (dnf(8): it sets clean_requirements_on_remove to
		// False).
		return []string{"dnf", "install", "-y", dnfNoWeak, dnfNoWait, name},
			[]string{"dnf", "remove", "-y", "--noautoremove", dnfNoWait, name},
			[]string{"dnf", "install", "--assumeno", dnfNoWeak, dnfNoWait, name}, nil
	}
	return nil, nil, nil, fmt.Errorf("packages: tool %d: %w", t, fs.ErrInvalid)
}

// Installed reads whether name is installed. Apt is detect.DpkgInstalled's
// rule, apk alpine.Installed's: exit 0 with nothing, the name, or the name and
// a version is Yes; exit 1 with no output on either stream is No
// (alpine323-vm.log); dnf rpmInstalled's; anything else is Unknown with the
// error.
func Installed(ctx context.Context, sys host.System, t Tool, name string) (detect.Tri, error) {
	if _, _, _, err := commands(t, name); err != nil {
		return detect.Unknown, err
	}
	var tri detect.Tri
	var err error
	switch t {
	case Apt:
		tri, err = detect.DpkgInstalled(ctx, sys, name)
	case Dnf:
		return rpmInstalled(ctx, sys, name)
	default:
		tri, err = alpine.Installed(ctx, sys, name)
	}
	if err != nil {
		return tri, fmt.Errorf("packages: %w", err)
	}
	return tri, nil
}

// rpmInstalled is rpm -q name: exit 0 with each line "<name>-<version>" is
// Yes (a kernel has a line per version); exit 1 with "package <name> is not
// installed" is No (rocky10-test-110, probe 4); anything else is Unknown.
func rpmInstalled(ctx context.Context, sys host.System, name string) (detect.Tri, error) {
	res, err := sys.Run(ctx, "rpm", "-q", name)
	out := strings.TrimSpace(string(res.Combined))
	var exit *host.ExitError
	switch {
	case err == nil && out != "" && !slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool { return !strings.HasPrefix(l, name+"-") }):
		return detect.Yes, nil
	case errors.As(err, &exit) && exit.ExitCode == 1 && out == "package "+name+" is not installed":
		return detect.No, nil
	case err != nil && !errors.As(err, &exit):
		return detect.Unknown, fmt.Errorf("packages: %w", err)
	}
	return detect.Unknown, fmt.Errorf("packages: rpm -q %s printed %q (exit %d): %w", name, out, res.ExitCode, detect.ErrUnrecognised)
}
