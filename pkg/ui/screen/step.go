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
// glyphs, its fixture's screen (ManageSudoFixture), the blank lines being
// theme.SectionGap:
//
//	hardn  v0.4.0              https://github.com/abbott/hardn
//
//	  hardn > User Management > Manage User
//
//	  Managing sudo method for: [george]
//
// The blocks: Header, Breadcrumb, SubjectLine; one that renders nothing is
// left out with its gap. Figma's frame opens with a "Manage User" TitleBar
// where the brief's layout has the Header and a Breadcrumb, whose current
// item takes the title bar's words (P-08, DEV-054).
type WorkflowStep struct {
	// Header is the wordmark, the version and the repository link, as on
	// the main menu.
	Header component.Header
	// Breadcrumb says where the action sits, such as "hardn › User
	// Management › Manage User".
	Breadcrumb component.Breadcrumb
	// Subject names what the action changes, such as "Managing sudo
	// method for: george"; with no subject it is left out.
	Subject component.SubjectLine
}

// Render renders the screen above the flow for c. See WorkflowStep for the
// layout.
func (vm WorkflowStep) Render(c component.Ctx) string {
	return join(vm.Header.Render(c), vm.Breadcrumb.Render(c), vm.Subject.Render(c))
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
