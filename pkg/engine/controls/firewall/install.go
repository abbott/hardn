package firewall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/packages"
	"github.com/abbott/hardn/pkg/host/revert"
)

// ufwPackage is the package that brings ufw, apt's and apk's name alike;
// ssPackage, the one that brings ss, which Enable reads the bound sockets
// with (reload.ReadBound), apt's and apk's name alike, in Alpine's main
// repository where ufw is in community.
const (
	ufwPackage = "ufw"
	ssPackage  = "iproute2"
)

// installWhy is the why of the undo step the journal holds.
const installWhy = "firewall"

// install is Install's plan (decision 1): packages.PlanInstall, which only
// reads, as one change with RiskPackageInstall, a plain y/N, its state after
// naming the other new packages the simulation lists; none where ufw is
// installed. A simulation that finds no package, or would change another, is a
// refusal saying what to do, before anything changes. Install brings ufw alone:
// on a stock Alpine, where ss is missing, item 1 is then Install iproute2
// (installSS) until it is there.
func install(ctx context.Context, sys host.System, f detect.Facts) ([]engine.Change, error) {
	return installPackage(ctx, sys, f, ufwPackage, "/install", "installed, inactive", []string{steps.ResourceUFW})
}

// installSS is Install iproute2's plan (the maintainer, 2026-11-07), as
// install's: ss, which Enable needs to read the bound sockets, where ufw is
// installed and ss is not (Sockets.NoSS). Where iproute2 is installed and ss
// still missing (a file removed), there is nothing to install, and saying so
// would leave item 1 offering it again: that is a refusal naming ss.
func installSS(ctx context.Context, sys host.System, f detect.Facts) ([]engine.Change, error) {
	changes, err := installPackage(ctx, sys, f, ssPackage, "/iproute2", "installed", nil)
	if err == nil && len(changes) == 0 && ReadSockets(ctx, sys, f).NoSS {
		return nil, &steps.Refusal{Code: codeNoSS, Why: "iproute2 is installed, but its ss isn't there",
			Fix: "reinstall iproute2, then try again", Err: ErrNoSS}
	}
	return changes, err
}

// installPackage is name's install, id ID+suffix, its state after named
// after, the other new packages after it.
func installPackage(ctx context.Context, sys host.System, f detect.Facts, name, suffix, after string, resources []string) ([]engine.Change, error) {
	tool, err := packages.ToolFor(f)
	if err != nil {
		return nil, &steps.Refusal{Code: codeNoTool, Why: packages.ErrNoTool.Error(), Err: err}
	}
	p, err := packages.PlanInstall(ctx, sys, tool, name)
	if err != nil {
		return nil, installRefusal(tool, name, err)
	}
	if p.Empty() {
		return nil, nil
	}
	title := "Install " + name
	if also := slices.DeleteFunc(slices.Clone(p.Adds), func(n string) bool { return n == name }); len(also) > 0 {
		after += ", with " + strings.Join(also, ", ")
	}
	return []engine.Change{{ID: ID + suffix, Summary: title, Title: title, Target: name,
		StateBefore: "not installed", StateAfter: after, Commands: []string{host.FormatArgv(p.Argv)},
		Risk: engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: resources,
		Rollback: engine.Undo{Steps: undoSteps(p.Undo)},
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			rep, err := packages.Execute(ctx, sys, p)
			u := engine.Undo{Steps: undoSteps(rep.Undo)}
			return engine.Ran{Rollback: &u, Detail: fmt.Sprintf("install: verified %v", rep.Installed)}, err
		}}}, nil
}

// undoSteps is argv as the journal's step, none for none.
func undoSteps(argv []string) []revert.Step {
	if len(argv) == 0 {
		return nil
	}
	return []revert.Step{{Op: revert.OpRun, Argv: slices.Clone(argv), Why: installWhy}}
}

// installRefusal is err, packages.PlanInstall's of package name, as a
// *steps.Refusal where it has words; any other error as it is. Alpine keeps
// ufw in its community repository, iproute2 in main.
func installRefusal(tool packages.Tool, name string, err error) error {
	var other *packages.OtherPackagesError
	switch {
	case errors.Is(err, packages.ErrNotInRepos) && tool == packages.Apk && name == ufwPackage:
		return &steps.Refusal{Code: codeNotInRepos, Why: "hardn can't find ufw in apk's package lists",
			Fix: "check the network and the community repository, run apk update, then try again", Err: err}
	case errors.Is(err, packages.ErrNotInRepos) && tool == packages.Apk:
		return &steps.Refusal{Code: codeNotInRepos, Why: "hardn can't find " + name + " in apk's package lists",
			Fix: "check the network, run apk update, then try again", Err: err}
	case errors.Is(err, packages.ErrNotInRepos):
		return &steps.Refusal{Code: codeNotInRepos, Why: "hardn can't find " + name + " in apt's package lists",
			Fix: "run apt-get update, then try again", Err: err}
	case errors.As(err, &other):
		return &steps.Refusal{Code: codeOtherPackages, Why: "Installing " + name + " would also change other packages: " + strings.Join(changeNames(other.Changes), ", "),
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
