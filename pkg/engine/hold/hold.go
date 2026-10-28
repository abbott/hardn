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

	"github.com/abbott/hardn/pkg/host"
)

// sigIgn is SIG_IGN's handler value, on Linux and macOS alike.
const sigIgn = 1

// held are the signals the hold ignores; caught, those it catches and
// logs, by name.
var (
	held   = []syscall.Signal{syscall.SIGINT}
	caught = map[os.Signal]string{syscall.SIGHUP: "SIGHUP", syscall.SIGTERM: "SIGTERM"}
)

func signals(ss []syscall.Signal) []os.Signal {
	out := make([]os.Signal, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Signals ignores SIGINT and catches SIGHUP and SIGTERM, logging each to
// log (nil discards), until release is called; meanwhile the commands
// host.Real starts run in sessions of their own (host.OwnSessions). The
// terminal's Ctrl-C and hangup, which a shell, sudo or the kernel send to
// hardn's whole process group, so stop neither hardn nor the ufw or
// systemctl the rollback is running: the commands are outside the group,
// and inherit SIGINT ignored besides ("signals set to be ignored (SIG_IGN)
// by the calling process image shall be set to be ignored by the new
// process image", POSIX exec), but not SIGHUP caught (a caught signal goes
// back to the default action). A command started otherwise than through
// host.Real during the hold has SIGINT ignored and SIGHUP's default.
// SIGKILL cannot be held off; the revert timer, or the next `hardn
// rollback <id>`, takes the rollback up.
//
// SIGHUP is caught, not ignored (since next/s3-hangup; the maintainer,
// 2026-10-28: "Yes, catch it"), so that a channel a caller registered for
// it with signal.Notify or signal.NotifyContext before the hold receives
// a hangup during it, and after it: cmd/hardn's screens end hardn after
// the apply that saw one, whose rollback this is. An ignored signal is
// discarded by the kernel and reaches no channel.
//
// Release gives SIGHUP and SIGTERM back the disposition they had, as
// os/signal's Stop does: the caller's registrations, or, with none, the
// default action (Go's handler: a hangup ends the program) or, for a
// signal hardn was started with ignored (nohup), ignored. It gives SIGINT
// back the disposition it had when the hold began: the default action
// (Go's handler, so a Ctrl-C ends the program and a command started
// afterwards gets the default too), or, for SIGINT ignored from the start,
// ignored. Not so after a signal.Notify of SIGINT before the hold (a
// caller's signal.NotifyContext, as cmd/hardn's around an apply): os/signal
// then reports it not ignored (signal.Ignored), and release gives it the
// default action. signal.Ignore sets the operating system's handler to
// SIG_IGN, and os/signal cannot undo that: a signal.Notify after it
// records SIG_IGN as the handler to go back to, which signal.Stop then
// puts back (Go 1.27 runtime/signal_unix.go, sigenable and sigdisable). So
// release sets SIGINT's default action itself first (restoreDefault, a
// sigaction(2) call; on Linux amd64 and arm64 and on macOS), then has the
// runtime install its handler again (a signal.Notify, then signal.Stop).
// Where the platform has no restore it logs that SIGINT stays ignored. A
// channel the caller had registered for SIGINT before the hold does not
// receive it after it: signal.Ignore ended that registration, so the
// caller registers again if it still wants it. Holds do not nest, and a
// hold not released leaves every later command host.Real starts in a
// session of its own: a caller where a panic could be recovered above
// the rollback defers the release. Its lines begin "hardn: " (SignalsAs).
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
	sessions := host.OwnSessions()
	c := make(chan os.Signal, len(caught))
	for s := range caught {
		signal.Notify(c, s)
	}
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case s := <-c:
				logf(log, "%s: %s received; the rollback finishes first", name, caught[s])
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(c)
		close(done)
		<-stopped
		sessions()
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
