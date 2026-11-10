package packages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Removal is one package removed alone: dnf's remove, which keeps the
// dependencies (--noautoremove), and its undo, the package's install
// (Rocky Linux and AlmaLinux's Auto Updates off, el-updates.md §1.3).
type Removal struct {
	Name       string
	Argv, Undo []string
}

// Empty says there is nothing to remove: the package isn't installed.
func (r Removal) Empty() bool { return len(r.Argv) == 0 }

// removeCommands are dnf's remove, its simulation, and the install that
// undoes it.
func removeCommands(name string) (remove, simulate, undo []string, err error) {
	install, _, _, err := commands(Dnf, name)
	if err != nil {
		return nil, nil, nil, err
	}
	return []string{"dnf", "remove", "-y", "--noautoremove", dnfNoWait, name},
		[]string{"dnf", "remove", "--assumeno", "--noautoremove", dnfNoWait, name}, install, nil
}

// PlanRemove plans removing name, through dnf alone (fs.ErrInvalid for
// another tool), and only reads: not installed plans nothing; else dnf's
// simulation must list name alone, under "Removing:". Anything else, a
// dependent package removed with it, is an *OtherPackagesError.
func PlanRemove(ctx context.Context, sys host.System, t Tool, name string) (Removal, error) {
	if t != Dnf {
		return Removal{}, fmt.Errorf("packages: remove with tool %d: %w", t, fs.ErrInvalid)
	}
	remove, simulate, undo, err := removeCommands(name)
	if err != nil {
		return Removal{}, err
	}
	switch tri, err := Installed(ctx, sys, t, name); {
	case err != nil:
		return Removal{}, err
	case tri == detect.No:
		return Removal{Name: name}, nil
	}
	res, err := sys.Run(ctx, simulate[0], simulate[1:]...)
	if err != nil && !aborted(res, err) {
		var exit *host.ExitError
		if errors.As(err, &exit) && exit.ExitCode == 200 {
			return Removal{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), ErrLocked)
		}
		return Removal{}, fmt.Errorf("packages: %w", err)
	}
	removes, refused := dnfRemovals(res.Stdout)
	if len(refused) == 0 && !slices.Equal(removes, []string{name}) {
		if len(removes) == 0 {
			return Removal{}, fmt.Errorf("packages: %s: no package change listed: %w", host.FormatArgv(simulate), detect.ErrUnrecognised)
		}
		refused = removes
	}
	if len(refused) > 0 {
		return Removal{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), &OtherPackagesError{Changes: refused})
	}
	return Removal{Name: name, Argv: remove, Undo: undo}, nil
}

// dnfRemovals reads dnf remove --assumeno's table as dnfChanges reads an
// install's: the rows under "Removing:", and every other section's, or a
// row of another package under it, refused ("<heading's first word>
// <name>").
func dnfRemovals(out []byte) (removes, refused []string) {
	section := ""
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case l == "Transaction Summary":
			return removes, refused
		case l == "":
		case l[0] != ' ' && strings.HasSuffix(l, ":"):
			section = l
		case l[0] != ' ' || section == "" || strings.HasPrefix(l, "  "):
		case section == "Removing:":
			removes = append(removes, strings.Fields(l)[0])
		default:
			verb, _, _ := strings.Cut(strings.TrimSuffix(section, ":"), " ")
			refused = append(refused, verb+" "+strings.Fields(l)[0])
		}
	}
	return removes, refused
}

// ExecuteRemove runs r, under its own bound as Execute does, then reads
// Installed: anything but No is ErrVerify. An empty r, or one that is not
// what PlanRemove makes (fs.ErrInvalid), runs nothing.
func ExecuteRemove(ctx context.Context, sys host.System, r Removal) error {
	if r.Empty() {
		return nil
	}
	remove, _, undo, err := removeCommands(r.Name)
	switch {
	case err != nil:
		return err
	case !slices.Equal(r.Argv, remove) || !slices.Equal(r.Undo, undo):
		return fmt.Errorf("packages: removal of %s names other commands: %w", r.Name, fs.ErrInvalid)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("packages: %w", err)
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), InstallTimeout)
	_, err = sys.Run(rctx, r.Argv[0], r.Argv[1:]...)
	cancel()
	if err != nil {
		return fmt.Errorf("packages: %w", err)
	}
	switch tri, err := Installed(ctx, sys, Dnf, r.Name); {
	case err != nil:
		return fmt.Errorf("%w: %w", ErrVerify, err)
	case tri != detect.No:
		return fmt.Errorf("packages: %s still installed: %w", r.Name, ErrVerify)
	}
	return nil
}
