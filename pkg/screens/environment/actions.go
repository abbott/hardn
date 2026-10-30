package environment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	envctl "github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The screen's own words (docs/engine/screen-environment.md §5): the
// questions and the line after a change applied, decided with decision 7
// (gallery-environment.md §6); the typed confirmation's label, the flow's
// (P-25 (d)), proposed there; cantConfirm, new in this unit, proposed.
// The state's, the change's and the refusals' words are the control's.
const (
	askKeep         = "Keep HARDN_CONFIG under %s?"
	askStop         = "Stop keeping HARDN_CONFIG under %s?"
	takesEffect     = "Takes effect at the next %s hardn."
	typeTheHostname = "Type the hostname"
	cantConfirm     = "Can't read the hostname to confirm with: %s"
)

// action returns what choosing the item keyed key does with the reading
// h: item 1, where the reading refuses it, says why before asking
// anything; where its change is lockout-risk, is to be applied, and the
// hostname it is confirmed by cannot be read, says that, before asking
// too; else its change. Plan only confirms nothing (the flow stops at the
// plan before its Confirm), so it needs no hostname. Item 2 says the
// guide.
func (r *runner) action(ctx context.Context, h reading, key string) func() error {
	switch key {
	case screen.EnvironmentKeepItem:
		if h.Refusal() != nil {
			return r.s.Say(screenkit.Warnings(refused(h.Reading)))
		}
		return func() error {
			if h.Risk && !r.o.DryRun {
				if _, _, err := hostname(ctx, r.o.System); err != nil {
					return r.cantConfirm(err)
				}
			}
			return r.change(ctx, h)
		}
	case screen.EnvironmentGuideItem:
		return r.s.Say(screen.EnvironmentGuide())
	}
	return func() error { return fmt.Errorf("environment screen: item %s has no action", key) } // not reached: SubMenu's items are those above
}

// refused are the words of item 1's refusal on r (Reading.Refusal), as the
// screen says it before asking: the refusal's Why, then its Fix, each a
// sentence (screenkit.RefusalWords), an error with no words of the
// control's as it is; then, on doas where hardn has no rule for the user,
// that the state row will not change once the user does what it says
// (Reading.Stays: the screen's, not the refusal's; control-environment.md
// §10, decision 18).
func refused(r envctl.Reading) string {
	words := screenkit.RefusalWords(r.Refusal())
	if s := r.Stays(); s != "" {
		words += " " + screenkit.FullStop(s)
	}
	return words
}

// change is item 1 through the flow (screenkit.Session.Change), under its
// title bar: the review, the User row and the state row as the change
// leaves it (envctl.Words of the way it turns); the plan, the engine's, of
// the Environment control keeping HARDN_CONFIG, or stopping once hardn's
// files keep it (envctl.Keep, Stop as Read's Kept: decision 5 (a)), the
// plan's findings listed with the review; the question, y/N, or, for a
// lockout-risk plan (on doas, a rule of hardn's that asks the user's
// password), the hostname typed; the apply. Then, once applied, that it
// takes effect at the next sudo hardn: this hardn was started without it,
// so the Value row stays as it was. A hostname that cannot be read or
// typed is said, nothing asked or applied.
func (r *runner) change(ctx context.Context, h reading) error {
	tool, question := h.ToolName(), askKeep
	if h.Kept {
		question = askStop
	}
	question = fmt.Sprintf(question, tool)
	word, note := envctl.Words(!h.Kept)
	var applied bool
	var phraseErr error
	s := r.s
	s.Ran = func(run output.Run) {
		applied = run.Mode == output.ModeApply && run.Err == nil && run.Result != nil && !run.Result.Nothing && run.Result.Failed == nil
		if r.o.Ran != nil {
			r.o.Ran(run)
		}
	}
	err := s.Change(ctx, screenkit.Change{
		Step: h.view.Step(),
		Flow: flow.Flow{
			Review: func() flow.Review { return h.view.Review(word, note) },
			Confirm: func(p flow.Plan) flow.Confirm {
				c := flow.Confirm{Question: question}
				if p.Typed() {
					c.Label = typeTheHostname
					c.Phrase, c.Short, phraseErr = hostname(ctx, r.o.System)
				}
				return c
			},
		},
		Controls: func(*config.Config) []engine.Control {
			return []engine.Control{envctl.Keep{Getenv: r.o.Getenv, Stop: h.Kept}}
		},
	})
	switch {
	case phraseErr != nil && errors.Is(err, nav.ErrPhrase):
		// The plan became lockout-risk after the screen read the host,
		// which said it was not, and the hostname cannot be read: the
		// flow asked nothing.
		return r.cantConfirm(phraseErr)
	case err == nil && applied:
		return r.s.Say(component.Messages{{Text: fmt.Sprintf(takesEffect, tool)}})()
	}
	return err
}

// cantConfirm says, as a warning, that the hostname a lockout-risk
// change is confirmed by cannot be read, and why; nothing is asked.
func (r *runner) cantConfirm(err error) error {
	return r.s.Say(screenkit.Warnings(screenkit.FullStop(fmt.Sprintf(cantConfirm, err))))()
}

// hostname is the phrase a lockout-risk change is confirmed by typing:
// the host's name as the kernel has it (header.HostnameFile), as
// hostname(1) prints it and the main menu's header shows it; and its
// first label where the name has more, asked in its place where the name
// does not show whole beside the label (flow.Confirm.Short; P-25 (e)). An
// error where the name cannot be read, or is no phrase nav can ask for
// (nav.CheckPhrase).
func hostname(ctx context.Context, sys host.System) (phrase, short string, err error) {
	data, err := sys.ReadFile(ctx, header.HostnameFile)
	if err != nil {
		return "", "", err
	}
	phrase = strings.TrimSpace(string(data))
	switch err := nav.CheckPhrase(phrase); {
	case phrase == "":
		return "", "", fmt.Errorf("%s is empty", header.HostnameFile)
	case err != nil:
		return "", "", err
	}
	if first, _, ok := strings.Cut(phrase, "."); ok && nav.CheckPhrase(first) == nil {
		short = first
	}
	return phrase, short, nil
}
