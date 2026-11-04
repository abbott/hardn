//go:build linux || darwin

package systemdetails

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestInterrupt: Ctrl-C at the screen's prompt aborts (prompt.ErrInterrupted, wrapping
// prompt.ErrAborted), writing nothing, as the end of the input does. The input is a
// pipe, a file as os.Stdin is; once the prompt is shown, SIGINT is sent to the test
// every 10 ms, as Ctrl-C sends it, until Run returns. One that lands outside a read
// reaches the Interrupter's guard instead of ending the test.
func TestInterrupt(t *testing.T) {
	sig := uitest.NewInterrupter(t)
	h := hosts["debian13"]()
	in, typed, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = typed.Close() })
	var out lockedBuffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	done := make(chan error, 1)
	go func() {
		done <- Run(context.Background(), Options{
			Prompter: nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(in, false), Out: env.Writer(&out)},
			System:   h, Facts: h.facts, Now: clock,
		})
	}()
	got := interrupt(t, sig, &out, "Enter selection", done)
	if !errors.Is(got, prompt.ErrInterrupted) || !errors.Is(got, prompt.ErrAborted) {
		t.Errorf("Run = %v, want Ctrl-C's abort:\n%s", got, out.String())
	}
	assertOnlyReads(t, "Ctrl-C", h)
}

// interrupt waits until out shows at, for 10 seconds at most, then sends SIGINT to the
// test every 10 ms until done, and returns what Run did; it fails the test when Run
// has not returned 10 seconds later.
// Each SIGINT is delivered before the next is sent or interrupt returns
// (uitest.Interrupter), so that none lands in a later case.
func interrupt(t *testing.T, sig *uitest.Interrupter, out *lockedBuffer, at string, done <-chan error) error {
	t.Helper()
	shown := time.After(10 * time.Second)
wait:
	for !strings.Contains(out.String(), at) {
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Millisecond):
		case <-shown:
			break wait
		}
	}
	timeout := time.After(10 * time.Second)
	for {
		if err := sig.Send(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Millisecond):
		case <-timeout:
			t.Fatalf("Run did not return on SIGINT:\n%s", out.String())
		}
	}
}

// lockedBuffer is a bytes.Buffer that the screen writes while the test reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
