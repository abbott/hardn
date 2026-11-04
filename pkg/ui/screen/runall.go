package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Run All screen (refactor plan slice 8, unit (c), next/s8-screen): the
// mock the maintainer adopted on 2026-11-04 (docs/engine/gallery-runall.md
// §5 and §6, "Adopt all, start (b)"). One screen: the "Controls" section,
// each row Run All's outcome alone (decision 10 (a)), and one item, Apply
// all; then the change's words, its review grouped by control. pkg/ui
// cannot reach the host: pkg/screens/runall reads it with Run All's
// planner (pkg/engine/runall), fills these with its rows, and runs the item
// (docs/engine/screen-runall.md).

// The screen's words: decided (gallery-runall.md §6, decision 9). The
// title, the question and the outcomes are the planner's too
// (runall.Title, runall.Question, runall.WordsRefused, runall.WordsPending),
// which pkg/screens/runall holds equal to these.
const (
	runAllTitle    = "Run All"
	runAllSection  = "Controls"
	runAllApply    = "Apply all"
	runAllPlan     = "Plan, review, confirm"
	runAllNothing  = "Nothing to change"
	runAllRefused  = "Refused"
	runAllPending  = "Change pending"
	runAllQuestion = "Apply this plan?"
)

// RunAllItem is the screen's one item's key.
const RunAllItem = "1"

// RunAllState is what Apply all does now.
type RunAllState uint8

// The item's states.
const (
	// RunAllPlanned: something is planned: "Plan, review, confirm".
	RunAllPlanned RunAllState = iota
	// RunAllNothing: nothing is planned and nothing refused: "Nothing to
	// change", unavailable.
	RunAllNothing
	// RunAllSkipped: nothing is planned, and rows refused: "Nothing to
	// change", which, chosen, says the refusals and goes back.
	RunAllSkipped
	// RunAllRefused: the one plan refuses: "Refused", which, chosen, says
	// why and goes back.
	RunAllRefused
	// RunAllPending: a change waits for hardn confirm: "Change pending",
	// which, chosen, says the planner's words and goes back.
	RunAllPending
)

// RunAllRow is one control's row.
type RunAllRow struct {
	// Label is the control's row label, the main menu's.
	Label string
	// Outcome is Run All's outcome: the change ("Root login off"), or why
	// none ("Already as asked", "Refused").
	Outcome string
	// Refusal, for a refused row, is its words, drawn after "Refused:" and
	// cut to the row; said whole before the review.
	Refusal string
}

// RunAll is the Run All screen's reading of the host, which
// pkg/screens/runall fills from the planner's.
type RunAll struct {
	// Rows are the controls, in the planner's order.
	Rows []RunAllRow
	// State is what Apply all does now.
	State RunAllState
	// Lockout: the plan is lockout-risk, confirmed from a new connection
	// or reverted, so the item is marked.
	Lockout bool
}

// runAllRows are rows as the "Controls" section draws them: each label
// and its outcome; a refused row's outcome followed by ":" and its words,
// cut to the row.
func runAllRows(rows []RunAllRow) component.BulletList {
	var items []component.KeyValue
	for _, r := range rows {
		kv := component.KeyValue{Key: r.Label, Value: r.Outcome}
		if r.Refusal != "" {
			kv.Value, kv.Note = r.Outcome+":", r.Refusal
		}
		items = append(items, kv)
	}
	return component.BulletList{Items: items}
}

// SubMenu returns the Run All screen: the title bar; the "Controls"
// section; 1) Apply all, described by its state and marked at lockout
// risk; Return; "Select an option"; "Enter your choice".
func (r RunAll) SubMenu() SubMenu {
	it := component.MenuItem{Key: RunAllItem, Label: runAllApply, Description: runAllPlan}
	switch r.State {
	case RunAllPlanned:
		it.Risk = r.Lockout
	case RunAllNothing:
		it.Description, it.Disabled = runAllNothing, true
	case RunAllSkipped:
		it.Description = runAllNothing
	case RunAllRefused:
		it.Description = runAllRefused
	case RunAllPending:
		it.Description = runAllPending
	}
	return SubMenu{
		Title:    trail(runAllTitle),
		Sections: []Section{{Header: component.SectionHeader{Title: runAllSection}, Rows: runAllRows(r.Rows)}},
		Heading:  fixtureHeading,
		Menu:     component.Menu{Items: []component.MenuItem{it}, Exit: returnItem()},
		Prompt:   figmaPrompt,
	}
}

// RunAllChange is Apply all's words: the title bar over its flow, its
// question, and its review.
type RunAllChange struct {
	Step     WorkflowStep
	Question string
	Review   flow.Review
}

// RunAllApplyAll is Apply all's change: its review grouped by control, one
// line per planned row, its label and the change, in the rows' order.
func RunAllApplyAll(planned []RunAllRow) RunAllChange {
	var items []component.KeyValue
	for _, r := range planned {
		items = append(items, component.KeyValue{Key: r.Label, Value: r.Outcome})
	}
	return RunAllChange{Step: WorkflowStep{Title: trail(runAllTitle, runAllApply)}, Question: runAllQuestion,
		Review: flow.Review{Title: runAllTitle, Items: items}}
}
