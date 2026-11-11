package firewall_test

import (
	"bytes"
	"testing"

	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestScreenGoldens pins the screen on every host of screenHosts, opened and
// left with 0, on the core set: its drawn rows, its words and its items, which
// the parity table does not compare (parity-firewall.md §2's not-rows: the open
// screen inactive, active with and without a rule, not installed, with
// profiles, its status failing, sshd elsewhere than hardn.yml's sshPort). Its
// application profiles too, with two and with none. The files are
// testdata/golden/screen/<host>.golden.
func TestScreenGoldens(t *testing.T) {
	type golden struct {
		name, host string
		lines      []string
	}
	var all []golden
	for _, name := range screenHosts {
		all = append(all, golden{name, name, []string{"0"}})
	}
	all = append(all, golden{"profiles-sub", "profiles", []string{"2", "0", "0"}}, golden{"profiles-none-sub", "active22", []string{"2", "0", "0"}})
	for _, g := range all {
		uitest.Golden(t, "screen/"+g.name, uitest.Core(), func(env term.Env) string {
			h, cfg := screenHost(t, g.host)
			var out bytes.Buffer
			p := playWith(system(g.host, h), h, cfg, false, func(o *fwscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, g.lines...), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", g.name, p.err)
			}
			return out.String()
		})
	}
}

// TestRefusalGolden pins a refusal said before asking, on the core set:
// Rocky Linux without firewalld and with no enabled repository, Install
// firewalld chosen, refused as a warning in screenkit.RefusalWords, no
// review or question; the Auto Updates screen's Enable says it alike. The
// file is testdata/golden/refusal/no-repos.golden.
func TestRefusalGolden(t *testing.T) {
	uitest.Golden(t, "refusal/no-repos", uitest.Core(), func(env term.Env) string {
		h, cfg := screenHost(t, "rocky-missing")
		h.fwd.noRepos = true
		var out bytes.Buffer
		p := playWith(h, h, cfg, false, func(o *fwscreen.Options) {
			o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "1", "0"), false), Out: env.Writer(&out)}
		})
		if p.err != nil || len(p.runs) != 0 {
			t.Errorf("Run = %v, runs %+v", p.err, p.runs)
		}
		return out.String()
	})
}
