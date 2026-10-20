package output

import (
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// engine.TitleMax is the longest title a step's line shows whole, in
// every state, in every cell of the design system's core set (80 columns,
// plain, 60 columns) and wider, with either glyph set: one cell more is cut
// with the ellipsis at 60 columns. The constant lives in pkg/engine, which
// may not import pkg/ui, so the two are pinned here.
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
	for _, cell := range uitest.Core() {
		for _, unicode := range []bool{true, false} {
			for _, width := range []int{cell.Env.Width, 120} {
				env := cell.Env
				env.Unicode, env.Width = unicode, width
				if cut(env, strings.Repeat("x", engine.TitleMax)) {
					t.Errorf("%s, width %d, unicode %v: a title of TitleMax (%d) cells is cut", cell.Name, width, unicode, engine.TitleMax)
				}
				if width == 60 && !cut(env, strings.Repeat("x", engine.TitleMax+1)) {
					t.Errorf("%s, unicode %v: a title of TitleMax+1 cells fits at 60 columns: TitleMax could be larger", cell.Name, unicode)
				}
			}
		}
	}
}
