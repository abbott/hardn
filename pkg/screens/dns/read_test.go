package dns_test

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"testing"

	hostdns "github.com/abbott/hardn/pkg/host/dns"
	dnsscreen "github.com/abbott/hardn/pkg/screens/dns"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// TestReadResolved: on systemd-resolved the nameservers in use are the
// global list, then each delegate's and each link's, with its name as the
// DNS findings give it; the search domains alike, less the route-only
// ones ("~"); and after Configure DNS the global list is hardn.yml's,
// eth0's still after it.
func TestReadResolved(t *testing.T) {
	ctx := context.Background()
	h, cfg := newHost(t, ubuntu2604, configured)
	h.global = []string{"192.0.2.54"}
	h.links = "Delegate corp: 192.0.2.9\nLink 2 (eth0): 192.0.2.1\n"
	h.domains = "Delegate corp: ~corp.example\nLink 2 (eth0): example.net ~.\n"
	v := dnsscreen.View(ctx, dnsscreen.Options{System: h, Facts: h.facts, Config: cfg})
	want := screen.DNS{Manager: screen.DNSResolved, Read: true, Configured: configured,
		InUse:  []screen.DNSEntry{{Value: "192.0.2.54"}, {Value: "192.0.2.9", Link: "delegate corp"}, {Value: "192.0.2.1", Link: "eth0"}},
		Search: []screen.DNSEntry{{Value: "example.net", Link: "eth0"}}}
	if fmt.Sprint(v) != fmt.Sprint(want) {
		t.Errorf("View = %+v\nwant   %+v", v, want)
	}
	h, cfg = newHost(t, ubuntu2604, configured)
	if p := play(h, cfg, wide, false, "1", "y", "0"); p.err != nil {
		t.Fatal(p.err)
	}
	v = dnsscreen.View(ctx, dnsscreen.Options{System: h, Facts: h.facts, Config: cfg})
	if want := []screen.DNSEntry{{Value: "192.0.2.54"}, {Value: "2001:db8::54"}, {Value: "192.0.2.1", Link: "eth0"}}; !slices.Equal(v.InUse, want) {
		t.Errorf("after Configure DNS, in use %+v; want %+v", v.InUse, want)
	}
}

// TestAddCheckIsValidate: Add nameserver's check refuses exactly the
// entries dns.Validate refuses after the configured list, for each kind of
// manager it writes, each kind of refusal in its own words (decision 9):
// none, origin/main's "Nameserver cannot be empty"; no address, its
// "Invalid IP address format"; one configured, its "Nameserver <ip> is
// already configured", a warning; one a resolver cannot use, "Not a
// usable nameserver address"; one more than a file takes. The oracle is
// dns.Validate's own refusal, read from its words.
func TestAddCheckIsValidate(t *testing.T) {
	entries := []string{"999.1.1.1", "192.0.2.54", "192.0.2.53", "2001:db8::53", "2001:DB8::54", "web-01.example.net",
		"192.0.2.53:53", "", "fe80::1", "fe80::1%eth0", "::ffff:192.0.2.53", "::", "0.0.0.0", "0.1.2.3", "255.255.255.255",
		"224.0.0.251", "ff02::1", "127.0.0.53", "127.0.0.54", "169.254.0.53", "192.0.2.054", "1.1.1.1"}
	lists := [][]string{nil, configured, {"192.0.2.51", "192.0.2.52", "192.0.2.54"}}
	for _, m := range []hostdns.Manager{hostdns.ManagerFile, hostdns.ManagerProxmox, hostdns.ManagerResolved} {
		for _, list := range lists {
			check := dnsscreen.AddCheck(list, m)
			for _, entry := range entries {
				_, verr := hostdns.Validate(hostdns.Config{Nameservers: append(slices.Clone(list), entry)}, m)
				want := kindOf(entry, list, m)
				got := check(entry)
				switch {
				case (got == nil) != (verr == nil):
					t.Errorf("%v %q + %q: check %v, dns.Validate %v", m, list, entry, got, verr)
				case got != nil && got.Error() != want:
					t.Errorf("%v %q + %q: %q; want %q (dns.Validate: %v)", m, list, entry, got, want, verr)
				case got != nil && strings.HasSuffix(want, "is already configured") != isWarn(got):
					t.Errorf("%v %q + %q: refused as a warning %t; want %t", m, list, entry, isWarn(got), !isWarn(got))
				}
			}
		}
	}
}

// isWarn reports whether err refuses with a warning (nav.Warn), as a form
// draws it: the refusal's kind survives RefuseFor.
func isWarn(err error) bool {
	return nav.RefuseFor(err) == nav.Warn(err.Error())
}

// kindOf is the words of entry's refusal after list for manager m, from
// dns.Validate's own refusals, the entry's first: none; not an address
// alone; twice with a configured one; unusable alone; then the count.
func kindOf(entry string, list []string, m hostdns.Manager) string {
	if entry == "" {
		return "Nameserver cannot be empty"
	}
	if _, err := hostdns.Validate(hostdns.Config{Nameservers: []string{entry}}, m); err != nil {
		if strings.Contains(err.Error(), "not an IPv4 or IPv6 address") {
			return "Invalid IP address format"
		}
		return "Not a usable nameserver address"
	}
	for _, c := range list {
		if _, err := hostdns.Validate(hostdns.Config{Nameservers: []string{c, entry}}, m); err != nil && strings.Contains(err.Error(), "given twice") {
			return "Nameserver " + netip.MustParseAddr(entry).String() + " is already configured"
		}
	}
	if _, err := hostdns.Validate(hostdns.Config{Nameservers: append(slices.Clone(list), entry)}, m); err != nil {
		return "/etc/resolv.conf takes 3 nameservers at most"
	}
	return ""
}
