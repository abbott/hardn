// Package logforward holds the Syslog Settings control (docs/roadmap.md,
// "Log forwarding"; docs/engine/gallery-logforward.md, adopted 2026-11-13;
// docs/engine/screen-logforward.md): whether the host sends its logs to a
// syslog server, and the two changes that make it so or stop it.
//
// Check and Read tell the logger and where it forwards (pkg/host/logfwd),
// the connections the host's sockets show, and the servers hardn.yml's
// syslogServers names (docs/engine/design-syslog-servers.md, adopted
// 2026-11-14). Plan, by Action, sets those servers (Set: hardn's own file
// under /etc/rsyslog.d, rsyslog and its TLS driver installed where missing,
// or busybox syslogd's -R on Alpine) or stops forwarding (Stop: hardn's own
// setting only). Neither edits a file hardn did not write, and neither
// saves hardn.yml.
package logforward

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/logfwd"
)

// ID is the control's ID, stable once released.
const ID = "logs.forward"

// Title is the row's label. The screen's title and the menu item are
// "Syslog Settings" (pkg/ui/screen).
const Title = "Syslog"

// The row's details (design-syslog-servers.md §2.4); a Configured row's is
// the server ("192.0.2.50:514 TCP"), or how many ("3 servers").
const (
	DetailOff        = "not forwarding"          // NotConfigured
	DetailNotReached = "server not reached"      // Partial: the one server, TCP or TLS, no connection
	DetailNotWanted  = "not hardn.yml's servers" // Partial: forwards elsewhere, or another way
	DetailCantRead   = "can't read"              // Unknown: the logger's settings
	DetailConnUnread = "connection not read"     // Unknown: the host's sockets

	detailServers  = "%d servers"           // Configured: several
	detailSomeDown = "%d of %d not reached" // Partial: several, some with no connection
)

// Action is what Forward's Plan does.
type Action uint8

// The actions.
const (
	// ActionNone is the status row's: Plan is engine.ErrNotMigrated.
	ActionNone Action = iota
	// ActionSet makes the host forward to hardn.yml's server.
	ActionSet
	// ActionStop takes hardn's own forwarding out.
	ActionStop
)

// Forward is Syslog Settings. Config is hardn.yml as loaded, whose
// syslogServers Check compares the host with; Plan reads the engine's
// (engine.Input.Config), which is the same file. The risk level never
// counts the row (decision 10): it is no row of risk.Rows.
type Forward struct {
	Action Action
	Config *config.Config
}

// Controls is the Syslog row against cfg.
func Controls(cfg *config.Config) []engine.Control { return []engine.Control{Forward{Config: cfg}} }

// ID returns ID.
func (Forward) ID() string { return ID }

// Title returns Title.
func (Forward) Title() string { return Title }

// Applies is true on every host: one whose logger hardn does not read is
// Unknown, not left out.
func (Forward) Applies(detect.Facts) (bool, string) { return true, "" }

// Reading is the host read for Syslog Settings.
type Reading struct {
	// Host is the logger and where it forwards. Its text is the host's:
	// Status and the screen make it safe to show.
	Host logfwd.Reading
	// Now are the forwardings shown (Host.Shown): hardn's own where there
	// are any, else every one; nil for none, or not read.
	Now []logfwd.Forward
	// Conns are what the host's sockets say of Now's servers, one for
	// each; ConnErr why they are not read. With no ConnErr, ConnUnknown is
	// sockets that do not tell (logfwd.ReadConns).
	Conns   []logfwd.ConnState
	ConnErr error
	// Want are the servers hardn.yml names, checked, in its order; nil for
	// none. WantErr is why syslogServers is not valid
	// (config.ErrSyslogServers): nothing is set from it.
	Want    []logfwd.Server
	WantErr error
}

// wanted are hardn.yml's servers as the host package writes them.
func wanted(cfg *config.Config) ([]logfwd.Server, error) {
	if cfg == nil {
		return nil, nil
	}
	servers, err := cfg.SyslogServers.Resolve()
	if err != nil {
		return nil, err
	}
	var out []logfwd.Server
	for _, s := range servers {
		proto := map[string]logfwd.Proto{config.LogUDP: logfwd.UDP, config.LogTCP: logfwd.TCP, config.LogTLS: logfwd.TLS}[s.Protocol]
		out = append(out, logfwd.Server{Address: s.Address, Port: strconv.Itoa(s.Port), Proto: proto, CAFile: s.CAFile})
	}
	return out, nil
}

// Read reads the host, writing nothing: the logger and its forwarding, the
// connection to each server it forwards to, and cfg's servers.
func Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config) Reading {
	r := Reading{Host: logfwd.Read(ctx, sys, f)}
	r.Want, r.WantErr = wanted(cfg)
	if r.Host.Err != nil {
		return r
	}
	if r.Now = r.Host.Shown(); len(r.Now) > 0 {
		var beside []logfwd.Server
		for _, f := range r.Beside() {
			beside = append(beside, f.Server)
		}
		r.Conns, r.ConnErr = logfwd.ReadConns(ctx, sys, r.Servers(), beside)
	}
	return r
}

// Servers are Now's servers, in order.
func (r Reading) Servers() []logfwd.Server {
	var out []logfwd.Server
	for _, f := range r.Now {
		out = append(out, f.Server)
	}
	return out
}

// Beside are the forwardings set outside hardn beside hardn's own, which
// Now leaves out; nil for none.
func (r Reading) Beside() []logfwd.Forward {
	if len(r.Now) == 0 || len(r.Now) == len(r.Host.Forwards) {
		return nil
	}
	var out []logfwd.Forward
	for _, f := range r.Host.Forwards {
		if !f.ByHardn {
			out = append(out, f)
		}
	}
	return out
}

// sameSet reports whether a and b hold the same servers, whatever their
// order: every message goes to each, so the order changes nothing.
func sameSet(a, b []logfwd.Server) bool {
	if len(a) != len(b) {
		return false
	}
	for _, s := range a {
		if !slices.Contains(b, s) {
			return false
		}
	}
	for _, s := range b {
		if !slices.Contains(a, s) {
			return false
		}
	}
	return true
}

// AsAsked reports whether the host forwards to exactly hardn.yml's
// servers, by whoever, in any order. Not with a forwarding set outside
// hardn beside hardn's own (Beside): the host then forwards to more than
// hardn's servers.
func (r Reading) AsAsked() bool {
	return len(r.Want) > 0 && len(r.Beside()) == 0 && sameSet(r.Want, r.Servers())
}

// Differs reports whether hardn.yml names servers and the host forwards to
// others, to more, or another way.
func (r Reading) Differs() bool {
	return len(r.Want) > 0 && len(r.Now) > 0 && !r.AsAsked()
}

// Shown reports whether the main menu draws the row whose check gave st
// (decision 10 (a)): where cfg, hardn.yml, names a server, or the host
// forwards. A host that forwards nowhere and is asked nothing has no row,
// read or not.
func Shown(cfg *config.Config, st engine.Status) bool {
	if cfg != nil && len(cfg.SyslogServers) > 0 {
		return true
	}
	return st.State == engine.Configured || st.State == engine.Partial || st.Detail == DetailConnUnread
}

// ServerWords is s as a row says it, safe to show: "192.0.2.50:514 TCP".
func ServerWords(s logfwd.Server) string {
	return logview.Safe(s.Address + ":" + s.Port + " " + strings.ToUpper(s.Proto.String()))
}

// conn is what the sockets say of Now's server i; ConnNone where they were
// not asked.
func (r Reading) conn(i int) logfwd.ConnState {
	if i < len(r.Conns) {
		return r.Conns[i]
	}
	return logfwd.ConnState{}
}

// Down is how many of Now's servers no connection reaches. Of several
// servers given by name on one port, some reached, it is those that are
// not: how many is known, which is not.
func (r Reading) Down() int {
	n := 0
	counted := map[string]bool{} // a port whose named servers are counted
	for i, f := range r.Now {
		switch c := r.conn(i); {
		case c.Conn != logfwd.ConnDown:
		case !c.Some():
			n++
		case !counted[f.Server.Port]:
			counted[f.Server.Port] = true
			n += c.Of - c.Open
		}
	}
	return n
}

// Status is the row (design-syslog-servers.md §2.4): Unknown where the
// logger's settings or the sockets are not read, never a pass; Not
// Configured with nothing forwarded; Partial where the host forwards
// elsewhere than hardn.yml says, or a server has no connection, saying how
// many of several; else Configured, with the server or how many. Its
// evidence is one line per server, those set outside hardn beside hardn's
// own last: its file and line, the server, and what is known of a
// connection that is not open.
func (r Reading) Status() engine.Status {
	switch {
	case r.Host.Err != nil:
		return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: []string{logview.Safe(r.Host.Err.Error())}}
	case len(r.Now) == 0:
		return engine.Status{State: engine.NotConfigured, Detail: DetailOff}
	}
	var ev []string
	unread := false
	for i, f := range r.Now {
		line := logview.Safe(f.File) + ":" + strconv.Itoa(f.Line) + " " + ServerWords(f.Server)
		switch c := r.conn(i); {
		case c.Conn == logfwd.ConnUnknown:
			unread = true
		case c.Some():
			line += fmt.Sprintf(", %d of %d open", c.Open, c.Of)
		case c.Conn == logfwd.ConnDown:
			line += ", not reached"
		}
		ev = append(ev, line)
	}
	for _, f := range r.Beside() {
		ev = append(ev, logview.Safe(f.File)+":"+strconv.Itoa(f.Line)+" "+ServerWords(f.Server))
	}
	down := r.Down()
	switch {
	case r.Differs():
		return engine.Status{State: engine.Partial, Detail: DetailNotWanted, Evidence: ev}
	case unread:
		if r.ConnErr != nil {
			ev = append(ev, logview.Safe(r.ConnErr.Error()))
		}
		return engine.Status{State: engine.Unknown, Detail: DetailConnUnread, Evidence: ev}
	case down > 0 && len(r.Now) == 1:
		return engine.Status{State: engine.Partial, Detail: DetailNotReached, Evidence: ev}
	case down > 0:
		return engine.Status{State: engine.Partial, Detail: fmt.Sprintf(detailSomeDown, down, len(r.Now)), Evidence: ev}
	case len(r.Now) == 1:
		return engine.Status{State: engine.Configured, Detail: ServerWords(r.Now[0].Server), Evidence: ev}
	}
	return engine.Status{State: engine.Configured, Detail: fmt.Sprintf(detailServers, len(r.Now)), Evidence: ev}
}

// Check is the row (Reading.Status). What is not read is Unknown with no
// error: nothing failed that the row does not say.
func (c Forward) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	return Read(ctx, sys, f, c.Config).Status(), ctx.Err()
}
