package revert

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Session is how the user reached the hardn that runs: over SSH (Remote) or
// at a console. It is a check against mistakes, not a security boundary:
// whoever runs hardn as root can go around it. The zero value is not
// established: ReadSession always sets Source.
type Session struct {
	Remote bool `json:"remote"`
	// Conn is SSH_CONNECTION as found ("client-addr client-port
	// server-addr server-port"); "" when Remote was established another
	// way, or not at all.
	Conn string `json:"conn,omitempty"`
	// SSHD is the session's closest sshd or sshd-session ancestor: the
	// server process of its connection, which started when the connection
	// was made. Zero when none was found (inside tmux or screen, for one).
	SSHD ProcStart `json:"sshd,omitzero"`
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

// loginName is the comm of login(1), which a console login, the Proxmox
// web console's shell among them, runs under.
const loginName = "login"

// maxAncestors bounds the walk up the process tree.
const maxAncestors = 64

// ReadSession establishes how hardn, process pid with environment env
// (os.Getenv, os.Getpid), was reached. It walks pid's ancestors in /proc,
// since sudo and doas reset the environment, up to init:
//
//   - Remote, with Conn, when SSH_CONNECTION is in env or in an ancestor's
//     initial environment (/proc/<pid>/environ); Remote when SSH_CLIENT or
//     SSH_TTY is in env; and, either way, SSHD is the closest ancestor
//     whose comm is sshd or sshd-session, which makes it Remote too. The
//     walk ends there.
//   - Otherwise local only on evidence of a console: an ancestor is
//     login(1), or the last process below init has a controlling terminal
//     that is not a pseudo-terminal (a virtual console or a serial line).
//   - Anything else (run0, systemd-run --pty, a tmux or screen server that
//     init adopted) is not established: Remote, with an error wrapping
//     ErrSessionUnknown, so the local-console exception never applies.
//
// When the walk cannot finish, it returns what it found, Remote, with an
// error wrapping ErrSessionUnknown: callers keep that session, which no
// exception applies to.
func ReadSession(ctx context.Context, sys host.System, env func(string) string, pid int) (Session, error) {
	var s Session
	if v := env("SSH_CONNECTION"); v != "" {
		s = Session{Remote: true, Conn: v, Source: "SSH_CONNECTION"}
	} else if k := firstSet(env, "SSH_CLIENT", "SSH_TTY"); k != "" {
		s = Session{Remote: true, Source: k}
	}
	unknown := func(err error) (Session, error) {
		s.Remote = true
		return s, fmt.Errorf("revert: session: %w: %w", ErrSessionUnknown, err)
	}
	login, top, topDir := "", procStat{}, ""
	for hops := 0; pid > 1; hops++ {
		if hops == maxAncestors {
			return unknown(fmt.Errorf("more than %d ancestors", maxAncestors))
		}
		dir := ProcDir + "/" + strconv.Itoa(pid)
		st, err := readStat(ctx, sys, dir)
		if err != nil {
			return unknown(err)
		}
		if s.Conn == "" {
			environ, err := sys.ReadFile(ctx, dir+"/environ")
			if err != nil {
				return unknown(err)
			}
			if v := environValue(environ, "SSH_CONNECTION"); v != "" {
				s.Remote, s.Conn, s.Source = true, v, dir+"/environ"
			}
		}
		if slices.Contains(sshdNames, st.comm) {
			s.Remote, s.SSHD = true, ProcStart{PID: pid, Start: st.start}
			if s.Source == "" {
				s.Source = dir + "/stat"
			}
			return s, nil
		}
		if st.comm == loginName && login == "" {
			login = dir
		}
		top, topDir, pid = st, dir, st.ppid
	}
	switch {
	case s.Remote:
		return s, nil // SSH, but no sshd among the ancestors: CheckConfirm refuses it
	case login != "":
		return Session{Source: "a console login (" + login + " is login)"}, nil
	case topDir != "" && consoleTTY(top.tty):
		return Session{Source: fmt.Sprintf("a console (%s has terminal %d:%d)", topDir, ttyMajor(top.tty), ttyMinor(top.tty))}, nil
	}
	s = Session{Remote: true, Source: "no SSH connection, sshd or console login among the ancestors (run0, systemd-run, tmux or screen?)"}
	return s, fmt.Errorf("revert: session: %w: not over SSH, and not a console", ErrSessionUnknown)
}

func firstSet(env func(string) string, keys ...string) string {
	for _, k := range keys {
		if env(k) != "" {
			return k
		}
	}
	return ""
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

// procStat is what hardn reads of /proc/<pid>/stat (proc_pid_stat(5)).
type procStat struct {
	pid   int    // field 1
	comm  string // field 2, without its parentheses
	state byte   // field 3
	ppid  int    // field 4
	tty   int    // field 7, tty_nr
	start uint64 // field 22, starttime, in clock ticks after boot
}

// parseStat reads a /proc/<pid>/stat line. comm may hold spaces and
// parentheses, so it ends at the last ')'.
func parseStat(data []byte) (procStat, error) {
	s := strings.TrimSpace(string(data))
	open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if open < 1 || end < open {
		return procStat{}, fmt.Errorf("stat %q: no (comm)", s)
	}
	var st procStat
	var err error
	if st.pid, err = strconv.Atoi(strings.TrimSpace(s[:open])); err != nil {
		return procStat{}, fmt.Errorf("stat: pid: %w", err)
	}
	st.comm = s[open+1 : end]
	f := strings.Fields(s[end+1:]) // f[n-3] is field n
	if len(f) < 20 || len(f[0]) != 1 {
		return procStat{}, fmt.Errorf("stat of %d: %d fields after comm", st.pid, len(f))
	}
	st.state = f[0][0]
	st.ppid, err = strconv.Atoi(f[1])
	if err == nil {
		st.tty, err = strconv.Atoi(f[4])
	}
	if err == nil {
		st.start, err = strconv.ParseUint(f[19], 10, 64)
	}
	if err != nil || st.ppid < 0 {
		return procStat{}, fmt.Errorf("stat of %d: %v", st.pid, err)
	}
	return st, nil
}

// readStat reads and parses dir/stat, dir being /proc/<pid> or /proc/self.
func readStat(ctx context.Context, sys host.System, dir string) (procStat, error) {
	data, err := sys.ReadFile(ctx, dir+"/stat")
	if err != nil {
		return procStat{}, err
	}
	st, err := parseStat(data)
	if err != nil {
		return procStat{}, fmt.Errorf("%s/stat: %w", dir, err)
	}
	return st, nil
}

// tty_nr's device numbers: "the minor device number is contained in the
// combination of bits 31 to 20 and 7 to 0; the major device number is in
// bits 15 to 8" (proc_pid_stat(5)).
func ttyMajor(nr int) int { return nr >> 8 & 0xff }
func ttyMinor(nr int) int { return nr&0xff | nr>>12&0xfff00 }

// consoleTTY reports whether tty_nr is a terminal that is not a
// pseudo-terminal: a virtual console or a serial line (major 4, among
// others), not none (0), not a BSD pty slave (major 3) and not a Unix98 one
// (136 to 143) (the kernel's devices.txt). SSH, run0, tmux and screen give
// their sessions a pseudo-terminal.
func consoleTTY(nr int) bool {
	m := ttyMajor(nr)
	return nr != 0 && m != 3 && (m < 136 || m > 143)
}

// sshdProcesses lists the sshd and sshd-session processes running now: the
// server end of every SSH connection open, and the listener.
func sshdProcesses(ctx context.Context, sys host.System) ([]ProcStart, error) {
	entries, err := sys.ReadDir(ctx, ProcDir)
	if err != nil {
		return nil, fmt.Errorf("revert: %w", err)
	}
	var out []ProcStart
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		dir := ProcDir + "/" + e.Name()
		st, err := readStat(ctx, sys, dir)
		if err != nil {
			if _, serr := sys.Lstat(ctx, dir); errors.Is(serr, fs.ErrNotExist) {
				continue // it ended
			}
			return nil, fmt.Errorf("revert: %w", err)
		}
		if slices.Contains(sshdNames, st.comm) {
			out = append(out, ProcStart{PID: pid, Start: st.start})
		}
	}
	return out, nil
}

// selfProc names hardn's own process, for a rollback's Runner: /proc/self,
// read through sys, is the process that reads it.
func selfProc(ctx context.Context, sys host.System) (ProcStart, error) {
	st, err := readStat(ctx, sys, ProcDir+"/self")
	if err != nil {
		return ProcStart{}, err
	}
	boot, err := sys.ReadFile(ctx, bootIDFile)
	if err != nil {
		return ProcStart{}, err
	}
	return ProcStart{PID: st.pid, Start: st.start, Boot: strings.TrimSpace(string(boot))}, nil
}

// bootIDFile holds a random id generated once per boot (random(4)).
const bootIDFile = ProcDir + "/sys/kernel/random/boot_id"

// alive reports whether process p still runs: its pid shows the same start
// time, in the same boot, and it is not a zombie. Anything unreadable is
// not alive.
func alive(ctx context.Context, sys host.System, p ProcStart) bool {
	if !p.known() {
		return false
	}
	st, err := readStat(ctx, sys, ProcDir+"/"+strconv.Itoa(p.PID))
	if err != nil || st.start != p.Start || st.state == 'Z' || st.state == 'X' {
		return false
	}
	if p.Boot != "" {
		boot, err := sys.ReadFile(ctx, bootIDFile)
		if err != nil || strings.TrimSpace(string(boot)) != p.Boot {
			return false
		}
	}
	return true
}

// CheckConfirm decides whether session s may confirm journal j (brief 2.4:
// a confirmation typed into the session that applied the change proves
// nothing; only a new connection does). The apply must have finished
// (j.Applied; ErrNotConfirmable until then). local is true when s is a
// console, not SSH: the local-console exception, which confirms with a
// warning (losing SSH is not a lockout at the console); the zero Session is
// not established (ErrSessionUnknown). Over SSH, the connection must be
// known (Conn, and its sshd process, SSHD; ErrSessionUnknown otherwise),
// and:
//
//   - it must come from another machine: a client address that is loopback
//     or the server's own is a connection from the host itself, which ufw
//     lets through on lo and no provider firewall sees (ErrLocalConn);
//   - it must not be the connection that applied the change
//     (ErrSameSession), nor one whose sshd process was running when the
//     apply finished (j.Applied.SSHD: ErrOldConnection): an existing
//     session survives an sshd reload and ufw keeps an established
//     connection, so neither shows that a new one works;
//   - with ConfirmVia, it must reach one of those listeners, which the
//     change added (ErrWrongListener otherwise): a connection over a held
//     old port proves nothing about the new one.
//
// A multiplexed session (ssh ControlMaster) shares its connection and its
// sshd process, so it is refused as the connection it rides on.
func CheckConfirm(j Journal, s Session) (local bool, err error) {
	if j.Applied == nil {
		return false, fmt.Errorf("revert: confirm %s: it is still being applied, or its apply stopped: %w", j.ID, ErrNotConfirmable)
	}
	if !s.Remote {
		if s.Source == "" {
			return false, fmt.Errorf("revert: confirm: session not established (ReadSession): %w", ErrSessionUnknown)
		}
		return true, nil
	}
	if s.Conn == "" {
		return false, fmt.Errorf("revert: confirm: found by %s: %w", sourceOr(s.Source), ErrSessionUnknown)
	}
	c, err := ParseConn(s.Conn)
	if err != nil {
		return false, fmt.Errorf("revert: confirm: %w: %w", ErrSessionUnknown, err)
	}
	if a := c.Client.Addr(); a.IsLoopback() || a == c.Server.Addr() {
		return false, fmt.Errorf("revert: confirm: connection from %s to %s: %w", c.Client, c.Server, ErrLocalConn)
	}
	if j.Session.Conn != "" {
		if a, err := ParseConn(j.Session.Conn); err == nil && a == c {
			return false, fmt.Errorf("revert: confirm: %s: %w", c.Client, ErrSameSession)
		}
	}
	if !s.SSHD.known() {
		return false, fmt.Errorf("revert: confirm: no sshd process among the session's ancestors, so when connection %s was made is not known (tmux or screen?): %w", c.Client, ErrSessionUnknown)
	}
	if slices.ContainsFunc(j.Applied.SSHD, s.SSHD.same) {
		return false, fmt.Errorf("revert: confirm: connection %s was open when the change was applied at %s: %w", c.Client, stamp(j.Applied.At), ErrOldConnection)
	}
	if len(j.ConfirmVia) > 0 && !slices.ContainsFunc(j.ConfirmVia, func(l Listener) bool {
		return l.Port == c.Server.Port() && (l.Addr.IsUnspecified() || l.Addr.Unmap() == c.Server.Addr())
	}) {
		return false, fmt.Errorf("revert: confirm: connected to %s, not %s: %w", c.Server, listenerList(j.ConfirmVia), ErrWrongListener)
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
