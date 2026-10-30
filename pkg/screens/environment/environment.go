// Package environment runs hardn's Environment screen on a host (refactor
// plan slice 9, Environment, unit (c), next/s9-screen): origin/main's main
// menu 8 as the maintainer adopted its mock on 2026-10-29
// (docs/engine/gallery-environment.md §5), drawn by pkg/ui/screen
// (screen.Environment) from what the Environment control
// (pkg/engine/controls/environment) reads of the host, writing nothing:
// whether sudo, or doas on Alpine, keeps HARDN_CONFIG for the user who
// ran hardn through it. Its one change, keeping HARDN_CONFIG or stopping,
// is a change through the plan/apply engine in pkg/ui's canonical flow:
// the review, y/N, or the hostname typed for a lockout-risk plan, the
// apply; then the screen drawn again under the result from a fresh
// reading. Item 2 is the guide. Run is the one entry point, which
// cmd/hardn's main menu item 8 calls (unit (d), next/s9-replace:
// runEnvironmentScreen); docs/engine/screen-environment.md has its
// states, its words and its API.
//
// The change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control and the engine (depguard rule
// screens-through-host), hardn's environment only through the lookup its
// caller gives (Options.Getenv), and renders only through pkg/ui and
// pkg/output.
package environment

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	envctl "github.com/abbott/hardn/pkg/engine/controls/environment"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the Environment screen runs with: what cmd/hardn has
// when its Environment menu item is chosen. It has the DNS screen's Facts
// and no Config: the Environment control reads the elevation tool from
// detect's facts, and no setting of hardn.yml, and its change saves none,
// so the screen neither reads nor adopts hardn.yml (its change's engine
// takes the backup settings the caller built it with).
type Options struct {
	// Prompter asks the questions and writes the screen through the one
	// line reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's, from which the control takes the elevation tool
	// (sudoers.Choose); the engine establishes its own when it plans.
	Facts detect.Facts
	// Getenv looks up hardn's environment, os.Getenv in hardn: who ran
	// hardn through the tool (SUDO_USER, DOAS_USER) and HARDN_CONFIG as
	// it passed it on. Required: a screen package may not take os
	// (depguard screens-through-host), and with no lookup every host would
	// read as a root login.
	Getenv func(string) string
	// NewEngine returns the engine the change plans and applies with,
	// built as the caller builds it (engine.New over System with its
	// options: its backups, its session, which the journal of a
	// lockout-risk plan names, hardn's path for the revert timer),
	// progress as its Options.Progress; one per change, made when the
	// change is planned. Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a change's apply runs
	// under, made from the screen's, and its stop, called once the apply
	// has returned; one per apply (screenkit.Planning.ApplyContext). Nil
	// applies under the screen's context.
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops the change at its plan: nothing applied.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from, a lockout-risk
	// change's countdown; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each change did once its flow has
	// ended, for a change that was planned: for the audit log.
	Ran func(output.Run)
}

// Run shows the Environment screen and runs the items the user chooses,
// until they go back with 0 (nil) or quit with q (nav.ErrQuit); the end
// of the input and Ctrl-C abort (prompt.ErrAborted). Each round reads the
// host with the Environment control (envctl.Read, on o.Facts and
// o.Getenv), writing nothing, under plan only too, and draws the screen
// under what the last item wrote. Item 1 is refused before it asks
// wherever the reading refuses it (Reading.Refusal: no tool, no user, a
// file of hardn's edited by hand, doas with no rule of hardn's for the
// user, a plan the journal refuses), in the refusal's words, then, on
// doas, that the state row stays as it is (Reading.Stays); else it keeps
// HARDN_CONFIG, or stops keeping it once hardn's files keep it, planned,
// reviewed, confirmed y/N, or by the hostname typed for a lockout-risk
// plan, and applied; n changes nothing, and a dry run stops at the plan.
// After a change applied it says that it takes effect at the next sudo
// hardn: the Value row, this hardn's environment, does not change. Item 2
// is the guide. Run returns an error wrapping fs.ErrInvalid, writing
// nothing, without a Prompter's In and Out, a System, Getenv or
// NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Getenv == nil || o.NewEngine == nil {
		return fmt.Errorf("environment screen: %w: it needs a Prompter with In and Out, a System, Getenv and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	// The session's configuration is read from no file: the change saves
	// no setting, so Session.Change plans with a copy of it that the
	// control does not read, and adopts nothing (Adopt: no path).
	r := &runner{o: o, s: screenkit.Session{Name: "environment screen", Prompter: o.Prompter, System: o.System, Config: &config.Config{},
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the Environment screen: its options, and the
// session its change runs with.
type runner struct {
	o Options
	s screenkit.Session
}

// draw reads the host and returns the screen, its items bound to their
// actions.
func (r *runner) draw(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	sub := h.view.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: r.action(ctx, h, it.Key)})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// reading is one reading of the host: the control's, and the screen's
// view of it.
type reading struct {
	envctl.Reading
	view screen.Environment
}

// read reads the host as the Environment control reads it (envctl.Read:
// the tool, who ran hardn, hardn's files and origin/main's for that user,
// and item 1's change built as its plan is, writing nothing).
func (r *runner) read(ctx context.Context) reading {
	h := reading{Reading: envctl.Read(ctx, r.o.System, r.o.Facts, r.o.Getenv)}
	h.view = viewOf(h.Reading)
	return h
}

// viewOf is r as the screen draws it, by the control's state
// (Reading.State, the one classification Check takes too): no user, no
// tool, not read, or read, kept by hardn's files, by origin/main's line,
// or not; in the control's words.
func viewOf(r envctl.Reading) screen.Environment {
	v := screen.Environment{Tool: r.ToolName(), User: r.User, Value: r.Value, Refused: r.Refusal() != nil, Risk: r.Risk}
	for _, f := range r.Later() {
		v.Later = append(v.Later, f.Detail)
	}
	switch state := r.State(); {
	case state == engine.NotApplicable && errors.Is(r.Err, envctl.ErrNoUser):
		v.State, v.User = screen.EnvironmentNotRun, r.NotRun()
	case state == engine.NotApplicable:
		v.State = screen.EnvironmentNoTool
	case state == engine.Unknown:
		v.State = screen.EnvironmentUnknown
	default:
		v.Word, v.Note = r.Words()
		switch {
		case r.Kept:
			v.State = screen.EnvironmentKept
		case state == engine.Configured:
			v.State = screen.EnvironmentKeptLegacy
		default:
			v.State = screen.EnvironmentNotKept
		}
	}
	return v
}
