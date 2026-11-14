package logfwd

import (
	"context"
	"fmt"
	"net/netip"
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
	// ConnUnknown: the sockets are not read (ReadConn's error).
	ConnUnknown
)

// connArgv lists the TCP sockets that are not listening, numeric, with
// their programs, without the header: one table, so no Netid column, as
// reload's listening tables (reload.md §2.10).
var connArgv = []string{"ss", "-Htnp"}

// rsyslogd is the program whose connection reaches a server named by name.
const rsyslogd = "rsyslogd"

// ReadConn reports whether a TCP connection to s is established (ss
// -Htnp): for a server given by address, an ESTAB socket whose peer is
// that address and port; for one given by name, which ss does not print,
// rsyslogd's ESTAB socket to that port. UDP is ConnNone, unasked. ss
// missing or failing is ConnUnknown with the error: never ConnDown.
func ReadConn(ctx context.Context, sys host.System, s Server) (Conn, error) {
	if s.Proto == UDP {
		return ConnNone, nil
	}
	res, err := sys.Run(ctx, connArgv[0], connArgv[1:]...)
	if err != nil {
		return ConnUnknown, fmt.Errorf("logfwd: %s: %w", host.FormatArgv(connArgv), err)
	}
	want, wantErr := netip.ParseAddr(s.Address)
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "ESTAB" {
			continue
		}
		i := strings.LastIndexByte(f[4], ':')
		if i < 0 || f[4][i+1:] != s.Port {
			continue
		}
		if wantErr != nil {
			if strings.Contains(line, `users:(("`+rsyslogd+`",pid=`) {
				return ConnOpen, nil
			}
			continue
		}
		peer, err := netip.ParseAddr(strings.Trim(f[4][:i], "[]"))
		if err == nil && peer.Unmap() == want.Unmap() {
			return ConnOpen, nil
		}
	}
	return ConnDown, nil
}
