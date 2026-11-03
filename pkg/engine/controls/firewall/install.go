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

// ufwPackage is the package that brings ufw, apt's and apk's name alike.
const ufwPackage = "ufw"

// installWhy is the why of the undo step the journal holds.
const installWhy = "firewall"

// install is Install's plan (decision 1): packages.PlanInstall, which only
// reads, as one change with RiskPackageInstall, a plain y/N, its state after
// naming the other new packages the simulation lists; none where ufw is
// installed. A simulation that finds no package, or would change another, is a
// refusal saying what to do, before anything changes. Install brings ufw alone:
// on a stock Alpine Enable then refuses for want of ss until iproute2 is there.
func install(ctx context.Context, sys host.System, f detect.Facts) ([]engine.Change, error) {
	tool, err := packages.ToolFor(f)
	if err != nil {
		return nil, &steps.Refusal{Code: codeNoTool, Why: packages.ErrNoTool.Error(), Err: err}
	}
	p, err := packages.PlanInstall(ctx, sys, tool, ufwPackage)
	if err != nil {
		return nil, installRefusal(tool, err)
	}
	if p.Empty() {
		return nil, nil
	}
	const title = "Install ufw"
	after := "installed, inactive"
	if also := slices.DeleteFunc(slices.Clone(p.Adds), func(n string) bool { return n == ufwPackage }); len(also) > 0 {
		after += ", with " + strings.Join(also, ", ")
	}
	return []engine.Change{{ID: ID + "/install", Summary: title, Title: title, Target: ufwPackage,
		StateBefore: "not installed", StateAfter: after, Commands: []string{host.FormatArgv(p.Argv)},
		Risk: engine.RiskPackageInstall, Stage: engine.StagePackages, Resources: []string{steps.ResourceUFW},
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

// installRefusal is err, packages.PlanInstall's, as a *steps.Refusal where it
// has words; any other error as it is.
func installRefusal(tool packages.Tool, err error) error {
	var other *packages.OtherPackagesError
	switch {
	case errors.Is(err, packages.ErrNotInRepos) && tool == packages.Apk:
		return &steps.Refusal{Code: codeNotInRepos, Why: "hardn can't find ufw in apk's package lists",
			Fix: "check the network and the community repository, run apk update, then try again", Err: err}
	case errors.Is(err, packages.ErrNotInRepos):
		return &steps.Refusal{Code: codeNotInRepos, Why: "hardn can't find ufw in apt's package lists",
			Fix: "run apt-get update, then try again", Err: err}
	case errors.As(err, &other):
		return &steps.Refusal{Code: codeOtherPackages, Why: "Installing ufw would also change other packages: " + strings.Join(changeNames(other.Changes), ", "),
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
