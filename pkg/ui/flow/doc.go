// Package flow runs brief 1C's canonical action flow: every action that
// changes the system collects its answers, reviews them, plans, confirms,
// applies and returns, in that order and never otherwise.
//
// # The steps
//
// A Flow is one action's definition: its form, a review builder, its
// Engine and its confirmation. Run takes it through the steps:
//
//  1. Collect: the form runs (pkg/ui/form). Nothing is planned or applied
//     until every answer is in, so no prompt is interleaved with a change.
//  2. Review: the answers, summed up as Figma sums up a new user: a
//     SectionHeader, such as "User Configuration", over a BulletList of
//     key-value rows.
//  3. Plan: the Engine plans, and the plan is shown as a PlanView. In
//     dry-run mode the flow stops here, the PlanView's footer saying that
//     nothing was changed. A plan with no changes says that nothing needs
//     changing, brief Phase 3's words, and the flow stops too.
//  4. Confirm: a y/N question whose default is no, as a one-field form
//     asks it; or, for a plan with lockout risk (Plan.Typed), a typed
//     confirmation of a phrase such as the hostname (nav.Prompter.Typed).
//     The answer no, and 0 or b, decline: an "Operation Cancelled"
//     Callout, and nothing applied.
//  5. Apply: the Engine applies the plan, one step per change, and the
//     steps show as a StepProgress. A failure is rolled back by the
//     Engine; the flow then shows an ErrorReport naming what was rolled
//     back, else an "Operation Successful" Callout.
//  6. Return: Run returns, leaving everything it wrote on screen; the
//     caller's menu draws itself again below, with its status read again
//     (nav.Run), and nothing is cleared.
//
// 0 or b at the form's first field leaves the flow (nav.ErrBack); q quits
// (nav.ErrQuit) and the end of the input or Ctrl-C aborts
// (prompt.ErrAborted) at any prompt; a context cancelled before Apply
// stops the flow too. Each returns before Apply, so nothing is applied.
// A plan that cannot be made, and an apply that fails, are results, not
// errors: Run returns the Outcome with a nil error, so the caller's menu
// carries on. Run returns an error only when the flow ends without a
// result.
//
// # Writing
//
// Every block is written to the Prompter's writer, which is
// term.Env.Writer around stdout, in one write, and set apart from what is
// above it by theme.SectionGap blank lines once anything is: a prompt was
// asked (prompt.Reader.Asked) or the flow wrote a block. A caller that
// draws something above a flow that asks nothing first, such as a
// workflow screen's title bar, ends it with those blank lines itself when
// Asked reports false, as for a form (pkg/ui/form).
//
// The Apply step animates only when term.Env.Animate is set: the steps
// are drawn once and drawn again in place, a spinner on the running step,
// with the cursor moved up over them (CPL) and each line erased (EL)
// before it is written again. Otherwise, as on a pipe, in plain mode or
// when the steps would not fit the terminal's height, each step's line is
// written once it settles, done, failed or skipped, so a step takes one
// line; what was rolled back is named by the ErrorReport that follows.
// Steps never reached after a failure show as skipped.
//
// # The engine
//
// Engine is the minimal interface the flow needs of brief Phase 3's
// plan/apply engine (next/p3-engine), which will satisfy it through one
// adapter per action, holding the action's answers. Until then the flow
// runs only on fixture plans: Fixture is an Engine that touches nothing.
// No code here reads or changes the system.
//
// Proposal P-27 puts the flow's own choices to the maintainer: back at the
// confirmation, what an empty plan says (the one sentence the flow adds),
// the plan with no heading of its own, and the steps' lines without
// animation. Each confirmation's words are the action's (P-10).
package flow
