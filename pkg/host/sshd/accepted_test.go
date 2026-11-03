package sshd_test

import (
	"context"
	"errors"
	"io/fs"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/sshd"
)

var journal = []string{"journalctl", "-q", "--no-pager", "-o", "short-iso", "-u", "ssh", "-u", "sshd"}

// The forms of the VMs' Accepted lines (01-inspect-b-*.log): journalctl
// -o short-iso on Debian 13 and Ubuntu 26.04; busybox syslogd -t's
// /var/log/messages on Alpine 3.23. Synthetic users, addresses and
// fingerprints.
const (
	journalLog = `2026-11-03T21:10:40+00:00 web-01 sshd-session[3101]: Accepted password for alice from 192.0.2.10 port 54804 ssh2
2026-11-03T21:11:02+00:00 web-01 sshd-session[3120]: Accepted publickey for george from 198.51.100.7 port 54806 ssh2: ED25519 SHA256:AAAAgeorgeAAAAsyntheticAAAAAAAAAAAAAAAAAAA
2026-11-03T21:11:05+00:00 web-01 sshd-session[3130]: Accepted publickey for carol from 192.0.2.11 port 54806 ssh2: RSA SHA256:BBBBcarolBBBBsyntheticBBBBBBBBBBBBBBBBBBBBB
2026-11-03T21:11:09+00:00 web-01 sshd-session[3140]: Accepted keyboard-interactive/pam for alice from 2001:db8::5 port 2230 ssh2
`
	messages = `Nov  3 21:11:02 web-01 auth.info sshd-session[3120]: Accepted publickey for george from 198.51.100.7 port 54806 ssh2: ED25519 SHA256:AAAAgeorgeAAAAsyntheticAAAAAAAAAAAAAAAAAAA
Nov  3 21:11:03 web-01 auth.info sshd-session[3121]: Connection closed by 192.0.2.10 port 54804
`
	rotated = `Nov  3 18:22:00 web-01 auth.info sshd-session[2990]: Accepted password for alice from 192.0.2.10 port 54804 ssh2
`
)

func TestReadAcceptedJournal(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Stdout: journalLog}, journal[0], journal[1:]...)
	ctx := context.Background()
	for _, tc := range []struct {
		name, conn string
		want       sshd.Accepted
	}{
		{"by key, the session's address on a shared port", "198.51.100.7 54806 192.0.2.1 22",
			sshd.Accepted{User: "george", Method: "publickey", KeyType: "ED25519", Fingerprint: "SHA256:AAAAgeorgeAAAAsyntheticAAAAAAAAAAAAAAAAAAA"}},
		{"the other address", "192.0.2.11 54806 192.0.2.1 22",
			sshd.Accepted{User: "carol", Method: "publickey", KeyType: "RSA", Fingerprint: "SHA256:BBBBcarolBBBBsyntheticBBBBBBBBBBBBBBBBBBBBB"}},
		{"by password", "192.0.2.10 54804 192.0.2.1 22", sshd.Accepted{User: "alice", Method: "password"}},
		{"IPv6", "2001:db8::5 2230 2001:db8::1 2230", sshd.Accepted{User: "alice", Method: "keyboard-interactive/pam"}},
	} {
		a, err := sshd.ReadAccepted(ctx, h, false, tc.conn)
		if err != nil || a != tc.want {
			t.Errorf("%s: ReadAccepted = %+v, %v; want %+v", tc.name, a, err, tc.want)
		}
	}
	if _, err := sshd.ReadAccepted(ctx, h, false, "192.0.2.12 54806 192.0.2.1 22"); !errors.Is(err, sshd.ErrNotLogged) {
		t.Errorf("no line: %v, want ErrNotLogged", err)
	}
	checkReadOnly(t, h)
	for _, r := range h.Runs() {
		if r[0] != "journalctl" {
			t.Errorf("ran %q", r)
		}
	}
}

func TestReadAcceptedMessages(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddFile("/var/log/messages", []byte(messages), 0o640, 0, 10)
	h.AddFile("/var/log/messages.0", []byte(rotated), 0o640, 0, 10)
	ctx := context.Background()
	a, err := sshd.ReadAccepted(ctx, h, true, "198.51.100.7 54806 192.0.2.1 22")
	if err != nil || a.User != "george" || a.KeyType != "ED25519" {
		t.Errorf("messages: %+v, %v", a, err)
	}
	if a, err = sshd.ReadAccepted(ctx, h, true, "192.0.2.10 54804 192.0.2.1 22"); err != nil || a != (sshd.Accepted{User: "alice", Method: "password"}) {
		t.Errorf("messages.0: %+v, %v", a, err)
	}
	if _, err = sshd.ReadAccepted(ctx, h, true, "192.0.2.10 1 192.0.2.1 22"); !errors.Is(err, sshd.ErrNotLogged) {
		t.Errorf("no line: %v, want ErrNotLogged", err)
	}
	if len(h.Runs()) != 0 {
		t.Errorf("ran %q", h.Runs())
	}
	checkReadOnly(t, h)
	if _, err = sshd.ReadAccepted(ctx, hostfake.New(host.Options{}), true, "192.0.2.10 54804 192.0.2.1 22"); !errors.Is(err, sshd.ErrNotLogged) {
		t.Errorf("no log at all: %v, want ErrNotLogged", err)
	}
	for _, conn := range []string{"", "192.0.2.10 54804", "web-01 54804 192.0.2.1 22", "192.0.2.10 0 192.0.2.1 22", "192.0.2.10 70000 192.0.2.1 22"} {
		if _, err := sshd.ReadAccepted(ctx, h, false, conn); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("conn %q: %v, want fs.ErrInvalid", conn, err)
		}
	}
}
