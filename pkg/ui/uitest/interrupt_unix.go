//go:build linux || darwin

package uitest

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// deliverWait is how long Send waits for its SIGINT to be delivered.
const deliverWait = 10 * time.Second

// Interrupter plays the user pressing Ctrl-C, for tests that check Ctrl-C
// at a prompt the line reader (pkg/ui/prompt) reads from a pipe: it sends
// SIGINT to the test's own process, as the terminal sends it to hardn.
//
// It sends one SIGINT at a time, and Send returns only once os/signal has
// delivered it. A test that sent a stream of them instead, every 10 ms
// while Run was still to return, could go on with one still on its way:
// on a loaded machine, delivery takes longer than 10 ms. That SIGINT then
// interrupted the next case's first prompt, failing it ("... not shown"),
// or, sent in a test's last case, arrived once nothing caught SIGINT any
// more and killed the test binary ("signal: interrupt").
//
// While the test runs, a SIGINT that arrives when no prompt waits reaches
// the Interrupter's guard instead of ending the test binary, as it would
// end hardn.
type Interrupter struct {
	guard chan os.Signal
}

// NewInterrupter starts catching SIGINT for t, until t ends. Call it before
// the cleanup that stops a goroutine calling Send, so that the goroutine
// stops first (cleanups run last first).
func NewInterrupter(t testing.TB) *Interrupter {
	t.Helper()
	in := &Interrupter{guard: make(chan os.Signal, 1)}
	signal.Notify(in.guard, os.Interrupt)
	t.Cleanup(func() { signal.Stop(in.guard) })
	return in
}

// Send sends SIGINT to the process and waits until os/signal delivers it
// to the guard. os/signal hands a signal to every channel registered for
// it while holding the lock that signal.Notify takes, so a channel
// registered after Send returns, such as the next prompt's, cannot get
// it. Send may be called from any goroutine, but from one at a time. It
// fails if the SIGINT is not delivered within 10 seconds.
func (in *Interrupter) Send() error {
	// Drop one that came from outside the test, such as a Ctrl-C at the
	// terminal running go test, so that it cannot stand in for this one.
	select {
	case <-in.guard:
	default:
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		return fmt.Errorf("uitest: SIGINT: %w", err)
	}
	select {
	case <-in.guard:
		return nil
	case <-time.After(deliverWait):
		return fmt.Errorf("uitest: SIGINT not delivered within %v", deliverWait)
	}
}
