package screen

import (
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/flow"
)

// The Log Forwarding screen (docs/roadmap.md, "Log forwarding";
// docs/engine/gallery-logforward.md, adopted 2026-11-13): whether the host
// sends its logs to a syslog server, where, and by which file; the server
// hardn.yml names; and one or two items named by their result. On the
// sub-menu layout: there is no Figma frame. pkg/ui cannot reach the host:
// pkg/screens/logforward reads the logger and fills LogForward
// (docs/engine/screen-logforward.md).

// The screen's words: decided (gallery-logforward.md, "Words").
const (
	logFwdTitle   = "Log Forwarding"
	logFwdSection = "Forwarding"

	logFwdLoggerKey = "Logger"
	logFwdServerKey = "Server"
	logFwdProtoKey  = "Protocol"
	logFwdCAKey     = "CA File"
	logFwdConnKey   = "Connection"
	logFwdFileKey   = "File"
	logFwdByKey     = "Set by"

	logFwdRsyslog  = "rsyslog"
	logFwdBusybox  = "busybox syslogd"
	logFwdJournald = "journald alone"
	logFwdNoLogger = "no syslog forwarder"

	logFwdNone       = "None"
	logFwdNoWant     = "No server"
	logFwdPlain      = "not encrypted"
	logFwdLossy      = "not encrypted, losses unseen"
	logFwdOpen       = "Open"
	logFwdHardn      = "hardn"
	logFwdNotHardn   = "Not hardn"
	logFwdSet        = "Set server"
	logFwdStop       = "Stop forwarding"
	logFwdDescStop   = "Undo hardn's change"
	logFwdDescNoWant = "No server in hardn.yml"
	logFwdDescOther  = "Set outside hardn"
	logFwdDescInst   = "Installs rsyslog"
	logFwdDescNoInst = "hardn can't install rsyslog here"
	logFwdDescUDP    = "busybox syslogd is UDP only"
	logFwdDescBad    = "hardn.yml is not valid"

	logFwdAskStop = "Stop forwarding logs?"
)

// The items' keys: Set server is 1 wherever it shows, and Stop forwarding is
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

// LogServer is a syslog server: the one the host forwards to, or the one
// hardn.yml names.
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

// LogConn is what the host's sockets say of the server.
type LogConn uint8

const (
	// LogConnNone: nothing to say, as over UDP, which has no connection:
	// no row.
	LogConnNone LogConn = iota
	// LogConnOpen: a connection to the server is open.
	LogConnOpen
	// LogConnDown: forwarding is set over TCP or TLS and no connection to
	// the server is open.
	LogConnDown
	// LogConnUnknown: the host's sockets are not read.
	LogConnUnknown
)

// LogForward is the Log Forwarding screen's reading of the host, which its
// caller fills.
type LogForward struct {
	// Status is the Log Forwarding row.
	Status component.StatusList
	// Logger is the host's syslog daemon.
	Logger LogLogger
	// NoInstall: with LogJournald, hardn installs no package on this
	// distribution, so Set server cannot install rsyslog.
	NoInstall bool
	// Unread is why the logger's settings cannot be read, in a few words,
	// such as "Can't read rsyslog's settings"; "" where they are read.
	Unread string
	// Now is the server the host forwards to; nil for none.
	Now *LogServer
	// File is the file that sets Now, such as
	// "/etc/rsyslog.d/60-hardn-forward.conf".
	File string
	// ByHardn: File's setting is hardn's own.
	ByHardn bool
	// Conn is what the host's sockets say of Now.
	Conn LogConn
	// Want is the server hardn.yml names; nil for none.
	Want *LogServer
	// WantBad: hardn.yml's logForwarding is not valid, so Want is nil and
	// nothing is set from it.
	WantBad bool
}

// differs reports whether hardn.yml names a server and the host forwards to
// another, or another way.
func (l LogForward) differs() bool {
	return l.Want != nil && l.Now != nil && *l.Want != *l.Now
}

// protoRow is the Protocol row: TLS is good, TCP says it is not encrypted,
// and UDP warns that it is neither encrypted nor checked.
func protoRow(p LogProto) component.KeyValue {
	row := component.KeyValue{Key: logFwdProtoKey, Value: logProtoWords[p]}
	switch p {
	case LogTLS:
		row.State = component.ValueOK
	case LogUDP:
		row.State, row.Note, row.KeepNote = component.ValueWarn, logFwdLossy, true
	default:
		row.Note = logFwdPlain
	}
	return row
}

// Rows are the Forwarding section's rows: the logger; where the logs go and
// how; the file that says so and whether it is hardn's; and the server
// hardn.yml names, a warning where the host forwards to another.
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
	case l.Now == nil:
		rows = append(rows, component.KeyValue{Key: logFwdServerKey, Value: logFwdNone})
	default:
		rows = append(rows, component.KeyValue{Key: logFwdServerKey, Value: l.Now.endpoint()}, protoRow(l.Now.Proto))
		if l.Now.CAFile != "" {
			rows = append(rows, component.KeyValue{Key: logFwdCAKey, Value: l.Now.CAFile})
		}
		switch l.Conn {
		case LogConnOpen:
			rows = append(rows, component.KeyValue{Key: logFwdConnKey, Value: logFwdOpen, State: component.ValueOK})
		case LogConnDown:
			rows = append(rows, component.KeyValue{Key: logFwdConnKey, Value: logFwdNone, State: component.ValueWarn})
		case LogConnUnknown:
			rows = append(rows, component.KeyValue{Key: logFwdConnKey, State: component.ValueUnknown})
		}
		by := component.KeyValue{Key: logFwdByKey, Value: logFwdNotHardn}
		if l.ByHardn {
			by.Value = logFwdHardn
		}
		rows = append(rows, component.KeyValue{Key: logFwdFileKey, Value: l.File}, by)
	}
	want := ""
	if l.Want != nil {
		want = l.Want.String()
	}
	return append(rows, wantRow(want, logFwdNoWant, l.WantBad, l.differs()))
}

// setItem is Set server, its description what it would do or what is in the
// way: the settings unread, no server in hardn.yml, a protocol busybox
// syslogd has not, an rsyslog to install, or one hardn does not install
// here; else the server.
func (l LogForward) setItem() component.MenuItem {
	it := component.MenuItem{Key: LogForwardFirstKey, Label: logFwdSet}
	switch {
	case l.Unread != "":
		it.Description = l.Unread
	case l.WantBad:
		it.Description = logFwdDescBad
	case l.Want == nil:
		it.Description = logFwdDescNoWant
	case l.Logger == LogBusybox && l.Want.Proto != LogUDP:
		it.Description = logFwdDescUDP
	case l.Logger == LogJournald && l.NoInstall:
		it.Description = logFwdDescNoInst
	case l.Logger == LogJournald:
		it.Description = logFwdDescInst
	default:
		it.Description = l.Want.String()
	}
	return it
}

// MenuItems are the screen's items, named by their result: Set server while
// the host does not forward to hardn.yml's server; Stop forwarding while it
// forwards by hardn's own file; and Set server shown as unavailable while it
// forwards by a file that is not hardn's.
func (l LogForward) MenuItems() []component.MenuItem {
	stop := component.MenuItem{Label: logFwdStop, Description: logFwdDescStop}
	forwards := l.Unread == "" && l.Now != nil
	off := ""
	if forwards && !l.ByHardn {
		off = logFwdDescOther
	}
	return setUndoItems(l.setItem(), stop, off, forwards && l.ByHardn, l.differs())
}

// Stops reports whether it, one of MenuItems, is Stop forwarding; Set
// server otherwise.
func (LogForward) Stops(it component.MenuItem) bool { return it.Label == logFwdStop }

// SubMenu returns the Log Forwarding screen: the title bar; the row; the
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

// SetChange is Set server's change: its review the server and the protocol
// as the change leaves them, and its question names the server. With no
// server in hardn.yml there is nothing to set, and ok is false.
func (l LogForward) SetChange() (ch Change, ok bool) {
	if l.Want == nil {
		return Change{}, false
	}
	items := []component.KeyValue{{Key: logFwdServerKey, Value: l.Want.endpoint()}, protoRow(l.Want.Proto)}
	if l.Want.CAFile != "" {
		items = append(items, component.KeyValue{Key: logFwdCAKey, Value: l.Want.CAFile})
	}
	return Change{Step: WorkflowStep{Title: trail(logFwdTitle, logFwdSet)},
		Question: "Forward logs to " + l.Want.Address + "?",
		Review:   flow.Review{Title: logFwdSection, Items: items}}, true
}

// StopChange is Stop forwarding's change: its review the Server row as the
// change leaves it.
func (l LogForward) StopChange() Change {
	return Change{Step: WorkflowStep{Title: trail(logFwdTitle, logFwdStop)}, Question: logFwdAskStop,
		Review: flow.Review{Title: logFwdSection, Items: []component.KeyValue{{Key: logFwdServerKey, Value: logFwdNone}}}}
}
