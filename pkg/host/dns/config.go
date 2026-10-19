package dns

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Config is the DNS configuration asked for.
type Config struct {
	// Nameservers are IPv4 or IPv6 addresses, in order; at least one.
	Nameservers []string
	// Search is the search domain list, given only with ReplaceSearch:
	// Validate refuses one without it.
	Search []string
	// ReplaceSearch: replace the host's search domains with Search (an
	// empty Search removes them). Otherwise they are kept as they are.
	ReplaceSearch bool
	// UDHCPCNoResolv: on Alpine, set RESOLV_CONF="no" in UDHCPCConf so
	// udhcpc stops rewriting /etc/resolv.conf on each lease. Only ever as
	// this explicit request; refused on any other host.
	UDHCPCNoResolv bool
}

// resolvedStubs are systemd-resolved's own listeners: a global server
// there would send queries back to it.
var resolvedStubs = []netip.Addr{netip.MustParseAddr("127.0.0.53"), netip.MustParseAddr("127.0.0.54")}

// thisNetwork is never a destination (RFC 6890); a datagram to the limited
// broadcast address needs SO_BROADCAST (ip(7)).
var (
	thisNetwork  = netip.MustParsePrefix("0.0.0.0/8")
	limitedBcast = netip.MustParseAddr("255.255.255.255")
)

// Validate returns c in canonical form for a host whose /etc/resolv.conf
// manager is m, or an error wrapping ErrInvalid. It refuses no nameserver
// (ErrNoNameservers; origin/main wrote "DNS=" for an empty list, V19), an
// entry that is not a plain IPv4 or IPv6 address (a zone, an IPv4-mapped
// IPv6 address, the unspecified address, 0.0.0.0/8, the limited broadcast
// address, multicast and IPv6 link-local, which needs the zone a plain
// address lacks, are refused too), a repeated one, a search domain that is
// not a host name of letters, digits and hyphens or repeats another in any
// case, and Search without ReplaceSearch. For a file (ManagerFile,
// ManagerProxmox) it refuses more than MaxNameservers and a search line
// longer than MaxLine; on Proxmox VE more than one search domain, since its
// node DNS settings keep only the first; on systemd-resolved its own stub
// addresses. Addresses come back as netip prints them; search domains as
// given.
func Validate(c Config, m Manager) (Config, error) {
	if len(c.Nameservers) == 0 {
		return Config{}, fmt.Errorf("dns: %w", ErrNoNameservers)
	}
	file := m == ManagerFile || m == ManagerProxmox
	if file && len(c.Nameservers) > MaxNameservers {
		return Config{}, fmt.Errorf("dns: %d nameservers; /etc/resolv.conf's resolvers use the first %d: %w", len(c.Nameservers), MaxNameservers, ErrInvalid)
	}
	out := Config{Search: slices.Clone(c.Search), ReplaceSearch: c.ReplaceSearch, UDHCPCNoResolv: c.UDHCPCNoResolv}
	for _, s := range c.Nameservers {
		a, err := netip.ParseAddr(s)
		switch {
		case err != nil:
			return Config{}, fmt.Errorf("dns: nameserver %q: not an IPv4 or IPv6 address: %w", s, ErrInvalid)
		case a.Zone() != "", a.Is4In6(), a.IsUnspecified(), a.IsMulticast():
			return Config{}, fmt.Errorf("dns: nameserver %q: a zone, an IPv4-mapped, unspecified or multicast address: %w", s, ErrInvalid)
		case thisNetwork.Contains(a), a == limitedBcast, a.Is6() && a.IsLinkLocalUnicast():
			return Config{}, fmt.Errorf("dns: nameserver %q: an address in 0.0.0.0/8, the broadcast address or IPv6 link-local, which a resolver cannot query as given: %w", s, ErrInvalid)
		case m == ManagerResolved && slices.Contains(resolvedStubs, a):
			return Config{}, fmt.Errorf("dns: nameserver %s is systemd-resolved's own stub: %w", a, ErrInvalid)
		case slices.Contains(out.Nameservers, a.String()):
			return Config{}, fmt.Errorf("dns: nameserver %s given twice: %w", a, ErrInvalid)
		}
		out.Nameservers = append(out.Nameservers, a.String())
	}
	if !c.ReplaceSearch {
		if len(c.Search) > 0 {
			return Config{}, fmt.Errorf("dns: search domains given without ReplaceSearch, which would leave them unchanged: %w", ErrInvalid)
		}
		out.Search = nil
		return out, nil
	}
	for i, d := range c.Search {
		if err := domainName(d); err != nil {
			return Config{}, fmt.Errorf("dns: search domain %q: %w: %w", d, err, ErrInvalid)
		}
		if slices.ContainsFunc(c.Search[:i], func(e string) bool { return strings.EqualFold(e, d) }) {
			return Config{}, fmt.Errorf("dns: search domain %q given twice: %w", d, ErrInvalid)
		}
	}
	switch line := len(searchLine(c.Search)) + 1; {
	case m == ManagerProxmox && len(c.Search) > 1:
		return Config{}, fmt.Errorf("dns: %d search domains; Proxmox VE's node DNS settings keep one: %w", len(c.Search), ErrInvalid)
	case file && line > MaxLine:
		return Config{}, fmt.Errorf("dns: the search line is %d bytes; musl skips lines over %d: %w", line, MaxLine, ErrInvalid)
	}
	return out, nil
}

// domainName accepts a host name (RFC 1123): dot-separated labels of 1 to
// 63 letters, digits and hyphens, not starting or ending with a hyphen, 253
// bytes at most, with no trailing dot. systemd-resolved's route-only "~"
// domains are not search domains and are refused.
func domainName(d string) error {
	if d == "" || len(d) > 253 {
		return fmt.Errorf("length %d", len(d))
	}
	for _, l := range strings.Split(d, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return fmt.Errorf("label %q", l)
		}
		if strings.Trim(l, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-") != "" {
			return fmt.Errorf("label %q: only letters, digits and hyphens", l)
		}
	}
	return nil
}

func searchLine(domains []string) string {
	return "search " + strings.Join(domains, " ")
}
