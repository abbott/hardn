package screen

import (
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
)

// sectionTitles are a sub-menu's sections' titles, in order.
func sectionTitles(vm SubMenu) []string {
	var out []string
	for _, s := range vm.Sections {
		out = append(out, s.Header.Title)
	}
	return out
}

// TestFirewallPortsRows: each port's row by how it stands against the rules,
// its process beside it; a rule's port with nothing listening the one
// warning; without ss the one row that says so; with no port a muted line.
func TestFirewallPortsRows(t *testing.T) {
	got := FirewallPorts{Ports: []FirewallPort{
		{Port: "22", Proto: "tcp", Process: "sshd", Reach: PortOpen},
		{Port: "9090", Proto: "tcp", Process: "prometheus", Reach: PortSome},
		{Port: "5353", Proto: "udp", Process: "avahi-daemon", Reach: PortBlocked},
		{Port: "5432", Proto: "tcp", Process: "postgres", Reach: PortLocal},
		{Port: "8080", Proto: "tcp", Reach: PortUnused},
		{Port: "80", Proto: "tcp", Reach: PortUnfiltered},
	}}.Rows()
	want := []component.KeyValue{
		{Key: "22/tcp", Value: "Open", Extra: "sshd"},
		{Key: "9090/tcp", Value: "Some sources", Extra: "prometheus"},
		{Key: "5353/udp", Value: "Blocked", Extra: "avahi-daemon"},
		{Key: "5432/tcp", Value: "Local only", Extra: "postgres"},
		{Key: "8080/tcp", Value: "Open", Note: "nothing listening", State: component.ValueWarn},
		{Key: "80/tcp", Value: "Not filtered"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows %+v", got)
	}
	noSS := []component.KeyValue{{Key: "Listening ports", Value: "ss not installed", State: component.ValueUnknown}}
	if got := (FirewallPorts{NoSS: true, Ports: []FirewallPort{{Port: "22", Proto: "tcp"}}}).Rows(); !slices.Equal(got, noSS) {
		t.Errorf("no ss: %+v", got)
	}
	if got := (Listeners{NoSS: true}).Rows(); !slices.Equal(got, noSS) {
		t.Errorf("no ss, listeners: %+v", got)
	}
	none := []component.KeyValue{{Key: "Nothing is listening", State: component.ValueNone}}
	if !slices.Equal(FirewallPorts{}.Rows(), none) || !slices.Equal(Listeners{NotRoot: true}.Rows(), none) {
		t.Errorf("no port: %+v, %+v", FirewallPorts{}.Rows(), Listeners{NotRoot: true}.Rows())
	}
}

// TestListenersRows: a row per socket, its address and its process; as a user,
// the last line says why no process shows.
func TestListenersRows(t *testing.T) {
	list := []Listener{{Port: "22", Proto: "tcp", Address: "[::]", Process: "sshd"}, {Port: "5432", Proto: "tcp", Address: "127.0.0.1"}}
	want := []component.KeyValue{{Key: "22/tcp", Value: "[::]", Extra: "sshd"}, {Key: "5432/tcp", Value: "127.0.0.1"}}
	if got := (Listeners{List: list}).Rows(); !slices.Equal(got, want) {
		t.Errorf("rows %+v", got)
	}
	want = append(want, component.KeyValue{Key: "Run hardn as root to see every process.", State: component.ValueNone})
	if got := (Listeners{List: list, NotRoot: true}).Rows(); !slices.Equal(got, want) {
		t.Errorf("as a user: %+v", got)
	}
}

// TestPortsSections: the section shows only where it has rows, so every screen
// drawn today is unchanged: Ports after Rules on ufw's screen and after
// firewalld's on its own, Listening Ports after Host and IPs.
func TestPortsSections(t *testing.T) {
	rows := FirewallPorts{NoSS: true}.Rows()
	fw := fwTestLockout
	if got := sectionTitles(fw.SubMenu()); slices.Contains(got, "Ports") {
		t.Errorf("ufw, no ports: %v", got)
	}
	fw.Ports, fw.Profiles = rows, fwTestProfiles.Rows
	if got := sectionTitles(fw.SubMenu()); !slices.Equal(got, []string{"ufw", "SSH", "Rules", "Ports", "Application Profiles"}) {
		t.Errorf("ufw: %v", got)
	}
	el := Firewalld{Rows: fwTestUFW}
	if got := sectionTitles(el.SubMenu()); !slices.Equal(got, []string{"firewalld"}) {
		t.Errorf("firewalld, no ports: %v", got)
	}
	el.Ports = rows
	if got := sectionTitles(el.SubMenu()); !slices.Equal(got, []string{"firewalld", "Ports"}) {
		t.Errorf("firewalld: %v", got)
	}
	seven := []string{"OS and Kernel", "Host and IPs", "Users", "CPU and Load", "Disk", "Memory", "Last Login"}
	if got := sectionTitles(SystemDetails{}.SubMenu()); !slices.Equal(got, seven) {
		t.Errorf("System Details, no ports: %v", got)
	}
	if got := sectionTitles(SystemDetails{Ports: rows}.SubMenu()); !slices.Equal(got, slices.Insert(seven, 2, "Listening Ports")) {
		t.Errorf("System Details: %v", got)
	}
}
