//go:build linux || darwin

package mainmenu

import (
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestMainMenuInterrupt checks that Ctrl-C at the main menu's prompt, on
// its layout (nav.Run asking through prompt.Stdin's reader), ends hardn by
// SIGINT (reraise) once the main menu is drawn, after ending the prompt's
// line: SIGINT is sent to the test process every 10 ms while the read
// waits on a pipe that never ends, as a terminal does. origin/main dropped
// Ctrl-C at its menu prompts, read in raw mode (side-effects D6).
// cmd/hardn's TestInterruptChild checks that the process then dies of the
// signal.
func TestMainMenuInterrupt(t *testing.T) {
	guard := make(chan os.Signal, 64)
	signal.Notify(guard, os.Interrupt)
	t.Cleanup(func() { signal.Stop(guard) })
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pr.Close(); _ = pw.Close() })
	m := liveMenu(t, plainEnv(), fixtureStatus(new(int)))
	useReader(t, prompt.New(pr, false))
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
				_, _ = pw.WriteString("q\n")
				return
			case <-tick.C:
				_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
			}
		}
	}()
	out, code, ended := session(t, func() { show(m) })
	if !ended || exited(code) != sigint || !strings.HasSuffix(out, "Enter selection [0-10 or q]: \n") {
		t.Errorf("ended %v with %d, want hardn ended by SIGINT at the prompt, its line ended:\n%q", ended, code, out)
	}
}
