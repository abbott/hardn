// Package logforward runs hardn's Syslog Settings screen on a host
// (docs/roadmap.md, "Log forwarding"; docs/engine/gallery-logforward.md,
// adopted 2026-11-13; docs/engine/screen-logforward.md): the screen
// pkg/ui/screen draws, from what the Syslog Settings control
// (pkg/engine/controls/logforward) reads of the host, writing nothing; its
// items, Set servers and Stop forwarding, the control's plans through the
// plan/apply engine in pkg/ui's canonical flow: the review with the
// control's notice, y/N, the apply, rolled back on failure, then the screen
// drawn again from a fresh reading. Every refusal is the control's or the
// engine's, said before anything is asked. Run is the one entry point,
// which cmd/hardn calls for the Configuration group's item 4 (main menu 8).
//
// The change runs through the glue every area's screen shares
// (pkg/screens/screenkit). The package reaches the host only through
// host.System, the control and the engine, and renders only through pkg/ui
// and pkg/output. Every word of the host it draws is made safe first
// (logview.Safe).
package logforward

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	ctl "github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the Syslog Settings screen runs with: what cmd/hardn has
// when its Syslog Settings menu item is chosen, as the Auto Updates
// screen's.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's: the distribution and the init system.
	Facts detect.Facts
	// Config is hardn.yml as loaded, whose syslogServers names the servers.
	// No change saves it. Nil has none.
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

// Run shows the Syslog Settings screen and runs its items until the user
// goes back with 0 (nil) or quits with q (nav.ErrQuit); the end of the
// input and Ctrl-C abort (prompt.ErrAborted). Each round reads the host
// with the control, writing nothing, and draws the row, the section and
// the items (View). An item chosen is planned by the engine first, from a
// fresh reading, and its refusal said before anything is asked; then the
// review with the control's notice, the plan, y/N, the apply. n changes
// nothing, and a dry run stops at the plan. Run returns an error wrapping
// fs.ErrInvalid, writing nothing, without a Prompter's In and Out, a System
// or NewEngine.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil || o.NewEngine == nil {
		return fmt.Errorf("syslog settings screen: %w: it needs a Prompter with In and Out, a System and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	r := &runner{o: o, cfg: cfg, s: screenkit.Session{Name: "syslog settings screen", Prompter: o.Prompter, System: o.System, Config: cfg,
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
	rd := ctl.Read(ctx, r.o.System, r.o.Facts, r.cfg)
	v := View(rd)
	sub := v.SubMenu()
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		o := nav.Option{Item: it}
		if !it.Disabled {
			action := ctl.ActionSet
			if v.Stops(it) {
				action = ctl.ActionStop
			}
			o.Action = func() error { return r.change(ctx, action) }
		}
		menu.Options = append(menu.Options, o)
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// change runs an item through the flow (screenkit.Session.Reviewed), plain
// y/N (decision 11): planned first and refused before asking; its review,
// the section as the change leaves it and the control's notice, from a
// fresh reading; its plan; the question.
func (r *runner) change(ctx context.Context, action ctl.Action) error {
	return r.s.Reviewed(ctx, r.cfg, ctl.Forward{Action: action, Config: r.cfg}, func() (screen.Change, string, bool) {
		rd := ctl.Read(ctx, r.o.System, r.o.Facts, r.cfg)
		v := View(rd)
		if action == ctl.ActionSet {
			ch, ok := v.SetChange()
			return ch, rd.Notice(action), ok
		}
		return v.StopChange(), rd.Notice(action), true
	})
}

// The words of what is not read, the item's description then.
const (
	unreadRsyslog = "Can't read rsyslog's settings"
	unreadBusybox = "Can't read syslogd's options"
	unreadLogger  = "Can't tell the logger"
)

// StatusRow is the Syslog row as the main menu draws it: the
// control's status through the main menu's own mapping.
func StatusRow(rd ctl.Reading) component.StatusList {
	return screenkit.StatusRow(ctl.ID, ctl.Title, rd.Status())
}

// server is s as the screen draws it, the host's text made safe.
func server(s logfwd.Server) screen.LogServer {
	proto := map[logfwd.Proto]screen.LogProto{logfwd.UDP: screen.LogUDP, logfwd.TCP: screen.LogTCP, logfwd.TLS: screen.LogTLS}[s.Proto]
	return screen.LogServer{Address: logview.Safe(s.Address), Port: logview.Safe(s.Port), Proto: proto, CAFile: logview.Safe(s.CAFile)}
}

// View is rd as the screen draws it: the mapping the gallery's mocks draw
// through too. The logger; what is not read, in a few words; each server
// the host forwards to with its connection, their file and whether they
// are hardn's, every word of the host's made safe; whether another
// setting forwards beside hardn's; and hardn.yml's servers.
func View(rd ctl.Reading) screen.LogForward {
	v := screen.LogForward{Status: StatusRow(rd), WantBad: rd.WantErr != nil}
	switch rd.Host.Logger {
	case logfwd.Rsyslog:
		v.Logger = screen.LogRsyslog
	case logfwd.Busybox:
		v.Logger = screen.LogBusybox
	case logfwd.Journald:
		v.Logger, v.NoInstall = screen.LogJournald, rd.Host.NoInstall
	default:
		v.Logger = screen.LogNoLogger
	}
	if rd.Host.Err != nil {
		v.Unread = map[logfwd.Logger]string{logfwd.Rsyslog: unreadRsyslog, logfwd.Busybox: unreadBusybox}[rd.Host.Logger]
		if v.Unread == "" {
			v.Unread = unreadLogger
		}
	}
	for _, s := range rd.Want {
		v.Want = append(v.Want, server(s))
	}
	var files []string
	for i, f := range rd.Now {
		n := screen.LogForwarded{LogServer: server(f.Server)}
		if i < len(rd.Conns) {
			c := rd.Conns[i]
			n.Conn = map[logfwd.Conn]screen.LogConn{logfwd.ConnOpen: screen.LogConnOpen, logfwd.ConnDown: screen.LogConnDown, logfwd.ConnUnknown: screen.LogConnUnknown}[c.Conn]
			n.Open, n.Of = c.Open, c.Of
		}
		v.Now = append(v.Now, n)
		if !slices.Contains(files, f.File) {
			files = append(files, f.File)
		}
	}
	if len(rd.Now) > 0 {
		v.File, v.Files, v.ByHardn = logview.Safe(files[0]), len(files), rd.Now[0].ByHardn
		v.Beside = len(rd.Beside()) > 0
	}
	return v
}
