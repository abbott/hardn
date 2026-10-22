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

// String returns the state's word, which a step shows in its status
// column: brief 1B's state names, and "Skipped" (DEV-039; proposal P-24
// asks the maintainer to approve them). A value outside the states is
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

// marker returns the state's marker for c and its style (P-42 R3, (a2)):
// Figma's status markers for a step done, failed or rolled back, in the
// state's colour; the key-value bullet for a step pending or skipped, in
// KeyValue.Bullet; and for a running step the bullet in the state's
// colour, or with Env.Animate the spinner's frame for frame.
func (s StepState) marker(c Ctx, frame int) (string, lipgloss.Style) {
	g, t := c.Theme.Glyphs, c.Theme.Step
	switch s {
	case StepRunning:
		if frames := strings.Split(g.Spinner, ""); c.Env.Animate && len(frames) > 0 {
			return frames[(frame%len(frames)+len(frames))%len(frames)], t.Running
		}
		return g.Bullet, t.Running
	case StepDone:
		return g.Configured, t.Done
	case StepFailed:
		return g.NotConfigured, t.Failed
	case StepRolledBack:
		return g.Partial, t.RolledBack
	}
	return g.Bullet, c.Theme.KeyValue.Bullet
}

// style returns the state's style in t, which colours its word.
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
	// Detail is an aside one space after the state's word, in
	// Step.Detail, with no parentheses: such as a failed step's code,
	// "COMMAND_FAILED", or why a step was skipped. Empty shows none.
	Detail string
}

// stepGap is the cells between a list's widest title and its status
// column.
const stepGap = 2

// widestState is the cells of the widest state word, "Rolled back": a
// list keeps its two columns only where its status column holds it, so
// that its form, and its height, stay whatever its steps' states.
func widestState() int {
	w := 0
	for s := StepPending; s <= StepSkipped; s++ {
		w = max(w, lipgloss.Width(s.String()))
	}
	return w
}

// stepColumn is where the rows of one list of steps put their state
// words (P-42 R2, (b1)): every row of the list, an ErrorReport's
// rolled-back rows with them, shares it.
type stepColumn struct {
	col     int  // the status column
	stacked bool // each state under its title, at theme.StackCol
}

// newStepColumn returns the status column of a list whose titles, as
// shown for c, are titles: stepGap cells after the widest, at
// theme.ValueCol at the least. The list stacks below theme.StackBelow, as
// every two-column row does, or where the column cannot hold the widest
// state word within the content width (DEV-021). Neither depends on the
// steps' states, so a row keeps its place, and the list its height, as
// they change.
func newStepColumn(c Ctx, titles []string) stepColumn {
	widest := 0
	for _, t := range titles {
		widest = max(widest, lipgloss.Width(t))
	}
	col := max(theme.ValueCol, theme.LabelCol+widest+stepGap)
	return stepColumn{col: col, stacked: c.stacked() || col+widestState() > c.width()}
}

// row lays out one row for c: the state's marker centred in the marker
// column, title at theme.LabelCol, and in the status column the state's
// word, regular, with detail one space after it as an aside; stacked, the
// word and aside go on the line under the title, at theme.StackCol. A row
// is never wrapped: an aside that does not fit is cut, or left out when
// not one cell of it fits beside the ellipsis, and a title too long for
// its line is cut.
func (sc stepColumn) row(c Ctx, state StepState, frame int, title span, detail string) []string {
	w := c.width()
	glyph, style := state.marker(c, frame)
	head := append(marker(glyph, style).to(theme.LabelCol), title)
	word := line{{state.String(), state.style(c.Theme.Step)}}
	if !sc.stacked {
		return []string{c.fit(c.aside(append(head.to(sc.col), word...), detail, w), w)}
	}
	return []string{c.fit(head, w), c.fit(c.aside(append(line{gap(theme.StackCol)}, word...), detail, w), w)}
}

// aside returns l with the aside text after it, one space on, in
// Step.Detail, cut to end within w cells; an aside with no room for a
// cell of its text beside the ellipsis glyph is left out.
func (c Ctx) aside(l line, text string, w int) line {
	text = c.text(text)
	room := w - l.width() - 1
	if text == "" || lipgloss.Width(text) > room && room <= lipgloss.Width(c.Theme.Glyphs.Ellipsis) {
		return l
	}
	return append(l, gap(1), span{c.truncate(text, room), c.Theme.Step.Detail})
}

// StepProgress is the canonical flow's Apply step (brief 1C), drawn in
// Figma's row grammar (P-42 R1 to R3, the maintainer, 2026-10-22): a row
// per step, the state's marker centred in the marker column, the step's
// title at theme.LabelCol in KeyValue.Key, and its state as a coloured
// word, regular, in one status column the list shares, any detail one
// space after the word as a faint aside. Here with no colour and ASCII
// glyphs, an SSH port change whose reload failed:
//
//	0 2   6                             36
//	  [+] Install hardn's boot unit     Done
//	  [+] Add SSH port 2230             Done
//	  [ ] Reload sshd (ssh.service)     Failed COMMAND_FAILED
//	   -  sshd -T shows ports 22, 2230  Skipped
//
// Markers (DEV-039): done Glyphs.Configured (⛛ [+]) in Step.Done, failed
// Glyphs.NotConfigured (⭘ [ ]) in Step.Failed, rolled back
// Glyphs.Partial (⭘ [ ]) in Step.RolledBack; pending and skipped
// Glyphs.Bullet in KeyValue.Bullet; running the bullet in Step.Running.
// Each state has its word (StepState.String), so none rests on colour or
// marker alone. The status column is two cells after the widest title, at
// theme.ValueCol at the least; below theme.StackBelow, or where it cannot
// hold "Rolled back" within the content width, every row stacks, its word
// under its title at theme.StackCol. A row is never wrapped, so a
// terminal redraws the block in place with the same number of lines
// whatever its steps' states.
//
// When Env.Animate is set, a running step's marker is the spinner frame
// Frame selects from Glyphs.Spinner; the caller advances Frame and
// renders again, and Render stays pure. Without it, as on a pipe or in
// plain mode, the caller writes each step's row once, when the step
// settles (Row), a summary line per step (brief 1C). Steps with no title
// are skipped, and a progress with none renders as the empty string.
type StepProgress struct {
	// Steps are the steps, in the order they run.
	Steps []Step
	// Frame selects the running step's spinner frame, counted from 0 and
	// taken modulo the number of frames.
	Frame int
}

// titles returns the titles of steps as shown for c.
func titles(c Ctx, steps []Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, c.text(s.Title))
	}
	return out
}

// Render renders the steps for c. See StepProgress for the layout.
func (vm StepProgress) Render(c Ctx) string {
	sc := newStepColumn(c, titles(c, vm.Steps))
	var out []string
	for _, s := range vm.Steps {
		if t := c.text(s.Title); t != "" {
			out = append(out, sc.row(c, s.State, vm.Frame, span{t, c.Theme.KeyValue.Key}, s.Detail)...)
		}
	}
	return strings.Join(out, "\n")
}

// Row renders step i alone for c, in the status column of the whole list,
// as Render lays it out: for a caller that writes each step's row once,
// as the step settles, where nothing animates (pkg/ui/flow). A step with
// no title, or an i outside Steps, renders as the empty string.
func (vm StepProgress) Row(c Ctx, i int) string {
	if i < 0 || i >= len(vm.Steps) {
		return ""
	}
	s := vm.Steps[i]
	t := c.text(s.Title)
	if t == "" {
		return ""
	}
	sc := newStepColumn(c, titles(c, vm.Steps))
	return strings.Join(sc.row(c, s.State, vm.Frame, span{t, c.Theme.KeyValue.Key}, s.Detail), "\n")
}
