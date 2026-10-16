package flow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/form"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// ErrFlow reports a Flow that cannot be run: it has no Engine or no
// Confirm, or its typed confirmation has nothing to ask it by, neither a
// label nor a question.
var ErrFlow = errors.New("flow: cannot be run")

// nothingToChange is what a plan with no changes says, in brief Phase 3's
// words for a plan that finds nothing to change (proposal P-27).
const nothingToChange = "Nothing needs changing."

// tooNarrow is what the flow says under "Operation Cancelled" when a
// typed confirmation does not show at the terminal's width; new words
// (proposal P-27 (e)).
const tooNarrow = "The terminal is too narrow to confirm. Widen it and try again."

// errNarrow reports a typed confirmation that does not show whole at the
// terminal's width, by its phrase or by its short one.
var errNarrow = errors.New("flow: the typed confirmation does not show at this width")

// Risk is a plan's risk class, which decides what the Confirm step asks
// (brief 1C).
type Risk int

// The risk classes. The zero value is RiskLow.
const (
	// RiskLow asks a plain y/N question, its default no.
	RiskLow Risk = iota
	// RiskLockout asks for a typed confirmation: for a change that could
	// lock the user out, such as one to SSH or the firewall (brief Phase
	// 3's LockoutRisk), and for Run All, which brief 1C confirms so too.
	RiskLockout
)

// Plan is what the Engine plans: the PlanView the Plan step shows and the
// risk class the Confirm step asks by.
type Plan struct {
	// View holds the changes, in the order they apply; each is one step of
	// the Apply step, titled by its Title. The flow sets View.DryRun.
	View component.PlanView
	// Risk is the plan's risk class.
	Risk Risk
}

// Typed reports whether the plan is confirmed by typing a phrase: its
// Risk is RiskLockout, or one of its changes has lockout risk, so that a
// plan never asks less than its PlanView shows.
func (p Plan) Typed() bool {
	if p.Risk >= RiskLockout {
		return true
	}
	for _, ch := range p.View.Changes {
		if ch.LockoutRisk {
			return true
		}
	}
	return false
}

// Report is how an Engine reports a step of the Apply step: step is the
// index of its change in the plan's View.Changes, state where the step
// now stands, and detail why it failed or was skipped, such as "exit
// status 1" (component.Step.Detail). A report for no step is ignored.
type Report func(step int, state component.StepState, detail string)

// Engine plans and applies one action: what the flow needs of brief Phase
// 3's plan/apply engine, which will satisfy it through an adapter holding
// the action's answers. Plan turns its Changes into the plan's changes, a
// Change's Summary, Diff and Risk becoming a PlanChange's Title, Diff and
// LockoutRisk, in the order it applies them; Apply applies them. In Phase
// 1 a Fixture satisfies it, and nothing reads or changes the system.
type Engine interface {
	// Plan returns the plan for the answers collected. It must change
	// nothing. An error is shown as an ErrorReport, with a *Failure's
	// details.
	Plan(ctx context.Context) (Plan, error)
	// Apply applies the changes Plan last returned, in order, reporting
	// each step running, then done. On the first failure it reports the
	// step failed, stops, rolls back the steps done, in reverse order,
	// reporting each rolled back, and returns an error, a *Failure for
	// the ErrorReport's details. It returns nil once every step is done.
	// Report may be called from any goroutine until Apply returns; once
	// the flow has drawn the steps a last time, a report is ignored. The
	// reports are the one record of what was undone: the ErrorReport
	// names only the steps reported rolled back before Apply returned.
	Apply(ctx context.Context, report Report) error
}

// Failure is an error an Engine returns when a step failed, or when it
// could not plan, with what the ErrorReport shows besides the steps rolled
// back (component.ErrorReport). With no Err the report says nothing of
// what failed: its callout stands alone.
type Failure struct {
	// Err says what failed and why, such as "Reloading ssh.service
	// failed: exit status 1".
	Err error
	// Command is the command that failed, such as "systemctl reload ssh",
	// and Output what it wrote; empty shows none.
	Command, Output string
	// LogPath is the log hardn wrote; empty shows none.
	LogPath string
	// Next is the suggested next step, a sentence; empty shows none.
	Next string
}

// Error returns Err's text, which the ErrorReport shows.
func (f *Failure) Error() string {
	if f.Err == nil {
		return "flow: a step failed"
	}
	return f.Err.Error()
}

// Unwrap returns Err.
func (f *Failure) Unwrap() error { return f.Err }

// Review is the Review step: the action's notice, if it has one, then the
// answers collected, as Figma sums up a new user under "User
// Configuration".
type Review struct {
	// Notice is said above the answers, as Figma warns before disabling
	// root SSH login; nil says nothing.
	Notice *Notice
	// Title is the section's title, such as "User Configuration".
	Title string
	// Items are the answers, as key-value rows, such as "Sudo" and
	// "Enabled, No password". With none no section is shown.
	Items []component.KeyValue
}

// Notice is what an action says before its review and its plan, such as
// the Warning Figma draws above the review before disabling root SSH
// login (SSH_LOGIN-TOGGLE_CONFIRMATION) and before requiring a password
// for sudo (USER-MANAGE_SUDO-CONFIRMATION): a Callout, then what it lists
// as bullet items, outside the callout's bar, as Figma draws its
// checklist (component.Callout).
type Notice struct {
	// Callout is the notice, such as a Warning whose body is "Before
	// disabling root SSH login, ensure:".
	Callout component.Callout
	// Items are listed under the callout, such as "Non-root user has
	// sudo privileges"; empty lists none.
	Items []string
}

// render renders the review: the notice's Callout and its items as a
// BulletList, then a SectionHeader and the rows as a BulletList, each
// block after theme.SectionGap blank lines.
func (v Review) render(c component.Ctx) string {
	var blocks []string
	if n := v.Notice; n != nil {
		items := make([]component.KeyValue, len(n.Items))
		for i, it := range n.Items {
			items[i].Key = it
		}
		blocks = append(blocks, n.Callout.Render(c), component.BulletList{Items: items}.Render(c))
	}
	if list := (component.BulletList{Items: v.Items}).Render(c); list != "" {
		blocks = append(blocks, component.SectionHeader{Title: v.Title}.Render(c), list)
	}
	blocks = slices.DeleteFunc(blocks, func(b string) bool { return b == "" })
	return strings.Join(blocks, strings.Repeat("\n", theme.SectionGap+1))
}

// Confirm is what the Confirm step asks: the action's question, asked as
// a y/N question, or, for a plan Typed reports, shown above a typed
// confirmation of Phrase.
type Confirm struct {
	// Question is the action's question, such as Figma's "Create user
	// 'george'?" or "Disable SSH login for 'root'?". It is asked as a y/N
	// question; above a typed confirmation it is shown on a line of its
	// own, as a form shows a question above a prompt line of its own (a
	// component.FieldHead, cut with the ellipsis when too long), so that
	// the screen still says what is confirmed.
	Question string
	// Label is a typed confirmation's label, before the phrase, such as
	// "Type the hostname", which nav.Prompter.Typed cuts before the
	// phrase. Empty asks Question there instead, with nothing above it.
	Label string
	// Phrase is what a typed confirmation must be answered with, exactly,
	// such as the hostname. A typed confirmation with no phrase, or one
	// nav.CheckPhrase refuses, is not asked (nav.ErrPhrase).
	Phrase string
	// Short is asked in Phrase's place when Phrase does not show whole
	// beside the label at the terminal's width, such as a long hostname's
	// first label; the action words it, and it must still name what is
	// confirmed. Empty asks nothing in Phrase's place.
	Short string
}

// Outcome is how a flow ended.
type Outcome int

// The outcomes. The zero value, NotRun, comes with an error: the flow
// ended before a result.
const (
	NotRun    Outcome = iota
	Applied           // every step was applied
	Failed            // planning or a step failed; what was applied was rolled back
	Cancelled         // the user declined at the Confirm step, or a typed confirmation did not show at the width
	Planned           // dry-run: the plan was shown, nothing applied
	Unchanged         // the plan had no changes
)

// Flow is one action that changes the system, as the canonical flow runs
// it (see the package doc).
type Flow struct {
	// Form collects the answers; with no fields it asks nothing.
	Form form.Form
	// Review sums up the answers, once collected; nil shows no review.
	Review func() Review
	// Engine plans and applies the action. Required.
	Engine Engine
	// Confirm returns what the Confirm step asks about plan, so that a
	// definition words a typed confirmation apart from a y/N question.
	// Required.
	Confirm func(plan Plan) Confirm
	// DryRun stops the flow after the Plan step.
	DryRun bool
}

// Run runs f through p's prompts and writer, with ctx for the Engine.
// Once the flow reaches a result it returns the Outcome, with a nil error
// unless writing the result failed; a write that fails during Apply does
// not stop the Engine. A typed confirmation that does not show whole at
// the terminal's width is such a result: "Operation Cancelled", saying
// that the terminal is too narrow, and Cancelled. When the flow ends
// before a result it returns NotRun and why: nav.ErrBack when the user
// goes back from the form's first field (at the Confirm step 0 or b
// declines instead), nav.ErrQuit, prompt.ErrAborted (form.ErrAborted),
// ctx's error when ctx is done before Apply, ErrFlow, form.ErrField,
// nav.ErrPhrase for a phrase nav.CheckPhrase refuses, or a write error.
// Nothing is applied unless Run reaches the Apply step.
func (f Flow) Run(ctx context.Context, p nav.Prompter) (Outcome, error) {
	if f.Engine == nil || f.Confirm == nil {
		return NotRun, fmt.Errorf("%w: it has no Engine or no Confirm", ErrFlow)
	}
	if err := ctx.Err(); err != nil {
		return NotRun, err
	}
	if err := f.Form.Run(p); err != nil {
		return NotRun, err
	}
	r, c := &run{p: p}, p.Ctx
	if f.Review != nil {
		if err := r.block(f.Review().render(c)); err != nil {
			return NotRun, err
		}
	}
	plan, err := f.Engine.Plan(ctx)
	switch {
	case ctx.Err() != nil:
		return NotRun, ctx.Err()
	case err != nil:
		return Failed, r.block(failure(err, nil).Render(c))
	case len(plan.View.Changes) == 0:
		return Unchanged, r.block(component.Message{Text: nothingToChange}.Render(c))
	}
	plan.View.DryRun = f.DryRun
	switch err := r.block(plan.View.Render(c)); {
	case f.DryRun:
		return Planned, err
	case err != nil:
		return NotRun, err
	}
	yes, err := r.confirm(plan, f.Confirm(plan))
	switch {
	case errors.Is(err, nav.ErrBack), err == nil && !yes:
		return Cancelled, r.block(component.Callout{Kind: component.MessageCancelled}.Render(c))
	case errors.Is(err, errNarrow):
		return Cancelled, r.block(component.Callout{Kind: component.MessageCancelled, Body: []string{tooNarrow}}.Render(c))
	case err != nil:
		return NotRun, err
	case ctx.Err() != nil:
		return NotRun, ctx.Err()
	}
	pr := newProgress(r, plan.View.Changes)
	err = f.Engine.Apply(ctx, pr.report)
	rolledBack, werr := pr.stop(err != nil)
	result, out := component.Callout{Kind: component.MessageOK}.Render(c), Applied
	if err != nil {
		result, out = failure(err, rolledBack).Render(c), Failed
	}
	if werr == nil {
		werr = r.block(result)
	}
	return out, werr
}

// failure is the ErrorReport of err, with the steps rolled back, named in
// the order they were undone. A Failure with no Err says nothing of what
// failed: the report's callout stands alone.
func failure(err error, rolledBack []string) component.ErrorReport {
	rep := component.ErrorReport{Failed: err.Error(), RolledBack: rolledBack}
	var f *Failure
	if errors.As(err, &f) {
		rep.Command, rep.Output, rep.LogPath, rep.Next = f.Command, f.Output, f.LogPath, f.Next
		if f.Err == nil {
			rep.Failed = ""
		}
	}
	return rep
}

// run is one run of a flow: where it writes, and whether it has written.
type run struct {
	p     nav.Prompter
	wrote bool
}

// gap returns the blank lines that set a block apart from what is above
// it: theme.SectionGap of them once anything is, a prompt asked or a block
// written, and none before that.
func (r *run) gap() string {
	if r.wrote || r.p.In.Asked() {
		return strings.Repeat("\n", theme.SectionGap)
	}
	return ""
}

// block writes s, a rendered block, after the gap and with a line break
// after it, in one write; an empty s writes nothing.
func (r *run) block(s string) error {
	if s == "" {
		return nil
	}
	return r.put(r.gap() + s + "\n")
}

// put writes s in one write; an empty s writes nothing.
func (r *run) put(s string) error {
	if s == "" {
		return nil
	}
	r.wrote = true
	if _, err := io.WriteString(r.p.Out, s); err != nil {
		return fmt.Errorf("flow: write: %w", err)
	}
	return nil
}

// confirm asks c about plan and reports whether the user confirmed: a
// typed confirmation for a plan Typed reports, else a y/N question asked
// as a one-field form, unnumbered, its default no. Either is set apart by
// the gap: nav's Typed writes none, and a form writes it only once a
// prompt was asked.
func (r *run) confirm(plan Plan, c Confirm) (bool, error) {
	if plan.Typed() {
		return r.typed(c)
	}
	if !r.p.In.Asked() {
		if err := r.put(r.gap()); err != nil {
			return false, err
		}
	}
	var yes bool
	err := form.Form{Fields: []form.Field{&form.YesNo{Title: c.Question, Value: &yes}}}.Run(r.p)
	return yes, err
}

// typed asks c as a typed confirmation and reports whether the phrase was
// typed: the gap, c's question on a line of its own and theme.PromptGap
// blank lines, as a form shows a field's head, then the label and the
// phrase (nav.Prompter.Typed). A phrase that does not show whole beside
// the label at the width is asked by c.Short when that shows (P-27 (e)).
// Everything is checked before anything is written: a phrase
// nav.CheckPhrase refuses, Phrase's or Short's, returns its error, and
// one that does not show at the width, errNarrow, so that nothing is
// written for a prompt never asked.
func (r *run) typed(c Confirm) (bool, error) {
	q, head := nav.Question{Label: c.Label}, c.Question
	if c.Label == "" {
		q.Label, head = c.Question, ""
	}
	if q.Label == "" {
		return false, fmt.Errorf("%w: its typed confirmation has no label or question", ErrFlow)
	}
	if err := nav.CheckPhrase(c.Phrase); err != nil {
		return false, err
	}
	if c.Short != "" {
		if err := nav.CheckPhrase(c.Short); err != nil {
			return false, err
		}
	}
	phrase := c.Phrase
	if r.p.CheckTyped(q, phrase) != nil {
		if c.Short == "" || r.p.CheckTyped(q, c.Short) != nil {
			return false, errNarrow
		}
		phrase = c.Short
	}
	above := r.gap()
	if h := (component.FieldHead{Title: head}).Render(r.p.Ctx); h != "" {
		above += h + strings.Repeat("\n", theme.PromptGap+1)
	}
	if err := r.put(above); err != nil {
		return false, err
	}
	err := r.p.Typed(q, phrase)
	return err == nil, err
}
