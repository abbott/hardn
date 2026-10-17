package ufw

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// StepReport is what one step did.
type StepReport struct {
	Step   Step
	Result host.Result
	// Undo is what undoes the step, or nil: an allow ufw skipped as an
	// existing rule changed nothing, and one whose answer does not show
	// what it added is not undone (Note says why), so that an undo never
	// deletes a rule hardn did not add.
	Undo []string
	Note string
}

// Report is what Execute did.
type Report struct {
	Steps []StepReport // in order, up to and including a failed step
	// Undo are the commands that undo the steps that took effect, last
	// first: the input to Rollback (and p2-revert's journal).
	Undo [][]string
	// Status is "ufw status verbose" after the steps, Coverage each
	// Need's Verdict under it. Verified: ufw shows the planned state, with
	// every Need the plan lets through let through. Unverified says why
	// a Report without an error is not Verified.
	Status     Status
	Coverage   []Coverage
	Verified   bool
	Unverified string
}

// allowWords are ufw's answers to an allow, per IP version: "Rule added"
// while active, "Rules updated" while inactive, "Skipping adding existing
// rule" for a rule already saved.
var (
	addedWords   = []string{"Rule added", "Rule added (v6)", "Rules updated", "Rules updated (v6)"}
	skippedWords = []string{"Skipping adding existing rule", "Skipping adding existing rule (v6)"}
)

// Execute runs p's steps in order, stopping at the first that fails, then
// reads "ufw status verbose" and checks it shows the plan's state: active
// or not, the default incoming policy, and every Need that p.After lets
// through still let through (ErrVerify otherwise; an "Aborted" from ufw
// exits 0 and is caught here). On any error the caller rolls back with
// Rollback(ctx, sys, report) while the session is still open. A done ctx is
// refused before anything runs; a rollback after Ctrl-C must pass
// context.WithoutCancel. Nothing runs for a NotApplicable plan.
func Execute(ctx context.Context, sys host.System, p Plan) (Report, error) {
	var rep Report
	if err := ctx.Err(); err != nil {
		return rep, fmt.Errorf("ufw: %w", err)
	}
	if p.NotApplicable {
		return rep, nil
	}
	for _, s := range p.Steps {
		if len(s.Argv) == 0 || s.Kind == 0 {
			return rep, fmt.Errorf("ufw: step %q: %w", s.Why, fs.ErrInvalid)
		}
		res, err := sys.Run(ctx, s.Argv[0], s.Argv[1:]...)
		sr := StepReport{Step: s, Result: res}
		if err == nil {
			sr.Undo, sr.Note = undo(s, res.Stdout)
		} else if s.Kind == StepAllow {
			sr.Note = "not undone: the allow failed, so what it saved is not established"
		}
		rep.Steps = append(rep.Steps, sr)
		if sr.Undo != nil {
			rep.Undo = append([][]string{sr.Undo}, rep.Undo...)
		}
		if err != nil {
			return rep, fmt.Errorf("ufw: %s: %w", host.FormatArgv(s.Argv), err)
		}
	}
	res, err := sys.Run(ctx, "ufw", "status", "verbose")
	if err != nil {
		return rep, fmt.Errorf("ufw: %w", err)
	}
	rep.Status = ParseStatus(res.Stdout)
	return rep, verify(&rep, p)
}

// undo reads ufw's answer to a step.
func undo(s Step, out []byte) ([]string, string) {
	if s.Kind != StepAllow {
		return s.Undo, ""
	}
	var added, skipped, other int
	for _, line := range strings.Split(string(out), "\n") {
		switch line = strings.TrimSpace(line); {
		case line == "":
		case slices.Contains(addedWords, line):
			added++
		case slices.Contains(skippedWords, line):
			skipped++
		default:
			other++
		}
	}
	switch {
	case added > 0 && skipped == 0 && other == 0:
		return s.Undo, ""
	case added == 0 && other == 0 && skipped > 0:
		return nil, "ufw kept an existing rule"
	}
	return nil, fmt.Sprintf("not undone: ufw's answer %q does not show it added the rule alone", strings.TrimSpace(string(out)))
}

func verify(rep *Report, p Plan) error {
	st := rep.Status
	switch {
	case !st.Known || len(st.Bad) > 0:
		return fmt.Errorf("ufw: %w: status not read whole (unread lines %q)", ErrVerify, st.Bad)
	case st.Active != p.Active:
		return fmt.Errorf("ufw: %w: active is %t, planned %t", ErrVerify, st.Active, p.Active)
	case !st.Active:
		rep.Unverified = "ufw is inactive: its rules are saved, not in force"
		return nil
	case st.Incoming != p.Incoming:
		return fmt.Errorf("ufw: %w: default incoming policy %q, planned %q", ErrVerify, st.Incoming, p.Incoming)
	}
	rep.Coverage = Evaluate(st.Rules, true, p.IPv6, st.Incoming, p.Needs)
	var missing []string
	for i, c := range rep.Coverage {
		if (i >= len(p.After) || p.After[i].Verdict.OK()) && !c.Verdict.OK() {
			missing = append(missing, fmt.Sprintf("%s (%s %d) is %s by %q", c.Need.Why, c.Need.Family, c.Need.Port, c.Verdict, c.By))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("ufw: %w: %s", ErrVerify, strings.Join(missing, "; "))
	}
	rep.Verified = true
	return nil
}

// Rollback runs rep.Undo in order, each command even after one fails, and
// returns their errors joined. After Ctrl-C pass context.WithoutCancel(ctx):
// host.System refuses a done context.
func Rollback(ctx context.Context, sys host.System, rep Report) error {
	var errs []error
	for _, argv := range rep.Undo {
		if len(argv) == 0 {
			continue
		}
		if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
			errs = append(errs, fmt.Errorf("ufw: %s: %w", host.FormatArgv(argv), err))
		}
	}
	return errors.Join(errs...)
}
