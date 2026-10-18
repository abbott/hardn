package revert

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Session is how the user reached the hardn that runs: over SSH (Remote) or
// not, a local console. It is a check against mistakes, not a security
// boundary: whoever runs hardn as root can go around it.
type Session struct {
	Remote bool `json:"remote"`
	// Conn is SSH_CONNECTION as found ("client-addr client-port
	// server-addr server-port"); "" when Remote was established another
	// way, or not at all.
	Conn string `json:"conn,omitempty"`
	// Source is where it was found: the environment variable, or the /proc
	// file of an ancestor process.
	Source string `json:"source,omitempty"`
}

// Conn is a parsed SSH_CONNECTION: one TCP connection's two ends.
type Conn struct {
	Client, Server netip.AddrPort
}

// ParseConn reads SSH_CONNECTION's four space-separated values: client
// address, client port, server address, server port.
func ParseConn(s string) (Conn, error) {
	f := strings.Fields(s)
	if len(f) != 4 {
		return Conn{}, fmt.Errorf("SSH_CONNECTION %q: want four fields", s)
	}
	var ends [2]netip.AddrPort
	for i := range ends {
		a, err := netip.ParseAddr(f[2*i])
		if err != nil {
			return Conn{}, fmt.Errorf("SSH_CONNECTION %q: %w", s, err)
		}
		p, err := strconv.ParseUint(f[2*i+1], 10, 16)
		if err != nil || p == 0 {
			return Conn{}, fmt.Errorf("SSH_CONNECTION %q: port %q", s, f[2*i+1])
		}
		ends[i] = netip.AddrPortFrom(a.Unmap(), uint16(p))
	}
	return Conn{Client: ends[0], Server: ends[1]}, nil
}

// ProcDir is where ReadSession reads the process tree.
const ProcDir = "/proc"

// sshdNames are the comm names of the sshd process a session runs under:
// sshd-session since OpenSSH 9.8, sshd before.
var sshdNames = []string{"sshd", "sshd-session"}

// maxAncestors bounds the walk up the process tree.
const maxAncestors = 64

// ReadSession establishes whether hardn, process pid with environment env
// (os.Getpid, os.Getenv), runs over SSH. Remote when SSH_CONNECTION,
// SSH_CLIENT or SSH_TTY is set; otherwise it walks pid's ancestors in
// /proc, since sudo and doas reset the environment: Remote, with Conn,
// when one's initial environment (/proc/<pid>/environ) holds
// SSH_CONNECTION; Remote without it when one's comm is sshd or
// sshd-session. Not Remote once the walk reaches init with neither. When
// the walk cannot finish, it returns a Remote session with no Conn and an
// error wrapping ErrSessionUnknown: callers keep that session, which no
// exception applies to.
func ReadSession(ctx context.Context, sys host.System, env func(string) string, pid int) (Session, error) {
	if v := env("SSH_CONNECTION"); v != "" {
		return Session{Remote: true, Conn: v, Source: "SSH_CONNECTION"}, nil
	}
	for _, k := range []string{"SSH_CLIENT", "SSH_TTY"} {
		if env(k) != "" {
			return Session{Remote: true, Source: k}, nil
		}
	}
	unknown := func(err error) (Session, error) {
		return Session{Remote: true}, fmt.Errorf("revert: session: %w: %w", ErrSessionUnknown, err)
	}
	for hops := 0; pid > 1; hops++ {
		if hops == maxAncestors {
			return unknown(fmt.Errorf("more than %d ancestors", maxAncestors))
		}
		dir := ProcDir + "/" + strconv.Itoa(pid)
		environ, err := sys.ReadFile(ctx, dir+"/environ")
		if err != nil {
			return unknown(err)
		}
		if v := environValue(environ, "SSH_CONNECTION"); v != "" {
			return Session{Remote: true, Conn: v, Source: dir + "/environ"}, nil
		}
		comm, err := sys.ReadFile(ctx, dir+"/comm")
		if err != nil {
			return unknown(err)
		}
		if slices.Contains(sshdNames, strings.TrimSpace(string(comm))) {
			return Session{Remote: true, Source: dir + "/comm"}, nil
		}
		status, err := sys.ReadFile(ctx, dir+"/status")
		if err != nil {
			return unknown(err)
		}
		if pid, err = parentPID(status); err != nil {
			return unknown(fmt.Errorf("%s/status: %w", dir, err))
		}
	}
	return Session{Source: "no SSH variable or sshd among the ancestors"}, nil
}

// environValue finds key in a NUL-separated environment block.
func environValue(block []byte, key string) string {
	for _, kv := range bytes.Split(block, []byte{0}) {
		if k, v, ok := bytes.Cut(kv, []byte{'='}); ok && string(k) == key {
			return string(v)
		}
	}
	return ""
}

// parentPID reads the PPid line of /proc/<pid>/status.
func parentPID(status []byte) (int, error) {
	for _, line := range strings.Split(string(status), "\n") {
		if v, ok := strings.CutPrefix(line, "PPid:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 {
				return 0, fmt.Errorf("PPid %q", v)
			}
			return n, nil
		}
	}
	return 0, fmt.Errorf("no PPid line")
}

// CheckConfirm decides whether session s may confirm journal j (brief 2.4:
// a confirmation typed into the session that applied the change proves
// nothing). local is true when s is not over SSH: the local console
// exception, which confirms with a warning (losing SSH is not a lockout at
// the console). Over SSH, the connection must be established (Conn), and:
//
//   - with ConfirmVia, it must reach one of those listeners, which the
//     change added, so it is a new connection that the change lets in
//     (ErrWrongListener otherwise);
//   - without, it must not be the connection that applied the change
//     (ErrSameSession), which must then be established too.
//
// A multiplexed session (ssh ControlMaster) shares its connection, so it is
// refused as the same one: it proves nothing about a new connection.
func CheckConfirm(j Journal, s Session) (local bool, err error) {
	if !s.Remote {
		return true, nil
	}
	if s.Conn == "" {
		return false, fmt.Errorf("revert: confirm: found by %s: %w", sourceOr(s.Source), ErrSessionUnknown)
	}
	c, err := ParseConn(s.Conn)
	if err != nil {
		return false, fmt.Errorf("revert: confirm: %w: %w", ErrSessionUnknown, err)
	}
	if len(j.ConfirmVia) > 0 {
		for _, l := range j.ConfirmVia {
			if l.Port == c.Server.Port() && (l.Addr.IsUnspecified() || l.Addr.Unmap() == c.Server.Addr()) {
				return false, nil
			}
		}
		return false, fmt.Errorf("revert: confirm: connected to %s, not %s: %w", c.Server, listenerList(j.ConfirmVia), ErrWrongListener)
	}
	if !j.Session.Remote {
		return false, nil // applied at the console: any SSH connection is a new one
	}
	if j.Session.Conn == "" {
		return false, fmt.Errorf("revert: confirm: the connection that applied the change is not established: %w", ErrSessionUnknown)
	}
	if a, err := ParseConn(j.Session.Conn); err != nil || a == c {
		return false, fmt.Errorf("revert: confirm: %s: %w", c.Client, ErrSameSession)
	}
	return false, nil
}

func sourceOr(s string) string {
	if s == "" {
		return "an unfinished search"
	}
	return s
}

func listenerList(ls []Listener) string {
	s := make([]string, len(ls))
	for i, l := range ls {
		s[i] = l.String()
	}
	return strings.Join(s, " or ")
}
