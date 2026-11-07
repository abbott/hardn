package mainmenu

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
)

// TestDNSItem: the main menu's 4 runs the DNS screen cmd/hardn sets
// (Options.DNS) from a cleared screen, once, and maps what it returns: Return
// (nil) or back (nav.ErrBack) to the main menu, drawn again on a cleared
// screen (under the screen, off a terminal), which shows the configuration
// as the screen left it; q
// (nav.ErrQuit) ends hardn as the main menu's q does, clearing the
// screen; the end of the input exits 130, Ctrl-C ends hardn by SIGINT,
// and any other error is printed and exits 1, as at every legacy prompt.
// With no screen set, a programming error, the item panics.
func TestDNSItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "4", name: "DNS", set: setDNS,
		edit: func(c *config.Config) { c.Nameservers = append(c.Nameservers, "192.0.2.55") },
		edited: func(c *config.Config) bool {
			return slices.Equal(c.Nameservers, []string{"192.0.2.53", "192.0.2.55"})
		},
	})
}

// TestBackupItem: the main menu's 7 runs the Backup screen cmd/hardn sets
// (Options.Backup), as TestDNSItem's 4 runs the DNS screen, and maps what it
// returns the same way (refactor plan slice 3, unit (d)); the main menu
// holds the backup settings as the screen left them.
func TestBackupItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "7", name: "BACKUP", set: setBackup,
		edit:   func(c *config.Config) { c.BackupPath = "/srv/backups/hardn" },
		edited: func(c *config.Config) bool { return c.BackupPath == "/srv/backups/hardn" },
	})
}

// TestEnvironmentItem: the main menu's 8 runs the Environment screen
// cmd/hardn sets (Options.Environment), as TestDNSItem's 4 runs the DNS screen,
// and maps what it returns the same way (refactor plan slice 9, unit (d));
// the screen saves no setting, so the main menu's configuration is as it
// was.
func TestEnvironmentItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "8", name: "ENVIRONMENT", set: setEnvironment,
		edit: func(*config.Config) {},
		edited: func(c *config.Config) bool {
			return slices.Equal(c.Nameservers, []string{"192.0.2.53"}) && c.BackupPath == config.DefaultConfig().BackupPath
		},
	})
}

// TestFirewallItem: the main menu's 3 runs the Firewall screen cmd/hardn
// sets (Options.Firewall), as TestDNSItem's 4 runs the DNS screen, and maps what
// it returns the same way (refactor plan slice 4, unit (d)); the main menu
// holds the application profiles as the screen left them.
func TestFirewallItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "3", name: "FIREWALL", set: func(m *Options, run func() error) { m.Firewall = run },
		edit: func(c *config.Config) {
			c.UfwAppProfiles = append(c.UfwAppProfiles, config.UfwAppProfile{Name: "WebServer", Title: "Web", Description: "Web", Ports: []string{"80/tcp"}})
		},
		edited: func(c *config.Config) bool {
			return len(c.UfwAppProfiles) == 1 && c.UfwAppProfiles[0].Name == "WebServer"
		},
	})
}

// TestFirewallItemOnPVE: the main menu's 3 reads "View pve-firewall" where
// cmd/hardn says the host is Proxmox VE (Options.Firewall's onPVE; decision 7,
// adopted 2026-11-01), and origin/main's "Configure UFW rules" where it
// says not, or says nothing; its key, label and action are the same.
func TestFirewallItemOnPVE(t *testing.T) {
	for _, tc := range []struct {
		name  string
		onPVE func() bool
		want  string
	}{
		{"Proxmox VE", func() bool { return true }, "View pve-firewall"},
		{"another host", func() bool { return false }, "Configure UFW rules"},
		{"not said", nil, "Configure UFW rules"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Options{Config: config.DefaultConfig()}
			m.Firewall, m.FirewallPVE = func() error { return nil }, tc.onPVE
			for _, o := range m.items().Options {
				if o.Item.Key == "3" {
					if o.Item.Label != "Firewall" || o.Item.Description != tc.want {
						t.Errorf("item 3 is %q, %q; want Firewall, %q", o.Item.Label, o.Item.Description, tc.want)
					}
					return
				}
			}
			t.Error("no item 3")
		})
	}
}

// TestUsersItem: the main menu's 1 runs the User Management screen
// cmd/hardn sets (Options.Users), as TestDNSItem's 4 runs the DNS screen, and
// maps what it returns the same way (refactor plan slice 5, unit (d)); no
// change the screen makes saves hardn.yml, so the main menu's
// configuration is as it was.
func TestUsersItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "1", name: "USERS", set: setUsers,
		edit: func(*config.Config) {},
		edited: func(c *config.Config) bool {
			return slices.Equal(c.Nameservers, []string{"192.0.2.53"}) && c.Username == config.DefaultConfig().Username
		},
	})
}

// TestSSHItem: the main menu's 2 runs the SSH Login screen cmd/hardn sets
// (Options.SSH), as TestDNSItem's 4 runs the DNS screen, and maps what it
// returns the same way (refactor plan slice 6, unit (d)); no change the
// screen makes saves hardn.yml, so the main menu's configuration is as it
// was.
func TestSSHItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "2", name: "SSH", set: setSSH,
		edit: func(*config.Config) {},
		edited: func(c *config.Config) bool {
			return slices.Equal(c.Nameservers, []string{"192.0.2.53"}) && c.SshPort == config.DefaultConfig().SshPort
		},
	})
}

// TestUpdatesItem: the main menu's 5 runs the Auto Updates screen
// cmd/hardn sets (Options.Updates), as TestDNSItem's 4 runs the DNS screen, and
// maps what it returns the same way (refactor plan slice 7, unit (d));
// the main menu holds enableUnattendedUpgrades as the screen's off saved
// it.
func TestUpdatesItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "5", name: "UPDATES", set: setUpdates,
		edit:   func(c *config.Config) { c.EnableUnattendedUpgrades = false },
		edited: func(c *config.Config) bool { return !c.EnableUnattendedUpgrades },
	})
}

// TestRunAllItem: the main menu's 6 runs the Run All screen cmd/hardn sets
// (Options.RunAll), as TestDNSItem's 4 runs the DNS screen, and maps what it
// returns the same way (refactor plan slice 8, unit (d)); no change the
// screen makes saves hardn.yml, so the main menu's configuration is as it
// was.
func TestRunAllItem(t *testing.T) {
	testScreenItem(t, screenItem{
		key: "6", name: "RUN ALL", set: setRunAll,
		edit: func(*config.Config) {},
		edited: func(c *config.Config) bool {
			return slices.Equal(c.Nameservers, []string{"192.0.2.53"}) && c.Username == config.DefaultConfig().Username
		},
	})
}

// TestSystemDetailsAndLogsItems: the main menu's 9 and 10 run the screens cmd/hardn
// sets, and map what they return as TestDNSItem's 3 does (refactor plan slice 1, unit
// (d)); neither saves a setting.
func TestSystemDetailsAndLogsItems(t *testing.T) {
	for _, it := range []screenItem{
		{key: "9", name: "SYSTEM DETAILS", set: setSystemDetails},
		{key: "10", name: "LOGS", set: setLogs},
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
	set       func(*Options, func() error)
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
		for _, env := range []term.Env{ttyEnv(), plainEnv()} {
			t.Run(tc.name+map[bool]string{false: ", on a terminal", true: ", off one"}[env.Plain], func(t *testing.T) {
				cfg := config.DefaultConfig()
				cfg.Nameservers = []string{"192.0.2.53"}
				passes := 0
				m := liveMenu(t, env, fixtureStatus(&passes), item.key, "q")
				m.Config = cfg
				calls := 0
				item.set(m, func() error {
					calls++
					fmt.Println(said)
					item.edit(cfg) // the screen adopts hardn.yml as saved
					return tc.err
				})
				out, code, ended := session(t, func() { show(m) })
				switch {
				case tc.code != 0 && (!ended || code != tc.code):
					t.Fatalf("ended %v with %d, want %d:\n%s", ended, code, tc.code, out)
				case tc.code == 0 && ended:
					t.Fatalf("ended hardn with %d:\n%s", code, out)
				}
				// The screen opens on a cleared screen on a terminal, after the
				// main menu's prompt and the key typed; off one nothing clears.
				noClearOff(t, env, out)
				if !strings.Contains(out, "[0-10 or q]: \n"+clearGapOn(env)+said+"\n") {
					t.Errorf("the screen does not open on a cleared screen:\n%q", out)
				}
				wantPasses := map[bool]int{false: 1, true: 2}[tc.again]
				if calls != 1 || passes != wantPasses {
					t.Errorf("the screen ran %d times, the main menu drawn %d times; want 1 and %d", calls, passes, wantPasses)
				}
				if tc.again && !strings.Contains(out, said+"\n"+clearGapOn(env)+"\n") {
					t.Errorf("the main menu is not drawn again on a cleared screen, two blank lines at its top (off a terminal, under the screen, a blank line between):\n%q", out)
				}
				if tc.code == 0 && !strings.HasSuffix(out, clearTopOn(env)) {
					t.Errorf("q does not end with a cleared screen, two blank lines at its top:\n%q", out)
				}
				if tc.said != "" && !strings.Contains(out, tc.said) {
					t.Errorf("printed %q, want %q", out, tc.said)
				}
				if !item.edited(m.Config) {
					t.Errorf("the main menu's configuration is not the screen's: %+v", m.Config)
				}
			})
		}
	}
	t.Run("no screen set", func(t *testing.T) {
		m := liveMenu(t, plainEnv(), fixtureStatus(new(int)), item.key)
		_, _, ended := session(t, func() {
			defer func() {
				if recover() == nil {
					t.Errorf("item %s ran with no screen set", item.key)
				}
			}()
			show(m)
		})
		if ended {
			t.Error("ended hardn; want the panic")
		}
	})
}
