//go:build linux || darwin

package uitest

import (
	"os"
	"os/signal"
	"testing"
	"time"
)

// TestInterrupter checks that each Send's SIGINT has been handed to the
// channels registered for it when Send returns, so that one registered
// after cannot get it: a probe registered before every Send holds every
// SIGINT, and one registered after it holds none, over many Sends.
func TestInterrupter(t *testing.T) {
	in := NewInterrupter(t)
	for i := range 200 {
		before := make(chan os.Signal, 1)
		signal.Notify(before, os.Interrupt)
		if err := in.Send(); err != nil {
			t.Fatal(err)
		}
		after := make(chan os.Signal, 1)
		signal.Notify(after, os.Interrupt)
		select {
		case <-before:
		case <-time.After(5 * time.Second):
			t.Fatalf("Send %d: the channel registered before it got no SIGINT", i)
		}
		signal.Stop(before)
		signal.Stop(after)
		if len(after) > 0 {
			t.Fatalf("Send %d: the channel registered after it got its SIGINT", i)
		}
	}
}
