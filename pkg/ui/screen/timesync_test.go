package screen

import (
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The NTP Settings view (timesync.go): its rows, its items and its changes, by
// the service, the clock, where each server is set and what hardn.yml names.

var (
	tsTestWant  = []string{"192.0.2.10", "192.0.2.11"}
	tsTestOwn   = []TimeServer{{Name: "192.0.2.10", From: TimeFromHardn}, {Name: "192.0.2.11", From: TimeFromHardn}}
	tsTestPool  = TimeServer{Name: "2.debian.pool.ntp.org"}
	tsTestBuilt = TimeServer{Name: "ntp.ubuntu.com", From: TimeFromBuiltIn}
)

// TestTimeSyncRows: the service, the clock, the server followed where the
// service says, each server with where it is set, hardn's file, and
// hardn.yml's servers, a warning where hardn's own differ.
func TestTimeSyncRows(t *testing.T) {
	kv := func(k, v string) component.KeyValue { return component.KeyValue{Key: k, Value: v} }
	note := func(v, n string) component.KeyValue { return component.KeyValue{Key: "Server", Value: v, Note: n} }
	ok := component.KeyValue{Key: "Clock", Value: "In sync", State: component.ValueOK}
	bad := component.KeyValue{Key: "Clock", Value: "Not in sync", State: component.ValueWarn}
	tests := []struct {
		name string
		view TimeSync
		want []component.KeyValue
	}{
		{"default", TimeSync{Clock: TimeClockInSync, Synced: "192.0.2.5", Servers: []TimeServer{tsTestPool}},
			[]component.KeyValue{kv("Service", "chrony"), ok, kv("Synced to", "192.0.2.5"), kv("Server", "2.debian.pool.ntp.org"), kv("hardn.yml", "No servers")}},
		{"hardn's, not reached", TimeSync{Clock: TimeClockNotInSync, Servers: tsTestOwn, File: "f", Want: tsTestWant, Restores: []TimeServer{tsTestPool}},
			[]component.KeyValue{kv("Service", "chrony"), bad, {Key: "Synced to", Value: "None", State: component.ValueWarn},
				note("192.0.2.10", "by hardn"), note("192.0.2.11", "by hardn"), kv("File", "f"),
				kv("hardn.yml", "192.0.2.10, 192.0.2.11")}},
		{"differs", TimeSync{Clock: TimeClockInSync, Synced: "192.0.2.10", Servers: tsTestOwn, File: "f", Want: []string{"192.0.2.11", "192.0.2.10"}},
			[]component.KeyValue{kv("Service", "chrony"), ok, kv("Synced to", "192.0.2.10"), note("192.0.2.10", "by hardn"), note("192.0.2.11", "by hardn"), kv("File", "f"),
				{Key: "hardn.yml", Value: "192.0.2.11, 192.0.2.10", Note: "differs", State: component.ValueWarn, KeepNote: true}}},
		{"dhcp and another file", TimeSync{Clock: TimeClockInSync, Servers: []TimeServer{{Name: "192.0.2.1", From: TimeFromDHCP}, {Name: "192.0.2.77", From: TimeFromOther, File: "site.sources"}}},
			[]component.KeyValue{kv("Service", "chrony"), ok, note("192.0.2.1", "from DHCP"), note("192.0.2.77", "in site.sources"), kv("hardn.yml", "No servers")}},
		{"timesyncd, built in", TimeSync{Service: TimeTimesyncd, Clock: TimeClockInSync, Synced: "ntp.ubuntu.com", Servers: []TimeServer{tsTestBuilt}},
			[]component.KeyValue{kv("Service", "systemd-timesyncd"), ok, kv("Synced to", "ntp.ubuntu.com"), note("ntp.ubuntu.com", "built in"), kv("hardn.yml", "No servers")}},
		{"timesyncd, set", TimeSync{Service: TimeTimesyncd, Clock: TimeClockInSync, Servers: tsTestOwn, File: "f", Restores: []TimeServer{tsTestBuilt}},
			[]component.KeyValue{kv("Service", "systemd-timesyncd"), ok, note("192.0.2.10", "by hardn"), note("192.0.2.11", "by hardn"), kv("File", "f"), kv("hardn.yml", "No servers")}},
		{"busybox says no server", TimeSync{Service: TimeBusybox, Clock: TimeClockNotInSync, Synced: "x", Servers: []TimeServer{{Name: "pool.ntp.org"}}},
			[]component.KeyValue{kv("Service", "busybox ntpd"), bad, kv("Server", "pool.ntp.org"), kv("hardn.yml", "No servers")}},
		{"stopped", TimeSync{Stopped: true, Clock: TimeClockNotInSync},
			[]component.KeyValue{{Key: "Service", Value: "chrony", Note: "not running", State: component.ValueWarn, KeepNote: true}, bad, kv("Server", "None"), kv("hardn.yml", "No servers")}},
		{"no service", TimeSync{Service: TimeNoService, Clock: TimeClockNotInSync, WantBad: true},
			[]component.KeyValue{{Key: "Service", Value: "None", State: component.ValueWarn}, bad, {Key: "hardn.yml", Value: "Not valid", State: component.ValueWarn}}},
		{"container", TimeSync{Service: TimeNoService, Container: true, Clock: TimeClockInSync, Want: tsTestWant},
			[]component.KeyValue{kv("Service", "None"), ok, {Key: "In a container: the host keeps the time.", State: component.ValueNone}}},
		{"not read", TimeSync{Unread: "Can't read chrony's settings", Servers: tsTestOwn, File: "f"},
			[]component.KeyValue{kv("Service", "chrony"), {Key: "Clock", State: component.ValueUnknown}, {Key: "Server", State: component.ValueUnknown}, kv("hardn.yml", "No servers")}},
	}
	for _, tt := range tests {
		if got := tt.view.Rows(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: rows %+v", tt.name, got)
		}
	}
}

// TestTimeSyncItems: Set servers while hardn's own setting is not
// hardn.yml's, its description the servers or what is in the way; Remove
// servers for hardn's own, item 2 beside Set servers; and Set servers
// unavailable where hardn sets nothing.
func TestTimeSyncItems(t *testing.T) {
	set := func(desc string) component.MenuItem {
		return component.MenuItem{Key: "1", Label: "Set servers", Description: desc}
	}
	off := func(desc string) component.MenuItem {
		return component.MenuItem{Key: "1", Label: "Set servers", Description: desc, Disabled: true}
	}
	remove := func(key string) component.MenuItem {
		return component.MenuItem{Key: key, Label: "Remove servers", Description: "Undo hardn's change"}
	}
	other := []TimeServer{{Name: "192.0.2.77", From: TimeFromOther, File: "site.sources"}}
	tests := []struct {
		name string
		view TimeSync
		want []component.MenuItem
	}{
		{"off", TimeSync{Servers: []TimeServer{tsTestPool}, Want: tsTestWant}, []component.MenuItem{set("192.0.2.10, 192.0.2.11")}},
		{"no servers", TimeSync{}, []component.MenuItem{set("No servers in hardn.yml")}},
		{"not valid", TimeSync{WantBad: true}, []component.MenuItem{set("hardn.yml is not valid")}},
		{"dhcp", TimeSync{Servers: []TimeServer{{Name: "192.0.2.1", From: TimeFromDHCP}}, Want: tsTestWant}, []component.MenuItem{set("192.0.2.10, 192.0.2.11")}},
		{"not read", TimeSync{Unread: "Can't read chrony's settings", Servers: tsTestOwn, Want: tsTestWant}, []component.MenuItem{set("Can't read chrony's settings")}},
		{"hardn's", TimeSync{Servers: tsTestOwn, Want: tsTestWant}, []component.MenuItem{remove("1")}},
		{"hardn's, no servers", TimeSync{Servers: tsTestOwn}, []component.MenuItem{remove("1")}},
		{"hardn's, differs", TimeSync{Servers: tsTestOwn, Want: []string{"192.0.2.12"}}, []component.MenuItem{set("192.0.2.12"), remove("2")}},
		{"another file's", TimeSync{Servers: other, Want: tsTestWant}, []component.MenuItem{off("Set outside hardn")}},
		{"another file's beside hardn's", TimeSync{Servers: append(slices.Clone(tsTestOwn), other...), Want: tsTestWant}, []component.MenuItem{remove("1")}},
		{"another file's beside hardn's, differs", TimeSync{Servers: append(slices.Clone(tsTestOwn), other...), Want: []string{"192.0.2.12"}}, []component.MenuItem{off("Set outside hardn"), remove("2")}},
		{"cleared by another file", TimeSync{Service: TimeTimesyncd, Servers: []TimeServer{tsTestBuilt}, Cleared: true, Want: tsTestWant}, []component.MenuItem{off("Set outside hardn")}},
		{"stopped", TimeSync{Service: TimeTimesyncd, Stopped: true, Want: tsTestWant}, []component.MenuItem{off("systemd-timesyncd is not running")}},
		{"no service", TimeSync{Service: TimeNoService, Want: tsTestWant}, []component.MenuItem{off("No time service")}},
		{"container", TimeSync{Container: true, Servers: tsTestOwn, Want: tsTestWant}, []component.MenuItem{off("The host keeps the time")}},
	}
	for _, tt := range tests {
		if got := tt.view.MenuItems(); !slices.Equal(got, tt.want) {
			t.Errorf("%s: items %+v", tt.name, got)
		}
	}
	items := TimeSync{Servers: tsTestOwn, Want: []string{"192.0.2.12"}}.MenuItems()
	if (TimeSync{}).Removes(items[0]) || !(TimeSync{}).Removes(items[1]) {
		t.Errorf("Removes over %+v", items)
	}
}

// TestTimeSyncChanges: Set servers' review is hardn.yml's servers in place
// of the service's own, DHCP's kept; with no servers there is no change;
// Remove servers' review is the servers put back and the others, or "None".
func TestTimeSyncChanges(t *testing.T) {
	if _, ok := (TimeSync{}).SetChange(); ok {
		t.Error("a change with no servers in hardn.yml")
	}
	hardn := func(v string) component.KeyValue {
		return component.KeyValue{Key: "Server", Value: v, Note: "by hardn"}
	}
	dhcp := TimeServer{Name: "192.0.2.1", From: TimeFromDHCP}
	ch, ok := TimeSync{Servers: []TimeServer{dhcp, tsTestPool, tsTestBuilt}, Want: tsTestWant}.SetChange()
	want := []component.KeyValue{hardn("192.0.2.10"), hardn("192.0.2.11"), {Key: "Server", Value: "192.0.2.1", Note: "from DHCP"}}
	if !ok || ch.Question != "Set time servers?" || ch.Step.Title.Title != "Set servers" || ch.Review.Title != "Time" || !slices.Equal(ch.Review.Items, want) {
		t.Errorf("set: %+v", ch)
	}
	rm := TimeSync{Servers: append(slices.Clone(tsTestOwn), dhcp), Restores: []TimeServer{tsTestPool}}.RemoveChange()
	if rm.Question != "Remove hardn's time servers?" || rm.Step.Title.Title != "Remove servers" ||
		!slices.Equal(rm.Review.Items, []component.KeyValue{{Key: "Server", Value: "2.debian.pool.ntp.org"}, want[2]}) {
		t.Errorf("remove: %+v", rm)
	}
	if none := (TimeSync{Servers: tsTestOwn}).RemoveChange(); !slices.Equal(none.Review.Items, []component.KeyValue{{Key: "Server", Value: "None"}}) {
		t.Errorf("remove, none left: %+v", none.Review.Items)
	}
}

// TestTimeSyncSubMenu: the screen is the row, one section and the items
// over Return.
func TestTimeSyncSubMenu(t *testing.T) {
	vm := TimeSync{Want: tsTestWant}.SubMenu()
	if got := sectionTitles(vm); !slices.Equal(got, []string{"Time"}) || vm.Title.Title != "NTP Settings" || len(vm.Menu.Items) != 1 || vm.Menu.Exit.Label != "Return" {
		t.Errorf("screen: %v, %+v", got, vm.Menu)
	}
}
