// Command p4status is a dev-only, read-only driver for refactor plan
// Phase 4 (next/p4-vmcheck): run as root on a host, it reads the host as
// hardn's main menu will, through the real code (detect's facts, once;
// Engine.Check over the status rows' controls in Figma's order: User,
// Firewall, SSH Login, SSH Auth, SSH Port, AppArmor, Auto Updates; the
// header probe; the risk level), and prints
//
//  1. the main menu as hardn will draw it: pkg/output's MainMenuOf over the
//     main menu's mock, as the gallery's status hosts draw it, rendered by
//     pkg/ui's screen (plain at 80 columns unless -profile, -width and
//     -height say otherwise, so that it pastes into a log);
//  2. the evidence, for a person to check each line against the host by
//     hand: detect's facts; per row its control, state, detail, evidence,
//     findings and error with its registry code; the header's facts, each
//     with its error; the risk score, band and factors with their costs;
//     and every command it ran with its exit status, and every call it
//     refused.
//
// It changes nothing, by construction: it reaches the host only through
// its guard (guard.go), a host.System over host.Real that passes the reads
// through, refuses every method that changes anything, and runs only the
// commands the checks and the probe run, with the shapes of their
// arguments (allow.go). A refused call is printed, the rows it was made
// for are named above the main menu and marked in the evidence (a check
// may carry on without the answer, so its row is not always Unknown), and
// the run exits 1. It never plans or applies.
//
// It is never part of a release, as p2drive: every build target of the
// makefile, the goreleaser configs and the workflows builds ./cmd/hardn
// alone, a main package cannot be imported, and depguard's dev-tools rule
// keeps every package outside internal/tools from importing one.
//
// Usage (docs/safety/vmtest.md §8 gives the VM run):
//
//	sudo ./p4status [-profile plain|none|16|256|truecolor] [-width 80] [-height 24]
//
// Exit status: 0 when it printed and refused nothing; 1 when it refused a
// call or could not read the host (detection failed) or write its output;
// 2 for a flag that is wrong, a host that is not Linux, or a user that is
// not root.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
)

// Exit statuses.
const (
	exitOK      = 0
	exitRefused = 1 // a call refused, the host not read, or the output not written
	exitUsage   = 2 // a flag, the platform or the user
)

// env is what run takes from the process, so that the tests give a fake
// host, and refuse nothing on macOS by accident: they set goos themselves.
type env struct {
	goos           string
	euid           func() int
	stdout, stderr io.Writer
	// system is the host, built only once the gates hold.
	system func() host.System
	// controls are the status rows' controls, statusControls; the tests
	// add one that changes the host, to see it refused.
	controls func() []engine.Control
}

func production() env {
	return env{
		goos:     runtime.GOOS,
		euid:     os.Geteuid,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		system:   func() host.System { return host.NewReal(host.Options{}) },
		controls: statusControls,
	}
}

func main() {
	// Ctrl-C and SIGTERM end the reading: host.Real kills the command it
	// is running, and nothing is left half done, since nothing is done.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], production())
	stop()
	os.Exit(code)
}

// usageLine is p4status's usage.
const usageLine = "usage: sudo ./p4status [-profile plain|none|16|256|truecolor] [-width 80] [-height 24]"

// run is main without the process: it returns the exit status.
func run(ctx context.Context, args []string, e env) int {
	fs := flag.NewFlagSet("p4status", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.Usage = func() {
		fmt.Fprintln(e.stderr, usageLine)
		fs.PrintDefaults()
	}
	profile := fs.String("profile", "plain", "the main menu's colour profile: plain, none, 16, 256 or truecolor")
	width := fs.Int("width", 80, "the terminal width the main menu is drawn for, in cells")
	height := fs.Int("height", 24, "the terminal height the main menu is drawn for, in lines (plain draws its full form at any height)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.stderr, "p4status: unexpected arguments %q\n%s\n", fs.Args(), usageLine)
		return exitUsage
	}
	look, err := renderEnv(*profile, *width, *height)
	if err != nil {
		fmt.Fprintf(e.stderr, "p4status: %v\n", err)
		return exitUsage
	}
	switch {
	case e.goos != "linux":
		fmt.Fprintf(e.stderr, "p4status: refused: it reads a Linux host, and this is %s\n", e.goos)
		return exitUsage
	case e.euid() != 0:
		fmt.Fprintln(e.stderr, "p4status: run as root")
		return exitUsage
	}

	g := newGuard(e.system())
	r, readErr := read(ctx, g, e.controls())
	calls := g.calls()
	var out []byte
	if readErr == nil {
		out = append(out, incomplete(calls)...)
		out = append(out, mainMenu(look, r)...)
		out = append(out, "\n\n"...)
	}
	out = append(out, evidence(r, readErr, calls)...)
	if _, err := look.Writer(e.stdout).Write(out); err != nil {
		fmt.Fprintf(e.stderr, "p4status: %v\n", err)
		return exitRefused
	}
	switch n := g.refusals(); {
	case readErr != nil:
		fmt.Fprintf(e.stderr, "p4status: the host was not read: %v\n", readErr)
		return exitRefused
	case n > 0:
		fmt.Fprintf(e.stderr, "p4status: %d call(s) refused; the evidence lists them\n", n)
		return exitRefused
	}
	return exitOK
}
