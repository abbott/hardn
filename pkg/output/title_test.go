package output

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// engine.TitleMax is a title a step's row shows whole, in every state, in
// every cell of the design system's core set (80 columns, plain, 60
// columns) and wider, with either glyph set. The constant lives in
// pkg/engine, which may not import pkg/ui, so the two are pinned here.
//
// It was sized for the one-line row of P-24, whose word and colon took 13
// cells of a 60-column line. Since Figma's row grammar (P-42, the
// maintainer, 2026-10-22), a list of steps stacks below 64 columns, and
// wherever its status column cannot hold "Rolled back", each title alone
// on its line from column 6: at 60 columns a title of 54 cells shows
// whole, one more is cut. The test pins that bound too, so that TitleMax
// may rise to it in pkg/engine's own unit.
func TestTitleMaxFitsAStepLine(t *testing.T) {
	states := []component.StepState{component.StepPending, component.StepRunning, component.StepDone,
		component.StepFailed, component.StepRolledBack, component.StepSkipped}
	cut := func(env term.Env, title string) bool {
		c := component.NewCtx(env)
		for _, s := range states {
			if strings.Contains(component.StepProgress{Steps: []component.Step{{Title: title, State: s}}}.Render(c), c.Theme.Glyphs.Ellipsis) {
				return true
			}
		}
		return false
	}
	const atSixty = 60 - theme.LabelCol // a stacked title's cells at 60 columns
	for _, cell := range uitest.Core() {
		for _, unicode := range []bool{true, false} {
			for _, width := range []int{cell.Env.Width, 120} {
				env := cell.Env
				env.Unicode, env.Width = unicode, width
				if cut(env, strings.Repeat("x", engine.TitleMax)) {
					t.Errorf("%s, width %d, unicode %v: a title of TitleMax (%d) cells is cut", cell.Name, width, unicode, engine.TitleMax)
				}
				if width == 60 && (cut(env, strings.Repeat("x", atSixty)) || !cut(env, strings.Repeat("x", atSixty+1))) {
					t.Errorf("%s, unicode %v: at 60 columns a title of %d cells is not the longest shown whole", cell.Name, unicode, atSixty)
				}
			}
		}
	}
}
