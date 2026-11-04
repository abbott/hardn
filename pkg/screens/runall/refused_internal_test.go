package runall

import (
	"bytes"
	"context"
	"strings"
	"testing"

	planner "github.com/abbott/hardn/pkg/engine/runall"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestRefusedItem: a reading with rows planned whose one plan refuses
// draws the item "Refused", and choosing it says the refusal and asks
// nothing. No host reaches this state: decision 5's rule (root login off
// left until after the firewall where both are planned) prevents the one
// known conflict in the one plan, so the reading is built here.
func TestRefusedItem(t *testing.T) {
	const why = "The engine refuses the plan whole: a synthetic refusal."
	h := reading{
		Reading: planner.Reading{Rows: []planner.Row{{ID: "ssh.login", Title: "SSH Login", Kind: planner.Planned, Words: "Root login off"}}, Refusal: why},
		rows:    []screen.RunAllRow{{Label: "SSH Login", Outcome: "Root login off"}},
	}
	if st := h.view().State; st != screen.RunAllRefused {
		t.Errorf("state %v, want RunAllRefused", st)
	}
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, "1", "0")
	pr := nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)}
	r := newRunner(Options{Prompter: pr})
	r.reader = func(context.Context) reading { return h }
	err := pr.Run(func() nav.Screen { return r.draw(context.Background()) })
	flat := strings.Join(strings.Fields(out.String()), " ")
	if err != nil || ty.Unread() != 0 || !strings.Contains(flat, "1) Apply all "+planner.WordsRefused) ||
		!strings.Contains(flat, why) || strings.Contains(flat, "[y/N") || strings.Contains(flat, "Type the hostname") {
		t.Errorf("Run = %v, %d unread:\n%s", err, ty.Unread(), out.String())
	}
}
