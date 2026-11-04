// Package updates runs hardn's Auto Updates screen on a host (refactor plan
// slice 7, unit (c), next/s7-screen): the mock the maintainer adopted on
// 2026-11-03 (docs/engine/gallery-updates.md §4 and §5), drawn by
// pkg/ui/screen from what the Auto Updates control
// (pkg/engine/controls/updates) reads of the host, writing nothing; its one
// item the control's plan, on or off, through the plan/apply engine in
// pkg/ui's canonical flow: the review with the control's notes, y/N, the
// apply, rolled back on failure, then the screen drawn again from a fresh
// reading. Every refusal is the control's or the engine's, said before
// anything is asked. Run is the one entry point, which cmd/hardn's main
// menu item 6 is to call (unit (d)); docs/engine/screen-updates.md has its
// API and what it decided.
//
// The change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control and the engine (depguard rule
// screens-through-host), and renders only through pkg/ui and pkg/output.
package updates

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	updatesctl "github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/nav"
)

// Options is what the Auto Updates screen runs with: what cmd/hardn has
// when its Auto Updates menu item is chosen. It has the SSH Login screen's,
// but Session and Getenv, which the control does not read, and the
// control's own two.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's, from which the control takes the distribution,
	// the init system and Proxmox VE; the engine establishes its own when it
	// plans.
	Facts detect.Facts
	// Config is hardn.yml as loaded: off saves enableUnattendedUpgrades
	// false through the control's plan where it says true; nothing else is
	// read. Nil has none.
	Config *config.Config
	// NewEngine returns the engine the item plans and applies with, built as
	// the caller builds it (engine.New over System with its options: its
	// backups, its session), progress as its Options.Progress: one when the
	// item is chosen, whose plan before asking says a refusal first, and one
	// for the change, made when it is planned. Drawing makes none. Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a change's apply runs
	// under, made from the screen's, and its stop, called once the apply has
	// returned; one per apply (screenkit.Planning.ApplyContext). Nil applies
	// under the screen's context.
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops the change at its plan: nothing applied.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each change did once its flow has ended,
	// for a change that was planned: for the audit log.
	Ran func(output.Run)
	// Version is hardn's, which the control's record carries.
	Version string
	// Records are the control's record's directory and clock
	// (updates.Auto.Records); zero in hardn: /var/lib/hardn, time.Now.
	Records revert.Options
}

// Run shows the Auto Updates screen and runs its item until the user goes
// back with 0 (nil) or quits with q (nav.ErrQuit); the end of the input and
// Ctrl-C abort (prompt.ErrAborted), from the screen and the question. Each
// round reads the host with the control (updates.Auto's Read), writing
// nothing, under plan only too, and draws the row as the main menu draws
// it, with the "changed since hardn" line where the control finds it, the
// section and the item.
//
// Item 1 is Enable automatic updates while off or partial, Disable
// automatic updates while on by hardn; Unavailable (P-22) on Proxmox VE and
// while on not by hardn; its description, where it is offered, what is in
// the way (R1). Chosen, it is planned by the engine first, from a fresh
// reading, and its refusal (the control's or the engine's) said before
// anything is asked; then the review with the control's notes, the plan,
// planned again from a fresh reading, y/N, the apply. n changes nothing,
// and a dry run stops at the plan. Run returns an error wrapping
// fs.ErrInvalid, writing nothing, without a Prompter's In and Out, a System
// or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.NewEngine == nil {
		return fmt.Errorf("updates screen: %w: it needs a Prompter with In and Out, a System and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{} // enableUnattendedUpgrades false: off plans no save
	}
	r := &runner{o: o, cfg: cfg, s: screenkit.Session{Name: "updates screen", Prompter: o.Prompter, System: o.System, Config: cfg,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: o.Ran}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the Auto Updates screen: its options, hardn.yml as
// the engine plans with it, and the session its change runs with.
type runner struct {
	o   Options
	cfg *config.Config
	s   screenkit.Session
}

// control is the Auto Updates control doing a, its record carrying
// hardn's version.
func (r *runner) control(a updatesctl.AutoAction) updatesctl.Auto {
	return updatesctl.Auto{Action: a, Version: r.o.Version, Records: r.o.Records}
}

// reading is one reading of the host: the control's, and why it was cut
// short (err: the context's).
type reading struct {
	updatesctl.Reading
	err error
}

// read reads the host as the control reads it (updates.Auto's Read,
// writing nothing, running only reads).
func (r *runner) read(ctx context.Context) reading {
	rd, err := r.control(updatesctl.AutoNone).Read(ctx, r.o.System, r.o.Facts, r.cfg)
	return reading{Reading: rd, err: err}
}
