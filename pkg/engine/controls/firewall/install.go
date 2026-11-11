package firewall

import (
	"context"

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
	name := ssPackage
	if f.Platform.Distro.EL() {
		name = elSSPackage
	}
	changes, err := installPackage(ctx, sys, f, name, "/iproute2", "installed", nil)
	if err == nil && len(changes) == 0 && ReadSockets(ctx, sys, f).NoSS {
		return nil, &steps.Refusal{Code: codeNoSS, Why: name + " is installed, but its ss isn't there",
			Fix: "reinstall " + name + ", then try again", Err: ErrNoSS}
	}
	return changes, err
}

// installPackage is name's install, id ID+suffix, its state after named
// after, the other new packages after it (steps.PlanPackage). Alpine keeps
// ufw in its community repository, iproute2 in main.
func installPackage(ctx context.Context, sys host.System, f detect.Facts, name, suffix, after string, resources []string) ([]engine.Change, error) {
	return steps.PlanPackage(ctx, sys, f, steps.Package{ID: ID + suffix, Name: name, After: after, Why: installWhy,
		Resources: resources, Community: name == ufwPackage})
}

// undoSteps is argv as the journal's step, none for none.
func undoSteps(argv []string) []revert.Step { return steps.PackageUndo(argv, installWhy) }

// installRefusal is err, packages.PlanInstall's of package name, as a
// *steps.Refusal where it has words (steps.PackageRefusal); any other error
// as it is.
func installRefusal(tool packages.Tool, name string, err error) error {
	return steps.PackageRefusal(tool, name, name == ufwPackage, err)
}
