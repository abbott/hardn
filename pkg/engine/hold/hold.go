// Package hold keeps a hangup, Ctrl-C or SIGTERM from stopping hardn while
// it rolls a change back in its own process (refactor plan P3,
// next/p3-engine; docs/safety/revert.md §2.17). It is the hold the engine
// puts around its in-process rollback, cmd/hardn around `hardn rollback`
// (and its --boot) and p2drive around its in-process rollback (since
// next/p3-engine-boot, which moved their own copies here, each keeping its
// log line through SignalsAs), with a release that gives the signals back
// their disposition: the engine's caller (a menu) goes on after it.
//
// It is the one part of the engine that touches the process's signal
// disposition, kept out of package engine so that the engine reaches
// nothing but host.System (depguard engine-through-host).
package hold

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// sigIgn is SIG_IGN's handler value, on Linux and macOS alike.
const sigIgn = 1

// held are the signals the hold ignores.
var held = []syscall.Signal{syscall.SIGHUP, syscall.SIGINT}

func signals(ss []syscall.Signal) []os.Signal {
	out := make([]os.Signal, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Signals ignores SIGHUP and SIGINT and catches SIGTERM, logging each SIGTERM
// to log (nil discards), until release is called. The ignored signals are
// inherited by the commands started meanwhile ("signals set to be ignored
// (SIG_IGN) by the calling process image shall be set to be ignored by the
// new process image", POSIX exec): host.Real's children share hardn's
// process group, so the terminal's Ctrl-C and hangup, which reach the whole
// foreground group, stop neither hardn nor the ufw or systemctl the
// rollback is running. SIGKILL cannot be held off; the revert timer, or
// the next `hardn rollback <id>`, takes the rollback up.
//
// Release gives SIGHUP and SIGINT back the disposition they had when the
// hold began: the default action (Go's handler, so a Ctrl-C ends the
// program and a command started afterwards gets the default too), or, for
// a signal hardn was started with ignored (nohup), ignored. signal.Ignore
// sets the operating system's handler to SIG_IGN, and os/signal cannot
// undo that: a signal.Notify after it records SIG_IGN as the handler to go
// back to, which signal.Stop then puts back (Go 1.27 runtime/signal_unix.go,
// sigenable and sigdisable). So release sets the default action itself
// first (restoreDefault, a sigaction(2) call; on Linux amd64 and arm64 and
// on macOS), then has the runtime install its handler again (a
// signal.Notify, then signal.Stop). Where the platform has no restore it
// logs that the signals stay ignored. A channel the caller had registered
// for SIGHUP or SIGINT with signal.Notify or signal.NotifyContext before
// the hold does not receive them after it: signal.Ignore ended that
// registration, so the caller registers again if it still wants them.
// Holds do not nest. Its lines begin "hardn: " (SignalsAs).
func Signals(log io.Writer) (release func()) { return SignalsAs("hardn", log) }

// SignalsAs is Signals with name ahead of each line it logs ("<name>:
// SIGTERM received; the rollback finishes first"), so that a command keeps
// the words its log has always had: "hardn rollback" for cmd/hardn,
// "p2drive" for the driver.
func SignalsAs(name string, log io.Writer) (release func()) {
	var restore []syscall.Signal
	for _, s := range held {
		if !signal.Ignored(s) {
			restore = append(restore, s)
		}
	}
	signal.Ignore(signals(held)...)
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-term:
				logf(log, "%s: SIGTERM received; the rollback finishes first", name)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(term)
		close(done)
		<-stopped
		if len(restore) == 0 {
			return
		}
		for _, s := range restore {
			if err := restoreDefault(s); err != nil {
				logf(log, "%s: %v stays ignored after the rollback: %v", name, s, err)
			}
		}
		c := make(chan os.Signal, 1)
		signal.Notify(c, signals(restore)...)
		signal.Stop(c)
	}
}

func logf(log io.Writer, format string, args ...any) {
	if log != nil {
		_, _ = fmt.Fprintf(log, format+"\n", args...)
	}
}
