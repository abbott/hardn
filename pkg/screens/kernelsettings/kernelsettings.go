// Package kernelsettings runs hardn's Kernel Settings screen on a host
// (docs/roadmap.md, "sysctl"; docs/engine/gallery-sysctl.md, adopted
// 2026-11-14, built in its §9): the screen pkg/ui/screen
// draws, from what the Kernel Settings control
// (pkg/engine/controls/kernelsettings) reads of the host, writing nothing;
// its items, Apply settings and Remove settings, the control's plans
// through the plan/apply engine in pkg/ui's canonical flow: the review with
// the control's notice, y/N, the apply, rolled back on failure, then the
// screen drawn again from a fresh reading. Every refusal is the control's
// or the engine's, said before anything is asked. Run is the one entry
// point, which cmd/hardn calls for main menu 4, Kernel.
//
// The change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control and the engine, and renders only through pkg/ui
// and pkg/output. Every word of the host it draws is made safe first
// (logview.Safe).
package kernelsettings

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	ctl "github.com/abbott/hardn/pkg/engine/controls/kernelsettings"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	khost "github.com/abbott/hardn/pkg/host/sysctl"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the Kernel Settings screen runs with: what cmd/hardn has
// when its menu item is chosen, as the NTP Settings screen's.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's: the init system, a container, Proxmox VE.
	Facts detect.Facts
	// Config is hardn.yml as loaded, for the engine. No change reads a key
	// of it or saves it. Nil has none.
	Config *config.Config
	// NewEngine returns the engine an item plans and applies with, progress
	// as its Options.Progress. Required.
	NewEngine func(progress func(engine.Event)) *engine.Engine
	// ApplyContext, when set, returns the context a change's apply runs
	// under (screenkit.Planning.ApplyContext).
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
}

// Run shows the Kernel Settings screen and runs its items until the user
// goes back with 0 (nil) or quits with q (nav.ErrQuit); the end of the
// input and Ctrl-C abort (prompt.ErrAborted). Each round reads the host
// with the control, writing nothing, and draws the row, the sections and
// the items (View). An item chosen is planned by the engine first, from a
// fresh reading, and its refusal said before anything is asked; then the
// review with the control's notice, the plan, y/N, the apply. n changes
// nothing, and a dry run stops at the plan. Run returns an error wrapping
// fs.ErrInvalid, writing nothing, without a Prompter's In and Out, a System
// or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.NewEngine == nil {
		return fmt.Errorf("kernel settings screen: %w: it needs a Prompter with In and Out, a System and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	r := &runner{o: o, cfg: cfg, s: screenkit.Session{Name: "kernel settings screen", Prompter: o.Prompter, System: o.System, Config: cfg,
		NewEngine: o.NewEngine, ApplyContext: o.ApplyContext, DryRun: o.DryRun, LogPath: o.LogPath, Now: o.Now, Ran: o.Ran}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the screen.
type runner struct {
	o   Options
	cfg *config.Config
	s   screenkit.Session
}

// draw reads the host and returns the screen, its items bound: an
// unavailable item has no action, as nav.Menu takes it.
func (r *runner) draw(ctx context.Context) nav.Screen {
	v := View(ctl.Read(ctx, r.o.System, r.o.Facts))
	sub := v.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		o := nav.Option{Item: it}
		if !it.Disabled {
			action := ctl.ActionApply
			if v.Removes(it) {
				action = ctl.ActionRemove
			}
			o.Action = func() error { return r.change(ctx, action) }
		}
		menu.Options = append(menu.Options, o)
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// change runs an item through the flow (screenkit.Session.Reviewed), plain
// y/N (decision 15): planned first and refused before asking; its review,
// the rows as the change leaves them and the control's notice, from a fresh
// reading; its plan; the question.
func (r *runner) change(ctx context.Context, action ctl.Action) error {
	return r.s.Reviewed(ctx, r.cfg, ctl.Settings{Action: action}, func() (screen.Change, string, bool) {
		rd := ctl.Read(ctx, r.o.System, r.o.Facts)
		v := View(rd)
		if action == ctl.ActionApply {
			ch, ok := v.ApplyChange()
			return ch, ctl.Notice(rd, action), ok
		}
		return v.RemoveChange(), ctl.Notice(rd, action), true
	})
}

// unread is the item's description where the settings are not read.
const unread = "Can't read the settings"

// StatusRow is the Kernel row as the main menu draws it: the
// control's status through the main menu's own mapping.
func StatusRow(rd khost.Reading) component.StatusList {
	return screenkit.StatusRow(ctl.ID, ctl.Title, ctl.Status(rd))
}

// states are the reader's verdicts as the screen's.
var states = map[khost.State]screen.KernelState{khost.Pass: screen.KernelPass, khost.Below: screen.KernelBelow, khost.Held: screen.KernelHeld,
	khost.LeftAlone: screen.KernelLeftAlone, khost.NoVerdict: screen.KernelNoVerdict, khost.Absent: screen.KernelAbsent, khost.Unread: screen.KernelUnread}

// row is rw as the screen draws it.
func row(rw khost.Row) screen.KernelRow {
	s := rw.Spec
	out := screen.KernelRow{Section: int(s.Section), Label: s.Name, Value: rw.Shown, Words: s.Words[rw.Shown], State: states[rw.State],
		Sets: s.Want, SetsWords: s.Words[s.Want], Routing: s.Kind == khost.Forwarding && rw.Shown == 1}
	if rw.By != "" {
		out.By = logview.Safe(path.Base(rw.By))
	}
	return out
}

// restores are the rows Remove settings sets back: each setting hardn's
// file recorded a value of, at the weakest it recorded.
func restores(rd khost.Reading) []screen.KernelRow {
	if rd.Own == nil {
		return nil
	}
	back := rd.Own.Restore()
	var out []screen.KernelRow
	for _, rw := range rd.Rows {
		s, found, n := rw.Spec, false, 0
		for _, v := range rw.Values {
			for _, kv := range back {
				if kv.Key == v.Key && (!found || !s.Passes(kv.Key, kv.N)) {
					found, n = true, kv.N
				}
			}
		}
		if found {
			out = append(out, screen.KernelRow{Section: int(s.Section), Label: s.Name, Value: n, Words: s.Words[n]})
		}
	}
	return out
}

// View is rd as the screen draws it: the mapping the gallery's hosts are
// drawn through too. Each row with its weakest value and what it means, the
// file that holds a key by its base name, made safe; hardn's file; and
// whether anything applies it at boot.
func View(rd khost.Reading) screen.KernelSettings {
	v := screen.KernelSettings{Status: StatusRow(rd), Container: rd.Container, Proxmox: rd.Proxmox && !rd.Routes,
		NotAtBoot: ctl.NotAtBoot(rd), BootFix: ctl.BootFix, Restores: restores(rd)}
	for _, rw := range rd.Rows {
		v.Rows = append(v.Rows, row(rw))
	}
	if rd.Own != nil {
		v.File = khost.File
	}
	if _, bad := rd.Unread(); bad || rd.Err != nil {
		v.Unread = unread
	}
	return v
}
