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
// left with 0, on the core set: its rows, its words and its items. The
// files are testdata/golden/screen/<host>.golden.
func TestScreenGoldens(t *testing.T) {
	for _, name := range sshHosts {
		uitest.Golden(t, "screen/"+name, uitest.Core(), func(env term.Env) string {
			h := ready(t, name)
			var out bytes.Buffer
			p := playWith(h, false, func(o *sshscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", name, p.err)
			}
			return out.String()
		})
	}
}
