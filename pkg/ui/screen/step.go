package screen

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// WorkflowStep is the workflow-step layout (brief 1B), after Figma's
// USER-MANAGE_SUDO-CONFIRMATION: the screen that names an action, above
// one run of the canonical flow, which writes each of its steps (the form,
// the review, the plan, the confirmation, the progress, the result or the
// error report) and asks their prompts. Here with no colour and ASCII
// glyphs, its fixture's screen (ManageSudoFixture), the blank line being
// theme.SectionGap:
//
//	# Manage User ##################################################
//
//	  Managing sudo method for: [george]
//
// The blocks: the TitleBar and the SubjectLine; one that renders nothing
// is left out with its gap. It opens with Figma's title bar, as P-08
// decided (2026-10-17): no Header and no Breadcrumb line. The bar shows
// the screen's title alone; its section trail, "User Management › Manage
// User", shows only when TitleTrail is set (DEV-054).
type WorkflowStep struct {
	// Title is the title bar, such as "Manage User", with the sections
	// above the screen as its Parents, such as "User Management". The
	// parents show only when TitleTrail is set.
	Title component.TitleBar
	// Subject names what the action changes, such as "Managing sudo
	// method for: george"; with no subject it is left out.
	Subject component.SubjectLine
}

// Render renders the screen above the flow for c. See WorkflowStep for the
// layout.
func (vm WorkflowStep) Render(c component.Ctx) string {
	return vm.render(c, TitleTrail)
}

// render is Render with the trail shown when withTrail is set.
func (vm WorkflowStep) render(c component.Ctx, withTrail bool) string {
	return join(titleBar(vm.Title, withTrail).Render(c), vm.Subject.Render(c))
}

// Run writes the screen to p's writer in one write, then runs f through
// p and returns what f.Run returns; an error writing the screen returns
// flow.NotRun and runs nothing. A flow that f.Run would refuse before
// writing anything (flow.Flow.Check: no Engine or no Confirm, or ctx
// already done) returns flow.NotRun and that error with nothing written,
// so that no screen is left on the terminal above nothing.
//
// The screen is set apart by theme.SectionGap blank lines on the side the
// flow leaves bare: the flow, and the form it opens with, write their gap
// before their first block or field only once a prompt has been asked
// (prompt.Reader.Asked). So before any prompt, as when the workflow is the
// first thing hardn shows, the gap follows the screen; after one, as when
// a menu's action opens the workflow, it goes before the screen, which
// then stands apart from the answer that chose it, and the flow writes
// the gap under it.
func (vm WorkflowStep) Run(ctx context.Context, p nav.Prompter, f flow.Flow) (flow.Outcome, error) {
	if err := f.Check(ctx); err != nil {
		return flow.NotRun, err
	}
	if s := vm.Render(p.Ctx); s != "" {
		gap := strings.Repeat("\n", theme.SectionGap)
		out := s + "\n" + gap
		if p.In.Asked() {
			out = gap + s + "\n"
		}
		if _, err := io.WriteString(p.Out, out); err != nil {
			return flow.NotRun, fmt.Errorf("screen: write: %w", err)
		}
	}
	return f.Run(ctx, p)
}
