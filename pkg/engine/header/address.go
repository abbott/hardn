package header

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// The kernel's network files the probe reads (linux net/ipv4/fib_trie.c,
// net/ipv6/ip6_fib.c, net/ipv6/addrconf.c; docs/engine/header.md §3).
const (
	RouteFile  = "/proc/net/route"      // the IPv4 main table
	Route6File = "/proc/net/ipv6_route" // the IPv6 routes, every table
	Inet6File  = "/proc/net/if_inet6"   // the IPv6 addresses, by interface
)

// Route flags (linux include/uapi/linux/route.h) and IPv6 address flags
// (include/uapi/linux/if_addr.h, the low byte that if_inet6 prints).
const (
	rtfUp     = 0x0001
	rtfReject = 0x0200

	ifaTemporary  = 0x01
	ifaDADFailed  = 0x08
	ifaDeprecated = 0x20
	ifaTentative  = 0x40
	ifaPermanent  = 0x80
)

// Address is the host's primary address in each family.
type Address struct {
	IPv4, IPv6 Family
}

// Family is one address family's default route and the primary address
// of the interface it leaves by.
type Family struct {
	// Iface is the interface of the default route with the lowest
	// metric; "" when the family has no default route.
	Iface string
	// Addr is Iface's primary address; the zero Addr when there is no
	// default route, or the interface has no global address of the
	// family.
	Addr netip.Addr
	// Err is why the route or the address is not established; nil when
	// both are.
	Err error
}

// Primary is the address the header shows: IPv4's, else IPv6's. known is
// false when neither is an address and one of them is not established;
// with known true and no address, the host has no default route in either
// family, or no address where it has one.
func (a Address) Primary() (addr netip.Addr, known bool) {
	switch {
	case a.IPv4.Addr.IsValid():
		return a.IPv4.Addr, true
	case a.IPv6.Addr.IsValid():
		return a.IPv6.Addr, true
	}
	return netip.Addr{}, a.IPv4.Err == nil && a.IPv6.Err == nil
}

// readAddress reads both families. IPv6's files are absent from a kernel
// with IPv6 off, which is no IPv6 default route, but only where the IPv4
// table shows that /proc is there.
func readAddress(ctx context.Context, sys host.System) Address {
	v4, procUp := readIPv4(ctx, sys)
	return Address{IPv4: v4, IPv6: readIPv6(ctx, sys, procUp)}
}

// route is a default route as the kernel lists it.
type route struct {
	iface   string
	gateway netip.Addr // the zero Addr for a route with no gateway
	metric  uint64
}

// lowest is the route of routes with the lowest metric. Two of them on
// different interfaces are a conflict: which one the kernel uses is not
// in the file (for IPv4 it depends on the gateways' reachability).
func lowest(routes []route) (route, error) {
	if len(routes) == 0 {
		return route{}, nil
	}
	slices.SortStableFunc(routes, func(a, b route) int {
		switch {
		case a.metric < b.metric:
			return -1
		case a.metric > b.metric:
			return 1
		}
		return 0
	})
	for _, r := range routes[1:] {
		if r.metric == routes[0].metric && r.iface != routes[0].iface {
			return route{}, fmt.Errorf("default routes on %s and %s, metric %d: %w", routes[0].iface, r.iface, r.metric, detect.ErrConflict)
		}
	}
	return routes[0], nil
}

// readIPv4 reads the default route from /proc/net/route, then its
// interface's addresses with ip: /proc has no IPv4 address by interface.
// procUp reports whether /proc/net/route was read.
func readIPv4(ctx context.Context, sys host.System) (fam Family, procUp bool) {
	data, err := sys.ReadFile(ctx, RouteFile)
	if err != nil {
		return Family{Err: fmt.Errorf("header: ipv4: %w", err)}, false
	}
	r, err := defaultRoute4(data)
	switch {
	case err != nil:
		return Family{Err: fmt.Errorf("header: ipv4: %s: %w", RouteFile, err)}, true
	case r.iface == "":
		return Family{}, true
	}
	res, err := sys.Run(ctx, "ip", "-4", "addr", "show", "dev", r.iface)
	if err != nil {
		return Family{Iface: r.iface, Err: fmt.Errorf("header: ipv4: %w", err)}, true
	}
	addr, err := primary4(res.Stdout, r.gateway)
	if err != nil {
		return Family{Iface: r.iface, Err: fmt.Errorf("header: ipv4: ip -4 addr show dev %s: %w", r.iface, err)}, true
	}
	return Family{Iface: r.iface, Addr: addr}, true
}

// defaultRoute4 finds the default route in /proc/net/route: a header line,
// then per route the interface ("*" for none), the destination, the
// gateway, the flags, the reference count, the use count, the metric, the
// mask and three more, tab-separated; the addresses as the kernel's
// __be32 printed with %08X, the flags with %04X, the metric with %u. A
// default route has destination and mask 0. Rejecting routes
// (unreachable, prohibit) and routes with no interface (blackhole) send
// nothing, and are skipped.
func defaultRoute4(data []byte) (route, error) {
	lines := strings.Split(string(data), "\n")
	if f := strings.Fields(lines[0]); len(f) < 8 || f[0] != "Iface" {
		return route{}, fmt.Errorf("no header line: %w", detect.ErrUnrecognised)
	}
	var routes []route
	for _, line := range lines[1:] {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) < 8 {
			return route{}, fmt.Errorf("%q: %w", strings.TrimSpace(line), detect.ErrUnrecognised)
		}
		dst, e1 := strconv.ParseUint(f[1], 16, 32)
		gw, e2 := strconv.ParseUint(f[2], 16, 32)
		flags, e3 := strconv.ParseUint(f[3], 16, 32)
		metric, e4 := strconv.ParseUint(f[6], 10, 32)
		mask, e5 := strconv.ParseUint(f[7], 16, 32)
		if errors.Join(e1, e2, e3, e4, e5) != nil {
			return route{}, fmt.Errorf("%q: %w", strings.TrimSpace(line), detect.ErrUnrecognised)
		}
		if dst != 0 || mask != 0 || flags&rtfUp == 0 || flags&rtfReject != 0 || f[0] == "*" {
			continue
		}
		r := route{iface: f[0], metric: metric}
		if gw != 0 {
			var b [4]byte
			binary.NativeEndian.PutUint32(b[:], uint32(gw)) // the __be32's bytes, in memory order
			r.gateway = netip.AddrFrom4(b)
		}
		routes = append(routes, r)
	}
	return lowest(routes)
}

// net4 is an IPv4 address of an interface with the network it reaches
// directly: its own prefix, or its peer's.
type net4 struct {
	addr netip.Addr
	net  netip.Prefix
}

// primary4 is the primary address in ip's listing of one interface's IPv4
// addresses (iproute2's and BusyBox's: per address, a line "inet
// <address>/<length>" or "inet <address> peer <address>/<length>", then
// brd, "scope <scope>", the flags and the label). As the kernel picks the
// source of a route through a gateway (inet_select_addr): the first
// address of global scope that is not secondary and is in the gateway's
// network, else the first such address; tentative and dadfailed
// addresses are not usable, and are skipped.
func primary4(out []byte, gateway netip.Addr) (netip.Addr, error) {
	var cands []net4
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "inet" {
			continue
		}
		a, err := parseNet4(f)
		scope := slices.Index(f, "scope")
		if err != nil || scope < 0 || scope == len(f)-1 {
			return netip.Addr{}, fmt.Errorf("%q: %w", strings.TrimSpace(line), detect.ErrUnrecognised)
		}
		if f[scope+1] != "global" || slices.ContainsFunc(f[scope+2:], func(w string) bool {
			return w == "secondary" || w == "tentative" || w == "dadfailed"
		}) {
			continue
		}
		cands = append(cands, a)
	}
	for _, c := range cands {
		if gateway.IsValid() && c.net.Contains(gateway) {
			return c.addr, nil
		}
	}
	if len(cands) > 0 {
		return cands[0].addr, nil
	}
	return netip.Addr{}, nil
}

// parseNet4 reads an inet line's address and network.
func parseNet4(f []string) (net4, error) {
	if p, err := netip.ParsePrefix(f[1]); err == nil && p.Addr().Is4() {
		return net4{addr: p.Addr(), net: p.Masked()}, nil
	}
	a, err := netip.ParseAddr(f[1])
	if err != nil || !a.Is4() || len(f) < 4 || f[2] != "peer" {
		return net4{}, detect.ErrUnrecognised
	}
	peer, err := netip.ParsePrefix(f[3])
	if err != nil || !peer.Addr().Is4() {
		return net4{}, detect.ErrUnrecognised
	}
	return net4{addr: a, net: peer.Masked()}, nil
}

// readIPv6 reads the default route from /proc/net/ipv6_route and its
// interface's addresses from /proc/net/if_inet6.
func readIPv6(ctx context.Context, sys host.System, procUp bool) Family {
	data, err := sys.ReadFile(ctx, Route6File)
	switch {
	case missing(err) && procUp:
		return Family{} // IPv6 off in this kernel
	case err != nil:
		return Family{Err: fmt.Errorf("header: ipv6: %w", err)}
	}
	r, err := defaultRoute6(data)
	switch {
	case err != nil:
		return Family{Err: fmt.Errorf("header: ipv6: %s: %w", Route6File, err)}
	case r.iface == "":
		return Family{}
	}
	data, err = sys.ReadFile(ctx, Inet6File)
	if err != nil {
		return Family{Iface: r.iface, Err: fmt.Errorf("header: ipv6: %w", err)}
	}
	addr, err := primary6(data, r.iface)
	if err != nil {
		return Family{Iface: r.iface, Err: fmt.Errorf("header: ipv6: %s: %w", Inet6File, err)}
	}
	return Family{Iface: r.iface, Addr: addr}
}

// defaultRoute6 finds the default route in /proc/net/ipv6_route: per
// route, the destination and its length, the source and its length, the
// next hop, the metric, the reference count, the use count and the flags,
// each in hex, then the device ("" for none). A default route has
// destination ::/0 and no source prefix. Rejecting routes, and routes on
// lo or on no device, send nothing out, and are skipped.
func defaultRoute6(data []byte) (route, error) {
	var routes []route
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if (len(f) != 9 && len(f) != 10) || !hexAddr(f[0]) || !hexAddr(f[2]) || !hexAddr(f[4]) {
			return route{}, fmt.Errorf("%q: %w", strings.TrimSpace(line), detect.ErrUnrecognised)
		}
		metric, merr := strconv.ParseUint(f[5], 16, 32)
		flags, ferr := strconv.ParseUint(f[8], 16, 32)
		if merr != nil || ferr != nil {
			return route{}, fmt.Errorf("%q: %w", strings.TrimSpace(line), detect.ErrUnrecognised)
		}
		dev := at(f, 9)
		if f[0] != zero6 || f[1] != "00" || f[3] != "00" || flags&rtfUp == 0 || flags&rtfReject != 0 || dev == "" || dev == "lo" {
			continue
		}
		routes = append(routes, route{iface: dev, metric: metric})
	}
	return lowest(routes)
}

// zero6 is the address :: as /proc/net/ipv6_route prints it.
var zero6 = strings.Repeat("0", 32)

// primary6 is iface's primary IPv6 address in /proc/net/if_inet6: per
// address, the address, the interface's index, the prefix length, the
// scope and the flags, each in hex, then the interface. The candidates
// are the addresses of global scope (scope 00) that are usable: not
// tentative, not failed duplicate address detection, not deprecated and
// not temporary (privacy addresses, which change). A permanent address
// (configured, not autoconfigured) comes first; among several, the lowest.
func primary6(data []byte, iface string) (netip.Addr, error) {
	var best netip.Addr
	bestPerm := false
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		raw, herr := hex.DecodeString(at(f, 0))
		scope, serr := strconv.ParseUint(at(f, 3), 16, 8)
		flags, ferr := strconv.ParseUint(at(f, 4), 16, 8)
		if len(f) != 6 || herr != nil || len(raw) != 16 || serr != nil || ferr != nil {
			return netip.Addr{}, fmt.Errorf("%q: %w", strings.TrimSpace(line), detect.ErrUnrecognised)
		}
		if f[5] != iface || scope != 0 || flags&(ifaTemporary|ifaDADFailed|ifaDeprecated|ifaTentative) != 0 {
			continue
		}
		a, perm := netip.AddrFrom16([16]byte(raw)), flags&ifaPermanent != 0
		if !best.IsValid() || perm && !bestPerm || perm == bestPerm && a.Less(best) {
			best, bestPerm = a, perm
		}
	}
	return best, nil
}

// hexAddr reports whether s is an IPv6 address as the kernel prints it
// with %pi6: 32 hex digits.
func hexAddr(s string) bool {
	_, err := hex.DecodeString(s)
	return len(s) == 32 && err == nil
}

// at is f[i], or "" past its end.
func at(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}
