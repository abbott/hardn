package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// holdSignals keeps a hangup, Ctrl-C or SIGTERM from stopping the
// in-process rollback part-way, until the returned function is called, as
// cmd/hardn's holdSignals does for `hardn rollback` (docs/safety/revert.md
// §2.17; revert.Rollback leaves signals to its caller). SIGHUP and SIGINT
// are ignored, which the commands the rollback runs inherit: host.Real's
// children stay in p2drive's process group, so the terminal's Ctrl-C would
// otherwise reach the ufw or systemctl the rollback is running. SIGTERM is
// caught and logged to log. Release puts Go's default handlers back for
// SIGHUP and SIGINT: once the rollback is done, a Ctrl-C ends p2drive.
func holdSignals(log io.Writer) (release func()) {
	signal.Ignore(syscall.SIGHUP, syscall.SIGINT)
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-term:
				fmt.Fprintln(log, "p2drive: SIGTERM received; the rollback finishes first")
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(term)
		close(done)
		<-stopped
		// Notify then Stop puts Go's own handler back, ending the Ignore.
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGHUP, syscall.SIGINT)
		signal.Stop(c)
	}
}
