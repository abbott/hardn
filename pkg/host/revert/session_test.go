package revert

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

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

// proc adds process pid to the fake's /proc: its initial environment, comm
// and parent.
func proc(f *hostfake.System, pid, ppid string, comm string, env ...string) {
	block := ""
	for _, kv := range env {
		block += kv + "\x00"
	}
	f.AddFile("/proc/"+pid+"/environ", []byte(block), 0o400, 0, 0)
	f.AddFile("/proc/"+pid+"/comm", []byte(comm+"\n"), 0o644, 0, 0)
	f.AddFile("/proc/"+pid+"/status", []byte("Name:\t"+comm+"\nUmask:\t0022\nState:\tS (sleeping)\nTgid:\t"+pid+"\nPid:\t"+pid+"\nPPid:\t"+ppid+"\n"), 0o444, 0, 0)
}

func TestReadSession(t *testing.T) {
	noEnv := func(string) string { return "" }
	tests := []struct {
		name    string
		env     func(string) string
		setup   func(f *hostfake.System)
		want    Session
		unknown bool
	}{
		{"SSH_CONNECTION in the environment", func(k string) string {
			if k == "SSH_CONNECTION" {
				return applyConn
			}
			return ""
		}, nil, Session{Remote: true, Conn: applyConn, Source: "SSH_CONNECTION"}, false},
		{"SSH_TTY only", func(k string) string {
			if k == "SSH_TTY" {
				return "/dev/pts/0"
			}
			return ""
		}, nil, Session{Remote: true, Source: "SSH_TTY"}, false},
		{"sudo reset the environment: found in the login shell's", noEnv, func(f *hostfake.System) {
			proc(f, "4000", "3990", "hardn", "PATH=/usr/sbin:/usr/bin", "SUDO_USER=alice")
			proc(f, "3990", "3980", "sudo", "USER=alice", "SSH_CONNECTION="+newConn)
			proc(f, "3980", "3970", "bash", "USER=alice", "SSH_CONNECTION="+newConn)
			proc(f, "3970", "1", "sshd-session")
		}, Session{Remote: true, Conn: newConn, Source: "/proc/3990/environ"}, false},
		{"su - cleared it all the way: sshd among the ancestors", noEnv, func(f *hostfake.System) {
			proc(f, "4000", "3990", "hardn")
			proc(f, "3990", "3980", "su")
			proc(f, "3980", "1", "sshd")
		}, Session{Remote: true, Source: "/proc/3980/comm"}, false},
		{"a local console: init reached", noEnv, func(f *hostfake.System) {
			proc(f, "4000", "3990", "hardn", "TERM=linux")
			proc(f, "3990", "3980", "bash", "TERM=linux")
			proc(f, "3980", "1", "login")
		}, Session{Source: "no SSH variable or sshd among the ancestors"}, false},
		{"an ancestor cannot be read", noEnv, func(f *hostfake.System) {
			proc(f, "4000", "3990", "hardn")
		}, Session{Remote: true}, true},
		{"no /proc (not Linux)", noEnv, nil, Session{Remote: true}, true},
		{"a status with no PPid", noEnv, func(f *hostfake.System) {
			proc(f, "4000", "3990", "hardn")
			f.AddFile("/proc/4000/status", []byte("Name:\thardn\n"), 0o444, 0, 0)
		}, Session{Remote: true}, true},
		{"a parent loop", noEnv, func(f *hostfake.System) {
			proc(f, "4000", "4001", "a")
			proc(f, "4001", "4000", "b")
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
				t.Fatalf("ReadSession = %+v, %v; want %+v, unknown %v", got, err, tt.want, tt.unknown)
			}
			if !tt.unknown && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckConfirm(t *testing.T) {
	via := lockout()
	noVia := lockout()
	noVia.ConfirmVia = nil
	console := noVia
	console.Session = Session{Source: "no SSH variable or sshd among the ancestors"}
	unknownApply := noVia
	unknownApply.Session = Session{Remote: true, Source: "/proc/3980/comm"}
	remote := func(conn string) Session { return Session{Remote: true, Conn: conn, Source: "SSH_CONNECTION"} }

	tests := []struct {
		name      string
		j         Journal
		s         Session
		wantLocal bool
		want      error
	}{
		{"local console confirms, with the warning", via, Session{}, true, nil},
		{"a new connection to the new port", via, remote(newConn), false, nil},
		{"the new port over IPv6, listener 0.0.0.0 any", via, remote("2001:db8::10 50123 2001:db8::20 2230"), false, nil},
		{"the session that applied it", via, remote(applyConn), false, ErrWrongListener},
		{"a new connection over the held old port", via, remote("192.0.2.10 50999 192.0.2.20 22"), false, ErrWrongListener},
		{"remote, connection not known", via, Session{Remote: true, Source: "/proc/3980/comm"}, false, ErrSessionUnknown},
		{"remote, connection unreadable", via, remote("192.0.2.10 x"), false, ErrSessionUnknown},
		{"no listener to reach: another connection", noVia, remote("192.0.2.11 40000 192.0.2.20 22"), false, nil},
		{"no listener to reach: the same connection (or a multiplexed one)", noVia, remote(applyConn), false, ErrSameSession},
		{"applied at the console, confirmed over SSH", console, remote(applyConn), false, nil},
		{"the applying connection unknown", unknownApply, remote(newConn), false, ErrSessionUnknown},
		{"a specific address", func() Journal {
			j := lockout()
			j.ConfirmVia = []Listener{{Addr: netip.MustParseAddr("192.0.2.20"), Port: 2230}}
			return j
		}(), remote("192.0.2.10 50123 192.0.2.21 2230"), false, ErrWrongListener},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, err := CheckConfirm(tt.j, tt.s)
			if local != tt.wantLocal || !errors.Is(err, tt.want) || (tt.want == nil) != (err == nil) {
				t.Fatalf("CheckConfirm = %v, %v; want %v, %v", local, err, tt.wantLocal, tt.want)
			}
		})
	}
	_, err := CheckConfirm(via, remote(applyConn))
	if err == nil || !strings.Contains(err.Error(), "192.0.2.20:22, not 0.0.0.0:2230") {
		t.Errorf("wrong listener error = %v; want both ends named", err)
	}
}
