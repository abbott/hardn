package screen

import (
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The Log Forwarding view (logforward.go): its rows, its items and its
// changes, by what the host forwards and what hardn.yml names.

var (
	lfTestTCP = LogServer{Address: "192.0.2.50", Port: "514", Proto: LogTCP}
	lfTestUDP = LogServer{Address: "192.0.2.50", Port: "514", Proto: LogUDP}
	lfTestTLS = LogServer{Address: "logs.example.net", Port: "6514", Proto: LogTLS, CAFile: "/etc/ssl/lab-ca.pem"}
)

// TestLogForwardRows: the logger, then the server or "None", the protocol
// with what it lacks, the connection where there is one to show, the file and
// whose it is, and hardn.yml's server, a warning where it differs.
func TestLogForwardRows(t *testing.T) {
	kv := func(k, v string) component.KeyValue { return component.KeyValue{Key: k, Value: v} }
	tcpNote := component.KeyValue{Key: "Protocol", Value: "TCP", Note: "not encrypted"}
	udpNote := component.KeyValue{Key: "Protocol", Value: "UDP", Note: "not encrypted, losses unseen", State: component.ValueWarn, KeepNote: true}
	other := LogServer{Address: "192.0.2.60", Port: "514", Proto: LogTCP}
	tests := []struct {
		name string
		view LogForward
		want []component.KeyValue
	}{
		{"off", LogForward{Want: &lfTestTCP},
			[]component.KeyValue{kv("Logger", "rsyslog"), kv("Server", "None"), kv("hardn.yml", "192.0.2.50:514 TCP")}},
		{"no server", LogForward{Logger: LogBusybox},
			[]component.KeyValue{kv("Logger", "busybox syslogd"), kv("Server", "None"), kv("hardn.yml", "No server")}},
		{"journald", LogForward{Logger: LogJournald},
			[]component.KeyValue{{Key: "Logger", Value: "journald alone", Note: "no syslog forwarder", State: component.ValueWarn, KeepNote: true},
				kv("Server", "None"), kv("hardn.yml", "No server")}},
		{"tcp", LogForward{Now: &lfTestTCP, File: "/etc/rsyslog.d/60-hardn-forward.conf", ByHardn: true, Conn: LogConnOpen, Want: &lfTestTCP},
			[]component.KeyValue{kv("Logger", "rsyslog"), kv("Server", "192.0.2.50:514"), tcpNote,
				{Key: "Connection", Value: "Open", State: component.ValueOK}, kv("File", "/etc/rsyslog.d/60-hardn-forward.conf"), kv("Set by", "hardn"),
				kv("hardn.yml", "192.0.2.50:514 TCP")}},
		{"udp, not hardn's", LogForward{Now: &lfTestUDP, File: "/etc/rsyslog.d/50-remote.conf"},
			[]component.KeyValue{kv("Logger", "rsyslog"), kv("Server", "192.0.2.50:514"), udpNote,
				kv("File", "/etc/rsyslog.d/50-remote.conf"), kv("Set by", "Not hardn"), kv("hardn.yml", "No server")}},
		{"tls, down, differs", LogForward{Now: &lfTestTLS, File: "f", ByHardn: true, Conn: LogConnDown, Want: &other},
			[]component.KeyValue{kv("Logger", "rsyslog"), kv("Server", "logs.example.net:6514"), {Key: "Protocol", Value: "TLS", State: component.ValueOK},
				kv("CA File", "/etc/ssl/lab-ca.pem"), {Key: "Connection", Value: "None", State: component.ValueWarn}, kv("File", "f"), kv("Set by", "hardn"),
				{Key: "hardn.yml", Value: "192.0.2.60:514 TCP", Note: "differs", State: component.ValueWarn, KeepNote: true}}},
		{"not read", LogForward{Unread: "Can't read rsyslog's settings", Now: &lfTestTCP, Want: &lfTestTCP},
			[]component.KeyValue{kv("Logger", "rsyslog"), {Key: "Server", State: component.ValueUnknown}, kv("hardn.yml", "192.0.2.50:514 TCP")}},
	}
	for _, tt := range tests {
		if got := tt.view.Rows(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: rows %+v", tt.name, got)
		}
	}
}

// TestLogForwardItems: Set server while the host does not forward to
// hardn.yml's server, its description the server or what is in the way; Stop
// forwarding for hardn's own setting, item 2 beside Set server; and a setting
// that is not hardn's leaves Set server unavailable.
func TestLogForwardItems(t *testing.T) {
	other := LogServer{Address: "192.0.2.60", Port: "514", Proto: LogTCP}
	set := func(desc string) component.MenuItem {
		return component.MenuItem{Key: "1", Label: "Set server", Description: desc}
	}
	stop := func(key string) component.MenuItem {
		return component.MenuItem{Key: key, Label: "Stop forwarding", Description: "Undo hardn's change"}
	}
	tests := []struct {
		name string
		view LogForward
		want []component.MenuItem
	}{
		{"off", LogForward{Want: &lfTestTCP}, []component.MenuItem{set("192.0.2.50:514 TCP")}},
		{"no server", LogForward{}, []component.MenuItem{set("No server in hardn.yml")}},
		{"journald", LogForward{Logger: LogJournald, Want: &lfTestTCP}, []component.MenuItem{set("Installs rsyslog")}},
		{"journald, no server", LogForward{Logger: LogJournald}, []component.MenuItem{set("No server in hardn.yml")}},
		{"busybox, udp", LogForward{Logger: LogBusybox, Want: &lfTestUDP}, []component.MenuItem{set("192.0.2.50:514 UDP")}},
		{"busybox, tcp", LogForward{Logger: LogBusybox, Want: &lfTestTCP}, []component.MenuItem{set("busybox syslogd is UDP only")}},
		{"not read", LogForward{Unread: "Can't read rsyslog's settings", Now: &lfTestTCP, ByHardn: true, Want: &lfTestTCP},
			[]component.MenuItem{set("Can't read rsyslog's settings")}},
		{"on by hardn", LogForward{Now: &lfTestTCP, ByHardn: true, Want: &lfTestTCP}, []component.MenuItem{stop("1")}},
		{"on by hardn, no server", LogForward{Now: &lfTestTCP, ByHardn: true}, []component.MenuItem{stop("1")}},
		{"on by hardn, differs", LogForward{Now: &lfTestTCP, ByHardn: true, Want: &other}, []component.MenuItem{set("192.0.2.60:514 TCP"), stop("2")}},
		{"not hardn's", LogForward{Now: &lfTestTCP, Want: &other},
			[]component.MenuItem{{Key: "1", Label: "Set server", Description: "Set outside hardn", Disabled: true}}},
	}
	for _, tt := range tests {
		if got := tt.view.MenuItems(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: items %+v", tt.name, got)
		}
	}
}

// TestLogForwardChanges: Set server's review is the server as hardn.yml names
// it and its question names it; with no server there is no change; Stop
// forwarding's review leaves no server.
func TestLogForwardChanges(t *testing.T) {
	if _, ok := (LogForward{}).SetChange(); ok {
		t.Error("a change with no server in hardn.yml")
	}
	ch, ok := LogForward{Want: &lfTestTLS}.SetChange()
	want := []component.KeyValue{{Key: "Server", Value: "logs.example.net:6514"}, {Key: "Protocol", Value: "TLS", State: component.ValueOK},
		{Key: "CA File", Value: "/etc/ssl/lab-ca.pem"}}
	if !ok || ch.Question != "Forward logs to logs.example.net?" || ch.Step.Title.Title != "Set server" || !slices.Equal(ch.Review.Items, want) {
		t.Errorf("set: %+v", ch)
	}
	stop := LogForward{Now: &lfTestTCP, ByHardn: true}.StopChange()
	if stop.Question != "Stop forwarding logs?" || stop.Step.Title.Title != "Stop forwarding" ||
		!slices.Equal(stop.Review.Items, []component.KeyValue{{Key: "Server", Value: "None"}}) {
		t.Errorf("stop: %+v", stop)
	}
}

// TestLogForwardSubMenu: the screen is the row, one section and the items
// over Return.
func TestLogForwardSubMenu(t *testing.T) {
	vm := LogForward{Want: &lfTestTCP}.SubMenu()
	if got := sectionTitles(vm); !slices.Equal(got, []string{"Forwarding"}) || vm.Title.Title != "Log Forwarding" || len(vm.Menu.Items) != 1 || vm.Menu.Exit.Label != "Return" {
		t.Errorf("screen: %v, %+v", got, vm.Menu)
	}
}

// TestLogForwardNotEstablished: what is not established is drawn so, never
// as a value: a logger hardn cannot tell, sockets not read, and a hardn.yml
// that is not valid, which Set server says and sets nothing from.
func TestLogForwardNotEstablished(t *testing.T) {
	v := LogForward{Logger: LogNoLogger, Unread: "Can't tell the logger", WantBad: true}
	want := []component.KeyValue{{Key: "Logger", State: component.ValueUnknown}, {Key: "Server", State: component.ValueUnknown},
		{Key: "hardn.yml", Value: "Not valid", State: component.ValueWarn}}
	if got := v.Rows(); !slices.Equal(got, want) {
		t.Errorf("rows %+v", got)
	}
	bad := LogForward{WantBad: true}
	if got := bad.MenuItems(); len(got) != 1 || got[0].Description != "hardn.yml is not valid" || got[0].Disabled {
		t.Errorf("items %+v", got)
	}
	unread := LogForward{Now: &lfTestTCP, File: "f", ByHardn: true, Conn: LogConnUnknown}
	if got := unread.Rows(); got[3] != (component.KeyValue{Key: "Connection", State: component.ValueUnknown}) {
		t.Errorf("the connection row %+v", got[3])
	}
	items := LogForward{Now: &lfTestTCP, ByHardn: true, Want: &lfTestTLS}.MenuItems()
	if len(items) != 2 || unread.Stops(items[0]) || !unread.Stops(items[1]) {
		t.Errorf("Stops over %+v", items)
	}
}
