// Package firewall runs hardn's Firewall screen on a host (refactor plan slice
// 4, unit (c), next/s4-screen): origin/main's main menu 4 as the maintainer
// adopted its mocks on 2026-11-01 (docs/engine/gallery-firewall.md §5), drawn by
// pkg/ui/screen from what the Firewall control (pkg/engine/controls/firewall)
// reads of the host, writing nothing, each of its changes a change through the
// plan/apply engine in pkg/ui's canonical flow: the review, y/N, or the hostname
// typed for a lockout-risk plan, the apply, then the screen drawn again under the
// result from a fresh reading. On Proxmox VE it is read only: no ufw call, and
// no item offered, so hardn.yml plays no part. Run is the one entry point, which cmd/hardn's main menu
// item 4 is to call (unit (d)); docs/engine/screen-firewall.md has its API and
// what it decided.
//
// Each change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control and the engine (depguard rule screens-through-host),
// and renders only through pkg/ui and pkg/output.
package firewall

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the Firewall screen runs with: what cmd/hardn has when its
// Firewall menu item is chosen. It has Backup's, and the Facts and the Session
// the control's reading takes, as the screen plans nothing itself.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's, from which the control takes Proxmox VE, the package
	// tool and the init system; the engine establishes its own when it plans.
	Facts detect.Facts
	// Session is the session hardn runs in, as the caller reads it, for what
	// firewall.Read plans of item 1: the port Enable must keep. The engine a
	// change plans with holds its own, which should be the same.
	Session revert.Session
	// Config is hardn.yml as loaded, whose application profiles the screen shows
	// and Add and Remove save. A change plans a copy; once it has applied and
	// hardn.yml holds the bytes it saved, Config adopts the copy, so that the
	// next save is not refused as CONFIG_CHANGED. On Proxmox VE no item is
	// offered, so it plays no part. Required.
	Config *config.Config
	// NewEngine returns the engine a change plans and applies with, built as the
	// caller builds it (engine.New over System with its options: its backups, its
	// session, hardn's path for the revert timer), progress as its
	// Options.Progress; one per change, made when the change is planned. Drawing
	// makes none. Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a change's apply runs under,
	// made from the screen's, and its stop, called once the apply has returned;
	// one per apply (screenkit.Planning.ApplyContext). Nil applies under the
	// screen's context.
	ApplyContext func(ctx context.Context) (context.Context, context.CancelFunc)
	// DryRun stops each change at its plan, Add and Remove too: nothing applied
	// or saved.
	DryRun bool
	// LogPath is the log a failure's report names; "" names none.
	LogPath string
	// Now is the clock output.AfterFlow counts from; nil is time.Now.
	Now func() time.Time
	// Ran, when set, is given what each change did once its flow has ended, for
	// a change that was planned: for the audit log.
	Ran func(output.Run)
}

// Run shows the Firewall screen and runs the items the user chooses, until they
// go back with 0 (nil) or quit with q (nav.ErrQuit); the end of the input and
// Ctrl-C abort (prompt.ErrAborted). Each round reads the host with the control
// (firewall.Read) and the Firewall row (Incoming.Check), writing nothing, under
// plan only too, and draws the screen under what the last item wrote.
//
// Item 1 is by Reading.First: Install, Enable in the words of what its plan does,
// or Disable; where its plan refuses it says why before asking. Item 2, where ufw
// is installed, opens the application profiles: Add, and with any Remove and
// Apply. Item 3, beside an item 1 that is not Disable on an active ufw, is
// Disable. Each change is planned, reviewed, confirmed y/N, or by the hostname
// typed for a lockout-risk plan, and applied; n and 0 change nothing, and a dry
// run stops at the plan. A lockout-risk plan whose hostname can't be read says so
// before asking; Add and Remove with no hardn.yml read say that before asking;
// Apply says its plan's refusal before asking. On Proxmox VE, Return alone. Run
// returns an error wrapping fs.ErrInvalid, writing nothing, without a Prompter's
// In and Out, a System, a Config or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.Config == nil || o.NewEngine == nil {
		return fmt.Errorf("firewall screen: %w: it needs a Prompter with In and Out, a System, a Config and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	r := &runner{o: o, s: screenkit.Session{Name: "firewall screen", Prompter: o.Prompter, System: o.System, Config: o.Config,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: o.Ran}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the Firewall screen: its options, and the session its
// changes run with.
type runner struct {
	o Options
	s screenkit.Session
}

// reading is one reading of the host: the control's, and the view of it on ufw's
// screen, or on Proxmox VE's.
type reading struct {
	firewallctl.Reading
	view screen.Firewall
	pve  screen.FirewallPVE
}

// read reads the host as the control reads it (firewall.Read, writing nothing,
// running only reads) and the Firewall row. The row is Check's own reading, a
// second ufw status, kept so that it is the main menu's row to the letter.
func (r *runner) read(ctx context.Context) reading {
	sys, f := r.o.System, r.o.Facts
	h := reading{Reading: firewallctl.Read(ctx, sys, f, r.o.Config, r.o.Session)}
	status := statusRow(ctx, sys, f)
	if h.OnPVE {
		h.pve = pveView(h.Reading, status)
		return h
	}
	h.view = mainView(h.Reading, status, f)
	return h
}

// draw reads the host and returns the screen, its items bound to their actions.
func (r *runner) draw(ctx context.Context) nav.Screen {
	h := r.read(ctx)
	if h.OnPVE {
		sub := h.pve.SubMenu()
		return sub.Screen(r.o.Prompter.Ctx, nav.Menu{Exit: sub.Menu.Exit})
	}
	sub := h.view.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: r.action(ctx, h, it.Key)})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}
