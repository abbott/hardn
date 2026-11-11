package users_test

import (
	"bytes"
	"testing"

	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestScreenGoldens pins the screen on every host of usrHosts, opened and
// left with 0, on the core set: its drawn rows, its words and its items;
// and on debian13 the user list, george's screen and george's keys. The
// files are testdata/golden/screen/<host>.golden.
func TestScreenGoldens(t *testing.T) {
	type golden struct {
		name, host string
		lines      []string
	}
	var all []golden
	for _, name := range usrHosts {
		all = append(all, golden{name, name, []string{"0"}})
	}
	all = append(all, golden{"select", "debian13", []string{"1", "0", "0"}}, golden{"manage", "debian13", []string{"1", "1", "0", "0", "0"}},
		golden{"keys", "debian13", []string{"1", "1", "3", "0", "0", "0", "0"}})
	for _, g := range all {
		uitest.Golden(t, "screen/"+g.name, uitest.Core(), func(env term.Env) string {
			h := newHost(t, g.host)
			var out bytes.Buffer
			p := playWith(h, false, func(o *usrscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, g.lines...), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", g.name, p.err)
			}
			return out.String()
		})
	}
}
