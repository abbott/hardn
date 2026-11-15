package logfwd

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Conn is what the host's sockets say of a server.
type Conn uint8

// The answers.
const (
	// ConnNone: nothing to say: UDP has no connection.
	ConnNone Conn = iota
	// ConnOpen: a TCP connection to the server is established.
	ConnOpen
	// ConnDown: none is.
	ConnDown
	// ConnUnknown: the sockets are not read (ReadConns' error), or do not
	// tell.
	ConnUnknown
)

// connArgv lists the TCP sockets that are not listening, numeric, with
// their programs, without the header: one table, so no Netid column, as
// reload's listening tables (reload.md §2.10).
var connArgv = []string{"ss", "-Htnp"}

// rsyslogd is the program whose connection reaches a server named by name.
const rsyslogd = "rsyslogd"

// ConnState is what the host's sockets say of one server of a list.
type ConnState struct {
	Conn Conn
	// Open and Of say more where Conn is ConnDown for a server given by
	// name whose port other named servers share: Open of those Of servers
	// have a connection, and which is not established. Both 0 otherwise.
	Open, Of int
}

// Some reports whether the server is one of several named ones on a port
// of which some, not all, are reached.
func (c ConnState) Some() bool { return c.Open > 0 }

// ReadConns reports, for each of servers, whether a TCP connection to it
// is established (ss -Htnp; design-syslog-servers.md §2.4). beside are the
// other servers the logger forwards to, which are not reported: their
// sockets are not counted as servers'. UDP is ConnNone. A server given by
// address is ConnOpen on an ESTAB socket whose peer is that address and
// port. ss prints no names, so the servers given by name are told per
// port: n, rsyslogd's ESTAB sockets to the port whose peer is no server of
// either list given by address, against m, the port's named servers of
// both lists. n is m: each is ConnOpen; n 0: each ConnDown; between, hardn
// knows how many are down and not which, so each is ConnDown with Open n
// and Of m, never ConnOpen. More sockets than names, or some of a port's
// names in beside and not every one reached, does not tell: ConnUnknown,
// with no error. With no TCP or TLS server ss is not run. ss missing or
// failing is ConnUnknown for each of them, with the error: never ConnDown.
func ReadConns(ctx context.Context, sys host.System, servers, beside []Server) ([]ConnState, error) {
	out := make([]ConnState, len(servers))
	asked := false
	for _, s := range servers {
		asked = asked || s.Proto != UDP
	}
	if !asked {
		return out, nil
	}
	res, err := sys.Run(ctx, connArgv[0], connArgv[1:]...)
	if err != nil {
		for i, s := range servers {
			if s.Proto != UDP {
				out[i].Conn = ConnUnknown
			}
		}
		return out, fmt.Errorf("logfwd: %s: %w", host.FormatArgv(connArgv), err)
	}
	type peer struct {
		addr     netip.Addr
		port     string
		rsyslogd bool
	}
	var peers []peer
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "ESTAB" {
			continue
		}
		i := strings.LastIndexByte(f[4], ':')
		if i < 0 {
			continue
		}
		addr, err := netip.ParseAddr(strings.Trim(f[4][:i], "[]"))
		if err != nil {
			continue
		}
		peers = append(peers, peer{addr.Unmap(), f[4][i+1:], strings.Contains(line, `users:(("`+rsyslogd+`",pid=`)})
	}
	byAddr := map[peer]bool{} // the servers given by address, of both lists
	named := map[string]int{} // port: the servers given by name, of both lists
	other := map[string]int{} // port: those of beside
	for i, s := range slices.Concat(servers, beside) {
		if s.Proto == UDP {
			continue
		}
		if a, err := netip.ParseAddr(s.Address); err == nil {
			byAddr[peer{addr: a.Unmap(), port: s.Port}] = true
			continue
		}
		named[s.Port]++
		if i >= len(servers) {
			other[s.Port]++
		}
	}
	est := map[peer]bool{}   // every established peer
	left := map[string]int{} // port: rsyslogd's sockets to no server given by address
	for _, p := range peers {
		k := peer{addr: p.addr, port: p.port}
		est[k] = true
		if p.rsyslogd && !byAddr[k] {
			left[p.port]++
		}
	}
	for i, s := range servers {
		if s.Proto == UDP {
			continue
		}
		out[i].Conn = ConnDown
		if a, err := netip.ParseAddr(s.Address); err == nil {
			if est[peer{addr: a.Unmap(), port: s.Port}] {
				out[i].Conn = ConnOpen
			}
			continue
		}
		switch n, m := left[s.Port], named[s.Port]; {
		case n == m:
			out[i].Conn = ConnOpen
		case n > m, n > 0 && other[s.Port] > 0:
			out[i].Conn = ConnUnknown
		case n > 0:
			out[i].Open, out[i].Of = n, m
		}
	}
	return out, nil
}
