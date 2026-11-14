// Package logforward runs hardn's Log Forwarding screen on a host
// (docs/roadmap.md, "Log forwarding"; docs/engine/gallery-logforward.md,
// adopted 2026-11-13; docs/engine/screen-logforward.md): the screen
// pkg/ui/screen draws, from what the Log Forwarding control
// (pkg/engine/controls/logforward) reads of the host, writing nothing; its
// items, Set server and Stop forwarding, the control's plans through the
// plan/apply engine in pkg/ui's canonical flow: the review with the
// control's notice, y/N, the apply, rolled back on failure, then the screen
// drawn again from a fresh reading. Every refusal is the control's or the
// engine's, said before anything is asked. Run is the one entry point,
// which cmd/hardn's main menu item 6 calls.
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
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	ctl "github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// Options is what the Log Forwarding screen runs with: what cmd/hardn has
// when its Log Forwarding menu item is chosen, as the Auto Updates
// screen's.
type Options struct {
	// Prompter asks the questions and writes the screen through the one line
	// reader, rendering with its Ctx, whose Env is the terminal's.
	Prompter nav.Prompter
	// System is the host: the screen reads it, and the engine changes it.
	System host.System
	// Facts are detect's: the distribution and the init system.
	Facts detect.Facts
	// Config is hardn.yml as loaded, whose logForwarding names the server.
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

// Run shows the Log Forwarding screen and runs its items until the user
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
		return fmt.Errorf("log forwarding screen: %w: it needs a Prompter with In and Out, a System and NewEngine", fs.ErrInvalid)
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	cfg := o.Config
	if cfg == nil {
		cfg = &config.Config{}
	}
	r := &runner{o: o, cfg: cfg, s: screenkit.Session{Name: "log forwarding screen", Prompter: o.Prompter, System: o.System, Config: cfg,
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

// change runs an item through the flow (screenkit.Session.Change), plain
// y/N (decision 11): planned first (Preplan) and refused before asking;
// its review, the section as the change leaves it and the control's
// notice, from a fresh reading; its plan; the question.
func (r *runner) change(ctx context.Context, action ctl.Action) error {
	c := ctl.Forward{Action: action, Config: r.cfg}
	switch words, err := r.s.Preplan(ctx, r.cfg, c); {
	case err != nil:
		return err
	case words != "":
		return r.s.Say(screenkit.Warnings(words))()
	}
	rd := ctl.Read(ctx, r.o.System, r.o.Facts, r.cfg)
	v := View(rd)
	ch := v.StopChange()
	if action == ctl.ActionSet {
		var ok bool
		if ch, ok = v.SetChange(); !ok {
			return nil // hardn.yml edited between the plan and here: the screen is drawn again
		}
	}
	ch.Review.Notice = &flow.Notice{Callout: component.Callout{Kind: component.MessageInfo, Body: []string{rd.Notice(action)}}}
	return r.s.Change(ctx, screenkit.Change{
		Step:     ch.Step,
		Flow:     flow.Flow{Review: func() flow.Review { return ch.Review }, Confirm: func(flow.Plan) flow.Confirm { return flow.Confirm{Question: ch.Question} }},
		Controls: func(*config.Config) []engine.Control { return []engine.Control{c} },
	})
}

// The words of what is not read, the item's description then.
const (
	unreadRsyslog = "Can't read rsyslog's settings"
	unreadBusybox = "Can't read syslogd's options"
	unreadLogger  = "Can't tell the logger"
)

// StatusRow is the Log Forwarding row as the main menu draws it: the
// control's status through the main menu's own mapping.
func StatusRow(rd ctl.Reading) component.StatusList {
	rp := engine.Report{Control: ctl.ID, Title: ctl.Title, Status: rd.Status()}
	view := output.StatusViewOf([]engine.Report{rp}, risk.Level{}).Status
	for _, row := range view.Rows {
		if row.Label == ctl.Title {
			return component.StatusList{Rows: []component.StatusRow{row}}
		}
	}
	return component.StatusList{}
}

// server is s as the screen draws it, the host's text made safe.
func server(s logfwd.Server) *screen.LogServer {
	proto := map[logfwd.Proto]screen.LogProto{logfwd.UDP: screen.LogUDP, logfwd.TCP: screen.LogTCP, logfwd.TLS: screen.LogTLS}[s.Proto]
	return &screen.LogServer{Address: logview.Safe(s.Address), Port: logview.Safe(s.Port), Proto: proto, CAFile: logview.Safe(s.CAFile)}
}

// View is rd as the screen draws it: the mapping the gallery's mocks draw
// through too. The logger; what is not read, in a few words; the server the
// host forwards to, its file and whether it is hardn's, every word of the
// host's made safe; the connection; and hardn.yml's server.
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
	if rd.Want != nil {
		v.Want = server(*rd.Want)
	}
	if rd.Now != nil {
		v.Now, v.File, v.ByHardn = server(rd.Now.Server), logview.Safe(rd.Now.File), rd.Now.ByHardn
		v.Conn = map[logfwd.Conn]screen.LogConn{logfwd.ConnOpen: screen.LogConnOpen, logfwd.ConnDown: screen.LogConnDown, logfwd.ConnUnknown: screen.LogConnUnknown}[rd.Conn]
	}
	return v
}
