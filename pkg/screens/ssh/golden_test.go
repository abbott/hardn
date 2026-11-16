package ssh_test

import (
	"bytes"
	"testing"

	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestScreenGoldens pins the screen on every host of sshHosts, opened and
// left with 0, on the core set: its rows, its words and its items; and on
// debian13 SSH keys, a sub-menu cleared on open and on 0. The files are
// testdata/golden/screen/<name>.golden.
func TestScreenGoldens(t *testing.T) {
	type golden struct {
		name, host string
		lines      []string
	}
	var all []golden
	for _, name := range sshHosts {
		all = append(all, golden{name, name, []string{"0"}})
	}
	all = append(all, golden{"keys-sub", "debian13", []string{"6", "0", "0"}})
	for _, g := range all {
		uitest.Golden(t, "screen/"+g.name, uitest.Core(), func(env term.Env) string {
			h := ready(t, g.host)
			var out bytes.Buffer
			p := playWith(h, false, func(o *sshscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, g.lines...), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", g.name, p.err)
			}
			return out.String()
		})
	}
}

// TestUnfixedGolden pins the screen on alpine-unfixed, which the gallery's
// menu does not list: item 5 and the alert pointing to it, between the rows
// and the section (the maintainer's drawing, 2026-11-08), SSH Auth with no
// line of the CVE's (2026-11-09). 27 lines at 80 columns, so at 80x24 its
// top three scroll off.
func TestUnfixedGolden(t *testing.T) {
	uitest.Golden(t, "screen/alpine-unfixed", uitest.Core(), func(env term.Env) string {
		h := ready(t, "alpine-unfixed")
		var out bytes.Buffer
		p := playWith(h, false, func(o *sshscreen.Options) {
			o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)}
		})
		if p.err != nil {
			t.Errorf("Run = %v", p.err)
		}
		return out.String()
	})
}
