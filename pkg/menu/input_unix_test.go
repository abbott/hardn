//go:build linux || darwin

package menu

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestInterrupt checks that Ctrl-C at any legacy read ends hardn with
// exit status 130, after ending the prompt's line: SIGINT is sent to the
// test process every 10 ms while the read waits on a pipe that never
// ends, as a terminal does. origin/main dropped Ctrl-C at its menu
// prompts, read in raw mode (side-effects D6), and left the terminal in
// cbreak mode when it killed hardn at a pause.
func TestInterrupt(t *testing.T) {
	// A SIGINT that lands outside the read's relay reaches guard rather
	// than ending the test binary.
	guard := make(chan os.Signal, 64)
	signal.Notify(guard, os.Interrupt)
	t.Cleanup(func() { signal.Stop(guard) })
	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			pr, pw, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
			useReader(t, prompt.New(pr, false))
			stop := make(chan struct{})
			go func() {
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
			out, code, ended := session(t, func() { r.read() })
			close(stop)
			if !ended || code != prompt.ExitInterrupted {
				t.Errorf("ended %v with %d, want exit status %d", ended, code, prompt.ExitInterrupted)
			}
			if out != "\n" {
				t.Errorf("printed %q, want the prompt's line ended", out)
			}
		})
	}
}
