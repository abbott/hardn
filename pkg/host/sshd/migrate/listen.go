package migrate

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/sshd"
)

// needed is a listener the migrated files must configure, and why: a
// socket's listener, so an unspecified IPv6 address is both families (a
// dual-stack socket, systemd's default; docs/safety/reload.md §3).
type needed struct {
	sshd.Listener
	from string // ListenConf, or sshd.SocketUnit for the socket's listeners now
}

// families is the listeners a socket's l accepts connections for: an
// unspecified IPv6 address is IPv6 and IPv4.
func families(l sshd.Listener) []sshd.Listener {
	if a := l.Addr; a.Is6() && !a.Is4In6() && a.IsUnspecified() {
		return []sshd.Listener{l, {Addr: netip.IPv4Unspecified(), Port: l.Port, RDomain: l.RDomain}}
	}
	return []sshd.Listener{l}
}

// covered reports whether one of ls accepts the connections b does: the
// same port and routing domain, and the same address or the unspecified
// address of its family (sshd binds :: IPv6 only).
func covered(ls []sshd.Listener, b sshd.Listener) bool {
	ba := b.Addr.Unmap()
	return slices.ContainsFunc(ls, func(l sshd.Listener) bool {
		la := l.Addr.Unmap()
		return l.Port == b.Port && l.RDomain == b.RDomain && (la == ba || la.IsUnspecified() && la.Is4() == ba.Is4())
	})
}

// overlaps lists the pairs of ls one host cannot listen on together: one
// port, routing domain and family, with equal addresses or one of them
// unspecified (docs/safety/sshd-migrate.md §3: the second bind is
// refused, and on a socket host that fails ssh.socket's whole start).
func overlaps(ls []sshd.Listener) []string {
	var out []string
	for i, a := range ls {
		for _, b := range ls[i+1:] {
			aa, ba := a.Addr.Unmap(), b.Addr.Unmap()
			if a.Port == b.Port && a.RDomain == b.RDomain && aa.Is4() == ba.Is4() && (aa == ba || aa.IsUnspecified() || ba.IsUnspecified()) {
				out = append(out, a.String()+" and "+b.String())
			}
		}
	}
	return out
}

// isKeyword reports whether a line sets the canonical keyword k.
func isKeyword(k string) func(outLine) bool {
	return func(l outLine) bool { return l.keyword == k }
}

// portsOf is the Port values of ls, or sshd's default 22 when there are
// none; ok is false for a value that is not a number from 1 to 65535 (a
// service name sshd would look up, say).
func portsOf(ls []outLine) (ports []uint16, ok bool) {
	for _, l := range ls {
		if l.keyword != "port" {
			continue
		}
		p, err := strconv.ParseUint(strings.Join(l.args, " "), 10, 16)
		if err != nil || p == 0 {
			return nil, false
		}
		ports = append(ports, uint16(p))
	}
	if len(ports) == 0 {
		ports = []uint16{22}
	}
	return ports, true
}

// addrListeners is what one ListenAddress line configures, by servconf.c's
// forms: address:port, [address]:port, or an address alone (an IPv6 one
// bare or in brackets) on every Port, each optionally followed by
// "rdomain <name>". ok is false for a host name, which sshd resolves.
func addrListeners(args []string, ports []uint16) (ls []sshd.Listener, ok bool) {
	rd := ""
	switch {
	case len(args) == 3 && strings.EqualFold(args[1], "rdomain"):
		rd = args[2]
	case len(args) != 1:
		return nil, false
	}
	host, port, arg := args[0], "", args[0]
	switch {
	case strings.HasPrefix(arg, "["):
		i := strings.Index(arg, "]")
		if i < 0 {
			return nil, false
		}
		host = arg[1:i]
		if rest := arg[i+1:]; rest != "" {
			if port, ok = strings.CutPrefix(rest, ":"); !ok {
				return nil, false
			}
		}
	case strings.Count(arg, ":") == 1: // more is an IPv6 address alone
		host, port, _ = strings.Cut(arg, ":")
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return nil, false
	}
	if port != "" {
		p, err := strconv.ParseUint(port, 10, 16)
		if err != nil || p == 0 {
			return nil, false
		}
		ports = []uint16{uint16(p)}
	}
	for _, p := range ports {
		ls = append(ls, sshd.Listener{Addr: a, Port: p, RDomain: rd})
	}
	return ls, true
}

// listenersOf is the listeners sshd configures from the global lines ls:
// each ListenAddress line's, or, with none, the unspecified address of
// each family AddressFamily allows on every Port. ok is false for
// anything it does not read, or AddressFamily lines that disagree, and
// then nothing may be concluded from it.
func listenersOf(ls []outLine) (out []sshd.Listener, ok bool) {
	ports, ok := portsOf(ls)
	if !ok {
		return nil, false
	}
	family := ""
	for _, l := range ls {
		if l.keyword != "addressfamily" {
			continue
		}
		f := strings.ToLower(strings.Join(l.args, " "))
		if family != "" && f != family {
			return nil, false
		}
		family = f
	}
	addrs := slices.DeleteFunc(slices.Clone(ls), func(l outLine) bool { return l.keyword != "listenaddress" })
	if len(addrs) == 0 {
		for _, p := range ports {
			if family != "inet" {
				out = append(out, sshd.Listener{Addr: netip.IPv6Unspecified(), Port: p})
			}
			if family != "inet6" {
				out = append(out, sshd.Listener{Addr: netip.IPv4Unspecified(), Port: p})
			}
		}
		return out, true
	}
	for _, l := range addrs {
		a, ok := addrListeners(l.args, ports)
		if !ok {
			return nil, false
		}
		out = append(out, a...)
	}
	return out, true
}

// addListeners adds the lines that make sshd configure every listener of
// need it does not configure yet: with ListenAddress lines in the files,
// ListenAddress <address>:<port>; without, a Port line. need is only ever
// a socket's (PlanFor: a socket host whose generator follows the files),
// where sshd's own set is configuration only, so sshd's default 22 is not
// kept for its own sake when the first Port line replaces it: it is in
// need, and gets its line, when ssh.socket listens on it now.
func (c *composed) addListeners(need []needed, rest []outLine) error {
	for _, n := range need {
		all := slices.Concat(c.out, rest)
		have, ok := listenersOf(all)
		if !ok {
			return fmt.Errorf("migrate: the Port, ListenAddress and AddressFamily lines are not ones hardn reads (a host name, say), so %s's %v cannot be kept: %w",
				n.from, n.Listener, ErrNotEstablished)
		}
		var missing []sshd.Listener
		for _, l := range families(n.Listener) {
			if !covered(have, l) {
				missing = append(missing, l)
			}
		}
		switch {
		case len(missing) == 0:
		case slices.ContainsFunc(all, isKeyword("listenaddress")):
			for _, m := range missing {
				args := []string{netip.AddrPortFrom(m.Addr, m.Port).String()}
				if m.RDomain != "" {
					args = append(args, "rdomain", m.RDomain)
				}
				c.out = append(c.out, outLine{text: render("ListenAddress", args), keyword: "listenaddress", args: args, from: n.from})
			}
		default:
			p := strconv.Itoa(int(n.Port))
			c.out = append(c.out, outLine{text: "Port " + p, keyword: "port", args: []string{p}, from: n.from})
		}
	}
	return nil
}

// dedupe drops a ListenAddress line that 00-hardn.conf took from a legacy
// file or added, when the other lines already configure every listener it
// does: sshd would bind the same connections twice, and the generator
// would give ssh.socket two sockets that cannot both be bound. The lines
// of Desired and of the files the migration leaves stay; Apply refuses an
// overlap that remains. Without another ListenAddress line nothing is
// dropped: sshd would then listen on every address.
func (c *composed) dedupe(rest []outLine) {
	ports, ok := portsOf(slices.Concat(c.out, rest))
	if !ok {
		return
	}
	for i := len(c.out) - 1; i >= 0; i-- {
		l := c.out[i]
		if l.keyword != "listenaddress" || l.from == "" {
			continue
		}
		others := slices.Concat(c.out[:i], c.out[i+1:], rest)
		mine, ok := addrListeners(l.args, ports)
		theirs, ok2 := listenersOf(others)
		if !ok || !ok2 || !slices.ContainsFunc(others, isKeyword("listenaddress")) ||
			slices.ContainsFunc(mine, func(m sshd.Listener) bool { return !covered(theirs, m) }) {
			continue
		}
		c.out = slices.Delete(c.out, i, i+1)
		if j := slices.IndexFunc(c.carried, func(k Carried) bool { return k.Origin == l.origin }); j >= 0 {
			c.carried = slices.Delete(c.carried, j, j+1)
			c.dropped = append(c.dropped, Carried{l.origin, "the other ListenAddress lines already configure its listeners, and a second socket on them would not bind"})
		}
	}
}
