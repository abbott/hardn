package flow

import (
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
)

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
	printed []component.StepState // without animation, the state each step's line showed; pending for none
	rolled  []string              // the steps rolled back, in the order reported
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
// An untitled change's step is titled "Unknown", as PlanView shows it,
// since StepProgress leaves out a step with no title.
func newProgress(r *run, changes []component.PlanChange) *progress {
	pr := &progress{r: r, steps: make([]component.Step, len(changes)), printed: make([]component.StepState, len(changes))}
	for i, ch := range changes {
		pr.steps[i].Title = ch.Title
		if strings.TrimSpace(ansi.Strip(ch.Title)) == "" {
			pr.steps[i].Title = "Unknown"
		}
	}
	env := r.p.Ctx.Env
	if pr.animate = env.Animate && len(changes) < env.Height; pr.animate {
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
		pr.rolled = append(pr.rolled, s.Title)
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

// print writes step's line as it stands, the first line after the gap.
func (pr *progress) print(step int) {
	line := component.StepProgress{Steps: pr.steps[step : step+1]}.Render(pr.r.p.Ctx)
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
// block, and then right under what it wrote before. It keeps the first
// write that failed and writes nothing after it.
func (pr *progress) write(s string) {
	switch {
	case pr.err != nil:
	case pr.started:
		pr.err = pr.r.put(s + "\n")
	default:
		pr.started = true
		pr.err = pr.r.block(s)
	}
}
