package host

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// interactiveWait is how long RunInteractive gives a child to end after
// SIGTERM, once ctx is done, before it kills it.
const interactiveWait = 5 * time.Second

// terminalSignals are the signals a terminal sends to its foreground process
// group, the child and hardn together, that would end hardn while the child
// has the terminal: Ctrl-C by Go's default for SIGINT, as nothing is
// registered outside a prompt or an apply (pkg/ui/prompt; cmd/hardn's
// readStatus), and Ctrl-\ by Go's for SIGQUIT, which also dumps the stacks.
var terminalSignals = []os.Signal{os.Interrupt, syscall.SIGQUIT}

// InteractiveEnv is the whole environment of a command RunInteractive runs
// with env: env and Run's PATH, which no entry of env replaces.
func InteractiveEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, "PATH=") {
			out = append(out, e)
		}
	}
	return append(out, "PATH="+SearchPath)
}

// RunInteractive runs name with args as a child with hardn's own stdin,
// stdout and stderr, and waits for it (see System).
//
// The child stays in hardn's session and process group, while OwnSessions
// holds too, as it must have the terminal; it is the foreground group's, so
// it reads the terminal and takes its signals. While it runs, hardn catches
// terminalSignals on a channel nothing reads: exec gives a caught signal back
// its default action in the child (POSIX exec) and keeps an ignored one
// ignored, so signal.Ignore would take Ctrl-C from the child, and catching one
// hardn started with ignored would give it to the child, so that one is left
// alone, as ReadLine leaves it. signal.Stop then gives each signal back what
// it had, any other registration untouched. Ctrl-Z is left to the group: both
// stop, and both go on at fg. A SIGTERM or a hangup sent to hardn ends it by
// whatever it has at the call.
//
// If stdin is a terminal, a child that ended without putting its modes back
// (killed, as below, or crashed in raw mode) has them put back, so the next
// prompt reads as before.
func (r *Real) RunInteractive(ctx context.Context, env []string, name string, args ...string) error {
	argv := append([]string{name}, args...)
	res := Result{Argv: argv, ExitCode: -1}
	id := r.log.Begin(argv, -1)
	err := r.interactive(ctx, &res, env, name, args)
	r.log.End(id, res, err)
	return err
}

func (r *Real) interactive(ctx context.Context, res *Result, env []string, name string, args []string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("host: run %s: %w", FormatArgv(res.Argv), err)
	}
	path, err := r.resolve(name)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Args[0] = name // as Run sets it
	cmd.Env = InteractiveEnv(env)
	cmd.Dir = "/"
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// SIGKILL would leave the terminal as the child set it: SIGTERM first.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = r.interactiveWait

	release := catchTerminalSignals()
	restore := keepTerminal()
	code, runErr := r.exec(cmd)
	restore()
	release()

	res.ExitCode = code
	return runOutcome(ctx, res, code, runErr)
}

// catchTerminalSignals catches terminalSignals, those not ignored, until the
// function it returns. The channel is never read: os/signal does not block
// sending to a full one, and a caught signal has done its work by being
// caught.
func catchTerminalSignals() (release func()) {
	c := make(chan os.Signal, 1)
	for _, sig := range terminalSignals {
		if !signal.Ignored(sig) {
			signal.Notify(c, sig)
		}
	}
	return func() { signal.Stop(c) }
}

// keepTerminal notes stdin's terminal modes, and returns the function that
// puts them back if they changed. It does nothing where stdin is no terminal.
func keepTerminal() (restore func()) {
	fd := int(os.Stdin.Fd())
	before, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return func() {}
	}
	return func() {
		if now, err := unix.IoctlGetTermios(fd, getTermios); err == nil && *now != *before {
			_ = unix.IoctlSetTermios(fd, setTermios, before)
		}
	}
}
