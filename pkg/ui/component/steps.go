package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// StepState is where one step of an apply stands: brief 1B's pending,
// running, done, failed and rolled back, and skipped, a step left out.
type StepState int

// The step states. The zero value is StepPending, a step not started.
const (
	StepPending StepState = iota
	StepRunning
	StepDone
	StepFailed
	StepRolledBack
	StepSkipped
)

// String returns the state's word, which a step shows before its title:
// brief 1B's state names, and "Skipped" (DEV-039; proposal P-24 asks the
// maintainer to approve them). A value outside the states is
// StepPending's.
func (s StepState) String() string {
	switch s {
	case StepRunning:
		return "Running"
	case StepDone:
		return "Done"
	case StepFailed:
		return "Failed"
	case StepRolledBack:
		return "Rolled back"
	case StepSkipped:
		return "Skipped"
	}
	return "Pending"
}

// glyph returns the state's glyph in g: for a running step, the spinner's
// frame for frame when animate is set, else Glyphs.Running.
func (s StepState) glyph(g theme.Glyphs, frame int, animate bool) string {
	switch s {
	case StepRunning:
		if frames := strings.Split(g.Spinner, ""); animate && len(frames) > 0 {
			return frames[(frame%len(frames)+len(frames))%len(frames)]
		}
		return g.Running
	case StepDone:
		return g.OK
	case StepFailed:
		return g.Bad
	case StepRolledBack:
		return g.RolledBack
	case StepSkipped:
		return g.Skipped
	}
	return g.Pending
}

// style returns the state's style in t, which colours its glyph and word.
func (s StepState) style(t theme.StepStyles) lipgloss.Style {
	switch s {
	case StepRunning:
		return t.Running
	case StepDone:
		return t.Done
	case StepFailed:
		return t.Failed
	case StepRolledBack:
		return t.RolledBack
	case StepSkipped:
		return t.Skipped
	}
	return t.Pending
}

// Step is one step of an apply, such as one planned change.
type Step struct {
	// Title names the step, such as "Set Port 2230". A step with no title
	// is not shown.
	Title string
	// State is where the step stands; the zero value is StepPending.
	State StepState
	// Detail follows the title in parentheses, in Step.Detail, as a menu
	// item's aside follows its description: why a step failed or was
	// skipped, such as "exit status 1". Give it without the parentheses;
	// empty shows none.
	Detail string
}

// row renders the step on one line for c, with the spinner at frame;
// ok is false for a step with no title.
func (s Step) row(c Ctx, frame int) (string, bool) {
	t := c.Theme.Step
	title := clean(s.Title)
	if title == "" {
		return "", false
	}
	style := s.State.style(t)
	l := append(marker(s.State.glyph(c.Theme.Glyphs, frame, c.Env.Animate), style).to(theme.LabelCol),
		span{s.State.String() + ":", style}, gap(1), span{title, t.Title})
	if d := clean(s.Detail); d != "" {
		l = append(l, gap(1), span{"(" + d + ")", t.Detail})
	}
	return c.fit(l, c.width()), true
}

// StepProgress is the canonical flow's Apply step (brief 1C): one line
// per step, as a Message is laid out, the state's glyph centred in the
// marker column and its word and a colon at theme.LabelCol, both in the
// state's colour, then the title and any detail in parentheses. Here with
// no colour and ASCII glyphs, an SSH port change whose reload failed:
//
//	0 2   6
//	  [<] Rolled back: Allow 2230/tcp in UFW
//	  [<] Rolled back: Set Port 2230
//	  [!] Failed: Reload ssh.service (exit status 1)
//	  [ ] Pending: Remove the rule for port 22
//
// Each state has its own glyph (Glyphs.Pending, Running, OK, Bad,
// RolledBack and Skipped) and word, so none rests on colour alone
// (DEV-039). A step is always one line, cut with the ellipsis glyph when
// too long, so that a terminal redraws the block in place with the same
// number of lines.
//
// When Env.Animate is set, a running step's glyph is the spinner frame
// Frame selects from Glyphs.Spinner; the caller advances Frame and
// renders again, and Render stays pure. Without it, as on a pipe or in
// plain mode, a running step shows Glyphs.Running and the caller prints
// each step's line once, a summary line per step (brief 1C). Steps with
// no title are skipped, and a progress with none renders as the empty
// string.
type StepProgress struct {
	// Steps are the steps, in the order they run.
	Steps []Step
	// Frame selects the running step's spinner frame, counted from 0 and
	// taken modulo the number of frames.
	Frame int
}

// Render renders the steps for c. See StepProgress for the layout.
func (vm StepProgress) Render(c Ctx) string {
	var out []string
	for _, s := range vm.Steps {
		if l, ok := s.row(c, vm.Frame); ok {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
