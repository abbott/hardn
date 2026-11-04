package main

import (
	"bytes"
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestPlaySSHGolden plays each flow end to end and pins the transcript:
// each change planned, typed and applied on the real engine; refusals
// before asking; plan only; the hosts' menu.
func TestPlaySSHGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"ssh-disallow-root", []string{"ssh/debian13"}, []string{"1", hostName, "0"}, append(uitest.Core(), asciiColour)},
		{"ssh-allow-root-key-only", []string{"ssh/hardened"}, []string{"1", hostName, "0"}, nil},
		{"ssh-pve-root", []string{"ssh/pve9"}, []string{"1", hostName, "0"}, nil},
		{"ssh-key-only", []string{"ssh/pve9"}, []string{"2", hostName, "0"}, nil},
		{"ssh-password-on", []string{"ssh/hardened"}, []string{"2", hostName, "0"}, nil},
		{"ssh-port", []string{"ssh/debian13"}, []string{"3", "22", "x", "2230", hostName, "0"}, append(uitest.Core(), asciiColour)},
		{"ssh-port-socket", []string{"ssh/ubuntu2604"}, []string{"3", "2230", hostName, "0"}, nil},
		{"ssh-port-alpine", []string{"ssh/alpine323"}, []string{"3", "2230", hostName, "0"}, nil},
		{"ssh-migrate-v032", []string{"ssh/legacy-v032"}, []string{"1", "4", "0"}, nil},
		{"ssh-migrate-main", []string{"ssh/legacy-main"}, []string{"4", hostName, "0"}, nil},
		{"ssh-root-session", []string{"ssh/root-session"}, []string{"1", "0"}, nil},
		{"ssh-password-session", []string{"ssh/password-session"}, []string{"2", "0"}, nil},
		{"ssh-not-read", []string{"ssh/not-read"}, []string{"1", "2", "3", "0"}, nil},
		{"ssh-confirm-window", []string{"ssh/confirm-window"}, []string{"1", "0"}, nil},
		{"ssh-plan-only", []string{"-sequence", "dry-run", "ssh/debian13"}, []string{"1", "2", "3", "2230", "0"}, nil},
		{"ssh-hosts", []string{"ssh"}, []string{"4", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return sessionIDs(t, out)
		})
	}
}

// TestSSHHosts: each host, the menu's and the variants, reads with the
// control (sshchange.Read), on the session revert.ReadSession finds there,
// with sshd -T read but on not-read, the ports sshd listens on its note's,
// and every call a read the model answers.
func TestSSHHosts(t *testing.T) {
	ports := map[string]string{"hardened": "2230", "legacy-v032": "22, 2230", "confirm-window": "22, 2230", "not-read": ""}
	if n := len(sshHosts()); n != 19 {
		t.Fatalf("%d hosts, 19 wanted", n)
	}
	ctx := context.Background()
	for _, sh := range slices.Concat(sshHosts(), sshVariants()) {
		h := sh.host()
		sys := h.sys()
		f, err := detect.Detect(ctx, sys)
		if err != nil {
			t.Fatalf("%s: %v", sh.name, err)
		}
		sess, _ := revert.ReadSession(ctx, sys, sh.getenv, applyPID)
		r, err := sshchange.Read(ctx, sys, f, nil, sess, sh.getenv)
		want, ok := ports[sh.name]
		if !ok {
			want = "22"
		}
		var got []string
		for _, p := range r.Effective.Listeners.Ports() {
			got = append(got, strconv.Itoa(int(p)))
		}
		if err != nil || (r.EffErr != nil) != (sh.name == "not-read") || strings.Join(got, ", ") != want {
			t.Errorf("%s: ports %q, sshd -T %v, %v; want %q", sh.name, got, r.EffErr, err, want)
		}
		if u := h.unanswered(); len(u) > 0 {
			t.Errorf("%s: unanswered %q", sh.name, u)
		}
	}
}

// TestSSHPortSocket: the port on ssh.socket plans with no refusal, every
// change of lockout risk but the engine's own and the boot's, on the
// gallery's engine; the refusals before asking are pkg/screens/ssh's tests'.
func TestSSHPortSocket(t *testing.T) {
	ctx := context.Background()
	h := sshUbuntu()
	c := sshchange.Port{To: 2230}
	p, err := sshEngine(h, applySession, nil).Plan(ctx, []engine.Control{c}, config.DefaultConfig())
	if err != nil || len(p.Refused) > 0 || len(p.Changes) == 0 {
		t.Fatalf("ubuntu2604, %s: %v, refused %v", c.ID(), err, p.Refused)
	}
	for _, ch := range p.Changes {
		if ch.Risk != engine.RiskLockout && !strings.HasPrefix(ch.ID, "engine.") && !strings.Contains(ch.ID, "boot") {
			t.Errorf("ubuntu2604, %s: change %s of risk %v, want lockout", c.ID(), ch.ID, ch.Risk)
		}
	}
}

// TestSSHMatchModel: the fake's sshd -T -C applies the Match block for a
// connection it matches, and not for one it does not.
func TestSSHMatchModel(t *testing.T) {
	h := sshMatchAllows()
	main, drops := h.files(sshd.MainConfig)
	in, _ := h.sshdT(matched(main, drops, "user=alice,host=192.0.2.10,addr=192.0.2.10"), drops)
	out, _ := h.sshdT(matched(main, drops, "user=alice,host=198.51.100.7,addr=198.51.100.7"), drops)
	if !strings.Contains(in, "permitrootlogin yes\n") || !strings.Contains(out, "permitrootlogin without-password\n") {
		t.Errorf("in:\n%s\nout:\n%s", in, out)
	}
}

// TestSSHUsage: play refuses what it does not take, and the listing is
// gone, with status 2 and nothing on stdout; the main menu's 2 leads to
// debian13's.
func TestSSHUsage(t *testing.T) {
	for _, args := range [][]string{
		{sshPrefix},
		{"play", "ssh/nope"},
		{"play", "-fail", "1", "ssh/debian13"},
		{"play", "-sequence", "failure", "ssh/debian13"},
	} {
		var out, errs bytes.Buffer
		if code := run(args, nil, offTerminal, &out, &errs); code != exitUsage || out.Len() > 0 || errs.Len() == 0 {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, out.String(), errs.String())
		}
	}
	if sshLinks[mainMenuMock+"/2"] != sshMainHost {
		t.Errorf("the main menu's 2 leads to %q", sshLinks[mainMenuMock+"/2"])
	}
}
