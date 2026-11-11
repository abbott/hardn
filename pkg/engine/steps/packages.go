package steps

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The codes of a package install's refusals, the registry's for the
// packages errors they wrap (pkg/output).
const (
	codeNoTool        = "PACKAGE_NO_TOOL"
	codeNotInRepos    = "PACKAGE_NOT_IN_REPOS"
	codeNoRepos       = "PACKAGE_NO_REPOS"
	codeOtherPackages = "PACKAGE_OTHER_CHANGES"
	codeKernel        = "PACKAGE_KERNEL"
	codeLocked        = "PACKAGE_LOCKED"
)

// Package is one package's install, as a control plans it (PlanPackage):
// Firewall's Install and Install iproute2, and the User control's Install
// shadow.
type Package struct {
	// ID is the change's id; Name the package's, apt's, apk's and dnf's
	// alike.
	ID, Name string
	// After is the change's state after it, ", with" the other new packages
	// added to it.
	After string
	// Why is the undo step's, as the journal holds it.
	Why string
	// Resources are what the change names (engine.Change.Resources).
	Resources []string
	// Community: Alpine keeps the package in its community repository, which
	// a refusal names.
	Community bool
}

// PackageToolRefusal is packages.ToolFor's error as a refusal.
func PackageToolRefusal(err error) error {
	return &Refusal{Code: codeNoTool, Why: packages.ErrNoTool.Error(), Err: err}
}

// PlanPackage is p's install: packages.PlanInstall, which only reads, as one
// change with RiskPackageInstall, a plain y/N, its state after naming the
// other new packages the simulation lists; none where p is installed. A
// simulation that finds no package, or would change another, is a refusal
// saying what to do (PackageRefusal), before anything changes. Its undo is
// the tool's removal of p alone, run as it is.
func PlanPackage(ctx context.Context, sys host.System, f detect.Facts, p Package) ([]engine.Change, error) {
	tool, err := packages.ToolFor(f)
	if err != nil {
		return nil, PackageToolRefusal(err)
	}
	pl, err := packages.PlanInstall(ctx, sys, tool, p.Name)
	if err != nil {
		return nil, PackageRefusal(tool, p.Name, p.Community, err)
	}
	if pl.Empty() {
		return nil, nil
	}
	title, after := "Install "+p.Name, p.After
	if also := slices.DeleteFunc(slices.Clone(pl.Adds), func(n string) bool { return n == p.Name }); len(also) > 0 {
		after += ", with " + strings.Join(also, ", ")
	}
	return []engine.Change{{ID: p.ID, Summary: title, Title: title, Target: p.Name,
		StateBefore: "not installed", StateAfter: after, Commands: []string{host.FormatArgv(pl.Argv)},
		Risk: engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: p.Resources,
		Rollback: engine.Undo{Steps: PackageUndo(pl.Undo, p.Why)},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			rep, err := packages.Execute(ctx, sys, pl)
			u := engine.Undo{Steps: PackageUndo(rep.Undo, p.Why)}
			return engine.Ran{Rollback: &u, Detail: fmt.Sprintf("install: verified %v", rep.Installed)}, err
		}}}, nil
}

// PackageUndo is argv as the journal's step, its why why; none for none.
func PackageUndo(argv []string, why string) []revert.Step {
	if len(argv) == 0 {
		return nil
	}
	return []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(argv), Why: why}}
}

// PackageRefusal is err, packages.PlanInstall's of package name, as a
// *Refusal where it has words; any other error as it is. community: Alpine
// keeps name in its community repository (ufw, shadow), not main (iproute2).
func PackageRefusal(tool packages.Tool, name string, community bool, err error) error {
	var other *packages.OtherPackagesError
	switch {
	case errors.Is(err, packages.ErrNoRepos):
		return &Refusal{Code: codeNoRepos, Why: "hardn can't install " + name + ": dnf has no enabled repositories",
			Fix: "register the host (RHEL: subscription-manager or rhc) or enable a repository, then try again", Err: err}
	case errors.Is(err, packages.ErrNotInRepos) && tool == packages.Apk && community:
		return &Refusal{Code: codeNotInRepos, Why: "hardn can't find " + name + " in apk's package lists",
			Fix: "check the network and the community repository, run apk update, then try again", Err: err}
	case errors.Is(err, packages.ErrNotInRepos) && tool == packages.Apk:
		return &Refusal{Code: codeNotInRepos, Why: "hardn can't find " + name + " in apk's package lists",
			Fix: "check the network, run apk update, then try again", Err: err}
	case errors.Is(err, packages.ErrNotInRepos) && tool == packages.Dnf:
		return &Refusal{Code: codeNotInRepos, Why: "hardn can't find " + name + " in dnf's repositories",
			Fix: "check the network and the repositories (dnf repolist), then try again", Err: err}
	case errors.Is(err, packages.ErrNotInRepos):
		return &Refusal{Code: codeNotInRepos, Why: "hardn can't find " + name + " in apt's package lists",
			Fix: "run apt-get update, then try again", Err: err}
	case errors.Is(err, packages.ErrKernel):
		var k *packages.KernelError
		errors.As(err, &k)
		return &Refusal{Code: codeKernel, Why: "Installing " + name + " would also install a new kernel (" + strings.Join(k.Packages, ", ") + ")",
			Fix: "run dnf upgrade and restart, then try again", Err: err}
	case errors.Is(err, packages.ErrLocked):
		return &Refusal{Code: codeLocked, Why: "another dnf is running", Fix: "wait for it to finish, then try again", Err: err}
	case errors.As(err, &other):
		return &Refusal{Code: codeOtherPackages, Why: "Installing " + name + " would also change other packages: " + strings.Join(changeNames(other.Changes), ", "),
			Fix: "install it by hand, then try again", Err: err}
	}
	return err
}

// changeNames are the packages of a simulation's changes, each once: the second
// word of "Remv x [ver]" and "Upgrading x (ver)", or a change that is a name
// alone.
func changeNames(changes []string) []string {
	var out []string
	for _, c := range changes {
		f := strings.Fields(c)
		if len(f) == 0 {
			continue
		}
		name := f[0]
		if len(f) > 1 {
			name = f[1]
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}
