package runall_test

import (
	"bytes"
	"testing"

	runallscreen "github.com/abbott/hardn/pkg/screens/runall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestScreenGoldens pins the screen on every host of runHosts, opened and
// left with 0, on the core set: its rows, its words and its item. The
// files are testdata/golden/screen/<host>.golden.
func TestScreenGoldens(t *testing.T) {
	for _, name := range runHosts {
		uitest.Golden(t, "screen/"+name, uitest.Core(), func(env term.Env) string {
			h := newHost(t, name)
			var out bytes.Buffer
			p := playWith(h, false, func(o *runallscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", name, p.err)
			}
			return out.String()
		})
	}
}
