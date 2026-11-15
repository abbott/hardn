package screen

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The Syslog Settings view (logforward.go): its rows, its items and its
// changes, by what the host forwards and what hardn.yml names
// (docs/engine/design-syslog-servers.md §2.4, §2.5).

var (
	lfTestTCP  = LogServer{Address: "192.0.2.50", Port: "514", Proto: LogTCP}
	lfTestUDP  = LogServer{Address: "192.0.2.50", Port: "514", Proto: LogUDP}
	lfTestTLS  = LogServer{Address: "logs.example.net", Port: "6514", Proto: LogTLS, CAFile: "/etc/ssl/lab-ca.pem"}
	lfTestTCP2 = LogServer{Address: "192.0.2.51", Port: "514", Proto: LogTCP}
	lfTestUDP2 = LogServer{Address: "192.0.2.52", Port: "514", Proto: LogUDP}
)

// lfNow is s as the host forwards to it, its connection conn.
func lfNow(s LogServer, conn LogConn) LogForwarded { return LogForwarded{LogServer: s, Conn: conn} }

// lfRow is a Server row: its value, its note and the note's state.
func lfRow(value, note string, state component.ValueState) component.KeyValue {
	return component.KeyValue{Key: "Server", Value: value, Note: note, State: state, KeepNote: note != "", StackLong: true}
}

// TestLogForwardRows: the logger; a Server row per server, or "None", each
// ending in its connection, a CA File row after its TLS server; the file
// and whose it is; and hardn.yml's server, or how many, a warning where it
// differs.
func TestLogForwardRows(t *testing.T) {
	kv := func(k, v string) component.KeyValue { return component.KeyValue{Key: k, Value: v} }
	const own = "/etc/rsyslog.d/60-hardn-forward.conf"
	other := LogServer{Address: "192.0.2.60", Port: "514", Proto: LogTCP}
	three := []LogServer{lfTestTLS, lfTestTCP2, lfTestUDP2}
	tests := []struct {
		name string
		view LogForward
		want []component.KeyValue
	}{
		{"off", LogForward{Want: []LogServer{lfTestTCP}},
			[]component.KeyValue{kv("Logger", "rsyslog"), kv("Server", "None"), kv("hardn.yml", "192.0.2.50:514 TCP")}},
		{"no server", LogForward{Logger: LogBusybox},
			[]component.KeyValue{kv("Logger", "busybox syslogd"), kv("Server", "None"), kv("hardn.yml", "No server")}},
		{"journald", LogForward{Logger: LogJournald},
			[]component.KeyValue{{Key: "Logger", Value: "journald alone", Note: "no syslog forwarder", State: component.ValueWarn, KeepNote: true},
				kv("Server", "None"), kv("hardn.yml", "No server")}},
		{"one, tcp, open", LogForward{Now: []LogForwarded{lfNow(lfTestTCP, LogConnOpen)}, File: own, ByHardn: true, Want: []LogServer{lfTestTCP}},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("192.0.2.50:514 TCP", "Open", component.ValueOK),
				kv("File", own), kv("Set by", "hardn"), kv("hardn.yml", "192.0.2.50:514 TCP")}},
		{"udp, not hardn's", LogForward{Now: []LogForwarded{lfNow(lfTestUDP, LogConnNone)}, File: "/etc/rsyslog.d/50-remote.conf"},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("192.0.2.50:514 UDP", "losses unseen", component.ValueWarn),
				kv("File", "/etc/rsyslog.d/50-remote.conf"), kv("Set by", "Not hardn"), kv("hardn.yml", "No server")}},
		{"tls, down, differs", LogForward{Now: []LogForwarded{lfNow(lfTestTLS, LogConnDown)}, File: "f", ByHardn: true, Want: []LogServer{other}},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("logs.example.net:6514 TLS", "unreachable", component.ValueWarn),
				kv("CA File", "/etc/ssl/lab-ca.pem"), kv("File", "f"), kv("Set by", "hardn"),
				{Key: "hardn.yml", Value: "192.0.2.60:514 TCP", Note: "differs", State: component.ValueWarn, KeepNote: true}}},
		// The design's screen (§2.4): three servers, one unreachable.
		{"three, one down", LogForward{Now: []LogForwarded{lfNow(lfTestTLS, LogConnOpen), lfNow(lfTestTCP2, LogConnDown), lfNow(lfTestUDP2, LogConnNone)},
			File: own, ByHardn: true, Want: three},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("logs.example.net:6514 TLS", "Open", component.ValueOK), kv("CA File", "/etc/ssl/lab-ca.pem"),
				lfRow("192.0.2.51:514 TCP", "unreachable", component.ValueWarn), lfRow("192.0.2.52:514 UDP", "losses unseen", component.ValueWarn),
				kv("File", own), kv("Set by", "hardn"), kv("hardn.yml", "3 servers")}},
		{"as asked in another order", LogForward{Now: []LogForwarded{lfNow(lfTestUDP2, LogConnNone), lfNow(lfTestTCP2, LogConnOpen)}, File: own, ByHardn: true,
			Want: []LogServer{lfTestTCP2, lfTestUDP2}},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("192.0.2.52:514 UDP", "losses unseen", component.ValueWarn), lfRow("192.0.2.51:514 TCP", "Open", component.ValueOK),
				kv("File", own), kv("Set by", "hardn"), kv("hardn.yml", "2 servers")}},
		{"a list that differs", LogForward{Now: []LogForwarded{lfNow(lfTestTCP2, LogConnOpen)}, File: own, ByHardn: true, Want: three},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("192.0.2.51:514 TCP", "Open", component.ValueOK), kv("File", own), kv("Set by", "hardn"),
				{Key: "hardn.yml", Value: "3 servers", Note: "differs", State: component.ValueWarn, KeepNote: true}}},
		{"two names on a port, one reached", LogForward{Now: []LogForwarded{
			{LogServer: LogServer{Address: "a.example.net", Port: "514"}, Conn: LogConnDown, Open: 1, Of: 2},
			{LogServer: LogServer{Address: "b.example.net", Port: "514"}, Conn: LogConnDown, Open: 1, Of: 2}}, File: own, ByHardn: true},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("a.example.net:514 TCP", "1 of 2 open", component.ValueWarn), lfRow("b.example.net:514 TCP", "1 of 2 open", component.ValueWarn),
				kv("File", own), kv("Set by", "hardn"), kv("hardn.yml", "No server")}},
		{"set outside hardn, in two files", LogForward{Now: []LogForwarded{lfNow(lfTestTCP, LogConnOpen), lfNow(lfTestUDP2, LogConnNone)}, File: "/etc/rsyslog.conf", Files: 2},
			[]component.KeyValue{kv("Logger", "rsyslog"), lfRow("192.0.2.50:514 TCP", "Open", component.ValueOK), lfRow("192.0.2.52:514 UDP", "losses unseen", component.ValueWarn),
				kv("File", "2 files"), kv("Set by", "Not hardn"), kv("hardn.yml", "No server")}},
		{"not read", LogForward{Unread: "Can't read rsyslog's settings", Now: []LogForwarded{lfNow(lfTestTCP, LogConnOpen)}, Want: []LogServer{lfTestTCP}},
			[]component.KeyValue{kv("Logger", "rsyslog"), {Key: "Server", State: component.ValueUnknown}, kv("hardn.yml", "192.0.2.50:514 TCP")}},
	}
	for _, tt := range tests {
		tt.want[len(tt.want)-1].StackLong = true // the hardn.yml row: one server with a long name
		if got := tt.view.Rows(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: rows %+v", tt.name, got)
		}
	}
}

// TestLogForwardLongName: a Server row over the row's width stacks, the key
// alone and the whole value under it; a name too long even there is cut
// before its port, so the port, the protocol and the connection stay
// (§2.4, "A long row").
func TestLogForwardLongName(t *testing.T) {
	c := plainAt(24, false)
	draw := func(name string) string {
		s := LogServer{Address: name, Port: "6514", Proto: LogTLS, CAFile: "/etc/ssl/lab-ca.pem"}
		v := LogForward{Now: []LogForwarded{lfNow(s, LogConnOpen)}, File: "f", ByHardn: true}
		return ansi.Strip(component.BulletList{Items: v.Rows()}.Render(c)) + "\n"
	}
	out := draw("syslog-collector.prod.example.com")
	if !strings.Contains(out, "  [+] Server\n      syslog-collector.prod.example.com:6514 TLS Open\n") {
		t.Errorf("a long name does not stack whole:\n%s", out)
	}
	if !strings.Contains(out, "   -  Logger               rsyslog\n") || !strings.Contains(out, "   -  CA File              /etc/ssl/lab-ca.pem\n") {
		t.Errorf("the other rows stack with it:\n%s", out)
	}
	if short := draw("logs.example.net"); !strings.Contains(short, "  [+] Server               logs.example.net:6514 TLS Open\n") {
		t.Errorf("a short name stacks:\n%s", short)
	}
	out = draw(strings.Repeat("a", 60) + ".example.com")
	var row string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasSuffix(l, ":6514 TLS Open") {
			row = l
		}
	}
	if len(row) != 64 || !strings.HasPrefix(row, "      aaa") || !strings.Contains(row, "aaa...:6514 TLS Open") || strings.Contains(row, "example.com") {
		t.Errorf("a name too long is not cut before its port, at the row's width: %q in\n%s", row, out)
	}
}

// TestLogForwardItems: Set servers while the host does not forward to
// hardn.yml's servers, its description the server, how many, or what is in
// the way; Stop forwarding for hardn's own setting, item 2 beside Set
// servers; and a setting that is not hardn's leaves Set servers unavailable.
func TestLogForwardItems(t *testing.T) {
	other := []LogServer{{Address: "192.0.2.60", Port: "514", Proto: LogTCP}}
	tcp, udp := []LogServer{lfTestTCP}, []LogServer{lfTestUDP}
	on := []LogForwarded{lfNow(lfTestTCP, LogConnOpen)}
	set := func(desc string) component.MenuItem {
		return component.MenuItem{Key: "1", Label: "Set servers", Description: desc}
	}
	stop := func(key string) component.MenuItem {
		return component.MenuItem{Key: key, Label: "Stop forwarding", Description: "Undo hardn's change"}
	}
	tests := []struct {
		name string
		view LogForward
		want []component.MenuItem
	}{
		{"off", LogForward{Want: tcp}, []component.MenuItem{set("192.0.2.50:514 TCP")}},
		{"off, several", LogForward{Want: []LogServer{lfTestTLS, lfTestTCP2, lfTestUDP2}}, []component.MenuItem{set("3 servers")}},
		{"no server", LogForward{}, []component.MenuItem{set("No server in hardn.yml")}},
		{"journald", LogForward{Logger: LogJournald, Want: tcp}, []component.MenuItem{set("Installs rsyslog")}},
		{"journald, no server", LogForward{Logger: LogJournald}, []component.MenuItem{set("No server in hardn.yml")}},
		{"busybox, udp", LogForward{Logger: LogBusybox, Want: udp}, []component.MenuItem{set("192.0.2.50:514 UDP")}},
		{"busybox, two udp", LogForward{Logger: LogBusybox, Want: []LogServer{lfTestUDP, lfTestUDP2}}, []component.MenuItem{set("2 servers")}},
		{"busybox, tcp", LogForward{Logger: LogBusybox, Want: tcp}, []component.MenuItem{set("busybox syslogd is UDP only")}},
		{"busybox, one tcp of three", LogForward{Logger: LogBusybox, Want: []LogServer{lfTestUDP, lfTestTCP2, lfTestUDP2}}, []component.MenuItem{set("busybox syslogd is UDP only")}},
		{"not read", LogForward{Unread: "Can't read rsyslog's settings", Now: on, ByHardn: true, Want: tcp},
			[]component.MenuItem{set("Can't read rsyslog's settings")}},
		{"on by hardn", LogForward{Now: on, ByHardn: true, Want: tcp}, []component.MenuItem{stop("1")}},
		{"on by hardn, no server", LogForward{Now: on, ByHardn: true}, []component.MenuItem{stop("1")}},
		{"on by hardn, differs", LogForward{Now: on, ByHardn: true, Want: other}, []component.MenuItem{set("192.0.2.60:514 TCP"), stop("2")}},
		{"on by hardn, one more asked", LogForward{Now: on, ByHardn: true, Want: []LogServer{lfTestTCP, lfTestTCP2}}, []component.MenuItem{set("2 servers"), stop("2")}},
		{"on by hardn, another setting beside it", LogForward{Now: on, ByHardn: true, Beside: true, Want: tcp}, []component.MenuItem{set("192.0.2.50:514 TCP"), stop("2")}},
		{"not hardn's", LogForward{Now: on, Want: other},
			[]component.MenuItem{{Key: "1", Label: "Set servers", Description: "Set outside hardn", Disabled: true}}},
	}
	for _, tt := range tests {
		if got := tt.view.MenuItems(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: items %+v", tt.name, got)
		}
	}
}

// TestLogForwardChanges: Set servers' review is a Server row per server of
// hardn.yml, and its question names the server, or how many; with no server
// there is no change; Stop forwarding's review leaves no server.
func TestLogForwardChanges(t *testing.T) {
	if _, ok := (LogForward{}).SetChange(); ok {
		t.Error("a change with no server in hardn.yml")
	}
	ch, ok := LogForward{Want: []LogServer{lfTestTLS}}.SetChange()
	want := []component.KeyValue{lfRow("logs.example.net:6514 TLS", "", component.ValuePlain), {Key: "CA File", Value: "/etc/ssl/lab-ca.pem"}}
	if !ok || ch.Question != "Forward logs to logs.example.net?" || ch.Step.Title.Title != "Set servers" || !slices.Equal(ch.Review.Items, want) {
		t.Errorf("set: %+v", ch)
	}
	ch, ok = LogForward{Want: []LogServer{lfTestTCP, lfTestUDP2, lfTestTLS}}.SetChange()
	want = []component.KeyValue{lfRow("192.0.2.50:514 TCP", "", component.ValuePlain), lfRow("192.0.2.52:514 UDP", "losses unseen", component.ValuePlain),
		lfRow("logs.example.net:6514 TLS", "", component.ValuePlain), {Key: "CA File", Value: "/etc/ssl/lab-ca.pem"}}
	if !ok || ch.Question != "Forward logs to 3 servers?" || !slices.Equal(ch.Review.Items, want) {
		t.Errorf("set three: %+v", ch)
	}
	stop := LogForward{Now: []LogForwarded{lfNow(lfTestTCP, LogConnOpen)}, ByHardn: true}.StopChange()
	if stop.Question != "Stop forwarding logs?" || stop.Step.Title.Title != "Stop forwarding" ||
		!slices.Equal(stop.Review.Items, []component.KeyValue{{Key: "Server", Value: "None"}}) {
		t.Errorf("stop: %+v", stop)
	}
}

// TestLogForwardSubMenu: the screen is the row, one section and the items
// over Return.
func TestLogForwardSubMenu(t *testing.T) {
	vm := LogForward{Want: []LogServer{lfTestTCP}}.SubMenu()
	if got := sectionTitles(vm); !slices.Equal(got, []string{"Forwarding"}) || vm.Title.Title != "Syslog Settings" || len(vm.Menu.Items) != 1 || vm.Menu.Exit.Label != "Return" {
		t.Errorf("screen: %v, %+v", got, vm.Menu)
	}
}

// TestLogForwardNotEstablished: what is not established is drawn so, never
// as a value: a logger hardn cannot tell, sockets not read, and a hardn.yml
// that is not valid, which Set servers says and sets nothing from.
func TestLogForwardNotEstablished(t *testing.T) {
	v := LogForward{Logger: LogNoLogger, Unread: "Can't tell the logger", WantBad: true}
	want := []component.KeyValue{{Key: "Logger", State: component.ValueUnknown}, {Key: "Server", State: component.ValueUnknown},
		{Key: "hardn.yml", Value: "Not valid", State: component.ValueWarn, StackLong: true}}
	if got := v.Rows(); !slices.Equal(got, want) {
		t.Errorf("rows %+v", got)
	}
	bad := LogForward{WantBad: true}
	if got := bad.MenuItems(); len(got) != 1 || got[0].Description != "hardn.yml is not valid" || got[0].Disabled {
		t.Errorf("items %+v", got)
	}
	unread := LogForward{Now: []LogForwarded{lfNow(lfTestTCP, LogConnUnknown)}, File: "f", ByHardn: true}
	if got := unread.Rows(); got[1] != lfRow("192.0.2.50:514 TCP", "", component.ValueUnknown) {
		t.Errorf("the server row %+v", got[1])
	}
	items := LogForward{Now: []LogForwarded{lfNow(lfTestTCP, LogConnOpen)}, ByHardn: true, Want: []LogServer{lfTestTLS}}.MenuItems()
	if len(items) != 2 || unread.Stops(items[0]) || !unread.Stops(items[1]) {
		t.Errorf("Stops over %+v", items)
	}
}
