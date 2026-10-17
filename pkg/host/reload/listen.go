package reload

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// ssArgv is how ReadBound lists the bound sockets: TCP (-t), listening (-l),
// numeric (-n), without the header line (-H).
var ssArgv = []string{"ss", "-Htln"}

// Bound is a TCP socket in the LISTEN state, as ss shows it: bound, which
// sshd -T and systemctl show -p Listen, both configuration, do not show.
type Bound struct {
	Addr netip.Addr // :: for ss's "*"
	Port uint16
	// Dual: ss printed "*", an IPv6 socket on :: that is not IPv6-only, so
	// it accepts IPv4 connections too.
	Dual   bool
	Device string // the interface after "%", for a socket bound to one
}

func (b Bound) String() string {
	a := b.Addr.String()
	switch {
	case b.Dual:
		a = "*"
	case b.Addr.Is6():
		a = "[" + a + "]"
	}
	if b.Device != "" {
		a += "%" + b.Device
	}
	return a + ":" + strconv.Itoa(int(b.Port))
}

// ReadBound lists the TCP sockets in the LISTEN state with ss -Htln. bad
// lists the LISTEN lines whose local address it cannot read. ss missing
// (iproute2 not installed) is host.ErrNotFound.
func ReadBound(ctx context.Context, sys host.System) (bound []Bound, bad []string, err error) {
	res, err := sys.Run(ctx, ssArgv[0], ssArgv[1:]...)
	if err != nil {
		return nil, nil, fmt.Errorf("reload: %s: %w", host.FormatArgv(ssArgv), err)
	}
	bound, bad = ParseBound(res.Stdout)
	return bound, bad, nil
}

// ParseBound reads ss -Htln's lines: State, Recv-Q, Send-Q, local
// address:port, peer address:port (no Netid column for one table, no
// Process column without -p). The local address is "0.0.0.0", "[::]", "*"
// (dual-stack ::), and "%<interface>" follows a socket bound to one. Lines
// in another state (UNCONN) are skipped; LISTEN lines it cannot read are in
// bad.
func ParseBound(out []byte) (bound []Bound, bad []string) {
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "LISTEN" {
			continue
		}
		b, ok := Bound{}, len(f) >= 5
		if ok {
			b, ok = parseLocal(f[3])
		}
		if !ok {
			bad = append(bad, strings.TrimSpace(line))
			continue
		}
		bound = append(bound, b)
	}
	return bound, bad
}

func parseLocal(s string) (Bound, bool) {
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return Bound{}, false
	}
	port, err := strconv.ParseUint(s[i+1:], 10, 16)
	if err != nil || port == 0 {
		return Bound{}, false
	}
	b := Bound{Port: uint16(port)}
	a := s[:i]
	if j := strings.LastIndexByte(a, '%'); j >= 0 && !strings.HasSuffix(a, "]") {
		a, b.Device = a[:j], a[j+1:]
	}
	switch v6, isV6 := strings.CutPrefix(a, "["); {
	case a == "*":
		b.Addr, b.Dual = netip.IPv6Unspecified(), true
	case isV6:
		inner, closed := strings.CutSuffix(v6, "]")
		b.Addr, err = netip.ParseAddr(inner)
		if !closed || err != nil || !b.Addr.Is6() {
			return Bound{}, false
		}
	default:
		b.Addr, err = netip.ParseAddr(a)
		if err != nil || !b.Addr.Is4() {
			return Bound{}, false
		}
	}
	return b, true
}

// covers reports whether a socket on addr:port accepts the connections want
// names: the same port, and the same address or an unspecified one of
// want's family (of either family when dual). Routing domains and devices
// are not compared: neither ss nor systemctl show sshd's rdomain.
func covers(addr netip.Addr, port uint16, dual bool, want sshd.Listener) bool {
	a, w := addr.Unmap(), want.Addr.Unmap()
	return port == want.Port && (a == w || a.IsUnspecified() && (dual || a.Is4() == w.Is4()))
}

// uncovered lists the listeners of want no listener of have covers. dual
// treats every unspecified address of have as both families, as
// sshd-effective compares systemd's dual-stack [::] with sshd's 0.0.0.0
// and [::].
func uncovered(want, have []sshd.Listener, dual bool) []sshd.Listener {
	var missing []sshd.Listener
	for _, w := range want {
		ok := false
		for _, h := range have {
			ok = ok || covers(h.Addr, h.Port, dual, w)
		}
		if !ok {
			missing = append(missing, w)
		}
	}
	return missing
}

// unbound lists the listeners of want no bound socket covers.
func unbound(want []sshd.Listener, bound []Bound) []sshd.Listener {
	var missing []sshd.Listener
	for _, w := range want {
		ok := false
		for _, b := range bound {
			ok = ok || covers(b.Addr, b.Port, b.Dual, w)
		}
		if !ok {
			missing = append(missing, w)
		}
	}
	return missing
}
