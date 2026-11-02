// Package packages installs one named package through apt-get or apk (refactor
// plan slice 4, decision 1), for the firewall's Install UFW and for slice 7.
//
// PlanInstall only reads: a package already installed plans nothing, any other
// is simulated, and a simulation that would remove, upgrade or replace anything
// is refused. Execute runs the plan and checks the package is installed. Every
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
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Tool is the program that installs packages. The zero value is none.
type Tool uint8

const (
	Apt Tool = iota + 1 // apt-get, with dpkg-query to read
	Apk
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
	// ErrOtherPackages: the simulation changes more than new packages
	// (*OtherPackagesError names the changes).
	ErrOtherPackages = errors.New("installing it would change other packages")
	// ErrVerify: the install ran, and the package does not read as installed.
	ErrVerify = errors.New("package not installed after the install")
)

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
// apk on Alpine. Anything else is ErrNoTool, Proxmox VE and a distribution not
// established among them, and one that is only like Debian: hardn never
// installs there.
func ToolFor(f detect.Facts) (Tool, error) {
	switch f.Platform.Distro {
	case detect.DistroDebian, detect.DistroUbuntu:
		return Apt, nil
	case detect.DistroAlpine:
		return Apk, nil
	}
	return 0, fmt.Errorf("packages: distribution %q: %w", f.Platform.Distro, ErrNoTool)
}

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
	}
	return nil, nil, nil, fmt.Errorf("packages: tool %d: %w", t, fs.ErrInvalid)
}

// Installed reads whether name is installed. Apt is detect.DpkgInstalled's
// rule. Apk is "apk info -e", by pkg/host/alpine's rule: exit 0 with nothing,
// the name, or the name and a version is Yes; exit 1 with no output on either
// stream is No (alpine323-vm.log); anything else is Unknown with the error.
func Installed(ctx context.Context, sys host.System, t Tool, name string) (detect.Tri, error) {
	if _, _, _, err := commands(t, name); err != nil {
		return detect.Unknown, err
	}
	if t == Apt {
		tri, err := detect.DpkgInstalled(ctx, sys, name)
		if err != nil {
			return tri, fmt.Errorf("packages: %w", err)
		}
		return tri, nil
	}
	res, err := sys.Run(ctx, "apk", "info", "-e", name)
	var exit *host.ExitError
	out := strings.TrimSuffix(string(res.Stdout), "\n")
	switch {
	case err == nil && (out == "" || out == name || versioned(out, name)):
		return detect.Yes, nil
	case errors.As(err, &exit) && exit.ExitCode == 1 && len(res.Stdout) == 0 && len(res.Stderr) == 0:
		return detect.No, nil
	case err == nil:
		err = fmt.Errorf("output %q: %w", res.Stdout, detect.ErrUnrecognised)
	}
	return detect.Unknown, fmt.Errorf("packages: apk info -e %s: %w", name, err)
}

// versioned: s is name-<version>, the version starting with a digit, on one
// line.
func versioned(s, name string) bool {
	v, ok := strings.CutPrefix(s, name+"-")
	return ok && v != "" && v[0] >= '0' && v[0] <= '9' && !strings.ContainsAny(v, " \n")
}
