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
	testScreenItem(t, screenItem{
		key: "3", name: "DNS", set: (*MainMenu).SetDNS,
		edit: func(c *config.Config) { c.Nameservers = append(c.Nameservers, "192.0.2.55") },
		edited: func(c *config.Config) bool {
			return slices.Equal(c.Nameservers, []string{"192.0.2.53", "192.0.2.55"})
		},
	})
}

// TestBackupItem: the main menu's 5 runs the Backup screen cmd/hardn sets
// (SetBackup), as TestDNSItem's 3 runs the DNS screen, and maps what it
// returns the same way (refactor plan slice 3, unit (d)); the main menu
// holds the backup settings as the screen left them.
func TestBackupItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "5", name: "BACKUP", set: (*MainMenu).SetBackup,
		edit:   func(c *config.Config) { c.BackupPath = "/srv/backups/hardn" },
		edited: func(c *config.Config) bool { return c.BackupPath == "/srv/backups/hardn" },
	})
}

// TestEnvironmentItem: the main menu's 8 runs the Environment screen
// cmd/hardn sets (SetEnvironment), as TestDNSItem's 3 runs the DNS screen,
// and maps what it returns the same way (refactor plan slice 9, unit (d));
// the screen saves no setting, so the main menu's configuration is as it
// was.
func TestEnvironmentItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "8", name: "ENVIRONMENT", set: (*MainMenu).SetEnvironment,
		edit: func(*config.Config) {},
		edited: func(c *config.Config) bool {
			return slices.Equal(c.Nameservers, []string{"192.0.2.53"}) && c.BackupPath == config.DefaultConfig().BackupPath
		},
	})
}

// TestSystemDetailsAndLogsItems: the main menu's 9 and 10 run the screens cmd/hardn
// sets, and map what they return as TestDNSItem's 3 does (refactor plan slice 1, unit
// (d)); neither saves a setting.
func TestSystemDetailsAndLogsItems(t *testing.T) {
	for _, it := range []screenItem{
		{key: "9", name: "SYSTEM DETAILS", set: (*MainMenu).SetSystemDetails},
		{key: "10", name: "LOGS", set: (*MainMenu).SetLogs},
	} {
		t.Run(it.name, func(t *testing.T) {
			it.edit = func(*config.Config) {}
			it.edited = func(c *config.Config) bool { return slices.Equal(c.Nameservers, []string{"192.0.2.53"}) }
			testScreenItem(t, it)
		})
	}
}

// screenItem is a main-menu item whose screen cmd/hardn sets: its key, the
// name its stand-in prints, its setter, and what the stand-in changes in
// the configuration, as the screen adopts hardn.yml as saved, and whether
// a configuration holds it.
type screenItem struct {
	key, name string
	set       func(*MainMenu, func() error)
	edit      func(*config.Config)
	edited    func(*config.Config) bool
}

// testScreenItem runs item's key against a stand-in screen returning each
// of what a screen returns, then with none set.
func testScreenItem(t *testing.T, item screenItem) {
	t.Helper()
	failed := errors.New("screen: write: broken pipe")
	said := item.name + " SCREEN"
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
			m := liveMenu(t, plainEnv(), fixtureStatus(&passes), item.key, "q")
			m.config = cfg
			calls := 0
			item.set(m, func() error {
				calls++
				fmt.Println(said)
				item.edit(cfg) // the screen adopts hardn.yml as saved
				return tc.err
			})
			out, code, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			switch {
			case tc.code != 0 && (!ended || code != tc.code):
				t.Fatalf("ended %v with %d, want %d:\n%s", ended, code, tc.code, out)
			case tc.code == 0 && ended:
				t.Fatalf("ended hardn with %d:\n%s", code, out)
			}
			// The screen opens on a cleared screen, after the main menu's
			// prompt and the key typed.
			if !strings.Contains(out, "[0-5, 7-10 or q]: \n"+clearScreen+said+"\n") {
				t.Errorf("the screen does not open on a cleared screen:\n%q", out)
			}
			wantPasses := map[bool]int{false: 1, true: 2}[tc.again]
			if calls != 1 || passes != wantPasses {
				t.Errorf("the screen ran %d times, the main menu drawn %d times; want 1 and %d", calls, passes, wantPasses)
			}
			if tc.again && !strings.Contains(out, said+"\n\nhardn ") {
				t.Errorf("the main menu is not drawn again under the screen, a blank line between:\n%q", out)
			}
			if tc.code == 0 && !strings.HasSuffix(out, clearScreen) {
				t.Errorf("q does not end with a cleared screen:\n%q", out)
			}
			if tc.said != "" && !strings.Contains(out, tc.said) {
				t.Errorf("printed %q, want %q", out, tc.said)
			}
			if !item.edited(m.config) {
				t.Errorf("the main menu's configuration is not the screen's: %+v", m.config)
			}
		})
	}
	t.Run("no screen set", func(t *testing.T) {
		m := liveMenu(t, plainEnv(), fixtureStatus(new(int)), item.key)
		_, _, ended := session(t, func() {
			defer func() {
				if recover() == nil {
					t.Errorf("item %s ran with no screen set", item.key)
				}
			}()
			m.ShowMainMenu("0.4.0", "", "")
		})
		if ended {
			t.Error("ended hardn; want the panic")
		}
	})
}
