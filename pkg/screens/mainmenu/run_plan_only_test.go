package mainmenu

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// planOnlySources are the starts the main menu's items are tested from
// (refactor plan slice 2b, unit (d), decision 6; slice 5, unit (d)): live, then plan only from -n
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

// TestPlanOnly: under plan only, from -n or hardn.yml, the main menu's
// User Management (1), SSH Login (2), Firewall (4), Backup (5), Auto
// Updates (6), Run All (7) and Environment (8) open as live (slice 8,
// unit (d), Options.DryRun of pkg/screens/runall; slice 7, unit (d); slice
// 6, unit (d); slice 5, unit (d),
// which lifts item 1's "Not available in plan only." as its screen goes
// on the flow; slice 4, unit (d); slice 3, unit
// (d), decision 10; slice 9, unit (d): their screens stop each change at
// its plan), each given the configuration in plan only as the item is
// chosen; no line refused, ./hardn.yml not written.
func TestPlanOnly(t *testing.T) {
	for _, src := range planOnlySources {
		t.Run("main menu, "+src.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			passes := 0
			m := liveMenu(t, plainEnv(), fixtureStatus(&passes), "1", "2", "4", "5", "6", "7", "8", "q")
			m.Config = src.start(t)
			opened := recordScreens(t, m)
			inPlanOnly := map[string]bool{}
			for name, set := range map[string]func(func() error){
				"users":       func(run func() error) { m.Users = run },
				"ssh":         func(run func() error) { m.SSH = run },
				"firewall":    func(run func() error) { m.Firewall = run },
				"backup":      func(run func() error) { m.Backup = run },
				"updates":     func(run func() error) { m.Updates = run },
				"runAll":      func(run func() error) { m.RunAll = run },
				"environment": func(run func() error) { m.Environment = run },
			} {
				set(func() error {
					inPlanOnly[name] = m.Config.DryRun
					*opened = append(*opened, name)
					return nil
				})
			}
			out, _, ended := session(t, func() { show(m) })
			want := []string{"users", "ssh", "firewall", "backup", "updates", "runAll", "environment"}
			if ended || !slices.Equal(*opened, want) || strings.Contains(out, "(x) ") || passes != 8 {
				t.Errorf("ended %v, opened %q, read %d times; want %q, none refused, read 8 times:\n%s", ended, *opened, passes, want, out)
			}
			for _, name := range want {
				if inPlanOnly[name] != src.planOnly {
					t.Errorf("the %s screen ran with the configuration's DryRun %t, want %t", name, inPlanOnly[name], src.planOnly)
				}
			}
			if _, err := os.Stat("hardn.yml"); err == nil {
				t.Error("./hardn.yml was written")
			}
		})
	}
}
