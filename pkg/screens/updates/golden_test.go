package updates_test

import (
	"bytes"
	"testing"

	updscreen "github.com/abbott/hardn/pkg/screens/updates"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestScreenGoldens pins the screen on every host of the gallery's menu
// (hosts), opened and left with 0, on the core set: its row, its section,
// its words and its item. The files are
// testdata/golden/screen/<host>.golden.
func TestScreenGoldens(t *testing.T) {
	for _, name := range hosts {
		uitest.Golden(t, "screen/"+name, uitest.Core(), func(env term.Env) string {
			m := newHost(t, name)
			var out bytes.Buffer
			p := playWith(m, false, func(o *updscreen.Options) {
				o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)}
			})
			if p.err != nil {
				t.Errorf("%s: Run = %v", name, p.err)
			}
			return out.String()
		})
	}
}
