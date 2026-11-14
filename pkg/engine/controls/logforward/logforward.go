// Package logforward holds the Log Forwarding control (docs/roadmap.md,
// "Log forwarding"; docs/engine/gallery-logforward.md, adopted 2026-11-13;
// docs/engine/screen-logforward.md): whether the host sends its logs to a
// syslog server, and the two changes that make it so or stop it.
//
// Check and Read tell the logger and where it forwards (pkg/host/logfwd),
// the connection the host's sockets show, and the server hardn.yml's
// logForwarding names. Plan, by Action, sets that server (Set: hardn's own
// file under /etc/rsyslog.d, rsyslog and its TLS driver installed where
// missing, or busybox syslogd's -R on Alpine) or stops forwarding (Stop:
// hardn's own setting only). Neither edits a file hardn did not write, and
// neither saves hardn.yml.
package logforward

import (
	"context"
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

// Title is the row's label, the screen's title and the menu item's.
const Title = "Log Forwarding"

// The row's details (gallery-logforward.md §5); a Configured row's is the
// server ("192.0.2.50:514 TCP").
const (
	DetailOff        = "not forwarding"         // NotConfigured
	DetailNotReached = "server not reached"     // Partial: TCP or TLS, no connection
	DetailNotWanted  = "not hardn.yml's server" // Partial: forwards elsewhere, or another way
	DetailCantRead   = "can't read"             // Unknown: the logger's settings
	DetailConnUnread = "connection not read"    // Unknown: the host's sockets
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

// Forward is Log Forwarding. Config is hardn.yml as loaded, whose
// logForwarding Check compares the host with; Plan reads the engine's
// (engine.Input.Config), which is the same file. The risk level never
// counts the row (decision 10): it is no row of risk.Rows.
type Forward struct {
	Action Action
	Config *config.Config
}

// Controls is the Log Forwarding row against cfg.
func Controls(cfg *config.Config) []engine.Control { return []engine.Control{Forward{Config: cfg}} }

// ID returns ID.
func (Forward) ID() string { return ID }

// Title returns Title.
func (Forward) Title() string { return Title }

// Applies is true on every host: one whose logger hardn does not read is
// Unknown, not left out.
func (Forward) Applies(detect.Facts) (bool, string) { return true, "" }

// Reading is the host read for Log Forwarding.
type Reading struct {
	// Host is the logger and where it forwards. Its text is the host's:
	// Status and the screen make it safe to show.
	Host logfwd.Reading
	// Now is the forwarding shown (Host.Now): hardn's own where there is
	// one, else the first; nil for none, or not read.
	Now *logfwd.Forward
	// Conn is what the host's sockets say of Now's server; ConnErr why
	// they are not read.
	Conn    logfwd.Conn
	ConnErr error
	// Want is the server hardn.yml names, checked; nil for none. WantErr
	// is why logForwarding is not valid (config.ErrLogForwarding): nothing
	// is set from it.
	Want    *logfwd.Server
	WantErr error
}

// wanted is hardn.yml's server as the host package writes one.
func wanted(cfg *config.Config) (*logfwd.Server, error) {
	if cfg == nil {
		return nil, nil
	}
	s, ok, err := cfg.LogForwarding.Resolve()
	if err != nil || !ok {
		return nil, err
	}
	proto := map[string]logfwd.Proto{config.LogUDP: logfwd.UDP, config.LogTCP: logfwd.TCP, config.LogTLS: logfwd.TLS}[s.Protocol]
	return &logfwd.Server{Address: s.Address, Port: strconv.Itoa(s.Port), Proto: proto, CAFile: s.CAFile}, nil
}

// Read reads the host, writing nothing: the logger and its forwarding, the
// connection to the server it forwards to, and cfg's server.
func Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config) Reading {
	r := Reading{Host: logfwd.Read(ctx, sys, f)}
	r.Want, r.WantErr = wanted(cfg)
	if r.Host.Err != nil {
		return r
	}
	if r.Now = r.Host.Now(); r.Now != nil {
		r.Conn, r.ConnErr = logfwd.ReadConn(ctx, sys, r.Now.Server)
	}
	return r
}

// Differs reports whether hardn.yml names a server and the host forwards to
// another, or another way.
func (r Reading) Differs() bool {
	return r.Want != nil && r.Now != nil && *r.Want != r.Now.Server
}

// Shown reports whether the main menu draws the row whose check gave st
// (decision 10 (a)): where cfg, hardn.yml, names a server, or the host
// forwards. A host that forwards nowhere and is asked nothing has no row,
// read or not.
func Shown(cfg *config.Config, st engine.Status) bool {
	if cfg != nil && cfg.LogForwarding.Server != "" {
		return true
	}
	return st.State == engine.Configured || st.State == engine.Partial || st.Detail == DetailConnUnread
}

// ServerWords is s as a row says it, safe to show: "192.0.2.50:514 TCP".
func ServerWords(s logfwd.Server) string {
	return logview.Safe(s.Address + ":" + s.Port + " " + strings.ToUpper(s.Proto.String()))
}

// Status is the row: Unknown where the logger's settings or the sockets
// are not read, never a pass; Not Configured with nothing forwarded;
// Partial where the host forwards elsewhere than hardn.yml says, or no
// connection to the server is open; else Configured, with the server.
func (r Reading) Status() engine.Status {
	switch {
	case r.Host.Err != nil:
		return engine.Status{State: engine.Unknown, Detail: DetailCantRead, Evidence: []string{logview.Safe(r.Host.Err.Error())}}
	case r.Now == nil:
		return engine.Status{State: engine.NotConfigured, Detail: DetailOff}
	}
	ev := []string{logview.Safe(r.Now.File) + ":" + strconv.Itoa(r.Now.Line)}
	switch {
	case r.Differs():
		return engine.Status{State: engine.Partial, Detail: DetailNotWanted, Evidence: ev}
	case r.Conn == logfwd.ConnDown:
		return engine.Status{State: engine.Partial, Detail: DetailNotReached, Evidence: ev}
	case r.Conn == logfwd.ConnUnknown:
		if r.ConnErr != nil {
			ev = append(ev, logview.Safe(r.ConnErr.Error()))
		}
		return engine.Status{State: engine.Unknown, Detail: DetailConnUnread, Evidence: ev}
	}
	return engine.Status{State: engine.Configured, Detail: ServerWords(r.Now.Server), Evidence: ev}
}

// Check is the row (Reading.Status). What is not read is Unknown with no
// error: nothing failed that the row does not say.
func (c Forward) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	return Read(ctx, sys, f, c.Config).Status(), ctx.Err()
}
