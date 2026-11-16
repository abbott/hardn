// Package timesync runs hardn's NTP Settings screen on a host (docs/roadmap.md,
// "Time sync"; docs/engine/gallery-timesync.md, adopted 2026-11-14;
// docs/engine/screen-timesync.md): the screen pkg/ui/screen draws, from
// what the NTP Settings control (pkg/engine/controls/timesync) reads of the
// host, writing nothing; its items, Set servers and Remove servers, the
// control's plans through the plan/apply engine in pkg/ui's canonical flow:
// the review with the control's notice, y/N, the apply, rolled back on
// failure, then the screen drawn again from a fresh reading. Every refusal
// is the control's or the engine's, said before anything is asked. Run is
// the one entry point, which cmd/hardn calls for the Configuration
// group's item 3 (main menu 8).
//
// The change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control and the engine, and renders only through pkg/ui
// and pkg/output. Every word of the host it draws is made safe first
// (logview.Safe).
package timesync

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	ctl "github.com/abbott/hardn/pkg/engine/controls/timesync"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	tshost "github.com/abbott/hardn/pkg/host/timesync"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the NTP Settings screen runs with: what cmd/hardn has when
// its NTP Settings menu item is chosen, as the Syslog Settings screen's.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's: the distribution, the init system, a container.
	Facts detect.Facts
	// Config is hardn.yml as loaded, whose ntpServers names the servers. No
	// change saves it. Nil has none.
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

// Run shows the NTP Settings screen and runs its items until the user goes
// back with 0 (nil) or quits with q (nav.ErrQuit); the end of the input and
// Ctrl-C abort (prompt.ErrAborted). Each round reads the host with the
// control, writing nothing, and draws the row, the section and the items
// (View). An item chosen is planned by the engine first, from a fresh
// reading, and its refusal said before anything is asked; then the review
// with the control's notice, the plan, y/N, the apply. n changes nothing,
// and a dry run stops at the plan. Run returns an error wrapping
// fs.ErrInvalid, writing nothing, without a Prompter's In and Out, a System
// or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.NewEngine == nil {
		return fmt.Errorf("ntp settings screen: %w: it needs a Prompter with In and Out, a System and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	r := &runner{o: o, cfg: cfg, s: screenkit.Session{Name: "ntp settings screen", Prompter: o.Prompter, System: o.System, Config: cfg,
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
	v := View(ctl.Read(ctx, r.o.System, r.o.Facts, r.cfg))
	sub := v.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		o := nav.Option{Item: it}
		switch {
		case screen.Refreshes(it):
			o.Action = func() error { return nil } // the next round reads the host again
		case !it.Disabled:
			action := ctl.ActionSet
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
// y/N (decision 13): planned first and refused before asking; its review,
// the servers as the change leaves them and the control's notice, from a
// fresh reading; its plan; the question.
func (r *runner) change(ctx context.Context, action ctl.Action) error {
	return r.s.Reviewed(ctx, r.cfg, ctl.Sync{Action: action, Config: r.cfg}, func() (screen.Change, string, bool) {
		rd := ctl.Read(ctx, r.o.System, r.o.Facts, r.cfg)
		v := View(rd)
		if action == ctl.ActionSet {
			ch, ok := v.SetChange()
			return ch, rd.Notice(action, r.o.Facts), ok
		}
		return v.RemoveChange(), rd.Notice(action, r.o.Facts), true
	})
}

// The words of what is not read, the item's description then.
var unread = map[tshost.Service]string{
	tshost.Chrony:    "Can't read chrony's settings",
	tshost.Timesyncd: "Can't read timesyncd's settings",
	tshost.Busybox:   "Can't read ntpd's options",
	tshost.NoService: "Can't tell the time service",
}

// StatusRow is the NTP row as the main menu draws it: the control's
// status through the main menu's own mapping.
func StatusRow(rd ctl.Reading) component.StatusList {
	return screenkit.StatusRow(ctl.ID, ctl.Title, rd.Status())
}

// servers are ss as the screen draws them, the host's text made safe: a
// file by its base name.
func servers(ss []tshost.Server) []screen.TimeServer {
	from := map[tshost.From]screen.TimeFrom{tshost.FromFile: screen.TimeFromFile, tshost.FromHardn: screen.TimeFromHardn, tshost.FromOther: screen.TimeFromOther,
		tshost.FromDHCP: screen.TimeFromDHCP, tshost.FromBuiltIn: screen.TimeFromBuiltIn}
	var out []screen.TimeServer
	for _, s := range ss {
		ts := screen.TimeServer{Name: logview.Safe(s.Name), From: from[s.From]}
		if s.File != "" {
			ts.File = logview.Safe(path.Base(s.File))
		}
		out = append(out, ts)
	}
	return out
}

// View is rd as the screen draws it: the mapping the gallery's hosts are
// drawn through too. The service and whether it runs; what is not read, in
// a few words; the clock and the server it follows; each server and where
// it is set, every word of the host's made safe; and hardn.yml's servers.
func View(rd ctl.Reading) screen.TimeSync {
	h := rd.Host
	v := screen.TimeSync{Status: StatusRow(rd), Container: h.Container, Want: rd.Want, WantBad: rd.WantErr != nil,
		Stopped: h.Service != tshost.NoService && h.Running == detect.No,
		Synced:  logview.Safe(h.Synced), Servers: servers(h.Servers), File: logview.Safe(h.File), Restores: servers(h.Restores), Cleared: h.Cleared != ""}
	v.Service = map[tshost.Service]screen.TimeService{tshost.Chrony: screen.TimeChrony, tshost.Timesyncd: screen.TimeTimesyncd,
		tshost.Busybox: screen.TimeBusybox, tshost.NoService: screen.TimeNoService}[h.Service]
	v.Clock = map[tshost.Clock]screen.TimeClock{tshost.InSync: screen.TimeClockInSync, tshost.NotInSync: screen.TimeClockNotInSync}[h.Clock]
	if h.Err != nil {
		v.Unread = unread[h.Service]
	}
	return v
}
