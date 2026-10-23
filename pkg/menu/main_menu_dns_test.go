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
// (nil) or back (nav.ErrBack) to the main menu, drawn again under the
// screen, which shows the configuration as the screen left it; q
// (nav.ErrQuit) ends hardn as the main menu's q does, clearing the
// screen; the end of the input exits 130, Ctrl-C ends hardn by SIGINT,
// and any other error is printed and exits 1, as at every legacy prompt.
// With no screen set, a programming error, the item panics.
func TestDNSItem(t *testing.T) {
	failed := errors.New("dns screen: write: broken pipe")
	for _, tc := range []struct {
		name  string
		err   error
		again bool // the main menu is drawn again, then q typed
		code  int  // the exit status hardn ended with; 0: it did not end
		said  string
	}{
		{name: "return", err: nil, again: true},
		{name: "back", err: nav.ErrBack, again: true},
		{name: "quit", err: nav.ErrQuit},
		{name: "the end of the input", err: fmt.Errorf("form: %w", prompt.ErrEndOfInput), code: prompt.ExitInterrupted},
		{name: "ctrl-c", err: prompt.ErrInterrupted, code: int(sigint)},
		{name: "another error", err: failed, code: 1, said: failed.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Nameservers = []string{"192.0.2.53"}
			passes := 0
			m := liveMenu(t, plainEnv(), fixtureStatus(&passes), "3", "q")
			m.config = cfg
			calls := 0
			m.SetDNS(func() error {
				calls++
				fmt.Println("DNS SCREEN")
				cfg.Nameservers = append(cfg.Nameservers, "192.0.2.55") // the screen adopts hardn.yml as saved
				return tc.err
			})
			out, code, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			switch {
			case tc.code != 0 && (!ended || code != tc.code):
				t.Fatalf("ended %v with %d, want %d:\n%s", ended, code, tc.code, out)
			case tc.code == 0 && ended:
				t.Fatalf("ended hardn with %d:\n%s", code, out)
			}
			// The DNS screen opens on a cleared screen, after the main
			// menu's prompt and the 3 typed.
			if !strings.Contains(out, "[0-10 or q]: \n"+clearScreen+"DNS SCREEN\n") {
				t.Errorf("the DNS screen does not open on a cleared screen:\n%q", out)
			}
			wantPasses := map[bool]int{false: 1, true: 2}[tc.again]
			if calls != 1 || passes != wantPasses {
				t.Errorf("the screen ran %d times, the main menu drawn %d times; want 1 and %d", calls, passes, wantPasses)
			}
			if tc.again && !strings.Contains(out, "DNS SCREEN\n\nhardn ") {
				t.Errorf("the main menu is not drawn again under the screen, a blank line between:\n%q", out)
			}
			if tc.code == 0 && !strings.HasSuffix(out, clearScreen) {
				t.Errorf("q does not end with a cleared screen:\n%q", out)
			}
			if tc.said != "" && !strings.Contains(out, tc.said) {
				t.Errorf("printed %q, want %q", out, tc.said)
			}
			if !slices.Equal(m.config.Nameservers, []string{"192.0.2.53", "192.0.2.55"}) {
				t.Errorf("the main menu's configuration lists %q, not the screen's", m.config.Nameservers)
			}
		})
	}
	t.Run("no screen set", func(t *testing.T) {
		m := liveMenu(t, plainEnv(), fixtureStatus(new(int)), "3")
		_, _, ended := session(t, func() {
			defer func() {
				if recover() == nil {
					t.Error("the DNS item ran with no screen set")
				}
			}()
			m.ShowMainMenu("0.4.0", "", "")
		})
		if ended {
			t.Error("ended hardn; want the panic")
		}
	})
}
