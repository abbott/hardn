package main

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// TestSSHGolden pins each listing mock on the core set and under LANG=C:
// the screen on every host, and the refusals.
func TestSSHGolden(t *testing.T) { goldenListing(t, sshListing) }

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

// TestSSHHosts holds each host's rows to the real readers: the three SSH
// rows' details, and the section's root, password and port values, every
// call one the model answers.
func TestSSHHosts(t *testing.T) {
	type want struct{ login, auth, port, root, pass, ports string }
	wants := map[string]want{
		"debian13":         {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
		"ubuntu2604":       {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
		"alpine323":        {"root locked", "key-only auth", "default (22)", "Locked", "Off", "22"},
		"hardened":         {"root disallowed", "key-only auth", "non-default (2230)", "Disallowed", "Off", "2230"},
		"password-login":   {"root key-only", "password auth on", "default (22)", "Key only", "On", "22"},
		"password-session": {"root key-only", "password auth on", "default (22)", "Key only", "On", "22"},
		"root-session":     {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
		"legacy-v032":      {"root disallowed", "key-only auth", "socket and sshd differ", "Disallowed", "Off", "22, 2230"},
		"legacy-main":      {"root disallowed", "password auth on", "default (22)", "Disallowed", "Off", "22"}, // by AuthenticationMethods
		"include-other":    {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
		"match-allows":     {"match block allows root", "match block allows passwords", "default (22)", "Key only", "Off", "22"},
		"allow-users":      {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
		"pve9":             {"root allowed", "password auth on", "default (22)", "Allowed", "On", "22"},
		"ufw-restricted":   {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
		"confirm-window":   {"root key-only", "key-only auth", "22 and 2230", "Key only", "Off", "22, 2230"},
		"not-read":         {"can't read", "can't read", "can't read", "Can't read", "Can't read", "Can't read"},
		"nopam-locked":     {"root locked", "key-only auth", "default (22)", "Locked", "Off", "22"},
		"rsa-small":        {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
		"admin-no-sudo":    {"root key-only", "key-only auth", "default (22)", "Key only", "Off", "22"},
	}
	if len(wants) != len(sshHosts()) {
		t.Fatalf("%d hosts, %d wanted", len(sshHosts()), len(wants))
	}
	ctx := context.Background()
	for _, sh := range sshHosts() {
		h := sh.host()
		r, err := readSSH(ctx, h.sys(), sh.env)
		if err != nil {
			t.Fatalf("%s: %v", sh.name, err)
		}
		w := wants[sh.name]
		var got []string
		for _, row := range r.status.Rows {
			got = append(got, row.Detail)
		}
		if strings.Join(got, "|") != strings.Join([]string{w.login, w.auth, w.port}, "|") {
			t.Errorf("%s: rows %q, want %q", sh.name, got, []string{w.login, w.auth, w.port})
		}
		vals := map[string]string{}
		for _, kv := range r.rows() {
			vals[kv.Key] = kv.Value
		}
		if vals[sshRowRoot] != w.root || !strings.HasPrefix(vals[sshRowPass], w.pass) || vals[sshRowPort] != w.ports {
			t.Errorf("%s: section %q, %q, %q; want %q, %q, %q", sh.name, vals[sshRowRoot], vals[sshRowPass], vals[sshRowPort], w.root, w.pass, w.ports)
		}
		if u := h.unanswered(); len(u) > 0 {
			t.Errorf("%s: unanswered %q", sh.name, u)
		}
	}
}

// TestSSHRefusals: every lockout path and refusal of sshRefusals refuses
// before asking, and the items each host offers without one plan only
// changes of lockout risk.
func TestSSHRefusals(t *testing.T) {
	ctx := context.Background()
	for _, rf := range sshRefusals() {
		words, err := refusalOf(ctx, rf)
		if err != nil || words == "" {
			t.Errorf("%v: %q, %v; want a refusal", rf, words, err)
		}
	}
	for _, tt := range []struct {
		host string
		c    engine.Control
	}{
		{"debian13", sshSet(ssh.Login{}, steps.SSH{Set: []migrate.Directive{directive("PermitRootLogin", "no")}})},
		{"hardened", sshSet(ssh.Login{}, steps.SSH{Set: []migrate.Directive{directive("PermitRootLogin", "prohibit-password")}})},
		{"hardened", sshSet(ssh.Auth{}, steps.SSH{Set: []migrate.Directive{directive("PasswordAuthentication", "yes")}})},
		{"pve9", sshSet(ssh.Auth{}, steps.SSH{Set: []migrate.Directive{directive("PasswordAuthentication", "no")}})},
		{"debian13", sshSet(ssh.Port{}, steps.SSH{Port: 2230})},
		{"ubuntu2604", sshSet(ssh.Port{}, steps.SSH{Port: 2230})},
		{"legacy-main", sshMigration("")},
		{"allow-users", sshSet(ssh.Login{}, steps.SSH{Set: []migrate.Directive{directive("PermitRootLogin", "no")}})},
	} {
		r, h, err := sshReadNamed(ctx, tt.host)
		if err != nil {
			t.Fatal(err)
		}
		p, err := sshEngine(h, r.sess.Conn, nil).Plan(ctx, []engine.Control{tt.c}, r.cfg)
		if err != nil || len(p.Refused) > 0 || len(p.Changes) == 0 {
			t.Errorf("%s, %s: %v, refused %v", tt.host, tt.c.ID(), err, p.Refused)
			continue
		}
		for _, c := range p.Changes {
			if c.Risk != engine.RiskLockout && !strings.HasPrefix(c.ID, "engine.") && !strings.Contains(c.ID, "boot") {
				t.Errorf("%s, %s: change %s of risk %v, want lockout", tt.host, tt.c.ID(), c.ID, c.Risk)
			}
		}
	}
}

// TestSSHPortInUse: a port another program has bound is refused at plan
// time by the real check (reload.ErrInUse), in the screen's words.
func TestSSHPortInUse(t *testing.T) {
	ctx := context.Background()
	h := sshDebian13()
	h.bound = append(h.bound, "0.0.0.0:8080")
	sh, _ := findSSHHost("debian13")
	r, err := readSSH(ctx, h.sys(), sh.env)
	if err != nil {
		t.Fatal(err)
	}
	e := &sshRunner{h: h}
	if words := e.preplan(ctx, r, sshSet(ssh.Port{}, steps.SSH{Port: 8080})); !strings.Contains(words, "in use") {
		t.Errorf("port 8080 in use: %q, want a refusal", words)
	}
}

// TestSSHAdminPaths holds sshLoginPaths, sshd's own answer for each admin:
// george by key on debian13; alice left out by AllowUsers; george locked
// without PAM; on match-allows, alice in by password through the Match
// block while passwords stay on; and each other way an admin is not able:
// AllowUsers' host, DenyUsers, PubkeyAuthentication, AuthenticationMethods,
// StrictModes, a key file sshd doesn't read, the key's type and size, and
// becoming root.
func TestSSHAdminPaths(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		host    string
		keyOnly bool
		want    string
	}{
		{"debian13", true, "george key|alice: alice has no key sshd reads"},
		{"allow-users", true, "george key|alice: alice isn't in AllowUsers"},
		{"nopam-locked", true, "george: george's account is locked (!) and sshd has no PAM"},
		{"match-allows", false, "george key|alice: alice has no key sshd reads, password"},
		{"allow-users-host", true, "george: AllowUsers lets george in from 198.51.100.7 alone|alice: alice isn't in AllowUsers"},
		{"deny-users", true, "george: george is in DenyUsers|alice: alice has no key sshd reads"},
		{"pubkey-off", true, "george: PubkeyAuthentication is off|alice: PubkeyAuthentication is off"},
		{"methods-password", false, "george: AuthenticationMethods publickey,password asks more than george has|alice: alice has no key sshd reads"},
		{"strict-home", true, "george: StrictModes ignores george's keys|alice: alice has no key sshd reads"},
		{"key-elsewhere", true, "george: george has no key sshd reads|alice: alice has no key sshd reads"},
		{"algorithms-rsa", true, "george: sshd doesn't accept george's key type (ssh-ed25519)|alice: alice has no key sshd reads"},
		{"rsa-small", true, "george: george's RSA key is 1024 bits, under RequiredRSASize 2048|alice: alice has no key sshd reads"},
		{"admin-no-sudo", true, "george: george can't become root: sudo asks a password george hasn't got"},
	} {
		r, h, err := sshReadNamed(ctx, tt.host)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, a := range r.sshLoginPaths(ctx, h.sys(), tt.keyOnly) {
			s := a.name + " key"
			if !a.key {
				s = a.name + ": " + a.why
			}
			if a.pwd {
				s += ", password"
			}
			got = append(got, s)
		}
		if strings.Join(got, "|") != tt.want {
			t.Errorf("%s: %q, want %q", tt.host, strings.Join(got, "|"), tt.want)
		}
	}
}

// TestSSHPlanOnlyWritesNothing: under plan only every item opens and stops
// at its plan, and no file outside hardn's own state changes.
func TestSSHPlanOnlyWritesNothing(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
	}{
		{"ssh/debian13", []string{"1", "2", "3", "2230", "0"}},
		{"ssh/hardened", []string{"1", "2", "0"}},
		{"ssh/legacy-main", []string{"4", "0"}},
		{"ssh/pve9", []string{"1", "2", "0"}},
	} {
		code, out, errs, unread := playGallery(tt.lines, "-profile", "plain", "-sequence", "dry-run", tt.host)
		_, record, _ := strings.Cut(out, "# simulated system ")
		if code != exitOK || errs != "" || unread != 0 || !strings.Contains(record, "Nothing was applied.") || strings.Contains(record, "Applied: ") {
			t.Errorf("%s: exit %d, stderr %q, %d unread; the record:\n%s", tt.host, code, errs, unread, record)
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

// TestSSHHeights: every host's screen is as many lines at 80x24 as at
// 80x40, Unicode and ASCII, the prompt on its last, no line over 80 cells.
// Those over 24 rows are the page's §4.
func TestSSHHeights(t *testing.T) {
	ctx := context.Background()
	for _, sh := range sshHosts() {
		r, _, err := sshReadNamed(ctx, sh.name)
		if err != nil {
			t.Fatal(err)
		}
		var n []int
		for _, unicode := range []bool{true, false} {
			for _, rows := range []int{24, 40} {
				env := term.Fixed(80, colorprofile.TrueColor, unicode, true)
				env.Height = rows
				lines := strings.Split(ansi.Strip(r.screen().Render(component.NewCtx(env))), "\n")
				for _, l := range lines {
					if w := ansi.StringWidth(l); w > 80 {
						t.Errorf("%s: %d cells: %q", sh.name, w, l)
					}
				}
				if !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s: the last line %q, want the prompt", sh.name, lines[len(lines)-1])
				}
				n = append(n, len(lines))
			}
		}
		t.Logf("%s: %v", sh.name, n)
		if n[0] != n[1] || n[0] != n[2] || n[0] != n[3] {
			t.Errorf("%s: lines %v, want the same at 24 and 40 rows", sh.name, n)
		}
	}
}

// TestSSHGuards holds the guards the hosts above leave untested: an admin
// under AllowGroups, outside an AllowUsers CIDR, or with only restricted
// keys, not shown able; on
// Proxmox VE, root key-only refused where root has no key; Key-only login
// refused for a session sshd's log lacks, for a Match block letting
// passwords in, and where no admin could log in by key; and the session's
// line found by its address as well as its port.
func TestSSHGuards(t *testing.T) {
	ctx := context.Background()
	sudo := map[string]string{"SSH_CONNECTION": applyConn, "SUDO_USER": "george"}
	read := func(t *testing.T, h *fakeHost, env map[string]string) sshReading {
		t.Helper()
		r, err := readSSH(ctx, h.sys(), env)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	georgeOf := func(r sshReading, h *fakeHost, keyOnly bool) sshAdmin {
		for _, a := range r.sshLoginPaths(ctx, h.sys(), keyOnly) {
			if a.name == "george" {
				return a
			}
		}
		return sshAdmin{}
	}
	t.Run("AllowGroups", func(t *testing.T) {
		h := sshDebian13().sshFile(sshd.DropInDir+"/10-site.conf", "AllowGroups sudo\n", 0o644)
		if a := georgeOf(read(t, h, sudo), h, true); a.key || a.why != "AllowGroups and DenyGroups aren't read" {
			t.Errorf("george: %+v, want not able, groups not read", a)
		}
	})
	t.Run("AllowUsers with a CIDR", func(t *testing.T) {
		for cidr, able := range map[string]bool{"192.0.2.0/24": true, "198.51.100.0/24": false} {
			h := sshDebian13().sshFile(sshd.DropInDir+"/10-site.conf", "AllowUsers george@"+cidr+"\n", 0o644)
			if a := georgeOf(read(t, h, sudo), h, true); a.key != able {
				t.Errorf("AllowUsers george@%s, the session from 192.0.2.10: %+v, want able %v", cidr, a, able)
			}
		}
	})
	t.Run("restricted keys", func(t *testing.T) {
		h := sshDebian13()
		h.f.AddFile("/home/george/.ssh/authorized_keys", []byte(`command="/bin/true" `+synthKey(1, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
		if a := georgeOf(read(t, h, sudo), h, true); a.key {
			t.Errorf("george with a command= key alone: %+v, want not able", a)
		}
	})
	t.Run("Proxmox VE, root without a key", func(t *testing.T) {
		h := usersOn(pve9Platform, false).rootLike("*").onConsole()
		_ = h.f.Remove(ctx, "/root/.ssh/authorized_keys")
		if w := read(t, h, sshConsoleEnv).rootRefusal(ctx, h.sys()); !strings.HasPrefix(w, "Root would lose SSH login") {
			t.Errorf("root key-only with no root key: %q, want a refusal", w)
		}
	})
	t.Run("Key-only, no line in sshd's log", func(t *testing.T) {
		h := sshPasswordLogin()
		h.ssh.log = ""
		if w := read(t, h, sudo).passwordRefusal(ctx, h.sys()); w != sshWordsNoSession {
			t.Errorf("%q, want %q", w, sshWordsNoSession)
		}
	})
	t.Run("Key-only, a Match block letting passwords in", func(t *testing.T) {
		h := sshPasswordLogin().sshFile(sshd.DropInDir+"/20-site.conf", "Match Address 198.51.100.0/24\n    PasswordAuthentication yes\n", 0o644)
		if w := read(t, h, sudo).passwordRefusal(ctx, h.sys()); !strings.HasPrefix(w, "Match Address 198.51.100.0/24, in 20-site.conf") {
			t.Errorf("%q, want the Match block's refusal", w)
		}
	})
	t.Run("Key-only, no admin able by key", func(t *testing.T) {
		h := usersOn(debian13Platform, true, george(debian13Platform)).rootLike("*")
		h.sshFile(sshd.MainConfig, debianMain, 0o644)
		h.f.AddFile("/home/george/.ssh/authorized_keys", []byte(rsaKey(1024, 0x41, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
		h.sshFile(sshd.DropInDir+"/10-rsa.conf", "RequiredRSASize 2048\n", 0o644)
		h.sshFile(sshd.DropInDir+"/50-cloud-init.conf", "PasswordAuthentication yes\n", 0o600).onConsole()
		if w := read(t, h, sshConsoleEnv).passwordRefusal(ctx, h.sys()); !strings.HasPrefix(w, "No admin could log in over SSH afterwards: george's RSA key is 1024 bits") {
			t.Errorf("%q, want no admin able", w)
		}
	})
	t.Run("the session's line by address and port", func(t *testing.T) {
		h := sshDebian13()
		h.ssh.log += "2026-10-20T11:59:00+00:00 " + hostName + " sshd-session[3901]: Accepted password for alice from 198.51.100.7 port " + sshSessionPort + " ssh2\n"
		if r := read(t, h, sudo); r.login.user != "george" || r.login.method != "publickey" {
			t.Errorf("login %+v, want george by publickey: another address's line on the same port taken", r.login)
		}
	})
}
