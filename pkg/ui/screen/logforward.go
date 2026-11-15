package screen

import (
	"fmt"
	"slices"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Syslog Settings screen (docs/roadmap.md, "Log forwarding";
// docs/engine/gallery-logforward.md, adopted 2026-11-13; several servers,
// docs/engine/design-syslog-servers.md, adopted 2026-11-14): whether the
// host sends its logs to syslog servers, where, and by which file; the
// servers hardn.yml names; and one or two items named by their result. On
// the sub-menu layout: there is no Figma frame. pkg/ui cannot reach the
// host: pkg/screens/logforward reads the logger and fills LogForward
// (docs/engine/screen-logforward.md).

// The screen's words: decided (gallery-logforward.md, "Words";
// design-syslog-servers.md §2.4).
const (
	logFwdTitle   = "Syslog Settings"
	logFwdSection = "Forwarding"

	logFwdLoggerKey = "Logger"
	logFwdServerKey = "Server"
	logFwdCAKey     = "CA File"
	logFwdFileKey   = "File"
	logFwdByKey     = "Set by"

	logFwdRsyslog  = "rsyslog"
	logFwdBusybox  = "busybox syslogd"
	logFwdJournald = "journald alone"
	logFwdNoLogger = "no syslog forwarder"

	logFwdNone       = "None"
	logFwdNoWant     = "No server"
	logFwdLossy      = "losses unseen"
	logFwdOpen       = "Open"
	logFwdDown       = "unreachable"
	logFwdSomeOpen   = "%d of %d open"
	logFwdServers    = "%d servers"
	logFwdFiles      = "%d files"
	logFwdHardn      = "hardn"
	logFwdNotHardn   = "Not hardn"
	logFwdSet        = "Set servers"
	logFwdStop       = "Stop forwarding"
	logFwdDescStop   = "Undo hardn's change"
	logFwdDescNoWant = "No server in hardn.yml"
	logFwdDescOther  = "Set outside hardn"
	logFwdDescInst   = "Installs rsyslog"
	logFwdDescNoInst = "hardn can't install rsyslog here"
	logFwdDescUDP    = "busybox syslogd is UDP only"
	logFwdDescBad    = "hardn.yml is not valid"

	logFwdAskSet     = "Forward logs to %s?"
	logFwdAskSetMany = "Forward logs to %d servers?"
	logFwdAskStop    = "Stop forwarding logs?"
)

// The items' keys: Set servers is 1 wherever it shows, and Stop forwarding is
// 1 alone and 2 beside it.
const (
	LogForwardFirstKey  = setUndoFirstKey
	LogForwardSecondKey = setUndoSecondKey
)

// LogLogger is the host's syslog daemon.
type LogLogger uint8

const (
	// LogRsyslog: rsyslog, which forwards over UDP, TCP or TLS by a file
	// under /etc/rsyslog.d.
	LogRsyslog LogLogger = iota
	// LogBusybox: busybox syslogd, Alpine's, which forwards over UDP alone
	// by its -R option.
	LogBusybox
	// LogJournald: systemd-journald with no syslog daemon, which forwards
	// to no syslog server.
	LogJournald
	// LogNoLogger: the host's logger is not established.
	LogNoLogger
)

// LogProto is how the logs are sent.
type LogProto uint8

const (
	// LogTCP: plain TCP.
	LogTCP LogProto = iota
	// LogUDP: UDP, which notices no lost message.
	LogUDP
	// LogTLS: TCP with TLS.
	LogTLS
)

// logProtoWords are the protocols' names.
var logProtoWords = map[LogProto]string{LogTCP: "TCP", LogUDP: "UDP", LogTLS: "TLS"}

// LogServer is a syslog server: one the host forwards to, or one hardn.yml
// names.
type LogServer struct {
	// Address is its address or name; Port its port, "514".
	Address, Port string
	// Proto is how the logs reach it.
	Proto LogProto
	// CAFile is the file TLS checks the server's certificate against; ""
	// for none, and for the other protocols.
	CAFile string
}

// endpoint is the server as a row writes it: "192.0.2.50:514".
func (s LogServer) endpoint() string { return s.Address + ":" + s.Port }

// String is the server and its protocol: "192.0.2.50:514 TCP".
func (s LogServer) String() string { return s.endpoint() + " " + logProtoWords[s.Proto] }

// LogConn is what the host's sockets say of a server.
type LogConn uint8

const (
	// LogConnNone: nothing to say, as over UDP, which has no connection.
	LogConnNone LogConn = iota
	// LogConnOpen: a connection to the server is open.
	LogConnOpen
	// LogConnDown: forwarding is set over TCP or TLS and no connection to
	// the server is open.
	LogConnDown
	// LogConnUnknown: the host's sockets are not read.
	LogConnUnknown
)

// LogForwarded is a server the host forwards to, and what its sockets say.
type LogForwarded struct {
	LogServer
	Conn LogConn
	// Open and Of, with LogConnDown: Open of the Of servers given by name
	// on this port are reached, and which is not known. Both 0 otherwise.
	Open, Of int
}

// LogForward is the Syslog Settings screen's reading of the host, which its
// caller fills.
type LogForward struct {
	// Status is the Syslog row.
	Status component.StatusList
	// Logger is the host's syslog daemon.
	Logger LogLogger
	// NoInstall: with LogJournald, hardn installs no package on this
	// distribution, so Set servers cannot install rsyslog.
	NoInstall bool
	// Unread is why the logger's settings cannot be read, in a few words,
	// such as "Can't read rsyslog's settings"; "" where they are read.
	Unread string
	// Now are the servers the host forwards to, in the order its settings
	// name them; nil for none.
	Now []LogForwarded
	// File is the file that sets Now, such as
	// "/etc/rsyslog.d/60-hardn-forward.conf"; Files how many set it, where
	// more than one does.
	File  string
	Files int
	// ByHardn: Now's setting is hardn's own.
	ByHardn bool
	// Beside: with ByHardn, the host also forwards by a setting that is not
	// hardn's, to servers Now leaves out: more than hardn.yml's.
	Beside bool
	// Want are the servers hardn.yml names; nil for none.
	Want []LogServer
	// WantBad: hardn.yml's syslogServers is not valid, so Want is nil and
	// nothing is set from it.
	WantBad bool
}

// asAsked reports whether the host forwards to exactly hardn.yml's
// servers, in any order.
func (l LogForward) asAsked() bool {
	if len(l.Want) != len(l.Now) {
		return false
	}
	now := make([]LogServer, len(l.Now))
	for i, n := range l.Now {
		now[i] = n.LogServer
	}
	for _, w := range l.Want {
		if !slices.Contains(now, w) {
			return false
		}
	}
	for _, n := range now {
		if !slices.Contains(l.Want, n) {
			return false
		}
	}
	return true
}

// differs reports whether hardn.yml names servers and the host forwards to
// others, to more (Beside), or another way.
func (l LogForward) differs() bool {
	return len(l.Want) > 0 && len(l.Now) > 0 && (l.Beside || !l.asAsked())
}

// logFwdStackRoom is the room a stacked row's value has at the content
// width: 58 cells.
const logFwdStackRoom = theme.ContentWidth - theme.StackCol

// logFwdEllipsis is the room kept for the ellipsis, which is "..." without
// Unicode.
const logFwdEllipsis = 3

// serverRow is a Server row: the server and its protocol, then note, what
// is known of it, in state. A row with no room beside its key stacks
// (KeyValue.StackLong); a name too long even there is cut, ending in the
// ellipsis, before its port.
func serverRow(s LogServer, note string, state component.ValueState) component.KeyValue {
	tail := ":" + s.Port + " " + logProtoWords[s.Proto]
	room := logFwdStackRoom - utf8.RuneCountInString(tail)
	if note != "" {
		room -= 1 + utf8.RuneCountInString(note)
	}
	addr := s.Address
	if r := []rune(addr); len(r) > room {
		addr = string(r[:max(0, room-logFwdEllipsis)]) + "…"
	}
	return component.KeyValue{Key: logFwdServerKey, Value: addr + tail, Note: note, State: state, KeepNote: note != "", StackLong: true}
}

// nowRow is the Server row of a server the host forwards to, ending in its
// connection: Open is good; unreachable, or some of a port's named servers
// open, a warning; UDP, which has no connection, warns that losses are
// not seen; sockets not read is Unknown.
func nowRow(n LogForwarded) component.KeyValue {
	switch {
	case n.Proto == LogUDP:
		return serverRow(n.LogServer, logFwdLossy, component.ValueWarn)
	case n.Conn == LogConnOpen:
		return serverRow(n.LogServer, logFwdOpen, component.ValueOK)
	case n.Conn == LogConnDown && n.Open > 0:
		return serverRow(n.LogServer, fmt.Sprintf(logFwdSomeOpen, n.Open, n.Of), component.ValueWarn)
	case n.Conn == LogConnDown:
		return serverRow(n.LogServer, logFwdDown, component.ValueWarn)
	case n.Conn == LogConnUnknown:
		return serverRow(n.LogServer, "", component.ValueUnknown)
	}
	return serverRow(n.LogServer, "", component.ValuePlain)
}

// wantRows are the review's rows of hardn.yml's servers, as Set servers
// leaves them: one Server row each, UDP's saying its losses are not seen,
// and a TLS server's CA File row after it.
func wantRows(servers []LogServer) []component.KeyValue {
	var rows []component.KeyValue
	for _, s := range servers {
		note := ""
		if s.Proto == LogUDP {
			note = logFwdLossy
		}
		rows = append(rows, serverRow(s, note, component.ValuePlain))
		if s.CAFile != "" {
			rows = append(rows, component.KeyValue{Key: logFwdCAKey, Value: s.CAFile})
		}
	}
	return rows
}

// wantWords is hardn.yml's servers in a few words: the server with one,
// "3 servers" with several; "" for none.
func wantWords(servers []LogServer) string {
	switch len(servers) {
	case 0:
		return ""
	case 1:
		return servers[0].String()
	}
	return fmt.Sprintf(logFwdServers, len(servers))
}

// Rows are the Forwarding section's rows: the logger; a Server row for each
// server the logs go to, a TLS server's CA File row after it; the file
// that says so and whether it is hardn's; and the servers hardn.yml names,
// a warning where the host forwards to others.
func (l LogForward) Rows() []component.KeyValue {
	logger := component.KeyValue{Key: logFwdLoggerKey, Value: logFwdRsyslog}
	switch l.Logger {
	case LogBusybox:
		logger.Value = logFwdBusybox
	case LogJournald:
		logger = component.KeyValue{Key: logFwdLoggerKey, Value: logFwdJournald, Note: logFwdNoLogger, State: component.ValueWarn, KeepNote: true}
	case LogNoLogger:
		logger = component.KeyValue{Key: logFwdLoggerKey, State: component.ValueUnknown}
	}
	rows := []component.KeyValue{logger}
	switch {
	case l.Unread != "":
		rows = append(rows, component.KeyValue{Key: logFwdServerKey, State: component.ValueUnknown})
	case len(l.Now) == 0:
		rows = append(rows, component.KeyValue{Key: logFwdServerKey, Value: logFwdNone})
	default:
		for _, n := range l.Now {
			rows = append(rows, nowRow(n))
			if n.CAFile != "" {
				rows = append(rows, component.KeyValue{Key: logFwdCAKey, Value: n.CAFile})
			}
		}
		by := component.KeyValue{Key: logFwdByKey, Value: logFwdNotHardn}
		if l.ByHardn {
			by.Value = logFwdHardn
		}
		file := component.KeyValue{Key: logFwdFileKey, Value: l.File}
		if l.Files > 1 {
			file.Value = fmt.Sprintf(logFwdFiles, l.Files)
		}
		rows = append(rows, file, by)
	}
	want := wantRow(wantWords(l.Want), logFwdNoWant, l.WantBad, l.differs())
	want.StackLong = true // one server with a long name
	return append(rows, want)
}

// setItem is Set servers, its description what it would do or what is in
// the way: the settings unread, no server in hardn.yml, a protocol busybox
// syslogd has not, an rsyslog to install, or one hardn does not install
// here; else the server, or how many.
func (l LogForward) setItem() component.MenuItem {
	it := component.MenuItem{Key: LogForwardFirstKey, Label: logFwdSet}
	switch {
	case l.Unread != "":
		it.Description = l.Unread
	case l.WantBad:
		it.Description = logFwdDescBad
	case len(l.Want) == 0:
		it.Description = logFwdDescNoWant
	case l.Logger == LogBusybox && slices.ContainsFunc(l.Want, func(s LogServer) bool { return s.Proto != LogUDP }):
		it.Description = logFwdDescUDP
	case l.Logger == LogJournald && l.NoInstall:
		it.Description = logFwdDescNoInst
	case l.Logger == LogJournald:
		it.Description = logFwdDescInst
	default:
		it.Description = wantWords(l.Want)
	}
	return it
}

// MenuItems are the screen's items, named by their result: Set servers
// while the host does not forward to hardn.yml's servers; Stop forwarding
// while it forwards by hardn's own file; and Set servers shown as
// unavailable while it forwards by a file that is not hardn's.
func (l LogForward) MenuItems() []component.MenuItem {
	stop := component.MenuItem{Label: logFwdStop, Description: logFwdDescStop}
	forwards := l.Unread == "" && len(l.Now) > 0
	off := ""
	if forwards && !l.ByHardn {
		off = logFwdDescOther
	}
	return setUndoItems(l.setItem(), stop, off, forwards && l.ByHardn, l.differs())
}

// Stops reports whether it, one of MenuItems, is Stop forwarding; Set
// servers otherwise.
func (LogForward) Stops(it component.MenuItem) bool { return it.Label == logFwdStop }

// SubMenu returns the Syslog Settings screen: the title bar; the row; the
// section; the items; Return; "Select an option"; "Option".
func (l LogForward) SubMenu() SubMenu {
	return SubMenu{
		Title:    trail(logFwdTitle),
		Status:   l.Status,
		Sections: []Section{{Header: component.SectionHeader{Title: logFwdSection}, Rows: component.BulletList{Items: l.Rows()}}},
		Heading:  fixtureHeading,
		Menu:     component.Menu{Items: l.MenuItems(), Exit: returnItem()},
		Prompt:   menuPrompt,
	}
}

// SetChange is Set servers' change: its review hardn.yml's servers as the
// change leaves them, and its question names the server, or how many. With
// no server in hardn.yml there is nothing to set, and ok is false.
func (l LogForward) SetChange() (ch Change, ok bool) {
	if len(l.Want) == 0 {
		return Change{}, false
	}
	ask := fmt.Sprintf(logFwdAskSet, l.Want[0].Address)
	if len(l.Want) > 1 {
		ask = fmt.Sprintf(logFwdAskSetMany, len(l.Want))
	}
	return Change{Step: WorkflowStep{Title: trail(logFwdTitle, logFwdSet)}, Question: ask,
		Review: flow.Review{Title: logFwdSection, Items: wantRows(l.Want)}}, true
}

// StopChange is Stop forwarding's change: its review the Server row as the
// change leaves it.
func (l LogForward) StopChange() Change {
	return Change{Step: WorkflowStep{Title: trail(logFwdTitle, logFwdStop)}, Question: logFwdAskStop,
		Review: flow.Review{Title: logFwdSection, Items: []component.KeyValue{{Key: logFwdServerKey, Value: logFwdNone}}}}
}
