package packages

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

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
// detect.ErrUnrecognised, as alpine.simulate's is. From dnf, one that adds a
// kernel is a *KernelError, and one dnf can't make for want of any enabled
// repository is ErrNoRepos. ErrNotInRepos when the tool cannot find it; any
// other failure is the error, wrapped.
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
	if err != nil && !(t == Dnf && aborted(res, err)) {
		var exit *host.ExitError
		switch {
		case noRepos(t, res, err):
			return Plan{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), ErrNoRepos)
		case notInRepos(t, name, res, err):
			return Plan{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), ErrNotInRepos)
		case t == Dnf && errors.As(err, &exit) && exit.ExitCode == 200:
			return Plan{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), ErrLocked)
		}
		return Plan{}, fmt.Errorf("packages: %w", err)
	}
	var adds, refused []string
	var rows []dnfRow
	switch t {
	case Apk:
		adds, refused = apkChanges(res.Combined)
	case Dnf:
		rows, refused = dnfChanges(res.Stdout)
		for _, r := range rows {
			adds = append(adds, r.name)
		}
	default:
		adds, refused = aptChanges(res.Combined)
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
	if k, err := newKernels(ctx, sys, rows); err != nil {
		return Plan{}, err
	} else if len(k) > 0 {
		return Plan{}, fmt.Errorf("packages: %s: %w", host.FormatArgv(simulate), &KernelError{Packages: k})
	}
	return Plan{Tool: t, Name: name, Argv: install, Undo: undo, Adds: adds}, nil
}

// aborted is dnf install --assumeno's answer to a transaction it would run:
// the table on stdout, "Operation aborted." on stderr, exit 1
// (rocky10-test-110, probe 4; dnf's cli.py raises it as a CliError).
func aborted(res host.Result, err error) bool {
	var exit *host.ExitError
	return errors.As(err, &exit) && exit.ExitCode == 1 && slices.Contains(lines(res.Stderr), "Operation aborted.") &&
		slices.Contains(lines(res.Stdout), "Transaction Summary")
}

func lines(b []byte) []string { return strings.Split(strings.TrimSpace(string(b)), "\n") }

// notKernels are kernel-… packages that install no kernel and no modules:
// headers and tools for userland, and the files modules are built with.
var notKernels = []string{"kernel-headers", "kernel-tools", "kernel-tools-libs", "kernel-tools-libs-devel", "kernel-devel",
	"kernel-devel-matched", "kernel-doc", "kernel-abi-stablelists", "kernel-srpm-macros", "kernel-cross-headers"}

// newKernels are the rows that are a kernel's packages (kernel and the
// kernel-… names, but notKernels) of another release than the running one: "<version>.<arch>"
// against uname -r ("6.12.0-211.16.1.el10_2.0.1.x86_64" runs, the rows
// were "6.12.0-211.74.1.el10_2" and "x86_64", probe 4). Modules for the
// running kernel make no boot entry and are let through. A kernel row
// whose version or arch isn't read (a wrapped row) counts as another
// release: a refusal is the safe mistake. uname runs only where a kernel
// row is listed.
func newKernels(ctx context.Context, sys host.System, rows []dnfRow) ([]string, error) {
	var running string
	var k []string
	for _, r := range rows {
		if r.name != "kernel" && !strings.HasPrefix(r.name, "kernel-") || slices.Contains(notKernels, r.name) {
			continue
		}
		if running == "" {
			res, err := sys.Run(ctx, "uname", "-r")
			if err != nil {
				return nil, fmt.Errorf("packages: %w", err)
			}
			if running = strings.TrimSpace(string(res.Stdout)); running == "" {
				return nil, fmt.Errorf("packages: uname -r printed nothing: %w", detect.ErrUnrecognised)
			}
		}
		if r.version == "" || r.arch == "" || r.version+"."+r.arch != running {
			k = append(k, r.name)
		}
	}
	return k, nil
}

// noReposPrefix starts dnf's line for a host with no enabled repository;
// the repository directories follow, dnf.conf's reposdir (dnf.conf(5)), so
// only the start is matched.
const noReposPrefix = "Error: There are no enabled repositories in "

// noRepos reads dnf's failed simulation on a host with no enabled
// repository, as an unregistered RHEL host is: noReposPrefix's line on
// stderr, exit 1; the subscription plugin's notices ("This system is not
// registered with an entitlement server") are on stdout (rhel10-test-114
// and rhel9-test-115, probe 3, 2026-11-10).
func noRepos(t Tool, res host.Result, err error) bool {
	var exit *host.ExitError
	if t != Dnf || !errors.As(err, &exit) || exit.ExitCode != 1 {
		return false
	}
	return slices.ContainsFunc(lines(res.Stderr), func(l string) bool { return strings.HasPrefix(l, noReposPrefix) })
}

// notInRepos reads a failed simulation: apt-get's "E: Unable to locate package
// x", exit 100 (deb13-vm.log, ubuntu26-vm.log); apk's "unable to select
// packages" with "(no such package)", exit 1 (ErrNotInRepos has the logs);
// dnf's "Error: Unable to find a match: x" on stderr, exit 1
// (rocky10-test-110, probe 3b).
func notInRepos(t Tool, name string, res host.Result, err error) bool {
	var exit *host.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	out := string(res.Combined)
	switch t {
	case Apt:
		return exit.ExitCode == 100 && slices.Contains(strings.Split(out, "\n"), "E: Unable to locate package "+name)
	case Dnf:
		return exit.ExitCode == 1 && slices.Contains(strings.Split(out, "\n"), "Error: Unable to find a match: "+name)
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

// dnfAdds are the sections of dnf's transaction table that only add new
// packages (dnf's output.py list_transaction headings). Any other section,
// Upgrading, Reinstalling, Downgrading, the Removing ones, group or module
// packages and the Skipping ones, is refused: dnf install of a package can
// upgrade others (probe 4: "Upgrading: sudo").
var dnfAdds = map[string]bool{"Installing:": true, "Installing dependencies:": true, "Installing weak dependencies:": true}

// dnfRow is one package row of dnf's table: its name, and its arch and
// version where they are on its line.
type dnfRow struct{ name, arch, version string }

// dnfChanges reads dnf install --assumeno's table on stdout (probe 4): a
// heading at the margin ending in ":", then its rows, each " <name> <arch>
// <version> <repository> <size>" with one space before the name; a row
// whose name is too wide goes on below, indented further. A "replacing"
// line (obsoletes) is refused. The table ends at "Transaction Summary". A
// refusal is "<the heading's first word> <name>" ("Upgrading sudo",
// "Replacing x.noarch"), as apk's lines read.
func dnfChanges(out []byte) (adds []dnfRow, refused []string) {
	section := ""
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case l == "Transaction Summary":
			return adds, refused
		case l == "":
		case l[0] != ' ' && strings.HasSuffix(l, ":"):
			section = l
		case l[0] != ' ' || section == "":
		case strings.HasPrefix(l, "  "):
			if f := strings.Fields(l); len(f) > 1 && f[0] == "replacing" {
				refused = append(refused, "Replacing "+f[1])
			}
		case dnfAdds[section]:
			f := strings.Fields(l)
			r := dnfRow{name: f[0]}
			if len(f) >= 3 {
				r.arch, r.version = f[1], f[2]
			}
			adds = append(adds, r)
		default:
			verb, _, _ := strings.Cut(strings.TrimSuffix(section, ":"), " ")
			refused = append(refused, verb+" "+strings.Fields(l)[0])
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

// InstallTimeout bounds an install, which runs to its end however its
// caller's ctx ends (Execute).
const InstallTimeout = 30 * time.Minute

// Execute runs p.Argv, then reads Installed: anything but Yes is ErrVerify, and
// an empty plan runs only that read. A failed command returns its error with no
// read after it. A done ctx, or a plan that is not what PlanInstall makes
// (fs.ErrInvalid), runs nothing. Once started, the install runs under its
// own bound (InstallTimeout), not ctx: a Ctrl-C or a dropped session would
// kill the tool in the middle of its transaction (host.Real kills a
// cancelled command), leaving the package database half done.
func Execute(ctx context.Context, sys host.System, p Plan) (Report, error) {
	var rep Report
	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("packages: %w", err)
	}
	if err := p.check(); err != nil {
		return rep, err
	}
	if !p.Empty() {
		ictx, cancel := context.WithTimeout(context.WithoutCancel(ctx), InstallTimeout)
		_, err := sys.Run(ictx, p.Argv[0], p.Argv[1:]...)
		cancel()
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
