package packages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Plan is one install. Adds are the new packages the simulation lists, Name's
// dependencies among them.
type Plan struct {
	Tool       Tool
	Name       string
	Argv, Undo []string
	Adds       []string
}

// Empty says there is nothing to run: the package is installed already.
func (p Plan) Empty() bool { return len(p.Argv) == 0 }

// PlanInstall plans installing name, and only reads: a package already
// installed plans nothing; any other is simulated, which changes nothing.
// A simulation that does more than add new packages is an
// *OtherPackagesError, one that adds them without name too (apt reads a name
// ending in "-" as a removal and one with "." as a pattern, and takes a virtual
// package for its provider [I]); one that lists no change is
// detect.ErrUnrecognised, as alpine.simulate's is. ErrNotInRepos when the tool
// cannot find it; any other failure is the error, wrapped.
func PlanInstall(ctx context.Context, sys host.System, t Tool, name string) (Plan, error) {
	install, undo, simulate, err := commands(t, name)
	if err != nil {
		return Plan{}, err
	}
	switch tri, err := Installed(ctx, sys, t, name); {
	case err != nil:
		return Plan{}, err
	case tri == detect.Yes:
		return Plan{Tool: t, Name: name}, nil
	}
	res, err := sys.Run(ctx, simulate[0], simulate[1:]...)
	if err != nil {
		if notInRepos(t, name, res, err) {
			return Plan{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), ErrNotInRepos)
		}
		return Plan{}, fmt.Errorf("packages: %w", err)
	}
	adds, refused := aptChanges(res.Combined)
	if t == Apk {
		adds, refused = apkChanges(res.Combined)
	}
	if len(refused) == 0 && !slices.Contains(adds, name) {
		if len(adds) == 0 {
			return Plan{}, fmt.Errorf("packages: %s: no package change listed: %w", host.FormatArgv(simulate), detect.ErrUnrecognised)
		}
		refused = adds
	}
	if len(refused) > 0 {
		return Plan{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), &OtherPackagesError{Changes: refused})
	}
	return Plan{Tool: t, Name: name, Argv: install, Undo: undo, Adds: adds}, nil
}

// notInRepos reads a failed simulation: apt-get's "E: Unable to locate package
// x", exit 100 (deb13-vm.log, ubuntu26-vm.log), and apk's "unable to select
// packages" with "(no such package)", exit 1 (ErrNotInRepos has the logs).
func notInRepos(t Tool, name string, res host.Result, err error) bool {
	var exit *host.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	out := string(res.Combined)
	if t == Apt {
		return exit.ExitCode == 100 && slices.Contains(strings.Split(out, "\n"), "E: Unable to locate package "+name)
	}
	return exit.ExitCode == 1 && strings.Contains(out, "unable to select packages") && strings.Contains(out, "(no such package)")
}

// aptChanges reads "apt-get install -s": "Inst x (ver ...)" is a new package;
// "Conf" is that package configured; "Remv x [ver]" (deb13-vm-2.log) is
// refused, and so are "Purg" and "Inst x [old] (new ...)", an upgrade [I: not
// in the logs, apt's documented simulate form]. Other lines are apt's prose.
func aptChanges(out []byte) (adds, refused []string) {
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) == 0:
		case f[0] == "Remv" || f[0] == "Purg":
			refused = append(refused, strings.TrimSpace(l))
		case f[0] != "Inst":
		case len(f) > 2 && strings.HasPrefix(f[2], "("):
			adds = append(adds, f[1])
		default:
			refused = append(refused, strings.TrimSpace(l))
		}
	}
	return adds, refused
}

// apkChanges reads "apk add --simulate": "Installing x (ver)" is a new package
// [I: the form is pkg/host/alpine's tests', no VM has shown it for ufw]; any
// other verb (Upgrading, Purging, Replacing, Downgrading, Reinstalling) is
// refused, as apk commits the whole world (alpine.simulate's reason).
func apkChanges(out []byte) (adds, refused []string) {
	for _, c := range alpine.Changes(out) {
		f := strings.Fields(c)
		if len(f) > 1 && f[0] == "Installing" {
			adds = append(adds, f[1])
		} else {
			refused = append(refused, c)
		}
	}
	return adds, refused
}

// Report is what Execute did.
type Report struct {
	// Undo is what undoes the install, set once the command ran, even when it
	// failed: it may have installed part of what it listed. Nil for an empty
	// plan and for a command that was not found.
	Undo []string
	// Installed: the read after the command says Yes.
	Installed bool
}

// Execute runs p.Argv, then reads Installed: anything but Yes is ErrVerify, and
// an empty plan runs only that read. A failed command returns its error with no
// read after it. A done ctx, or a plan that is not what PlanInstall makes
// (fs.ErrInvalid), runs nothing.
func Execute(ctx context.Context, sys host.System, p Plan) (Report, error) {
	var rep Report
	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("packages: %w", err)
	}
	if err := p.check(); err != nil {
		return rep, err
	}
	if !p.Empty() {
		_, err := sys.Run(ctx, p.Argv[0], p.Argv[1:]...)
		if !errors.Is(err, host.ErrNotFound) {
			rep.Undo = p.Undo
		}
		if err != nil {
			return rep, fmt.Errorf("packages: %w", err)
		}
	}
	tri, err := Installed(ctx, sys, p.Tool, p.Name)
	switch {
	case err != nil:
		return rep, fmt.Errorf("%w: %w", ErrVerify, err)
	case tri != detect.Yes:
		return rep, fmt.Errorf("packages: %s: %w", p.Name, ErrVerify)
	}
	rep.Installed = true
	return rep, nil
}

// check refuses a plan whose commands are not those of its tool and name, so
// that Execute runs nothing else.
func (p Plan) check() error {
	install, undo, _, err := commands(p.Tool, p.Name)
	switch {
	case err != nil:
		return err
	case p.Empty() && len(p.Undo) == 0:
		return nil
	case !slices.Equal(p.Argv, install) || !slices.Equal(p.Undo, undo):
		return fmt.Errorf("packages: plan for %s names other commands: %w", p.Name, fs.ErrInvalid)
	}
	return nil
}
