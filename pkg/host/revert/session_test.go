package revert

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

func TestParseConn(t *testing.T) {
	c, err := ParseConn(applyConn)
	if err != nil || c.Client != netip.MustParseAddrPort("192.0.2.10:50022") || c.Server != netip.MustParseAddrPort("192.0.2.20:22") {
		t.Errorf("ParseConn(%q) = %+v, %v", applyConn, c, err)
	}
	c, err = ParseConn("2001:db8::10 50022 ::ffff:192.0.2.20 2230")
	if err != nil || c.Client.Addr() != netip.MustParseAddr("2001:db8::10") || c.Server != netip.MustParseAddrPort("192.0.2.20:2230") {
		t.Errorf("IPv6 and a mapped address: %+v, %v", c, err)
	}
	for _, bad := range []string{"", "192.0.2.10 50022 192.0.2.20", "192.0.2.10 x 192.0.2.20 22", "web-01.example.net 1 192.0.2.20 22", "192.0.2.10 0 192.0.2.20 22", "192.0.2.10 1 192.0.2.20 65536", "a b c d e"} {
		if _, err := ParseConn(bad); err == nil {
			t.Errorf("ParseConn(%q) accepted", bad)
		}
	}
}

func TestParseStat(t *testing.T) {
	st, err := parseStat([]byte(statLine(4000, 3990, "hardn", ptsTTY, 123456)))
	if err != nil || st != (procStat{pid: 4000, comm: "hardn", state: 'S', ppid: 3990, tty: ptsTTY, start: 123456}) {
		t.Fatalf("parseStat = %+v, %v", st, err)
	}
	// A comm with spaces and parentheses ends at the last ')'.
	st, err = parseStat([]byte(statLine(4000, 1, "a) (b", 0, 7)))
	if err != nil || st.comm != "a) (b" || st.ppid != 1 || st.start != 7 {
		t.Fatalf("parseStat(odd comm) = %+v, %v", st, err)
	}
	for _, bad := range []string{"", "4000 hardn S 1", "x (hardn) S 1 1 1 0", "4000 (hardn) S 1 1 1 0 -1", "4000 (hardn) S x" + strings.Repeat(" 0", 20)} {
		if _, err := parseStat([]byte(bad)); err == nil {
			t.Errorf("parseStat(%q) accepted", bad)
		}
	}
	if ttyMajor(ptsTTY) != 136 || ttyMinor(ptsTTY) != 3 || ttyMinor(0x01_00_8a_05) != 0x1005 {
		t.Errorf("tty numbers: %d:%d, %#x", ttyMajor(ptsTTY), ttyMinor(ptsTTY), ttyMinor(0x01_00_8a_05))
	}
	for nr, want := range map[int]bool{0: false, ptsTTY: false, 143<<8 | 9: false, legacyPTYTTY: false, consoleTTY1: true, serialTTYS0: true, 229 << 8: true} {
		if consoleTTY(nr) != want {
			t.Errorf("consoleTTY(%d:%d) = %v", ttyMajor(nr), ttyMinor(nr), !want)
		}
	}
}

func TestReadSession(t *testing.T) {
	noEnv := func(string) string { return "" }
	envOf := func(k, v string) func(string) string {
		return func(key string) string {
			if key == k {
				return v
			}
			return ""
		}
	}
	// An SSH login: sshd listener, the connection's sshd-session, bash.
	sshLogin := func(f *hostfake.System) {
		proc(f, listenerPID, 1, "sshd", 0)
		proc(f, newSSHD, listenerPID, "sshd-session", 0)
		proc(f, 3980, newSSHD, "bash", ptsTTY, "USER=alice", "SSH_CONNECTION="+newConn)
	}
	tests := []struct {
		name    string
		env     func(string) string
		setup   func(f *hostfake.System)
		want    Session
		unknown bool
	}{
		{"root over SSH, no sudo: the variable, and the walk to sshd-session", envOf("SSH_CONNECTION", newConn), func(f *hostfake.System) {
			sshLogin(f)
			proc(f, 4000, 3980, "hardn", ptsTTY)
		}, Session{Remote: true, Conn: newConn, SSHD: sshdOf(newSSHD), Source: "SSH_CONNECTION"}, false},
		{"SSH_TTY only: the connection from the login shell", envOf("SSH_TTY", "/dev/pts/3"), func(f *hostfake.System) {
			sshLogin(f)
			proc(f, 4000, 3980, "hardn", ptsTTY)
		}, Session{Remote: true, Conn: newConn, SSHD: sshdOf(newSSHD), Source: "/proc/3980/environ"}, false},
		{"sudo reset the environment: found in the login shell's", noEnv, func(f *hostfake.System) {
			sshLogin(f)
			proc(f, 3990, 3980, "sudo", ptsTTY, "USER=alice", "SSH_CONNECTION="+newConn)
			proc(f, 4000, 3990, "hardn", ptsTTY, "PATH=/usr/sbin:/usr/bin", "SUDO_USER=alice")
		}, Session{Remote: true, Conn: newConn, SSHD: sshdOf(newSSHD), Source: "/proc/3990/environ"}, false},
		{"su - cleared it all the way: sshd among the ancestors", noEnv, func(f *hostfake.System) {
			proc(f, 3980, 1, "sshd", 0)
			proc(f, 3990, 3980, "su", ptsTTY)
			proc(f, 4000, 3990, "hardn", ptsTTY)
		}, Session{Remote: true, SSHD: sshdOf(3980), Source: "/proc/3980/stat"}, false},
		{"tmux attached over SSH: the variable, no sshd", noEnv, func(f *hostfake.System) {
			proc(f, 3000, 1, "tmux: server", 0, "SSH_CONNECTION="+applyConn)
			proc(f, 3980, 3000, "bash", ptsTTY, "SSH_CONNECTION="+newConn)
			proc(f, 4000, 3980, "hardn", ptsTTY)
		}, Session{Remote: true, Conn: newConn, Source: "/proc/3980/environ"}, false},
		{"a console login", noEnv, func(f *hostfake.System) {
			proc(f, 3970, 1, "login", consoleTTY1)
			proc(f, 3980, 3970, "bash", consoleTTY1, "TERM=linux")
			proc(f, 3990, 3980, "sudo", consoleTTY1)
			proc(f, 4000, 3990, "hardn", ptsTTY, "TERM=linux") // sudo's use_pty
		}, Session{Source: "a console login (/proc/3970 is login)"}, false},
		{"Proxmox's web console: login under termproxy", noEnv, func(f *hostfake.System) {
			proc(f, 2000, 1, "pvedaemon", 0)
			proc(f, 3000, 2000, "termproxy", 0)
			proc(f, 3970, 3000, "login", ptsTTY)
			proc(f, 3980, 3970, "bash", ptsTTY)
			proc(f, 4000, 3980, "hardn", ptsTTY)
		}, Session{Source: "a console login (/proc/3970 is login)"}, false},
		{"Alpine's console: busybox login exec'd the shell on tty1", noEnv, func(f *hostfake.System) {
			proc(f, 3980, 1, "ash", consoleTTY1)
			proc(f, 3990, 3980, "doas", consoleTTY1)
			proc(f, 4000, 3990, "hardn", consoleTTY1)
		}, Session{Source: "a console (/proc/3980 has terminal 4:1)"}, false},
		{"a serial console", noEnv, func(f *hostfake.System) {
			proc(f, 3980, 1, "bash", serialTTYS0)
			proc(f, 4000, 3980, "hardn", serialTTYS0)
		}, Session{Source: "a console (/proc/3980 has terminal 4:64)"}, false},
		{"run0 hardn confirm: the service manager's child", noEnv, func(f *hostfake.System) {
			proc(f, 4000, 1, "hardn", ptsTTY, "TERM=xterm-256color", "SUDO_USER=alice")
		}, Session{Remote: true, Source: "no SSH connection, sshd or console login among the ancestors (run0, systemd-run, tmux or screen?)"}, true},
		{"tmux started at the console", noEnv, func(f *hostfake.System) {
			proc(f, 3000, 1, "tmux: server", 0)
			proc(f, 3980, 3000, "bash", ptsTTY)
			proc(f, 4000, 3980, "hardn", ptsTTY)
		}, Session{Remote: true, Source: "no SSH connection, sshd or console login among the ancestors (run0, systemd-run, tmux or screen?)"}, true},
		{"an ancestor cannot be read", noEnv, func(f *hostfake.System) {
			proc(f, 4000, 3990, "hardn", ptsTTY)
		}, Session{Remote: true}, true},
		{"the variable, then an ancestor that cannot be read", envOf("SSH_CONNECTION", newConn), func(f *hostfake.System) {
			proc(f, 4000, 3990, "hardn", ptsTTY)
		}, Session{Remote: true, Conn: newConn, Source: "SSH_CONNECTION"}, true},
		{"no /proc (not Linux)", noEnv, nil, Session{Remote: true}, true},
		{"a stat that does not parse", noEnv, func(f *hostfake.System) {
			proc(f, 4000, 3990, "hardn", ptsTTY)
			f.AddFile("/proc/4000/stat", []byte("4000 (hardn) S\n"), 0o444, 0, 0)
		}, Session{Remote: true}, true},
		{"a parent loop", noEnv, func(f *hostfake.System) {
			proc(f, 4000, 4001, "a", 0)
			proc(f, 4001, 4000, "b", 0)
		}, Session{Remote: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := hostfake.New(host.Options{})
			if tt.setup != nil {
				tt.setup(f)
			}
			got, err := ReadSession(context.Background(), f, tt.env, 4000)
			if tt.unknown != errors.Is(err, ErrSessionUnknown) || got != tt.want {
				t.Fatalf("ReadSession = %+v, %v\nwant %+v, unknown %v", got, err, tt.want, tt.unknown)
			}
			if !tt.unknown && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckConfirm(t *testing.T) {
	applied := &Applied{At: testNow.Add(10 * time.Second), SSHD: []ProcStart{sshdOf(listenerPID), sshdOf(applySSHD), sshdOf(keptSSHD)}}
	via := lockout()
	via.Applied = applied
	noVia := via
	noVia.ConfirmVia = nil
	console := noVia
	console.Session = Session{Source: "a console login (/proc/3970 is login)"}
	unknownApply := noVia
	unknownApply.Session = Session{Remote: true, Source: "/proc/3980/stat"}
	applying := via
	applying.Applied = nil
	local := Session{Source: "a console login (/proc/3970 is login)"}

	tests := []struct {
		name      string
		j         Journal
		s         Session
		wantLocal bool
		want      error
	}{
		{"local console confirms, with the warning", via, local, true, nil},
		{"the zero session is not a console", via, Session{}, false, ErrSessionUnknown},
		{"still being applied", applying, newSession, false, ErrNotConfirmable},
		{"still being applied, at the console", applying, local, false, ErrNotConfirmable},
		{"a new connection to the new port", via, newSession, false, nil},
		{"the new port over IPv6, listener 0.0.0.0 any", via, remote("2001:db8::10 50123 2001:db8::20 2230", newSSHD), false, nil},
		{"the session that applied it", via, remote(applyConn, applySSHD), false, ErrSameSession},
		{"a new connection over the held old port", via, remote("192.0.2.10 50999 192.0.2.20 22", newSSHD), false, ErrWrongListener},
		{"remote, connection not known", via, Session{Remote: true, Source: "/proc/3980/stat"}, false, ErrSessionUnknown},
		{"remote, connection unreadable", via, remote("192.0.2.10 x", newSSHD), false, ErrSessionUnknown},
		{"remote, no sshd process (tmux)", via, Session{Remote: true, Conn: newConn, Source: "/proc/3980/environ"}, false, ErrSessionUnknown},
		{"ssh -p 2230 localhost from the applying session", via, remote("127.0.0.1 40000 127.0.0.1 2230", newSSHD), false, ErrLocalConn},
		{"a -L 2230:localhost:2230 tunnel over the held port 22", via, remote("127.0.0.1 40000 127.0.0.1 2230", newSSHD), false, ErrLocalConn},
		{"from the host to its own address", via, remote("192.0.2.20 40000 192.0.2.20 2230", newSSHD), false, ErrLocalConn},
		{"ssh localhost over IPv6", noVia, remote("::1 40000 ::1 22", newSSHD), false, ErrLocalConn},
		{"no listener to reach: a new connection", noVia, remote("192.0.2.11 40000 192.0.2.20 22", newSSHD), false, nil},
		{"no listener to reach: the same connection (or a multiplexed one)", noVia, remote(applyConn, applySSHD), false, ErrSameSession},
		{"no listener to reach: a connection kept open from before, another client port", noVia, remote("192.0.2.10 49001 192.0.2.20 22", keptSSHD), false, ErrOldConnection},
		{"with a listener: an old connection's multiplexed session on 2230 is refused", via, remote("192.0.2.10 49001 192.0.2.20 2230", keptSSHD), false, ErrOldConnection},
		{"a pid reused since the apply is a new process", noVia, Session{Remote: true, Conn: "192.0.2.11 40000 192.0.2.20 22", SSHD: ProcStart{PID: keptSSHD, Start: procStart(keptSSHD) + 1}, Source: "SSH_CONNECTION"}, false, nil},
		{"applied at the console, confirmed over SSH", console, remote("192.0.2.11 40000 192.0.2.20 22", newSSHD), false, nil},
		{"applied at the console, a session kept open since", console, remote("192.0.2.11 40000 192.0.2.20 22", keptSSHD), false, ErrOldConnection},
		{"the applying connection unknown: the sshd processes still tell", unknownApply, remote("192.0.2.11 40000 192.0.2.20 22", newSSHD), false, nil},
		{"a specific address", func() Journal {
			j := via
			j.ConfirmVia = []Listener{{Addr: netip.MustParseAddr("192.0.2.20"), Port: 2230}}
			return j
		}(), remote("192.0.2.10 50123 192.0.2.21 2230", newSSHD), false, ErrWrongListener},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, err := CheckConfirm(tt.j, tt.s)
			if local != tt.wantLocal || !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("CheckConfirm = %v, %v; want %v, %v", local, err, tt.wantLocal, tt.want)
			}
		})
	}
	_, err := CheckConfirm(via, remote("192.0.2.10 50999 192.0.2.20 22", newSSHD))
	if err == nil || !strings.Contains(err.Error(), "192.0.2.20:22, not 0.0.0.0:2230") {
		t.Errorf("wrong listener error = %v; want both ends named", err)
	}
	_, err = CheckConfirm(noVia, remote("192.0.2.10 49001 192.0.2.20 22", keptSSHD))
	if err == nil || !strings.Contains(err.Error(), "open when the change was applied at 2026-10-18 12:00:10 UTC") {
		t.Errorf("old connection error = %v", err)
	}
}

func TestSSHDProcesses(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	sshds(f)
	proc(f, 3980, applySSHD, "bash", ptsTTY)
	proc(f, 3000, 1, "sshd-auth", 0) // OpenSSH 10's pre-auth helper: not a session's server end
	got, err := sshdProcesses(ctx, f)
	want := []ProcStart{sshdOf(keptSSHD), sshdOf(applySSHD), sshdOf(listenerPID)}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("sshdProcesses = %+v, %v; want %+v", got, err, want)
	}
	// A process that ended during the listing is skipped; one whose stat
	// cannot be read, while it runs, fails the listing.
	f.AddDir("/proc/4444", 0o555, 0, 0)
	f.AddFile("/proc/4444/stat", []byte("garbage"), 0o444, 0, 0)
	if _, err := sshdProcesses(ctx, f); err == nil {
		t.Error("an unreadable stat was skipped")
	}
	if _, err := sshdProcesses(ctx, hostfake.New(host.Options{})); err == nil {
		t.Error("no /proc listed nothing")
	}
}

func TestAlive(t *testing.T) {
	ctx := context.Background()
	f := newHost(t)
	proc(f, 6000, 1, "hardn", 0)
	f.AddFile("/proc/6001/stat", []byte(strings.Replace(statLine(6001, 1, "hardn", 0, procStart(6001)), " S ", " Z ", 1)), 0o444, 0, 0)
	for _, tt := range []struct {
		p    ProcStart
		want bool
	}{
		{ProcStart{PID: 6000, Start: procStart(6000), Boot: testBootID}, true},
		{ProcStart{PID: 6000, Start: procStart(6000)}, true},
		{ProcStart{PID: 6000, Start: procStart(6000) - 1}, false}, // the pid was reused
		{ProcStart{PID: 6000, Start: procStart(6000), Boot: "another boot"}, false},
		{ProcStart{PID: 6001, Start: procStart(6001)}, false}, // a zombie
		{ProcStart{PID: 6002, Start: 1}, false},
		{ProcStart{}, false},
	} {
		if got := alive(ctx, f, tt.p); got != tt.want {
			t.Errorf("alive(%+v) = %v", tt.p, got)
		}
	}
	if p, err := selfProc(ctx, f); err != nil || p != (ProcStart{PID: selfPID, Start: procStart(selfPID), Boot: testBootID}) {
		t.Errorf("selfProc = %+v, %v", p, err)
	}
}
