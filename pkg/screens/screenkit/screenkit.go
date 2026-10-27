// Package screenkit is the glue every area's screen shares (refactor plan
// slice 3, unit (b), next/s3-shared): what pkg/screens/dns held to itself
// until Backup's screen became its second user (docs/engine/screen-kit.md).
// A screen runs each of its changes with Session.Change: the form, the
// review with the plan's findings, the plan of the change's controls on the
// engine through output.FlowEngine (Planning), y/N, the apply, then what the
// flow does not draw (output.AfterFlow), what the change did given to Ran,
// and hardn.yml adopted as the change saved it (Adopt). An item that cannot
// run says why before it asks anything (Session.Say), in a refusal's words
// (RefusalWords; NotLoaded with no hardn.yml read).
//
// What a screen reads of its host, its items, its words and its view-model
// stay in its own package, pkg/screens/<area>. Like those, this package
// reaches the host only through host.System and the engine, and draws only
// through pkg/ui and pkg/output (depguard rule screens-through-host).
package screenkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// Session is one run of a screen: what its caller gave it (pkg/screens/dns's
// Options), which each of its changes runs with.
type Session struct {
	// Name names the screen in a failed write's error: "dns screen".
	Name string
	// Prompter asks the questions and writes the screen through the one
	// line reader, rendering with its Ctx.
	Prompter nav.Prompter
	// System is the host hardn.yml is read back from after a change
	// (Adopt); the engine changes it.
	System host.System
	// Config is hardn.yml as loaded. Each change plans a copy of it, edited
	// as the change asks (Change.Controls), and Config adopts the copy once
	// the change has saved it (Adopt).
	Config *config.Config
	// NewEngine returns the engine one change plans and applies with,
	// progress as its Options.Progress, so that the flow shows each step;
	// called when the change is planned, once its form is answered.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a change's apply runs
	// under, made from the screen's, and its stop (Planning.ApplyContext).
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops each change at its plan: nothing applied or saved.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each change did once its flow has
	// ended, for a change that was planned.
	Ran func(output.Run)
}

// Write writes text to the screen's writer.
func (s Session) Write(text string) error {
	if _, err := io.WriteString(s.Prompter.Out, text); err != nil {
		return fmt.Errorf("%s: write: %w", s.Name, err)
	}
	return nil
}

// Say returns an action that writes msgs set apart as an action's result
// is (Apart), and goes back to the screen.
func (s Session) Say(msgs component.Messages) func() error {
	return func() error { return s.Write(Apart(s.Prompter.Ctx, msgs)) }
}

// Apart is msgs rendered for c and set apart as nav.Prompter.Run sets an
// action's result apart: theme.SectionGap blank lines before them, a
// newline after.
func Apart(c component.Ctx, msgs component.Messages) string {
	return strings.Repeat("\n", theme.SectionGap) + msgs.Render(c) + "\n"
}

// Warnings are paragraphs as one warning, each a warning message, which
// component.Messages draws as one callout.
func Warnings(paragraphs ...string) component.Messages {
	msgs := make(component.Messages, len(paragraphs))
	for i, p := range paragraphs {
		msgs[i] = component.Message{Kind: component.MessageWarn, Text: p}
	}
	return msgs
}

// Change is one change of a screen, which Session.Change runs.
type Change struct {
	// Step is the title bar the change's flow runs under.
	Step screen.WorkflowStep
	// Flow is the change's form, review and question (Form, Review,
	// Confirm); its Engine and DryRun are the session's.
	Flow flow.Flow
	// Controls returns the controls the change plans with, once its form
	// is answered, after editing edited as the change asks: edited is a
	// copy of the session's configuration, made as the change begins, and
	// the configuration the engine plans with, whose save the change's
	// control plans. Required.
	Controls func(edited *config.Config) []engine.Control
}

// Change runs c under its title bar in pkg/ui's canonical flow on the
// engine (Planning): the form; the review and the plan's findings; the
// plan of c's controls from a copy of hardn.yml edited as c asks, built
// once the form is answered; the question; the apply. Then, for a change
// that was planned, what the flow does not draw (output.AfterFlow), what
// the change did given to Ran, and, once it applied, hardn.yml adopted as
// saved (Adopt). n changes and saves nothing; a dry run stops at the plan.
// It returns the flow's error: nav.ErrBack from the form's first field,
// which goes back to the screen, nav.ErrQuit, an abort, or a write that
// failed.
func (s Session) Change(ctx context.Context, c Change) error {
	now := s.Now
	if now == nil {
		now = time.Now
	}
	edited := *s.Config
	fe := &output.FlowEngine{DryRun: s.DryRun, LogPath: s.LogPath}
	p := &Planning{FlowEngine: fe, ApplyContext: s.ApplyContext, Build: func() {
		if fe.Engine == nil {
			fe.Engine = s.NewEngine(fe.Progress)
		}
		fe.Controls = c.Controls(&edited)
		fe.Config = &edited
	}}
	f := c.Flow
	f.Engine, f.DryRun = p, s.DryRun
	out, err := c.Step.Run(ctx, s.Prompter, f)
	if !p.Planned() {
		return err
	}
	run := fe.Run(out, err)
	if after := output.AfterFlow(s.Prompter.Ctx.Env, run, now()); after != "" {
		if werr := s.Write(after); err == nil {
			err = werr
		}
	}
	if s.Ran != nil {
		s.Ran(run)
	}
	if out == flow.Applied && !s.DryRun {
		Adopt(ctx, s.System, s.Config, &edited)
	}
	return err
}

// Planning is a change's flow.Engine: output.FlowEngine, its engine,
// controls and configuration built when the flow plans (Build), once the
// form is answered, as FlowEngine reads them then; and its apply under
// ApplyContext's context. One Planning serves one run of a flow, as its
// FlowEngine does.
type Planning struct {
	*output.FlowEngine
	// Build sets the FlowEngine's Engine, Controls and Config from the
	// form's answers. Required.
	Build func()
	// ApplyContext, when set, returns the context the apply runs under,
	// made from the flow's, and its stop, called once the apply has
	// returned; one per apply. cmd/hardn's is cancelled by Ctrl-C, SIGTERM
	// and a hangup during the apply alone (engine.md §2.14), so that a
	// change they interrupt is rolled back and the screen goes on. Nil
	// applies under the flow's context.
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	planned      bool
}

var _ flow.Engine = (*Planning)(nil)

// Plan builds the engine's inputs from the answers, then plans as
// FlowEngine does.
func (p *Planning) Plan(ctx context.Context) (flow.Plan, error) {
	p.Build()
	p.planned = true
	return p.FlowEngine.Plan(ctx)
}

// Apply applies as FlowEngine does, under the context ApplyContext makes
// for the apply alone, when set.
func (p *Planning) Apply(ctx context.Context, report flow.Report) error {
	if p.ApplyContext != nil {
		var stop context.CancelFunc
		ctx, stop = p.ApplyContext(ctx)
		defer stop()
	}
	return p.FlowEngine.Apply(ctx, report)
}

// Planned reports whether the flow asked for a plan: false when it ended
// at the form, so that nothing was planned and nothing ran.
func (p *Planning) Planned() bool { return p.planned }

// Adopt makes c, the configuration a screen holds, edited, the copy a
// change planned, once hardn.yml at c's path holds the bytes edited saved
// there and not the ones c records: the change saved it, and the engine did
// not roll the save back. edited's other settings are c's as they were in
// memory when the change began (Session.Change copies them), the save
// having written only the settings its control names, so that a flag's
// value in memory is never saved. A configuration not loaded from a file,
// or a file that cannot be read (a link: ReadFileNoFollow), adopts nothing;
// the next change is then planned from c as it was, and its save refused
// if the file changed (CONFIG_CHANGED).
func Adopt(ctx context.Context, sys host.System, c, edited *config.Config) {
	if c.Path() == "" {
		return
	}
	data, _, err := sys.ReadFileNoFollow(ctx, c.Path())
	if err != nil || c.LoadedFrom(data) || !edited.LoadedFrom(data) {
		return
	}
	*c = *edited
}

// NotLoaded are the words of the save's refusal of cfg when it was not read
// from a file (steps.PlanSave's, CONFIG_NOT_LOADED), which every item of a
// screen that saves hardn.yml says before asking; nil when it was. id and
// settings are the save's change and the settings it writes, as the
// screen's control plans them. PlanSave refuses such a configuration before
// it reads anything: with no path there is no file.
func NotLoaded(ctx context.Context, sys host.System, cfg *config.Config, id string, settings ...string) []string {
	if cfg.Path() != "" {
		return nil
	}
	_, err := steps.PlanSave(ctx, sys, id, cfg, settings)
	if err == nil {
		return nil // not reached: PlanSave refuses a configuration with no path
	}
	return []string{RefusalWords(err)}
}

// RefusalWords are a refusal's words on a screen: its Why, then its Fix,
// each a sentence ("NetworkManager manages DNS here. Set the nameservers
// with nmcli."), as the adopted DNS mock words NetworkManager's
// (gallery-dns.md decision 4); its code is left to what shows a run in
// full. The Why is written as it starts, a program's name in its own case
// ("resolvconf manages DNS here."); the Fix, written to follow "To go on: ",
// starts with a capital. An error that is no refusal says itself.
func RefusalWords(err error) string {
	var rf *steps.Refusal
	if !errors.As(err, &rf) || rf.Why == "" {
		return FullStop(err.Error())
	}
	if rf.Fix == "" {
		return FullStop(rf.Why)
	}
	first, n := utf8.DecodeRuneInString(rf.Fix)
	return FullStop(rf.Why) + " " + FullStop(string(unicode.ToUpper(first))+rf.Fix[n:])
}

// FullStop is s ending with one full stop.
func FullStop(s string) string {
	return strings.TrimSuffix(strings.TrimSpace(s), ".") + "."
}
