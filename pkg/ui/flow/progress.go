package flow

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
)

// unknownStep titles the step of a change whose title shows nothing: the
// word PlanView shows for it, so that the steps never hide a change the
// plan showed.
const unknownStep = "Unknown"

// spinEvery is how often the spinner of a running step turns when the
// steps animate; a variable so that tests can turn it faster.
var spinEvery = 100 * time.Millisecond

// progress shows the Apply step as the Engine reports it: animated in
// place when the Env allows it, else one line per step (see the package
// doc). Its methods may be called from any goroutine.
type progress struct {
	mu      sync.Mutex
	r       *run
	steps   []component.Step
	titles  []string              // the changes' own titles, which the ErrorReport names
	printed []component.StepState // without animation, the state each step's line showed; pending for none
	rolled  []string              // the titles of the steps rolled back, in the order reported
	frame   int
	lines   int // with animation, the lines drawn, to go back up over
	animate bool
	started bool  // whether any of the steps was written
	done    bool  // whether Apply has returned
	err     error // the first write that failed
	quit    chan struct{}
	ticked  chan struct{}
}

// newProgress returns the progress of changes, one step each, all pending,
// and draws it when it animates, with a spinner turning every spinEvery.
// StepProgress leaves out a step whose title shows nothing, such as an
// empty one or one of control or zero-width characters alone, while
// PlanView shows such a change as "Unknown"; so a step StepProgress would
// leave out, by its own rule, is titled so. The ErrorReport is given the
// change's own title, which it shows as "Unknown" by the same rule.
func newProgress(r *run, changes []component.PlanChange) *progress {
	n := len(changes)
	pr := &progress{r: r, steps: make([]component.Step, n), titles: make([]string, n), printed: make([]component.StepState, n)}
	for i, ch := range changes {
		pr.steps[i].Title, pr.titles[i] = ch.Title, ch.Title
		if (component.StepProgress{Steps: pr.steps[i : i+1]}).Render(r.p.Ctx) == "" {
			pr.steps[i].Title = unknownStep
		}
	}
	// The block is drawn again in place only while it fits under the
	// terminal's top: a line per step, two where the rows stack, whatever
	// their states (component.StepProgress).
	env, block := r.p.Ctx.Env, component.StepProgress{Steps: pr.steps}.Render(r.p.Ctx)
	if pr.animate = env.Animate && strings.Count(block, "\n")+1 < env.Height; pr.animate {
		pr.draw()
		pr.quit, pr.ticked = make(chan struct{}), make(chan struct{})
		go pr.spin()
	}
	return pr
}

// spin turns the spinner until stop.
func (pr *progress) spin() {
	defer close(pr.ticked)
	t := time.NewTicker(spinEvery)
	defer t.Stop()
	for {
		select {
		case <-pr.quit:
			return
		case <-t.C:
			pr.mu.Lock()
			if slices.ContainsFunc(pr.steps, func(s component.Step) bool { return s.State == component.StepRunning }) {
				pr.frame++
				pr.draw()
			}
			pr.mu.Unlock()
		}
	}
}

// report is the Engine's Report: it records the step's state and shows
// it, unless Apply has returned.
func (pr *progress) report(step int, state component.StepState, detail string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	if pr.done || step < 0 || step >= len(pr.steps) {
		return
	}
	s := &pr.steps[step]
	if s.State == state && s.Detail == detail {
		return
	}
	s.State, s.Detail = state, detail
	if state == component.StepRolledBack {
		pr.rolled = append(pr.rolled, pr.titles[step])
	}
	switch {
	case pr.animate:
		pr.draw()
	case state == component.StepDone || state == component.StepFailed || state == component.StepSkipped:
		pr.print(step)
	}
}

// stop ends the progress once Apply has returned: after a failure, the
// steps still pending are skipped. With animation it draws the steps a
// last time; without, it writes the line of each step not yet shown. It
// returns the steps rolled back, in order, and the first write that
// failed.
func (pr *progress) stop(failed bool) ([]string, error) {
	if pr.quit != nil {
		close(pr.quit)
		<-pr.ticked
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.done = true
	for i := range pr.steps {
		if failed && pr.steps[i].State == component.StepPending {
			pr.steps[i].State = component.StepSkipped
		}
		if !pr.animate && pr.printed[i] == component.StepPending {
			pr.print(i)
		}
	}
	if pr.animate {
		pr.draw()
	}
	return pr.rolled, pr.err
}

// print writes step's row as it stands, in the status column of the
// whole list, the first after the gap.
func (pr *progress) print(step int) {
	line := component.StepProgress{Steps: pr.steps}.Row(pr.r.p.Ctx, step)
	pr.printed[step] = pr.steps[step].State
	pr.write(line)
}

// draw writes the steps as they stand: the first time after the gap, then
// over the lines drawn before, moving up over them and erasing each line
// before it is written again.
func (pr *progress) draw() {
	block := component.StepProgress{Steps: pr.steps, Frame: pr.frame}.Render(pr.r.p.Ctx)
	if pr.lines > 0 {
		block = ansi.CursorPreviousLine(pr.lines) + ansi.EraseEntireLine + strings.ReplaceAll(block, "\n", "\n"+ansi.EraseEntireLine)
	}
	pr.lines = strings.Count(block, "\n") + 1
	pr.write(block)
}

// write writes s and a line break: the first time after the gap, as a
// block, and then right under what it wrote before. An empty s writes
// nothing, and the gap is left for the first line written. It keeps the
// first write that failed and writes nothing after it.
func (pr *progress) write(s string) {
	switch {
	case pr.err != nil, s == "":
	case pr.started:
		pr.err = pr.r.put(s + "\n")
	default:
		pr.started = true
		pr.err = pr.r.block(s)
	}
}
