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
// drawn again, its hint unchanged. Live, it opens as before. (The
// Firewall menu's profile add and remove, refused here too, went with
// that menu in slice 4's unit (d).) Firewall (4), Backup (5) and
// Environment (8) open under plan only as live (slice 4, unit (d); slice
// 3, unit (d), decision 10; slice 9, unit (d): their screens stop each
// change at its plan), each given the configuration in plan only as the
// item is chosen.
func TestPlanOnlyRefusals(t *testing.T) {
	for _, src := range planOnlySources {
		t.Run("main menu, "+src.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			passes := 0
			m := liveMenu(t, plainEnv(), fixtureStatus(&passes), "1", "4", "5", "8", "q")
			m.config = src.start(t)
			opened := recordScreens(t, m)
			fwInPlanOnly, backupInPlanOnly, envInPlanOnly := false, false, false
			m.SetFirewall(func() error {
				fwInPlanOnly = m.config.DryRun
				*opened = append(*opened, "firewall")
				return nil
			}, nil)
			m.SetBackup(func() error {
				backupInPlanOnly = m.config.DryRun
				*opened = append(*opened, "backup")
				return nil
			})
			m.SetEnvironment(func() error {
				envInPlanOnly = m.config.DryRun
				*opened = append(*opened, "environment")
				return nil
			})
			out, _, ended := session(t, func() { m.ShowMainMenu("0.4.0", "", "") })
			const asked = "  > Enter selection [0-5, 7-10 or q]: \n"
			refused, want, reads := strings.Count(out, "  (x) Not available in plan only.\n"+asked), []string{"firewall", "backup", "environment"}, 4
			if !src.planOnly {
				want, reads = []string{"users", "firewall", "backup", "environment"}, 5
			}
			if ended || !slices.Equal(*opened, want) || refused != map[bool]int{true: 1}[src.planOnly] || passes != reads {
				t.Errorf("ended %v, opened %q, %d refused, read %d times; want %q, read %d times:\n%s", ended, *opened, refused, passes, want, reads, out)
			}
			if fwInPlanOnly != src.planOnly || backupInPlanOnly != src.planOnly || envInPlanOnly != src.planOnly {
				t.Errorf("the Firewall, Backup and Environment screens ran with the configuration's DryRun %t, %t and %t, want %t", fwInPlanOnly, backupInPlanOnly, envInPlanOnly, src.planOnly)
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
}
