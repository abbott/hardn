package main

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/host/sshd"
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
		"legacy-main":      {"root disallowed", "key-only auth", "default (22)", "Disallowed", "Off", "22"}, // by AuthenticationMethods
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
// before asking, through the engine's plan of the item's control. The
// refusals' logic, words and codes are sshchange's tests'
// (pkg/engine/controls/sshchange); kept here, the one plan they lack: the
// port on ssh.socket, of lockout risk alone.
func TestSSHRefusals(t *testing.T) {
	ctx := context.Background()
	for _, rf := range sshRefusals() {
		words, err := refusalOf(ctx, rf)
		if err != nil || words == "" {
			t.Errorf("%v: %q, %v; want a refusal", rf, words, err)
		}
	}
	r, h, err := sshReadNamed(ctx, "ubuntu2604")
	if err != nil {
		t.Fatal(err)
	}
	c := sshchange.Port{To: 2230}
	p, err := sshEngine(h, r.session(), nil).Plan(ctx, []engine.Control{c}, r.cfg)
	if err != nil || len(p.Refused) > 0 || len(p.Changes) == 0 {
		t.Fatalf("ubuntu2604, %s: %v, refused %v", c.ID(), err, p.Refused)
	}
	for _, ch := range p.Changes {
		if ch.Risk != engine.RiskLockout && !strings.HasPrefix(ch.ID, "engine.") && !strings.Contains(ch.ID, "boot") {
			t.Errorf("ubuntu2604, %s: change %s of risk %v, want lockout", c.ID(), ch.ID, ch.Risk)
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
	if words := e.preplan(ctx, r, sshchange.Port{To: 8080}); !strings.Contains(words, "in use") {
		t.Errorf("port 8080 in use: %q, want a refusal", words)
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
