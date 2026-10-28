package menu

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/term"
)

// planOnlySources are the starts the refusals of decision 6 (refactor
// plan slice 2b, unit (d)) are tested from: live, then plan only from -n
// and from hardn.yml's dryRun: true, each as cmd/hardn resolves it.
var planOnlySources = []struct {
	name     string
	planOnly bool
	start    func(t *testing.T) *config.Config
}{
	{"live", false, func(*testing.T) *config.Config { c := config.DefaultConfig(); c.ResolveDryRun(false, false); return c }},
	{"-n", true, func(*testing.T) *config.Config { c := config.DefaultConfig(); c.ResolveDryRun(true, true); return c }},
	{"hardn.yml", true, func(t *testing.T) *config.Config {
		f := hostfake.New(host.Options{})
		f.AddFile(hardnYML, []byte("dryRun: true\n"), 0o644, 0, 0)
		c, err := config.Read(context.Background(), f, hardnYML)
		if err != nil {
			t.Fatal(err)
		}
		c.ResolveDryRun(false, false)
		return c
	}},
}

// TestPlanOnlyRefusals: under plan only, from -n or hardn.yml, the main
// menu's User Management (1) is refused under the prompt, "Not available
// in plan only.", as a key no item has is refused: no screen built or
// opened, so nothing it writes is written, the main menu not read or
// drawn again, its hint unchanged; and in Firewall's profiles, adding and
// removing one are refused in that screen's words for a choice it does
// not take, before anything is asked or ./hardn.yml saved, the list
// unchanged. Live, all three run as before: the screens open, and a
// profile added or removed is saved. Backup (5) opens under plan only as
// live (slice 3, unit (d), decision 10: its screen stops each change at
// its plan), given the configuration in plan only as the item is chosen.
func TestPlanOnlyRefusals(t *testing.T) {
	for _, src := range planOnlySources {
		t.Run("main menu, "+src.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			passes := 0
			m := liveMenu(t, plainEnv(), fixtureStatus(&passes), "1", "5", "q")
			m.config = src.start(t)
			opened := recordScreens(t, m)
			backupInPlanOnly := false
			m.SetBackup(func() error {
				backupInPlanOnly = m.config.DryRun
				*opened = append(*opened, "backup")
				return nil
			})
			out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			const asked = "  > Enter selection [0-5, 7-10 or q]: \n"
			refused, want, reads := strings.Count(out, "  (x) Not available in plan only.\n"+asked), []string{"backup"}, 2
			if !src.planOnly {
				want, reads = []string{"users", "backup"}, 3
			}
			if ended || !slices.Equal(*opened, want) || refused != map[bool]int{true: 1}[src.planOnly] || passes != reads {
				t.Errorf("ended %v, opened %q, %d refused, read %d times; want %q, read %d times:\n%s", ended, *opened, refused, passes, want, reads, out)
			}
			if backupInPlanOnly != src.planOnly {
				t.Errorf("the Backup screen ran with the configuration's DryRun %t, want %t", backupInPlanOnly, src.planOnly)
			}
			if _, err := os.Stat("hardn.yml"); err == nil {
				t.Error("./hardn.yml was written")
			}
		})
	}
	t.Run("main menu, Unicode", func(t *testing.T) {
		env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80,
			Environ: []string{"TERM=xterm-256color", "LANG=C.UTF-8", "NO_COLOR=1"}})
		m := liveMenu(t, env, fixtureStatus(new(int)), "1", "q")
		m.config = planOnlySources[1].start(t)
		recordScreens(t, m)
		out, _, _ := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
		if !strings.Contains(style.StripAnsi(out), "\n   ⨳  Not available in plan only.\n") {
			t.Errorf("want the refusal under the prompt with ⨳:\n%s", out)
		}
	})

	profile := config.UfwAppProfile{Name: "web", Title: "Web", Description: "Web server", Ports: []string{"80/tcp"}}
	for _, src := range planOnlySources {
		for _, tc := range []struct {
			action, script string
			saved          []config.UfwAppProfile // live: the profiles saved
			done           string                 // live: the screen's result
		}{
			{"add", "1\nmail\nMail\nMail server\n25/tcp\n", []config.UfwAppProfile{profile, {Name: "mail", Title: "Mail", Description: "Mail server", Ports: []string{"25/tcp"}}},
				"Application profile 'mail' added successfully"},
			{"remove", "2\n1\ny\n", []config.UfwAppProfile{}, "Application profile 'web' removed successfully"},
		} {
			t.Run("firewall profiles, "+tc.action+", "+src.name, func(t *testing.T) {
				t.Chdir(t.TempDir())
				cfg := src.start(t)
				cfg.UfwAppProfiles = []config.UfwAppProfile{profile}
				script := tc.script + "0\nnext\n"
				if src.planOnly {
					script = tc.script[:2] + "\n0\nnext\n" // the choice, then Enter at the pause
				}
				scripted(t, script)
				out, code, ended := session(t, NewFirewallMenu(nil, cfg, nil).manageAppProfiles)
				if ended {
					t.Fatalf("ended hardn with %d:\n%s", code, out)
				}
				plain := style.StripAnsi(out)
				_, err := os.Stat("hardn.yml")
				if src.planOnly {
					if !strings.Contains(plain, "\n"+style.SymCrossMark+" Not available in plan only.\n\n"+style.SymRightCarrot+" Press Enter to continue...") ||
						strings.Contains(plain, "Enter profile") || err == nil || !slices.EqualFunc(cfg.UfwAppProfiles, []config.UfwAppProfile{profile}, profilesEqual) {
						t.Errorf("want the refusal and the pause, nothing asked, the list %+v unchanged and ./hardn.yml unsaved (%v):\n%s", cfg.UfwAppProfiles, err, plain)
					}
				} else {
					saved, rerr := os.ReadFile("hardn.yml")
					if strings.Contains(plain, "Not available") || !strings.Contains(plain, tc.done) || rerr != nil ||
						!slices.EqualFunc(cfg.UfwAppProfiles, tc.saved, profilesEqual) || !strings.Contains(string(saved), "ufwAppProfiles:") {
						t.Errorf("want %q, the list %+v and ./hardn.yml saved (%v):\n%s\n%s", tc.done, cfg.UfwAppProfiles, rerr, plain, saved)
					}
				}
				var next string
				if _, _, ended := session(t, func() { next = ReadInput() }); ended || next != "next" {
					t.Errorf("the line after the screen's = %q (ended %v): the screen read too few or too many lines", next, ended)
				}
			})
		}
	}
}

// profilesEqual reports whether a and b are the same profile.
func profilesEqual(a, b config.UfwAppProfile) bool {
	return a.Name == b.Name && a.Title == b.Title && a.Description == b.Description && slices.Equal(a.Ports, b.Ports)
}
