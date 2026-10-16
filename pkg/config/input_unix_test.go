//go:build linux || darwin

package config

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestInterrupt checks that Ctrl-C at the first-run question ends hardn
// with exit status 130, the status a shell reports for a process Ctrl-C
// ended (brief Phase 3), and prints nothing more: SIGINT is sent to the
// test process every 10 ms while the question waits on a pipe that never
// ends.
func TestInterrupt(t *testing.T) {
	// A SIGINT that lands outside the read's relay reaches guard rather
	// than ending the test binary.
	guard := make(chan os.Signal, 64)
	signal.Notify(guard, os.Interrupt)
	t.Cleanup(func() { signal.Stop(guard) })
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	useReader(t, prompt.New(pr, false))
	// The sender is stopped and waited for before guard is stopped
	// (cleanups run last first), so no SIGINT follows signal.Stop.
	stop, sent := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(stop); <-sent })
	go func() {
		defer close(sent)
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		giveUp := time.After(10 * time.Second)
		for {
			select {
			case <-stop:
				return
			case <-giveUp:
				_, _ = pw.WriteString("not interrupted\n")
				return
			case <-tick.C:
				_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
			}
		}
	}()
	out, code, ended := ask(t, func() { ShouldCreateDefaultConfig() })
	if !ended || code != prompt.ExitInterrupted {
		t.Errorf("ended %v with %d, want exit status %d", ended, code, prompt.ExitInterrupted)
	}
	if want := "No configuration file found. Would you like to create one? [Y/n]\n"; out != want {
		t.Errorf("printed %q, want %q", out, want)
	}
}
