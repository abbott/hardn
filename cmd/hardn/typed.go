package main

// A flag's confirmation of a lockout-risk plan (refactor plan slice 4,
// unit (d); gallery-firewall.md §5, decision 4, adopted 2026-11-01: "on a
// terminal ask the typed hostname and arm the timer; off a terminal
// refuse", and slice 6's -d meets the same question): runControls' option,
// so that any flag whose plan may be lockout-risk takes it, -w first.

import (
	"context"
	"errors"
	"io"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
)

// A typedConfirm is how runControls confirms a flag's plan where it may be
// lockout-risk. On a terminal, stdin and stdout both, and not under plan
// only, the plan goes through pkg/ui's canonical flow as a screen's change
// does: the plan, then question above the hostname typed
// (screenkit.Hostname's phrase, and its short form where the name does not
// show whole), never y/N, whatever the plan's risk; 0 and q decline; the
// apply, the revert timer armed as the engine arms it for a lockout-risk
// plan; then what the flow does not draw (output.AfterFlow: the
// countdown, and sudo hardn confirm). A hostname that can't be read is said
// in place of the question, nothing asked (Session.CantConfirm). Off a
// terminal a lockout-risk plan is refused with offTerminal, shown with the
// plan and not applied, nothing armed; a plain plan is applied as
// runControls applies any flag's. Under plan only nothing is asked or
// refused: the plan alone.
type typedConfirm struct {
	question    string
	offTerminal error
}

// onTerminal reports whether hardn can ask: stdin and stdout are both a
// terminal (stdoutEnv's InputTTY and TTY).
func onTerminal() bool {
	env := stdoutEnv()
	return env.TTY && env.InputTTY
}

// refuse refuses p off a terminal when it is lockout-risk (engine.RiskLockout),
// as runControls' refuse.
func (q typedConfirm) refuse(p *engine.Plan) error {
	if p.Risk == engine.RiskLockout {
		return q.offTerminal
	}
	return nil
}

// typedEngine is the flow's engine for a typedConfirm: output.FlowEngine,
// each plan confirmed by the hostname typed (flow.RiskLockout), and its
// apply under the context apply makes, whose stop is the caller's.
type typedEngine struct {
	*output.FlowEngine
	apply func(ctx context.Context) context.Context
}

var _ flow.Engine = typedEngine{}

// Plan plans as FlowEngine does, and asks for the hostname whatever the
// plan's risk.
func (e typedEngine) Plan(ctx context.Context) (flow.Plan, error) {
	p, err := e.FlowEngine.Plan(ctx)
	p.Risk = flow.RiskLockout
	return p, err
}

// Apply applies as FlowEngine does, under apply's context.
func (e typedEngine) Apply(ctx context.Context, report flow.Report) error {
	return e.FlowEngine.Apply(e.apply(ctx), report)
}

// typedReraise ends hardn by SIGINT after Ctrl-C at the hostname's
// prompt, as at every prompt (prompt.Reraise); a seam for the tests.
var typedReraise = prompt.Reraise

// runTyped is runControls on a terminal with q: controls planned over c
// with engineOptions, then the flow (typedConfirm), on the one line reader
// every prompt shares and stdout's render environment. The apply and what
// is written after it run under engineSignals and a hangup's watch,
// registered as the apply starts and stopped as runTyped returns, as
// runControls registers them, so that the exit status and how a signal
// ends hardn are runControls' (endControls). Ctrl-C at the prompt ends
// hardn by SIGINT, as at every prompt; the end of the input exits 130.
func runTyped(c *config.Config, controls []engine.Control, q typedConfirm) int {
	ctx := context.Background()
	sys := engineHost()
	env := stdoutEnv()
	p := nav.Prompter{Ctx: component.NewCtx(env), In: engineInput(), Out: env.Writer(engineStdout)}
	fe := &output.FlowEngine{Controls: controls, Config: c, LogPath: output.LegacyLogPath}
	fe.Engine = newEngine(sys, engineOptions(ctx, sys, c))(fe.Progress)
	actx := ctx // a signal's, around the apply and what follows it alone
	hungUp := func() bool { return false }
	var stops []func()
	defer func() {
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
	}()
	apply := func(ctx context.Context) context.Context {
		hup := watchHangup() // first: no hangup engineSignals catches goes unmarked
		stops = append(stops, func() { hup.stop() })
		hungUp = hup.seen
		var stop context.CancelFunc
		actx, stop = engineSignals(ctx)
		stops = append(stops, stop)
		return actx
	}
	var phraseErr error
	f := flow.Flow{
		Engine: typedEngine{FlowEngine: fe, apply: apply},
		Confirm: func(flow.Plan) flow.Confirm {
			cf := flow.Confirm{Question: q.question, Label: screenkit.TypeTheHostname}
			cf.Phrase, cf.Short, phraseErr = screenkit.Hostname(ctx, sys)
			return cf
		},
	}
	out, err := f.Run(ctx, p)
	if errors.Is(err, prompt.ErrInterrupted) {
		typedReraise()
	}
	run := fe.Run(out, err)
	switch {
	case phraseErr != nil && errors.Is(err, nav.ErrPhrase):
		err = screenkit.Session{Name: "hardn", Prompter: p}.CantConfirm(phraseErr)
	case err == nil:
		if after := output.AfterFlow(env, run, revertNow()); after != "" {
			_, err = io.WriteString(p.Out, after)
		}
	}
	if err != nil && !errors.Is(err, nav.ErrQuit) && !errors.Is(err, nav.ErrBack) && !errors.Is(err, nav.ErrPhrase) && !errors.Is(err, prompt.ErrAborted) {
		logging.LogError("%v", err)
	}
	return endControls(run, actx, hungUp)
}
