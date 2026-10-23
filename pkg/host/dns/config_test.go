package dns_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/dns"
)

// TestValidate: an empty list (V19), anything that is not a plain address,
// a repeat, more than glibc and musl read, and a bad search domain are
// refused; addresses come back in netip's form.
func TestValidate(t *testing.T) {
	long := strings.Repeat("a", 60) + ".example.net"
	tests := []struct {
		name    string
		c       dns.Config
		m       dns.Manager
		want    []string // nameservers, when ok
		wantErr error
	}{
		{"canonical", dns.Config{Nameservers: []string{ns1, "2001:DB8:0::0053"}}, dns.ManagerFile, []string{ns1, ns6}, nil},
		{"empty list (V19)", dns.Config{}, dns.ManagerResolved, nil, dns.ErrNoNameservers},
		{"not an address", dns.Config{Nameservers: []string{"web-01.example.net"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"with a port", dns.Config{Nameservers: []string{"192.0.2.53:53"}}, dns.ManagerResolved, nil, dns.ErrInvalid},
		{"zone", dns.Config{Nameservers: []string{"fe80::1%eth0"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"IPv4-mapped", dns.Config{Nameservers: []string{"::ffff:192.0.2.53"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"unspecified", dns.Config{Nameservers: []string{"0.0.0.0"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"multicast", dns.Config{Nameservers: []string{"224.0.0.251"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"IPv6 link-local, no zone", dns.Config{Nameservers: []string{"fe80::1"}}, dns.ManagerResolved, nil, dns.ErrInvalid},
		{"limited broadcast", dns.Config{Nameservers: []string{"255.255.255.255"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"this network", dns.Config{Nameservers: []string{"0.1.2.3"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"IPv4 link-local", dns.Config{Nameservers: []string{"169.254.169.253"}}, dns.ManagerFile, []string{"169.254.169.253"}, nil},
		{"repeated", dns.Config{Nameservers: []string{ns6, "2001:db8:0:0::53"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"four in a file", dns.Config{Nameservers: []string{ns1, ns2, ns6, "192.0.2.55"}}, dns.ManagerProxmox, nil, dns.ErrInvalid},
		{"four for resolved", dns.Config{Nameservers: []string{ns1, ns2, ns6, "192.0.2.55"}}, dns.ManagerResolved, []string{ns1, ns2, ns6, "192.0.2.55"}, nil},
		{"resolved's stub", dns.Config{Nameservers: []string{"127.0.0.53"}}, dns.ManagerResolved, nil, dns.ErrInvalid},
		{"a local resolver in a file", dns.Config{Nameservers: []string{"127.0.0.53"}}, dns.ManagerFile, []string{"127.0.0.53"}, nil},
		{"search", dns.Config{Nameservers: []string{ns1}, Search: []string{"example.net", "lan"}, ReplaceSearch: true}, dns.ManagerFile, []string{ns1}, nil},
		{"search, leading hyphen", dns.Config{Nameservers: []string{ns1}, Search: []string{"-x.example.net"}, ReplaceSearch: true}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"search, empty label", dns.Config{Nameservers: []string{ns1}, Search: []string{"example..net"}, ReplaceSearch: true}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"search, underscore", dns.Config{Nameservers: []string{ns1}, Search: []string{"a_b.example.net"}, ReplaceSearch: true}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"search, route-only", dns.Config{Nameservers: []string{ns1}, Search: []string{"~example.net"}, ReplaceSearch: true}, dns.ManagerResolved, nil, dns.ErrInvalid},
		{"search, trailing dot", dns.Config{Nameservers: []string{ns1}, Search: []string{"example.net."}, ReplaceSearch: true}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"search, repeated", dns.Config{Nameservers: []string{ns1}, Search: []string{"lan", "lan"}, ReplaceSearch: true}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"search, repeated in another case", dns.Config{Nameservers: []string{ns1}, Search: []string{"example.net", "Example.NET"}, ReplaceSearch: true}, dns.ManagerResolved, nil, dns.ErrInvalid},
		{"search, two on Proxmox", dns.Config{Nameservers: []string{ns1}, Search: []string{"example.net", "lan"}, ReplaceSearch: true}, dns.ManagerProxmox, nil, dns.ErrInvalid},
		{"search line too long for musl", dns.Config{Nameservers: []string{ns1}, Search: []string{long, "b" + long, "c" + long, "d" + long}, ReplaceSearch: true}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"long search for resolved", dns.Config{Nameservers: []string{ns1}, Search: []string{long, "b" + long, "c" + long, "d" + long}, ReplaceSearch: true}, dns.ManagerResolved, []string{ns1}, nil},
		{"search given without ReplaceSearch", dns.Config{Nameservers: []string{ns1}, Search: []string{"lan"}}, dns.ManagerFile, nil, dns.ErrInvalid},
		{"search kept", dns.Config{Nameservers: []string{ns1}}, dns.ManagerResolved, []string{ns1}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := dns.Validate(tt.c, tt.m)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr != nil && !errors.Is(err, dns.ErrInvalid)) {
				t.Fatalf("err %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(got.Nameservers, tt.want) {
				t.Errorf("nameservers %q, want %q", got.Nameservers, tt.want)
			}
			if err == nil && !tt.c.ReplaceSearch && got.Search != nil {
				t.Errorf("search %q kept without ReplaceSearch", got.Search)
			}
		})
	}
}

// TestParseResolvConf reads keywords as glibc does: at the start of the
// line, and the last search or domain line wins.
func TestParseResolvConf(t *testing.T) {
	ns, search := dns.ParseResolvConf([]byte("# c\nsearch a.example.net b.example.net\nnameserver 192.0.2.1 # first\n nameserver 192.0.2.9\nnameserver\tfe80::1\ndomain example.net\n"))
	if !slices.Equal(ns, []string{"192.0.2.1", "fe80::1"}) || !slices.Equal(search, []string{"example.net"}) {
		t.Errorf("got %q %q", ns, search)
	}
}

// TestCodes pins the words and the numbers of the enums: a kept plan
// stores the numbers.
func TestCodes(t *testing.T) {
	for _, tt := range []struct{ got, want string }{
		{dns.ManagerUnknown.String(), "unknown"}, {dns.ManagerFile.String(), "file"}, {dns.ManagerProxmox.String(), "proxmox"},
		{dns.ManagerResolved.String(), "systemd-resolved"}, {dns.ManagerResolvconf.String(), "resolvconf"},
		{dns.ManagerNetworkManager.String(), "networkmanager"}, {dns.ManagerForeign.String(), "foreign"},
		{dns.StepUDHCPC.String(), "udhcpc-no-resolv"}, {dns.StepResolvConf.String(), "write-resolv-conf"},
		{dns.StepDropIn.String(), "write-drop-in"}, {dns.StepReload.String(), "reload-resolved"}, {dns.Step(0).String(), "unknown"},
		{dns.FindingPerLink.String(), "dns-per-link"}, {dns.FindingDHCPClient.String(), "dns-dhcp-client"},
		{dns.FindingUDHCPC.String(), "dns-udhcpc"}, {dns.FindingProxmox.String(), "dns-proxmox-node"},
		{dns.FindingUDHCPCUnknown.String(), "dns-udhcpc-unknown"}, {dns.FindingKind(0).String(), "unknown"},
	} {
		if tt.got != tt.want {
			t.Errorf("%q, want %q", tt.got, tt.want)
		}
	}
	for _, tt := range []struct{ got, want int }{
		{int(dns.ManagerUnknown), 0}, {int(dns.ManagerFile), 1}, {int(dns.ManagerProxmox), 2}, {int(dns.ManagerResolved), 3},
		{int(dns.ManagerResolvconf), 4}, {int(dns.ManagerNetworkManager), 5}, {int(dns.ManagerForeign), 6},
		{int(dns.StepUDHCPC), 1}, {int(dns.StepResolvConf), 2}, {int(dns.StepDropIn), 3}, {int(dns.StepReload), 4},
		{int(dns.FindingPerLink), 1}, {int(dns.FindingDHCPClient), 2}, {int(dns.FindingUDHCPC), 3}, {int(dns.FindingProxmox), 4},
		{int(dns.FindingUDHCPCUnknown), 5},
		{int(dns.EntryUnknown), 0}, {int(dns.EntryAbsent), 1}, {int(dns.EntryFile), 2}, {int(dns.EntrySymlink), 3}, {int(dns.EntryOther), 4},
	} {
		if tt.got != tt.want {
			t.Errorf("an enum's number is %d, want %d: a kept plan would read differently", tt.got, tt.want)
		}
	}
}
