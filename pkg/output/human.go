package output

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/config"
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
		v.Changes = append(v.Changes, component.PlanChange{Title: ch.Summary, Target: ch.Target, LockoutRisk: ch.Risk == engine.RiskLockout,
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
var stepStates = map[string]component.StepState{StatusApplied: component.StepDone, StatusRolledBack: component.StepDone,
	StatusFailed: component.StepFailed, StatusUnjournalled: component.StepFailed, StatusNotRun: component.StepSkipped,
	StatusPlanned: component.StepPending}

// applyBlocks is an apply's: the steps, then how it ended.
func applyBlocks(c component.Ctx, r Run, d Document, now time.Time) []string {
	res := d.Result
	switch {
	case res.Outcome == OutcomeNothingToDo:
		return []string{component.Message{Text: nothingToChange}.Render(c)}
	case r.Result == nil:
		return []string{planBlock(c, r, d)}
	}
	var sp component.StepProgress
	var undone []string
	for _, ch := range d.Changes {
		step := component.Step{Title: ch.Summary, State: stepStates[ch.Status]}
		if ch.Status == StatusFailed || ch.Status == StatusUnjournalled {
			step.Detail = CodeOf(failedErr(r, ch.ID))
		}
		sp.Steps = append(sp.Steps, step)
		if ch.Status == StatusRolledBack {
			undone = append(undone, ch.Summary)
		}
	}
	slices.Reverse(undone) // undone last first
	blocks := []string{sp.Render(c)}
	switch {
	case res.Rollback != nil:
		blocks = append(blocks, errorsBlock(c, d, r.Result.Failed.Err, undone, rollbackNext(*res.Rollback)))
	case res.Confirm != nil:
		blocks = append(blocks, countdown(c, *res.Confirm, res.ID, now)...)
	case len(d.Errors) == 0:
		blocks = append(blocks, component.Callout{Kind: component.MessageOK}.Render(c))
	}
	return blocks
}

// failedErr is the error of the change id that failed.
func failedErr(r Run, id string) error {
	if res := r.Result; res != nil {
		if res.Boot != nil && res.Boot.Err != nil && id == engine.BootChangeID {
			return res.Boot.Err
		}
		if f := res.Failed; f != nil {
			if f.Unjournalled != nil && f.Unjournalled.Change == id {
				return engine.ErrUnjournalled
			}
			return f.Err
		}
	}
	return nil
}

// rollbackNext is the next step after a failed apply: finishing its
// rollback when it did not finish here.
func rollbackNext(rb Rollback) string {
	switch {
	case rb.Command != "":
		return "The rollback did not finish: run " + rb.Command + " to finish it."
	case rb.ByAnotherProcess:
		return "Another hardn process is rolling the change back; wait for it to end."
	case rb.Unjournalled != "":
		return "What " + rb.Unjournalled + " did may be left on the host: check it, as the error says."
	}
	return ""
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
// the configuration's deprecated values are Deprecations', on stderr.
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

// Deprecations renders the configuration's values read in a deprecated
// form (config.Deprecation: refactor plan §3.17, approved, a bool
// permitRootLogin) as warnings, each after the file it was read from, for
// stderr; "" when there are none.
func Deprecations(env term.Env, path string, deps []config.Deprecation) string {
	c := component.NewCtx(env)
	var lines []string
	for _, d := range deps {
		text := d.String()
		if path != "" {
			text = path + ": " + text
		}
		lines = append(lines, component.Message{Kind: component.MessageWarn, Text: text}.Render(c))
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
