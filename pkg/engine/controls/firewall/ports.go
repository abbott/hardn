package firewall

import (
	"cmp"
	"context"
	"errors"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The listening ports against the firewall's rules (docs/roadmap.md,
// "Listening ports"; the maintainer, 2026-11-13: "Adopt all" of
// docs/engine/gallery-ports.md): the Firewall screen's Ports section, read
// only. Every listening socket ss shows (reload.ReadListening) is set against
// what ufw, or on Rocky Linux and AlmaLinux firewalld, lets in, one row for
// each port and protocol, and each port a rule lets in from anywhere with
// nothing listening is listed too. docs/engine/screen-ports.md has the rules.

// Reach is how a port stands against the firewall's rules.
type Reach uint8

const (
	// ReachUnknown: not established: a rule or the firewall's state was not
	// read, a rich rule hardn does not read may decide, or a container
	// runtime forwards the port ahead of the host's rules.
	ReachUnknown    Reach = iota
	ReachOpen             // listening, and let in from anywhere
	ReachSome             // listening, and let in from some sources, on some addresses or interfaces only
	ReachBlocked          // listening, and not let in
	ReachLocal            // listening on loopback alone
	ReachUnused           // let in from anywhere, and nothing listens
	ReachUnfiltered       // listening, and no firewall hardn reads is running
)

// Port is one row of the section: a port something listens on, or one a
// rule lets in.
type Port struct {
	// Spec is the port, "443", or for a rule's range with nothing
	// listening, the range as the firewall writes it ("6000:6007",
	// "6000-6007"). Proto is "tcp" or "udp"; "" for a ufw rule of both
	// with nothing listening on either.
	Spec, Proto string
	// Process is the first program ss names on it, as ss printed it: the
	// host's word, which the screen makes safe. "" for none.
	Process string
	Reach   Reach
	low     uint16
}

// Ports is the section's reading.
type Ports struct {
	// NoSS: ss is not installed. Failed: ss ran and failed. Either way
	// nothing is listed.
	NoSS, Failed bool
	// Partial: ss printed lines that were not read, so List may lack
	// sockets, and no port is said to have nothing listening.
	Partial bool
	List    []Port
}

// ReadPorts reads the listening sockets and sets them against r's firewall:
// ufw's status (r.State), or where r.EL firewalld's runtime zones, which it
// reads where firewalld runs (r.Firewalld, firewalld.ReadRuntime). It only
// reads. Read calls it; the gallery's mocks do too.
func ReadPorts(ctx context.Context, sys host.System, r Reading) Ports {
	socks, bad, err := reload.ReadListening(ctx, sys)
	switch {
	case errors.Is(err, host.ErrNotFound):
		return Ports{NoSS: true}
	case err != nil:
		return Ports{Failed: true}
	}
	p := Ports{Partial: len(bad) > 0}
	var judge func(reload.Socket) Reach
	var unused []Port
	switch {
	case r.EL:
		judge, unused = firewalldJudge(ctx, sys, r, socks)
	default:
		judge, unused = ufwJudge(r, socks)
	}
	p.List = portRows(socks, judge)
	if !p.Partial {
		p.List = append(p.List, unused...)
	}
	slices.SortStableFunc(p.List, func(a, b Port) int {
		return cmp.Or(cmp.Compare(a.low, b.low), cmp.Compare(a.Proto, b.Proto), cmp.Compare(a.Spec, b.Spec))
	})
	return p
}

// local: s is bound to loopback, an address of it or the interface lo, which
// nothing outside the host reaches, whatever the rules.
func local(s reload.Socket) bool { return s.Addr.Unmap().IsLoopback() || s.Device == "lo" }

// portRows are the sockets as one row for each port and protocol, in ss's
// order: the first program named on it, and the widest reach of its sockets
// off loopback, as judge gives each. Open on one socket is Open; else one not
// established leaves the port not established; then Some, then Blocked. A
// port on loopback alone is Local.
func portRows(socks []reload.Socket, judge func(reload.Socket) Reach) []Port {
	var rows []Port
	for _, s := range socks {
		i := slices.IndexFunc(rows, func(p Port) bool { return p.low == s.Port && p.Proto == s.Proto })
		if i < 0 {
			rows = append(rows, Port{Spec: strconv.Itoa(int(s.Port)), Proto: s.Proto, Reach: ReachLocal, low: s.Port})
			i = len(rows) - 1
		}
		row := &rows[i]
		if row.Process == "" {
			row.Process = s.Process
		}
		if local(s) {
			continue
		}
		row.Reach = wider(row.Reach, judge(s))
	}
	return rows
}

// wider is the wider of a port's reach so far and one more socket's.
func wider(have, more Reach) Reach {
	rank := func(r Reach) int {
		return slices.Index([]Reach{ReachLocal, ReachBlocked, ReachSome, ReachUnknown, ReachUnfiltered, ReachOpen}, r)
	}
	if rank(more) > rank(have) {
		return more
	}
	return have
}

// listening reports whether a socket of socks, on any address, is on a port
// of lo to hi of proto ("" either).
func listening(socks []reload.Socket, lo, hi uint16, proto string) bool {
	return slices.ContainsFunc(socks, func(s reload.Socket) bool {
		return s.Port >= lo && s.Port <= hi && (proto == "" || s.Proto == proto)
	})
}

// dhcpClient6 is the DHCPv6 client's port, as dhcpClient is DHCPv4's: a
// firewall lets its server's answers in, and the client holds the socket
// only while it asks, so neither is listed as let in with nothing listening.
const dhcpClient6 = 546

func dhcpPort(port uint16, proto string) bool {
	return proto == "udp" && (port == dhcpClient || port == dhcpClient6)
}

// ufwJudge is how ufw stands to each socket, and the ports its rules let in
// from anywhere with nothing listening. ufw not installed, or not active:
// nothing is filtered. Its state or status not read whole: not established.
// Active, each socket's verdict is ufw.Evaluate's under the status's rules,
// for its address in its IP version, and IPv4 too for a dual-stack socket
// (socketNeeds), the widest counting: Allowed and Limited are Open;
// Restricted and Excepted, Some; Blocked and Denied, Blocked, but the UDP
// ports before.rules accepts ahead of the user's rules (ufwBeforeUDP), which
// are Some. docker-proxy's ports are not established (dockerProxy).
func ufwJudge(r Reading, socks []reload.Socket) (func(reload.Socket) Reach, []Port) {
	st := r.State
	all := func(v Reach) func(reload.Socket) Reach { return func(reload.Socket) Reach { return v } }
	switch {
	case r.StateErr != nil:
		return all(ReachUnknown), nil
	case !st.Installed:
		return all(ReachUnfiltered), nil
	case !st.Status.Known || len(st.Status.Bad) > 0:
		return all(ReachUnknown), nil
	case !st.Status.Active:
		return all(ReachUnfiltered), nil
	}
	fw := ufw.Firewall{Active: true, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Status.Incoming}
	rules := st.Status.Rules
	judge := func(s reload.Socket) Reach {
		if s.Process == dockerProxy {
			return ReachUnknown
		}
		out := ReachLocal // below every verdict: an IPv6 socket no connection can arrive on adds nothing
		for _, n := range socketNeeds(s) {
			v := ReachBlocked
			switch ufw.Evaluate(rules, fw, []ufw.Need{n})[0].Verdict {
			case ufw.Allowed, ufw.Limited:
				v = ReachOpen
			case ufw.Restricted, ufw.Excepted:
				v = ReachSome
			case ufw.VerdictUnknown:
				v = ReachUnknown
			case ufw.Unreachable:
				continue
			default:
				if n.Proto == "udp" && slices.Contains(ufwBeforeUDP[n.Family], n.Port) {
					v = ReachSome
				}
			}
			out = wider(out, v)
		}
		if out == ReachLocal {
			return ReachBlocked
		}
		return out
	}
	return judge, ufwUnused(rules, fw, socks)
}

// ufwUnused are the ports ufw's rules let in from anywhere with nothing
// listening: each port, or range, of an allow or limit rule for incoming
// connections with no source, address or interface, which ufw.Evaluate
// lets in at its first port (an earlier deny may stand before the rule),
// where no socket is on it. A rule of both protocols is one row, with
// nothing listening on either. A rule with no port (Anywhere) and the
// default policy name no port, so they have no row.
func ufwUnused(rules []ufw.Rule, fw ufw.Firewall, socks []reload.Socket) []Port {
	var out []Port
	for _, r := range rules {
		if r.Action != "allow" && r.Action != "limit" || r.Dir != "in" || r.DPort == "" || r.Src != "" || r.Dst != "" || r.Iface != "" {
			continue
		}
		proto := r.Proto
		switch proto {
		case "tcp", "udp":
		case "any":
			proto = ""
		default:
			continue
		}
		for _, item := range strings.Split(r.DPort, ",") {
			lo, hi, ok := portRange(item, ":")
			if !ok || listening(socks, lo, hi, proto) || dhcpPort(lo, proto) ||
				slices.ContainsFunc(out, func(p Port) bool { return p.Spec == item && p.Proto == proto }) {
				continue
			}
			addr, np := netip.IPv4Unspecified(), cmp.Or(proto, "tcp")
			if r.Family == ufw.IPv6 {
				addr = netip.IPv6Unspecified()
			}
			if v := ufw.Evaluate(rules, fw, []ufw.Need{{Port: lo, Family: r.Family, Addr: addr, Proto: np}})[0].Verdict; v != ufw.Allowed && v != ufw.Limited {
				continue
			}
			out = append(out, Port{Spec: item, Proto: proto, Reach: ReachUnused, low: lo})
		}
	}
	return out
}

// portRange reads "8080" or a range with sep between its ends
// ("6000:6007", "6000-6007").
func portRange(s, sep string) (lo, hi uint16, ok bool) {
	a, b, isRange := strings.Cut(s, sep)
	l, err := strconv.ParseUint(a, 10, 16)
	h := l
	if err == nil && isRange {
		h, err = strconv.ParseUint(b, 10, 16)
	}
	if err != nil || l == 0 || h < l {
		return 0, 0, false
	}
	return uint16(l), uint16(h), true
}

// firewalldJudge is how firewalld stands to each socket, and the ports it
// lets in from anywhere with nothing listening. Not installed, or stopped:
// nothing is filtered. Not read, or with a rich rule in a zone a connection
// can arrive in: not established. Running, a connection arrives in the zone
// its source is bound to, else its interface's, else the default zone, so a
// port is Open where every zone that takes connections lets it in, Some
// where only some do, Blocked where none does. A default zone that lists
// no interface and no source takes the interfaces no zone names, which are
// not read: where another zone has an interface and counting the default
// zone changes the word, the port is not established. So is a port a zone
// forwards, or forwards another onto. firewalld's zones filter both IP
// versions alike. docker-proxy's ports are not established.
func firewalldJudge(ctx context.Context, sys host.System, r Reading, socks []reload.Socket) (func(reload.Socket) Reach, []Port) {
	all := func(v Reach) func(reload.Socket) Reach { return func(reload.Socket) Reach { return v } }
	st := r.Firewalld
	switch {
	case r.FirewalldErr != nil:
		return all(ReachUnknown), nil
	case st.Installed == detect.No || st.Running == detect.No:
		return all(ReachUnfiltered), nil
	case st.Running != detect.Yes:
		return all(ReachUnknown), nil
	}
	rt, err := firewalld.ReadRuntime(ctx, sys, st)
	if err != nil || rt.Rich() || len(rt.Zones) == 0 {
		return all(ReachUnknown), nil
	}
	// bare: the default zone lists nothing it takes; named: another zone
	// lists an interface, so the default zone may take none.
	bare := func(z firewalld.RuntimeZone) bool { return z.Default && len(z.Interfaces) == 0 && len(z.Sources) == 0 }
	named := slices.ContainsFunc(rt.Zones, func(z firewalld.RuntimeZone) bool { return !z.Default && len(z.Interfaces) > 0 })
	among := func(port uint16, proto string, withBare bool) Reach {
		every, some := true, false
		for _, z := range rt.Zones {
			if bare(z) && !withBare {
				continue
			}
			lets := z.Lets(port, proto)
			every, some = every && lets, some || lets
		}
		switch {
		case every:
			return ReachOpen
		case some:
			return ReachSome
		}
		return ReachBlocked
	}
	reach := func(port uint16, proto string) Reach {
		v := among(port, proto, true)
		if rt.Forwards(port, proto) || named && slices.ContainsFunc(rt.Zones, bare) && among(port, proto, false) != v {
			return ReachUnknown
		}
		return v
	}
	judge := func(s reload.Socket) Reach {
		if s.Process == dockerProxy {
			return ReachUnknown
		}
		return reach(s.Port, s.Proto)
	}
	var unused []Port
	for _, z := range rt.Zones {
		for _, spec := range z.Specs {
			ports, proto, ok := strings.Cut(spec, "/")
			lo, hi, okRange := portRange(ports, "-")
			if !ok || !okRange || proto != "tcp" && proto != "udp" || listening(socks, lo, hi, proto) || dhcpPort(lo, proto) ||
				reach(lo, proto) != ReachOpen || slices.ContainsFunc(unused, func(p Port) bool { return p.Spec == ports && p.Proto == proto }) {
				continue
			}
			unused = append(unused, Port{Spec: ports, Proto: proto, Reach: ReachUnused, low: lo})
		}
	}
	return judge, unused
}
