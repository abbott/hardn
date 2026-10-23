package screen

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// TestDNSScreen: the DNS screen's rows and items for each kind of reading
// (decisions 2, 3, 4): the manager's words, "reported only" in the
// warning's colour for a manager hardn only reports; the lists in use, a
// link's with the link, "None" when read and empty, Unknown when not
// read; hardn.yml's list, or the muted empty item; item 3 only with a
// list; origin/main's words and prompt.
func TestDNSScreen(t *testing.T) {
	tests := []struct {
		name          string
		d             DNS
		manager       component.KeyValue
		inUse, search component.KeyValue
		listed        []component.KeyValue
		keys          []string
	}{
		{name: "a file", d: DNS{Manager: DNSFile, Read: true, InUse: []DNSEntry{{Value: "192.0.2.1"}}, Search: []DNSEntry{{Value: "example.net"}},
			Configured: []string{"192.0.2.54", "2001:db8::54"}},
			manager: component.KeyValue{Key: "DNS Implementation", Value: "direct (/etc/resolv.conf)"},
			inUse:   component.KeyValue{Key: "Nameservers", Value: "192.0.2.1"}, search: component.KeyValue{Key: "Search Domains", Value: "example.net"},
			listed: []component.KeyValue{{Key: "Nameserver 1", Value: "192.0.2.54"}, {Key: "Nameserver 2", Value: "2001:db8::54"}},
			keys:   []string{"1", "2", "3", "4", "5", "6"}},
		{name: "systemd-resolved, a link's own", d: DNS{Manager: DNSResolved, Read: true,
			InUse:  []DNSEntry{{Value: "192.0.2.54"}, {Value: "192.0.2.1", Link: "eth0"}},
			Search: []DNSEntry{{Value: "example.net", Link: "eth0"}}, Configured: []string{"192.0.2.54"}},
			manager: component.KeyValue{Key: "DNS Implementation", Value: "systemd-resolved"},
			inUse:   component.KeyValue{Key: "Nameservers", Value: "192.0.2.54, 192.0.2.1 (eth0)"},
			search:  component.KeyValue{Key: "Search Domains", Value: "example.net (eth0)"},
			listed:  []component.KeyValue{{Key: "Nameserver 1", Value: "192.0.2.54"}}, keys: []string{"1", "2", "3", "4", "5", "6"}},
		{name: "Proxmox VE, none in use", d: DNS{Manager: DNSProxmox, Read: true},
			manager: component.KeyValue{Key: "DNS Implementation", Value: "Proxmox VE (/etc/resolv.conf)"},
			inUse:   component.KeyValue{Key: "Nameservers", Value: "None"}, search: component.KeyValue{Key: "Search Domains", Value: "None"},
			listed: []component.KeyValue{{Key: "No nameservers configured", State: component.ValueNone}}, keys: []string{"1", "2", "4", "5", "6"}},
		{name: "NetworkManager, reported only", d: DNS{Manager: DNSNetworkManager, Configured: []string{"192.0.2.54"}},
			manager: component.KeyValue{Key: "DNS Implementation", Value: "NetworkManager,", State: component.ValueWarn, Note: "reported only"},
			inUse:   component.KeyValue{Key: "Nameservers", State: component.ValueUnknown}, search: component.KeyValue{Key: "Search Domains", State: component.ValueUnknown},
			listed: []component.KeyValue{{Key: "Nameserver 1", Value: "192.0.2.54"}}, keys: []string{"1", "2", "3", "4", "5", "6"}},
		{name: "a container", d: DNS{Manager: DNSContainer},
			manager: component.KeyValue{Key: "DNS Implementation", Value: "container,", State: component.ValueWarn, Note: "reported only"},
			inUse:   component.KeyValue{Key: "Nameservers", State: component.ValueUnknown}, search: component.KeyValue{Key: "Search Domains", State: component.ValueUnknown},
			listed: []component.KeyValue{{Key: "No nameservers configured", State: component.ValueNone}}, keys: []string{"1", "2", "4", "5", "6"}},
		{name: "not read", d: DNS{},
			manager: component.KeyValue{Key: "DNS Implementation", State: component.ValueUnknown},
			inUse:   component.KeyValue{Key: "Nameservers", State: component.ValueUnknown}, search: component.KeyValue{Key: "Search Domains", State: component.ValueUnknown},
			listed: []component.KeyValue{{Key: "No nameservers configured", State: component.ValueNone}}, keys: []string{"1", "2", "4", "5", "6"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := tt.d.SubMenu()
			if vm.Title.Title != "DNS Configuration" || len(vm.Sections) != 2 || vm.Sections[0].Header.Title != "Current DNS Configuration" ||
				vm.Sections[1].Header.Title != "Configured Nameservers" || vm.Prompt != "Enter selection" || vm.Menu.Exit.Label != "Return" {
				t.Fatalf("the screen's frame: %+v", vm)
			}
			if got, want := vm.Sections[0].Rows.Items, []component.KeyValue{tt.manager, tt.inUse, tt.search}; !slices.Equal(got, want) {
				t.Errorf("current rows %+v\nwant %+v", got, want)
			}
			if got := vm.Sections[1].Rows.Items; !slices.Equal(got, tt.listed) {
				t.Errorf("configured rows %+v\nwant %+v", got, tt.listed)
			}
			var keys []string
			for _, it := range vm.Menu.Items {
				keys = append(keys, it.Key)
			}
			if !slices.Equal(keys, tt.keys) {
				t.Errorf("items %q, want %q", keys, tt.keys)
			}
		})
	}
}

// TestDNSWords: origin/main's items and descriptions, its presets, the
// workflows' title bars and the review's rows.
func TestDNSWords(t *testing.T) {
	d := DNS{Manager: DNSFile, Read: true, Search: []DNSEntry{{Value: "example.net"}}, Configured: []string{"192.0.2.54"}}
	c := component.NewCtx(term.Fixed(80, colorprofile.NoTTY, false, true))
	got := ansi.Strip(d.SubMenu().Render(c))
	for _, w := range []string{"1) Configure DNS        Apply nameserver settings from con...", "2) Add nameserver       Add a new DNS server to configuration",
		"3) Remove nameserver    Remove a DNS server from configura...", "4) Use Cloudflare DNS   Set nameservers to 1.1.1.1, 1.0.0.1",
		"5) Use Google DNS       Set nameservers to 8.8.8.8, 8.8.4.4", "6) Use Quad9 DNS        Set nameservers to 9.9.9.9, 149.11...",
		"0) Return", "> Enter selection [0-6 or q]:"} {
		if !strings.Contains(got, w) {
			t.Errorf("%q not drawn:\n%s", w, got)
		}
	}
	presets := DNSPresets()
	presets[0].Nameservers[0] = "192.0.2.9"
	if DNSPresets()[0].Nameservers[0] != "1.1.1.1" {
		t.Error("DNSPresets shares its lists")
	}
	if s := d.Step("Add nameserver"); s.Title.Title != "Add nameserver" || !slices.Equal(s.Title.Parents, []string{"DNS Configuration"}) {
		t.Errorf("Step = %+v", s)
	}
	r := d.Review([]string{"1.1.1.1", "1.0.0.1"})
	if want := []component.KeyValue{{Key: "Nameservers", Value: "1.1.1.1, 1.0.0.1"}, {Key: "Search Domains", Value: "example.net"}}; r.Title != "DNS Configuration" || !slices.Equal(r.Items, want) {
		t.Errorf("Review = %+v", r)
	}
	if r := (DNS{}).Review([]string{"1.1.1.1"}); r.Items[1].State != component.ValueUnknown {
		t.Errorf("the review of a host not read: %+v", r)
	}
}
