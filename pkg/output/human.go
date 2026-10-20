package output

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// nothingToChange is brief Phase 3's words for a plan that finds nothing
// to change, as pkg/ui/flow says them (P-27 (b)).
const nothingToChange = "Nothing needs changing."

// Human renders run r for a person, at env, through pkg/ui's components
// only (design-system README §9.15): a check's StatusList; a plan's
// PlanView with its dry-run footer; an apply's StepProgress, then
// "Operation Successful", the confirm countdown, or an ErrorReport naming
// what was rolled back; the findings as warnings; each error with its code,
// a refusal by its Why, and its Fix as the next step. now is the clock the
// countdown counts from. It is pure: the caller writes the string through
// env.Writer, in one write.
//
// Human is for a run no flow drew. After pkg/ui/flow's Flow.Run, which has
// drawn the plan, the steps and the result, write AfterFlow instead: what
// the flow does not draw.
func Human(env term.Env, r Run, now time.Time) string {
	c, d := component.NewCtx(env), Build(r)
	var blocks []string
	switch r.Mode {
	case ModeCheck:
		blocks = append(blocks, statusList(d.Status).Render(c))
	case ModePlan:
		blocks = append(blocks, planBlock(c, r, d))
	case ModeApply:
		blocks = append(blocks, applyBlocks(c, r, d, now)...)
	}
	blocks = append(blocks, findingsBlock(c, d.Findings))
	if r.Mode != ModeApply || d.Result.Rollback == nil {
		blocks = append(blocks, errorsBlock(c, d, r.Err, nil, ""))
	}
	blocks = slices.DeleteFunc(blocks, func(b string) bool { return b == "" })
	if len(blocks) == 0 {
		return ""
	}
	return strings.Join(blocks, strings.Repeat("\n", theme.SectionGap+1)) + "\n"
}

// statusStates are the components' states of the schema's words.
var statusStates = map[string]component.StatusState{"configured": component.StatusConfigured,
	"not-configured": component.StatusNotConfigured, "partial": component.StatusPartial,
	"not-applicable": component.StatusNotApplicable}

func statusList(ss []Status) component.StatusList {
	var l component.StatusList
	for _, s := range ss {
		l.Rows = append(l.Rows, component.StatusRow{Label: s.Title, State: statusStates[s.State], Detail: s.Detail})
	}
	return l
}

// planView is the plan's changes as the PlanView shows them, in the order
// the apply runs them (engine.md §7).
func planView(p *engine.Plan) component.PlanView {
	var v component.PlanView
	if p == nil {
		return v
	}
	for _, ch := range p.Changes {
		v.Changes = append(v.Changes, component.PlanChange{Title: ch.Heading(), Target: ch.Target, LockoutRisk: ch.Risk == engine.RiskLockout,
			Diff: ch.Diff, Before: ch.StateBefore, After: ch.StateAfter, Commands: ch.Commands})
	}
	return v
}

// planBlock is a plan's: its changes with the dry-run footer, or that
// nothing needs changing.
func planBlock(c component.Ctx, r Run, d Document) string {
	v := planView(r.Plan)
	if len(v.Changes) == 0 {
		if len(d.Errors) > 0 {
			return ""
		}
		return component.Message{Text: nothingToChange}.Render(c)
	}
	v.DryRun = true
	return v.Render(c)
}

// stepStates are the components' states of the schema's change statuses,
// as pkg/ui/flow's Apply step left them: a change rolled back after the
// failure was done, and the ErrorReport names it rolled back.
// A change whose state is unknown was done, and was never reported rolled
// back.
var stepStates = map[string]component.StepState{StatusApplied: component.StepDone, StatusRolledBack: component.StepDone,
	StatusUnknown: component.StepDone, StatusFailed: component.StepFailed, StatusUnjournalled: component.StepFailed,
	StatusNotRun: component.StepSkipped, StatusPlanned: component.StepPending}

// heading is a change's line in the apply's steps: its engine change's
// Heading (engine.Change.Title, short enough for one step line), found by
// id in the plan, else the document's summary.
func heading(r Run, ch Change) string {
	if r.Plan != nil {
		for _, pc := range r.Plan.Changes {
			if pc.ID == ch.ID {
				return pc.Heading()
			}
		}
	}
	return ch.Summary
}

// applyBlocks is an apply's: the steps, then how it ended.
func applyBlocks(c component.Ctx, r Run, d Document, now time.Time) []string {
	res := d.Result
	switch {
	case res.Outcome == OutcomeNothingToDo:
		return []string{component.Message{Text: nothingToChange}.Render(c)}
	case res.Outcome == OutcomeDeclined:
		blocks := []string{component.Callout{Kind: component.MessageCancelled}.Render(c)}
		if r.Plan != nil && !r.Plan.Empty() {
			blocks = append([]string{planBlock(c, r, d)}, blocks...)
		}
		return blocks
	case r.Result == nil:
		return []string{planBlock(c, r, d)}
	}
	var sp component.StepProgress
	var undone []string
	for _, ch := range d.Changes {
		step := component.Step{Title: heading(r, ch), State: stepStates[ch.Status]}
		if err := failedErr(r, ch.ID); err != nil {
			step.Detail = CodeOf(err)
		}
		sp.Steps = append(sp.Steps, step)
		if ch.Status == StatusRolledBack {
			undone = append(undone, heading(r, ch))
		}
	}
	slices.Reverse(undone) // undone last first
	blocks := []string{sp.Render(c)}
	switch {
	case res.Rollback != nil:
		blocks = append(blocks, errorsBlock(c, d, r.Result.Failed.Err, undone, rollbackNext(*res.Rollback, r.Result.Failed)))
	case res.Confirm != nil:
		blocks = append(blocks, countdown(c, *res.Confirm, res.ID, now)...)
	case len(d.Errors) == 0:
		blocks = append(blocks, component.Callout{Kind: component.MessageOK}.Render(c))
	}
	return blocks
}

// failedErr is the error that stopped the apply at change id, the one
// rule for a step's code, as the ErrorReport and the flow's progress
// (FlowEngine.Progress) give it too: the boot unit's, or the ApplyError's
// step's error for the change it names (failed, not run, or unjournalled);
// nil for any other change.
func failedErr(r Run, id string) error {
	res := r.Result
	switch {
	case res == nil:
		return nil
	case res.Boot != nil && res.Boot.Err != nil && id == engine.BootChangeID:
		return res.Boot.Err
	case res.Failed != nil && res.Failed.Change == id && id != "":
		return res.Failed.Err
	case res.Failed != nil && res.Failed.Unjournalled != nil && res.Failed.Unjournalled.Change == id:
		return res.Failed.Err
	}
	return nil
}

// rollbackNext is the next step after the failed apply ae: finishing its
// rollback when it did not finish here, or waiting for the process that
// has it; and checking a change the journal did not cover, with what
// undoes it.
func rollbackNext(rb Rollback, ae *engine.ApplyError) string {
	var next []string
	switch {
	case rb.Command != "":
		next = append(next, "The rollback did not finish: run "+rb.Command+" to finish it.")
	case rb.ByAnotherProcess && ae != nil && ae.Rollback.Nothing:
		next = append(next, "Another hardn process had taken the change, so this run undid nothing: "+strings.TrimSuffix(ae.Rollback.Why, ".")+".")
	case rb.ByAnotherProcess:
		next = append(next, "Another hardn process is rolling the change back; wait for it to end.")
	}
	if u := rb.Unjournalled; u != nil {
		next = append(next, "What "+u.Change+" did may be left on the host: check it, and undo it if it is: "+unjournalledWords(*u)+".")
	}
	return strings.Join(next, " ")
}

// AfterFlow renders, for a person, what pkg/ui/flow does not draw of run
// r, the Run of FlowEngine.Run: for a lockout-risk change waiting for
// confirmation, the confirm countdown counted from now, when it is rolled
// back unless confirmed ("at about" when the deadline is approximate) and
// how to undo it now; and the plan's findings as warnings. The flow ends a
// lockout-risk apply with "Operation Successful" alone, so a caller that
// runs a flow on a FlowEngine must write AfterFlow on the flow's writer
// right after Flow.Run returns, or the person never learns that the change
// is rolled back unless confirmed. The string starts with the flow's gap
// between blocks and ends with a line break, ready to write as it is; ""
// when there is nothing to add.
func AfterFlow(env term.Env, r Run, now time.Time) string {
	c, d := component.NewCtx(env), Build(r)
	var blocks []string
	if cf := d.Result.Confirm; cf != nil && r.Mode == ModeApply {
		blocks = append(blocks, countdown(c, *cf, d.Result.ID, now)...)
	}
	blocks = append(blocks, findingsBlock(c, d.Findings))
	blocks = slices.DeleteFunc(blocks, func(b string) bool { return b == "" })
	if len(blocks) == 0 {
		return ""
	}
	gap := strings.Repeat("\n", theme.SectionGap)
	return gap + strings.Join(blocks, gap+"\n") + "\n"
}

// countdown is a change waiting for confirmation: the confirm countdown,
// counted from now, and when it is rolled back unless confirmed, "about"
// when the deadline is the journal's (engine.Confirm.Approximate).
func countdown(c component.Ctx, cf Confirm, id string, now time.Time) []string {
	left := cf.Deadline.Sub(now)
	cd := component.ConfirmCountdown{Remaining: int((left + time.Second - 1) / time.Second), Port: onePort(cf.Via), Command: cf.Command}
	about := ""
	if cf.Approximate {
		about = "about "
	}
	when := fmt.Sprintf("Unless confirmed, change %s is rolled back at %s%s. To undo it now, run %s.",
		id, about, cf.Deadline.UTC().Format("15:04:05 MST"), cf.Undo)
	return []string{cd.Render(c), component.Message{Text: when}.Render(c)}
}

// onePort is the port every listener of via is on, the port a confirming
// connection must reach; 0 when there are none, or more than one port.
func onePort(via []string) int {
	port := 0
	for _, v := range via {
		i := strings.LastIndexByte(v, ':')
		var p int
		if _, err := fmt.Sscan(v[i+1:], &p); err != nil || (port != 0 && p != port) {
			return 0
		}
		port = p
	}
	return port
}

// findingsBlock is each finding as a warning, with its code and source;
// the configuration's deprecated values are cmd/hardn's warning, on stderr
// as the configuration loads.
func findingsBlock(c component.Ctx, fs []Finding) string {
	var lines []string
	for _, f := range fs {
		if f.Code == CodeDeprecated {
			continue
		}
		text := f.Detail + " (" + f.Code
		if f.Source != "" {
			text += ", " + f.Source
		}
		lines = append(lines, component.Message{Kind: component.MessageWarn, Text: text + ")"}.Render(c))
	}
	return strings.Join(lines, "\n")
}

// errorsBlock is the run's errors: the first as an ErrorReport, with the
// changes rolled back, the command that failed and its output, and the
// next step (its Fix, then next), the others as error messages, each with
// its fix.
func errorsBlock(c component.Ctx, d Document, failed error, undone []string, next string) string {
	es := d.Errors
	if len(es) == 0 {
		return ""
	}
	first := es[0]
	if first.Fix != "" {
		next = strings.TrimSpace(toGoOn(first.Fix) + " " + next)
	}
	rep := component.ErrorReport{Failed: errorText(d, first), RolledBack: undone, Next: next}
	var xe *host.ExitError
	if errors.As(failed, &xe) {
		rep.Command, rep.Output = host.FormatArgv(xe.Argv), strings.Join(xe.Tail, "\n")
	}
	blocks := []string{rep.Render(c)}
	var more []string
	for _, e := range es[1:] {
		more = append(more, component.Message{Kind: component.MessageBad, Text: errorText(d, e)}.Render(c))
		if e.Fix != "" {
			more = append(more, component.Message{Text: toGoOn(e.Fix)}.Render(c))
		}
	}
	if len(more) > 0 {
		blocks = append(blocks, strings.Join(more, "\n"))
	}
	return strings.Join(blocks, strings.Repeat("\n", theme.SectionGap+1))
}

// toGoOn is a refusal's Fix as a sentence, in steps.Refusal's words.
func toGoOn(fix string) string { return "To go on: " + strings.TrimSuffix(fix, ".") + "." }

// errorText is an error in words with its code, a refusal by its Why, after
// the summary of the change it is about.
func errorText(d Document, e Error) string {
	text := e.Message
	if e.Why != "" {
		text = e.Why
	}
	text += " (" + e.Code + ")"
	for _, ch := range d.Changes {
		if e.Change != "" && ch.ID == e.Change {
			return ch.Summary + ": " + text
		}
	}
	return text
}
