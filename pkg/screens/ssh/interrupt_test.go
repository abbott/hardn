//go:build linux || darwin

package ssh_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestInterrupt: Ctrl-C at the screen, at the port's field and at the
// typed hostname aborts (prompt.ErrInterrupted, wrapping prompt.ErrAborted),
// writing nothing, as the end of the input does (TestNavigation). The input
// is a pipe, a file as os.Stdin is; once the prompt is shown, SIGINT is sent
// to the test itself every 10 ms, as Ctrl-C sends it, until Run returns. One
// that lands outside a read reaches the Interrupter's guard instead of ending
// the test.
func TestInterrupt(t *testing.T) {
	sig := uitest.NewInterrupter(t)
	for _, tt := range []struct {
		host, lines, at string
	}{
		{"debian13", "", "Enter your choice"}, {"debian13", "3\n", "New SSH port"}, {"debian13", "1\n", "Type the hostname"},
		{"pve9", "2\n", "Type the hostname"}, {"legacy-main", "4\n", "Type the hostname"},
	} {
		h := ready(t, tt.host)
		n := len(h.Calls())
		in, typed, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = in.Close(); _ = typed.Close() })
		if _, err := typed.WriteString(tt.lines); err != nil {
			t.Fatal(err)
		}
		var out lockedBuffer
		env := term.Resolve(term.Options{}, term.Facts{Width: 120})
		done := make(chan played, 1)
		go func() {
			done <- playWith(h, false, func(o *sshscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(in, false), Out: env.Writer(&out)}
			})
		}()
		p := interrupt(t, sig, &out, tt.at, done)
		if !errors.Is(p.err, prompt.ErrInterrupted) || !errors.Is(p.err, prompt.ErrAborted) {
			t.Errorf("%s %q: Run = %v, want Ctrl-C's abort:\n%s", tt.host, tt.lines, p.err, out.String())
		}
		if s := out.String(); !strings.Contains(s, tt.at) {
			t.Errorf("%s %q: %q not shown:\n%s", tt.host, tt.lines, tt.at, s)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// interrupt waits until out shows at, then sends SIGINT to the test every 10
// ms until done, and returns what Run did; it fails the test after 10
// seconds.
// Each SIGINT is delivered before the next is sent or interrupt returns
// (uitest.Interrupter), so that none lands in a later case.
func interrupt(t *testing.T, sig *uitest.Interrupter, out *lockedBuffer, at string, done <-chan played) played {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for !strings.Contains(out.String(), at) {
		select {
		case p := <-done:
			return p
		case <-time.After(10 * time.Millisecond):
		case <-timeout:
			t.Fatalf("%q not shown:\n%s", at, out.String())
		}
	}
	for {
		if err := sig.Send(); err != nil {
			t.Fatal(err)
		}
		select {
		case p := <-done:
			return p
		case <-time.After(10 * time.Millisecond):
		case <-timeout:
			t.Fatalf("Run did not return on SIGINT:\n%s", out.String())
		}
	}
}

// lockedBuffer is a bytes.Buffer that the screen writes while the test reads
// it.
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
