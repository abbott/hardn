package sshd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Accepted is sshd's log line for a session's login: "Accepted <method>
// for <user> from <addr> port <port> ssh2", for publickey followed by ":
// <KEYTYPE> <fingerprint>" (sshd-session's auth.c auth_log, as Debian 13,
// Ubuntu 26.04 and Alpine 3.23 print it).
type Accepted struct {
	User, Method string
	// KeyType and Fingerprint: publickey's, as logged ("ED25519",
	// "SHA256:…"); "" for another method.
	KeyType, Fingerprint string
}

// ErrNotLogged: sshd's log, where hardn reads it, has no Accepted line for
// the session's address and port.
var ErrNotLogged = errors.New("sshd's log has no login for this session")

// The logs ReadAccepted reads on OpenRC: busybox syslogd's (Alpine 3.23,
// syslogd -t), then the copy its rotation keeps.
const (
	messagesLog     = "/var/log/messages"
	messagesRotated = "/var/log/messages.0"
)

// ReadAccepted finds the session's login in sshd's log. conn is
// SSH_CONNECTION, "<client addr> <client port> <server addr> <server
// port>"; the line read is the last Accepted line from the client's
// address and port. On systemd it reads journalctl -q --no-pager -o
// short-iso -u ssh -u sshd (Debian and Ubuntu name the unit ssh, others
// sshd); on OpenRC (openRC) messagesLog, then messagesRotated, a file
// not there skipped. None: ErrNotLogged. A conn that does not parse is
// fs.ErrInvalid, before anything is read. It writes nothing.
func ReadAccepted(ctx context.Context, sys host.System, openRC bool, conn string) (Accepted, error) {
	f := strings.Fields(conn)
	if len(f) != 4 {
		return Accepted{}, fmt.Errorf("sshd: connection %q: %w", conn, fs.ErrInvalid)
	}
	addr, err := netip.ParseAddr(f[0])
	port, perr := strconv.ParseUint(f[1], 10, 16)
	if err != nil || perr != nil || port == 0 {
		return Accepted{}, fmt.Errorf("sshd: connection %q: %w", conn, fs.ErrInvalid)
	}
	if !openRC {
		res, err := sys.Run(ctx, "journalctl", "-q", "--no-pager", "-o", "short-iso", "-u", "ssh", "-u", "sshd")
		if err != nil {
			return Accepted{}, fmt.Errorf("sshd: log: %w", err)
		}
		if a, ok := lastAccepted(string(res.Stdout), addr, f[1]); ok {
			return a, nil
		}
		return Accepted{}, ErrNotLogged
	}
	for _, name := range []string{messagesLog, messagesRotated} {
		data, err := sys.ReadFile(ctx, name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Accepted{}, fmt.Errorf("sshd: log: %w", err)
		}
		if a, ok := lastAccepted(string(data), addr, f[1]); ok {
			return a, nil
		}
	}
	return Accepted{}, ErrNotLogged
}

// lastAccepted is out's last Accepted line from addr and port.
func lastAccepted(out string, addr netip.Addr, port string) (Accepted, bool) {
	var a Accepted
	found := false
	for _, l := range lines(out) {
		_, rest, ok := strings.Cut(l, ": Accepted ")
		w := strings.Fields(rest)
		if !ok || len(w) < 8 || w[1] != "for" || w[3] != "from" || w[5] != "port" || w[6] != port || !strings.HasPrefix(w[7], "ssh2") {
			continue
		}
		if from, err := netip.ParseAddr(w[4]); err != nil || from.Unmap() != addr.Unmap() {
			continue
		}
		a, found = Accepted{User: w[2], Method: w[0]}, true
		if w[0] == "publickey" && w[7] == "ssh2:" && len(w) >= 10 {
			a.KeyType, a.Fingerprint = w[8], w[9]
		}
	}
	return a, found
}
