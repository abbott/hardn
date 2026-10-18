// Command p2drive is a dev-only driver for the acceptance of refactor plan
// Phase 2 (next/p2-vmtest). It composes the safety primitives under pkg/host
// without the Phase 3 engine, in the order brief 2.3 and docs/safety/revert.md
// §7 give the engine, so that brief Phase 2's scenario can run on a
// disposable VM before the slices exist: the SSH port moved to 2230 under
// confirm-or-revert (both Port lines during the window, ssh.socket on
// Ubuntu), validated in context, reloaded and its listener verified, then
// either confirmed from a new session with `hardn confirm` or rolled back by
// the timer's `hardn rollback <id>`; and besides it, PermitRootLogin
// prohibit-password on Proxmox VE, a sudoers grant checked with visudo, and
// UFW's allow rules before its enable.
//
// It changes sshd, the firewall and sudoers on the machine it runs on, so it
// refuses to run (exit 2, nothing read or run) unless all of these hold:
//
//   - the machine is Linux; macOS is refused by name, before anything else;
//   - the flag --i-am-on-a-disposable-vm is given;
//   - HARDN_P2DRIVE_DISPOSABLE=yes is in its environment;
//   - it runs as root.
//
// It is never part of a release: every build target of the makefile, the
// goreleaser configs and the workflows builds ./cmd/hardn alone, a main
// package cannot be imported, and depguard's dev-tools rule keeps every
// package outside internal/tools from importing one.
//
// Usage (docs/safety/vmtest.md gives the order and what each step proves):
//
//	p2drive --i-am-on-a-disposable-vm <command> [flags]
//
//	facts       read-only: detect's facts, sshd's effective values and
//	            conflicts, legacy SSH files, ufw, Proxmox VE, sudoers, the
//	            change journal
//	broken      validate a broken 00-hardn.conf and show it refused before
//	            it takes effect; changes nothing
//	port        move SSH to --port (2230) under confirm-or-revert, the old
//	            port held until the change is confirmed
//	keyonly     PasswordAuthentication and KbdInteractiveAuthentication no,
//	            under confirm-or-revert (after key login is confirmed)
//	pve-root    PermitRootLogin prohibit-password on Proxmox VE, under
//	            confirm-or-revert (a change that moves no port)
//	ufw-enable  allow the SSH listeners, then default deny incoming and
//	            enable ufw, under confirm-or-revert
//	migrate     plan the migration of legacy hardn SSH files (a dry run);
//	            --apply carries it out under confirm-or-revert
//	sudoers     grant --user sudo or doas (--grant nopasswd, password or
//	            none), checked with visudo or doas -C first
//
// The lockout-risk commands (port, keyonly, pve-root, ufw-enable, migrate
// --apply) take --timeout (the confirm window, 180s by default, at least
// 60s), --hardn (the hardn binary the revert timer runs, /usr/local/bin/hardn
// by default), --backup-path (/var/backups/hardn) and --plan-only (stop after
// the plan); port, keyonly and pve-root take --ufw (keep or enable).
// Every command they and the others run is printed as host.System's run log.
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
	"slices"
	"syscall"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The gates: the flag and the environment variable that say this is a
// disposable VM. Both are required, so that neither a stray alias nor an
// exported variable alone runs the driver.
const (
	gateFlag  = "i-am-on-a-disposable-vm"
	gateEnv   = "HARDN_P2DRIVE_DISPOSABLE"
	gateValue = "yes"
)

// errRefused is a gate that does not hold; errUsage a command line that is
// not one. Both exit 2.
var (
	errRefused = errors.New("refused")
	errUsage   = errors.New("usage")
)

// env is what run takes from the process, so the tests can give a fake
// host and refuse nothing on macOS by accident: they set goos themselves.
type env struct {
	goos           string
	euid           func() int
	getenv         func(string) string
	now            func() time.Time
	stdout, stderr io.Writer
	// system is the host, built only after the gates hold; runLog receives
	// every command it runs.
	system func(runLog io.Writer) host.System
	// detect and session establish the facts and the applying session
	// (detect.Detect, revert.ReadSession of this process).
	detect  func(ctx context.Context, sys host.System) (detect.Facts, error)
	session func(ctx context.Context, sys host.System) (revert.Session, error)
}

func production() env {
	return env{
		goos:   runtime.GOOS,
		euid:   os.Geteuid,
		getenv: os.Getenv,
		now:    time.Now,
		stdout: os.Stdout,
		stderr: os.Stderr,
		system: func(runLog io.Writer) host.System { return host.NewReal(host.Options{RunLog: runLog}) },
		detect: detect.Detect,
		session: func(ctx context.Context, sys host.System) (revert.Session, error) {
			return revert.ReadSession(ctx, sys, os.Getenv, os.Getpid())
		},
	}
}

func main() {
	// Ctrl-C and SIGTERM cancel the context: a lockout-risk change then
	// rolls back while the session is open (revert.Rollback runs without
	// the cancellation). A hangup ends the process; the timer, armed before
	// the first step, rolls the change back at the deadline.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], production())
	stop()
	os.Exit(code)
}

// command is one subcommand: setup registers its flags on fs and returns
// what runs once they are parsed and the gates hold.
type command struct {
	name, summary string
	setup         func(fs *flag.FlagSet) func(ctx context.Context, d *driver) error
}

func commands() []command {
	return []command{
		{"facts", "read-only report of what the primitives see", setupFacts},
		{"broken", "show a broken 00-hardn.conf refused before it takes effect", setupBroken},
		{"port", "move the SSH port under confirm-or-revert", setupPort},
		{"keyonly", "turn password logins off under confirm-or-revert", setupKeyOnly},
		{"pve-root", "PermitRootLogin prohibit-password on Proxmox VE", setupPVERoot},
		{"ufw-enable", "allow the SSH listeners, then enable ufw", setupUFWEnable},
		{"migrate", "plan (or --apply) the migration of legacy SSH files", setupMigrate},
		{"sudoers", "grant a user sudo or doas, checked first", setupSudoers},
	}
}

// run is main without the process: it returns the exit code, 0 for
// success, 1 for a failure, 2 for a usage error or a gate that does not
// hold.
func run(ctx context.Context, args []string, e env) int {
	if e.goos == "darwin" {
		fmt.Fprintln(e.stderr, "p2drive: refused: this is macOS. p2drive changes sshd, the firewall and sudoers, and runs only on a disposable Linux VM (docs/safety/vmtest.md).")
		return 2
	}
	gate := false
	global := flag.NewFlagSet("p2drive", flag.ContinueOnError)
	global.SetOutput(e.stderr)
	global.BoolVar(&gate, gateFlag, false, "confirm that this host is a disposable VM")
	global.Usage = func() { usage(e.stderr) }
	if err := global.Parse(args); err != nil {
		return 2
	}
	if global.NArg() == 0 {
		usage(e.stderr)
		return 2
	}
	name := global.Arg(0)
	i := slices.IndexFunc(commands(), func(c command) bool { return c.name == name })
	if i < 0 {
		fmt.Fprintf(e.stderr, "p2drive: unknown command %q\n", name)
		usage(e.stderr)
		return 2
	}
	fs := flag.NewFlagSet("p2drive "+name, flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	fs.BoolVar(&gate, gateFlag, gate, "confirm that this host is a disposable VM")
	do := commands()[i].setup(fs)
	if err := fs.Parse(global.Args()[1:]); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(e.stderr, "p2drive %s: unexpected arguments %q\n", name, fs.Args())
		return 2
	}
	if err := preflight(e, gate); err != nil {
		fmt.Fprintf(e.stderr, "p2drive: %v\n", err)
		return 2
	}
	d := &driver{env: e, out: e.stdout}
	d.sys = e.system(e.stdout)
	if err := do(ctx, d); err != nil {
		fmt.Fprintf(e.stderr, "p2drive %s: %v\n", name, err)
		if errors.Is(err, errUsage) {
			return 2
		}
		return 1
	}
	return 0
}

// preflight checks the gates, in order; the first that does not hold is
// the error.
func preflight(e env, gate bool) error {
	switch {
	case e.goos != "linux":
		return fmt.Errorf("%w: p2drive runs on a disposable Linux VM only, not on %s", errRefused, e.goos)
	case !gate:
		return fmt.Errorf("%w: --%s is not given: p2drive changes sshd, the firewall and sudoers on this host", errRefused, gateFlag)
	case e.getenv(gateEnv) != gateValue:
		return fmt.Errorf("%w: %s=%s is not set: p2drive changes sshd, the firewall and sudoers on this host", errRefused, gateEnv, gateValue)
	case e.euid() != 0:
		return fmt.Errorf("%w: p2drive must run as root (sudo env %s=%s p2drive --%s ...)", errRefused, gateEnv, gateValue, gateFlag)
	}
	return nil
}

func usage(w io.Writer) {
	fmt.Fprintf(w, "usage: p2drive --%s <command> [flags]\n\nA dev-only driver for refactor plan Phase 2's VM acceptance; it runs only on a\ndisposable Linux VM, as root, with %s=%s. Commands:\n\n", gateFlag, gateEnv, gateValue)
	for _, c := range commands() {
		fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w, "\nSee docs/safety/vmtest.md for the order and what each step proves.")
}

// driver is one run: the host, the output, and the facts once read.
type driver struct {
	env
	sys host.System
	out io.Writer
}

// say prints one line of the transcript.
func (d *driver) say(format string, args ...any) {
	fmt.Fprintf(d.out, "p2drive: "+format+"\n", args...)
}
