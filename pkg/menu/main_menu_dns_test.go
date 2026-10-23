package menu

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// TestDNSItem: the main menu's 3 runs the DNS screen cmd/hardn sets
// (SetDNS) from a cleared screen, once, and maps what it returns: Return
// (nil) back to the main menu, which shows the configuration as the
// screen left it; q (nav.ErrQuit) ends hardn as the main menu's q does;
// the end of the input exits 130, Ctrl-C ends hardn by SIGINT, and any
// other error is printed and exits 1, as at every legacy prompt. With no
// screen set, a programming error, the item panics, printing nothing.
func TestDNSItem(t *testing.T) {
	const clear = "\033[H\033[2J"
	failed := errors.New("dns screen: write: broken pipe")
	for _, tc := range []struct {
		name string
		err  error
		quit bool // handleMenuChoice reports that hardn should end
		code int  // the exit status hardn ended with; 0: it did not end
		said string
	}{
		{name: "return", err: nil},
		{name: "quit", err: nav.ErrQuit, quit: true},
		{name: "the end of the input", err: fmt.Errorf("form: %w", prompt.ErrEndOfInput), code: prompt.ExitInterrupted},
		{name: "ctrl-c", err: prompt.ErrInterrupted, code: int(sigint)},
		{name: "another error", err: failed, code: 1, said: failed.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scripted(t, "")
			cfg := config.DefaultConfig()
			cfg.Nameservers = []string{"192.0.2.53"}
			m := NewMainMenu(nil, cfg, nil, nil)
			calls := 0
			m.SetDNS(func() error {
				calls++
				cfg.Nameservers = append(cfg.Nameservers, "192.0.2.55") // the screen adopts hardn.yml as saved
				return tc.err
			})
			var quit bool
			out, code, ended := session(t, func() { quit = m.handleMenuChoice("3") })
			switch {
			case tc.code != 0 && (!ended || code != tc.code):
				t.Fatalf("ended %v with %d, want %d:\n%s", ended, code, tc.code, out)
			case tc.code == 0 && ended:
				t.Fatalf("ended hardn with %d:\n%s", code, out)
			case tc.code == 0 && quit != tc.quit:
				t.Errorf("handleMenuChoice = %v, want %v", quit, tc.quit)
			}
			if !strings.HasPrefix(out, clear) {
				t.Errorf("the screen is not cleared first: %q", out)
			}
			if !strings.Contains(out, tc.said) || tc.said == "" && out != clear {
				t.Errorf("printed %q, want %q after the clear", out, tc.said)
			}
			if want := 1; calls != want {
				t.Errorf("the screen ran %d times, want %d", calls, want)
			}
			if !slices.Equal(m.config.Nameservers, []string{"192.0.2.53", "192.0.2.55"}) {
				t.Errorf("the main menu's configuration lists %q, not the screen's", m.config.Nameservers)
			}
		})
	}
	t.Run("no screen set", func(t *testing.T) {
		scripted(t, "")
		m := NewMainMenu(nil, config.DefaultConfig(), nil, nil)
		out, _, ended := session(t, func() {
			defer func() {
				if recover() == nil {
					t.Error("the DNS item ran with no screen set")
				}
			}()
			m.handleMenuChoice("3")
		})
		if ended || out != "" {
			t.Errorf("ended %v, printed %q", ended, out)
		}
	})
}
