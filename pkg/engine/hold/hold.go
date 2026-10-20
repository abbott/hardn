// Package hold keeps a hangup, Ctrl-C or SIGTERM from stopping hardn while
// it rolls a change back in its own process (refactor plan P3,
// next/p3-engine; docs/safety/revert.md §2.17). It is the hold cmd/hardn
// puts around `hardn rollback` and p2drive around its in-process rollback,
// shared for the engine; those two keep their own copies until their
// callers move onto the engine.
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
// Release puts Go's default handlers back for SIGHUP and SIGINT (a
// signal.Notify then signal.Stop ends the Ignore): a channel the caller had
// registered for them with signal.Notify or signal.NotifyContext before
// the hold no longer receives them, so once the rollback is done a Ctrl-C
// ends the program. Holds do not nest.
func Signals(log io.Writer) (release func()) {
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT)
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-term:
				if log != nil {
					_, _ = fmt.Fprintln(log, "hardn: SIGTERM received; the rollback finishes first")
				}
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(term)
		close(done)
		<-stopped
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGHUP, syscall.SIGINT)
		signal.Stop(c)
	}
}
