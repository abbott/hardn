package ssh_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host/sshd"
	sshscreen "github.com/abbott/hardn/pkg/screens/ssh"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The flow's words a test looks for: the typed confirmation's refusal, plan
// only's end, an apply's.
const (
	cantConfirm = "Can't confirm with the hostname:"
	planned     = "No actual changes were made"
	applied     = "Operation Successful"
	typeIt      = "Type the hostname"
)

// The port form's words, the adopted mock's.
const (
	wordsPortRange = "Type a port from 1 to 65535."
	wordsPortHave  = "sshd listens on 22 already."
)

// TestSSHHosts holds each host's rows to the real readers, as the gallery's
// TestSSHHosts did: the three rows' details, and the section's root,
// password and port values, every call one the fake answers; drawing writes
// nothing and makes no engine, live and under plan only.
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
	if len(wants) != len(sshHosts) {
		t.Fatalf("%d hosts, %d wanted", len(sshHosts), len(wants))
	}
	for _, name := range sshHosts {
		for _, dryRun := range []bool{false, true} {
			h := ready(t, name)
			n := len(h.Calls())
			p := play(h, dryRun, "0")
			w := wants[name]
			got := []string{line(p.out, true, "SSH Login"), line(p.out, true, "SSH Auth"), line(p.out, true, "SSH Port"),
				line(p.out, false, "Root SSH Login"), line(p.out, false, "Password Login"), line(p.out, false, "SSH Port")}
			for i, s := range []string{w.login, w.auth, w.port, w.root, w.pass, w.ports} {
				if !strings.HasSuffix(got[i], " "+s) && !strings.Contains(got[i], " "+s+" ") {
					t.Errorf("%s: row %d %q, want %q", name, i, got[i], s)
				}
			}
			if p.err != nil || p.engines != 0 || len(writes(h, n)) > 0 {
				t.Errorf("%s, plan only %v: Run = %v, %d engines, wrote %v", name, dryRun, p.err, p.engines, writes(h, n))
			}
			checkHost(t, name, h)
		}
	}
}

// line is the first line of out with label after its mark, from the label
// on: a status row's where status is set, above the section, else a
// section row's.
func line(out string, status bool, label string) string {
	head, section, _ := strings.Cut(out, "\n  SSH Configuration ")
	if status {
		section = head
	}
	for _, l := range strings.Split(section, "\n") {
		f := strings.TrimSpace(l)
		if i := strings.Index(f, label); i >= 0 && strings.TrimSpace(strings.TrimLeft(f[:i], "[]+ -!?x")) == "" {
			return strings.Join(strings.Fields(f[i:]), " ")
		}
	}
	return ""
}

// refusals are the lockout paths and refusals of gallery-ssh.md §6, by
// item and host, in their decided words, as the gallery's refusals mock
// drew them: each said before asking.
var refusals = []struct{ item, host, words string }{
	{"1", "root-session", "You're logged in as root over SSH. Turning root login off would close the way you came in. Log in as an admin and run hardn through sudo."},
	{"1", "nopam-locked", "No admin could log in over SSH afterwards: george's account is locked (!) and sshd has no PAM. Fix that first."},
	{"1", "rsa-small", "No admin could log in over SSH afterwards: george's RSA key is 1024 bits, under RequiredRSASize 2048; alice has no key sshd reads. Fix that first."},
	{"1", "admin-no-sudo", "No admin could log in over SSH afterwards: george can't become root: sudo asks a password george hasn't got. Fix that first."},
	{"1", "allow-users-host", "No admin could log in over SSH afterwards: AllowUsers lets george in from 198.51.100.7 alone; alice isn't in AllowUsers. Fix that first."},
	{"1", "no-accepted", "hardn can't tell how you logged in: sshd's log has no line for this connection. Nothing that closes a way in can change."},
	{"1", "match-allows", "Match Address 192.0.2.0/24, in 20-site.conf, lets more in for some connections, and hardn's setting wouldn't change that. Change it there first."},
	{"1", "not-read", "/run/sshd isn't there yet, so sshd -T can't run. Open one SSH connection, then try again."},
	{"2", "not-read", "/run/sshd isn't there yet, so sshd -T can't run. Open one SSH connection, then try again."},
	{"3", "not-read", "/run/sshd isn't there yet, so sshd -T can't run. Open one SSH connection, then try again."},
	{"1", "legacy-main", "hardn's old SSH files are still here (hardn.conf). Move them into 00-hardn.conf first: item 4."},
	{"2", "legacy-main", "hardn's old SSH files are still here (hardn.conf). Move them into 00-hardn.conf first: item 4."},
	{"3", "legacy-main", "hardn's old SSH files are still here (hardn.conf). Move them into 00-hardn.conf first: item 4."},
	{"1", "include-other", "sshd_config doesn't start with \"Include /etc/ssh/sshd_config.d/*.conf\", so hardn's settings wouldn't be read first. Put that line first, then try again."},
	{"1", "confirm-window", "A change is waiting for hardn confirm. Confirm it from a new connection, or let it roll back, then try again."},
	{"2", "password-login", "alice would lose SSH login: alice has a password and no key. Add a key in User Management first."},
	{"2", "password-session", "You logged in with a password. Turning passwords off would close the way you came in. Log in with a key first."},
	{"1", "pve9-password", "You logged in as root with a password. Making root key-only would close the way you came in. Log in as root with a key first."},
	{"3", "pve9", "The SSH port stays 22 on Proxmox VE: the cluster and pve-firewall use it."},
	{"3", "ufw-restricted", "ufw lets port 22 in from 192.0.2.0/24 alone. A new port would be open to anyone. Move that rule to the new port by hand first."},
	{"1", "legacy-v032", "hardn's old SSH files are still here (manage.conf, listen.conf). Move them into 00-hardn.conf first: item 4."},
	{"3", "legacy-v032", "hardn's old SSH files are still here (manage.conf, listen.conf). Move them into 00-hardn.conf first: item 4."},
	{"4", "legacy-v032", "hardn v0.3.x's listen.conf (port 2230 for ssh.socket) never took effect: ssh.socket listens on 22. Moving it now couldn't be undone. " +
		"Let 2230 through the firewall, run systemctl daemon-reload and systemctl restart ssh.socket, check a new connection to 2230 works, then try again."},
}

// TestRefusals: every refusal of gallery-ssh.md §6 is said before asking,
// in its words, live and under plan only, from the engine's plan of the
// item's control (one engine), writing nothing; the port's before its
// field.
func TestRefusals(t *testing.T) {
	for _, rf := range refusals {
		for _, dryRun := range []bool{false, true} {
			h := ready(t, rf.host)
			n := len(h.Calls())
			p := play(h, dryRun, rf.item, "0")
			if p.err != nil || !shows(p.out, rf.words) || asked(p.out) || strings.Contains(p.out, "New SSH port") || p.engines != 1 || len(p.runs) != 0 {
				t.Errorf("%s on %s, plan only %v: Run = %v, %d engines, %d runs:\n%s", rf.item, rf.host, dryRun, p.err, p.engines, len(p.runs), p.out)
			}
			if w := writes(h, n); len(w) > 0 {
				t.Errorf("%s on %s: wrote %v", rf.item, rf.host, w)
			}
			checkHost(t, rf.host, h)
		}
	}
}

// TestNoHostKeys: where sshd is installed but has no host keys, the three
// rows say so, Unknown, and items 1, 2 and 3 are refused before asking
// anything, in the refusal's words for the host (on Alpine, apk del with
// World's entries that keep sshd installed), writing nothing, live and
// under plan only.
func TestNoHostKeys(t *testing.T) {
	const why = "sshd has no host keys, so hardn can't read its settings. "
	for host, words := range map[string]string{
		"no-hostkeys-alpine": why + "Run rc-service sshd start, or apk del openssh openssh-server-pam, then try again.",
		"no-hostkeys-debian": why + "Run ssh-keygen -A and systemctl start ssh, or apt remove openssh-server, then try again.",
	} {
		for _, dryRun := range []bool{false, true} {
			h := ready(t, host)
			p := play(h, dryRun, "0")
			for _, row := range []string{"SSH Login", "SSH Auth", "SSH Port"} {
				if got := line(p.out, true, row); !strings.HasSuffix(got, " sshd off, no host keys") {
					t.Errorf("%s: %s row %q, want sshd off, no host keys", host, row, got)
				}
			}
			checkHost(t, host, h)
			for _, item := range []string{"1", "2", "3"} {
				h := ready(t, host)
				n := len(h.Calls())
				p := play(h, dryRun, item, "0")
				if p.err != nil || !shows(p.out, words) || asked(p.out) || strings.Contains(p.out, "New SSH port") || p.engines != 1 || len(p.runs) != 0 {
					t.Errorf("%s on %s, plan only %v: Run = %v, %d engines, %d runs:\n%s", item, host, dryRun, p.err, p.engines, len(p.runs), p.out)
				}
				if w := writes(h, n); len(w) > 0 {
					t.Errorf("%s on %s: wrote %v", item, host, w)
				}
				checkHost(t, host, h)
			}
		}
	}
}

// TestDisallow: Disallow root login on debian13, typed and applied, writes
// PermitRootLogin no to 00-hardn.conf, and the screen drawn again from a
// fresh reading offers Allow root key-only; under plan only it stops at
// its plan.
func TestDisallow(t *testing.T) {
	h := ready(t, "debian13")
	p := play(h, false, "1", hostName, "0")
	data, _ := h.ReadFile(context.Background(), sshd.HardnDropIn)
	if p.err != nil || !shows(p.out, "Disallow root login?") || !shows(p.out, applied) || len(p.runs) != 1 || p.runs[0].Err != nil ||
		!strings.Contains(string(data), "PermitRootLogin no") || !shows(p.out[strings.LastIndex(p.out, "# SSH Login #"):], "Allow root key-only") {
		t.Errorf("Disallow: Run = %v, runs %+v, 00-hardn.conf %q:\n%s", p.err, p.runs, data, p.out)
	}
	checkHost(t, "debian13", h)
	// Allow root key-only back, on hardened.
	h = ready(t, "hardened")
	p = play(h, false, "1", hostName, "0")
	data, _ = h.ReadFile(context.Background(), sshd.HardnDropIn)
	if p.err != nil || !shows(p.out, "Allow root to log in by key?") || !strings.Contains(string(data), "PermitRootLogin prohibit-password") {
		t.Errorf("Allow root key-only: Run = %v:\n%s", p.err, p.out)
	}
	h = ready(t, "debian13")
	n := len(h.Calls())
	p = play(h, true, "1", "0")
	if p.err != nil || !shows(p.out, planned) || shows(p.out, applied) || len(writes(h, n)) > 0 {
		t.Errorf("Disallow, plan only: Run = %v, wrote %v:\n%s", p.err, writes(h, n), p.out)
	}
}

// TestItems: every item of the mocks, planned, typed and applied on its
// host: Root key-only and Key-only login on Proxmox VE, Password login on,
// the port on ssh.service, ssh.socket and OpenRC, Move old settings on
// legacy-main.
func TestItems(t *testing.T) {
	for _, tt := range []struct {
		host, item, question string
		extra                []string
		file, has            string
	}{
		{"pve9", "1", "Make root key-only?", nil, sshd.HardnDropIn, "PermitRootLogin prohibit-password"},
		{"pve9", "2", "Turn password login off?", nil, sshd.HardnDropIn, "PasswordAuthentication no"},
		{"hardened", "2", "Turn password login on?", nil, sshd.HardnDropIn, "PasswordAuthentication yes"},
		{"debian13", "3", "Change the SSH port?", []string{"2230"}, sshd.HardnDropIn, "Port 2230"},
		{"ubuntu2604", "3", "Change the SSH port?", []string{"2230"}, sshd.HardnDropIn, "Port 2230"},
		{"alpine323", "3", "Change the SSH port?", []string{"2230"}, sshd.HardnDropIn, "Port 2230"},
		{"legacy-main", "4", "Move hardn's old settings?", nil, sshd.HardnDropIn, "PermitRootLogin no"},
	} {
		h := ready(t, tt.host)
		lines := append(append([]string{tt.item}, tt.extra...), hostName, "0")
		p := play(h, false, lines...)
		data, _ := h.ReadFile(context.Background(), tt.file)
		if p.err != nil || !shows(p.out, tt.question) || !strings.Contains(p.out, typeIt) || !shows(p.out, applied) || p.unread != 0 ||
			!strings.Contains(string(data), tt.has) {
			t.Errorf("%s %s: Run = %v, runs %+v, %s %q:\n%s", tt.host, tt.item, p.err, p.runs, tt.file, data, p.out)
		}
		checkHost(t, tt.host, h)
	}
}

// TestPortForm: the port refused at its field outside 1 to 65535, "-1"
// and "-22" among them, and signed ("+22"), and on a port sshd has (its
// plan empty), in the mock's words, nothing planned; its default
// hardn.yml's sshPort, said, where sshd does not listen on it, its plan to
// that port; none from a sshPort sshd has, or with no hardn.yml.
func TestPortForm(t *testing.T) {
	h := ready(t, "debian13")
	p := play(h, true, "3", "x", "70000", "-1", "-22", "+22", "22", "0", "0")
	if p.err != nil || strings.Count(flat(p.out), wordsPortRange) != 5 || !shows(p.out, wordsPortHave) || shows(p.out, planned) || p.unread != 0 {
		t.Errorf("refused at the field: Run = %v:\n%s", p.err, p.out)
	}
	for _, tt := range []struct {
		yml  string
		def  bool
		want string
	}{{"sshPort: 2230\n", true, "Port 2230"}, {"sshPort: 22\n", false, ""}, {"", false, ""}} {
		h := ready(t, "debian13")
		p := playWith(h, true, func(o *sshscreen.Options) {
			if tt.yml != "" {
				o.Config = loadYML(t, h, tt.yml)
			}
		}, "3", "", "0", "0")
		if got := shows(p.out, "Default: 2230"); got != tt.def || tt.def && (!shows(p.out, planned) || !shows(p.out, tt.want)) || !tt.def && !shows(p.out, wordsPortRange) {
			t.Errorf("hardn.yml %q: default shown %v, want %v, plan to %q:\n%s", tt.yml, got, tt.def, tt.want, p.out)
		}
	}
}

// onFirstRead is a Reader that calls f before its first Read: after the
// screen is drawn, before its first line is typed.
type onFirstRead struct {
	io.Reader
	f func()
}

func (r *onFirstRead) Read(b []byte) (int, error) {
	if r.f != nil {
		r.f()
		r.f = nil
	}
	return r.Reader.Read(b)
}

// TestPortReadAgain: item 3 reads the host again when chosen: sshd given
// 2230 beside 22 by hand after the draw, hardn.yml's sshPort 2230 is no
// default, and the field refuses 2230 as sshd's.
func TestPortReadAgain(t *testing.T) {
	h := ready(t, "debian13")
	cfg := loadYML(t, h, "sshPort: 2230\n")
	var out bytes.Buffer
	env := term.Resolve(term.Options{}, term.Facts{Width: 120})
	ty := uitest.NewTypist(&out, "3", "", "2230", "0", "0")
	in := &onFirstRead{Reader: ty, f: func() { h.sshFile(sshd.HardnDropIn, "Port 22\nPort 2230\n", 0o644) }}
	p := playWith(h, true, func(o *sshscreen.Options) {
		o.Config = cfg
		o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(in, false), Out: env.Writer(&out)}
	})
	got := out.String()
	if p.err != nil || !shows(got, "Now 22") || shows(got, "Default: 2230") || !shows(got, wordsPortRange) || !shows(got, "sshd listens on 2230 already.") ||
		shows(got, planned) || ty.Unread() != 0 {
		t.Errorf("sshd moved after the draw: Run = %v:\n%s", p.err, got)
	}
}

// TestPortInUse: a port another program has bound is refused by the
// engine's plan at the field, in the in-use words, and nothing is asked or
// applied.
func TestPortInUse(t *testing.T) {
	h := ready(t, "debian13")
	h.bound = append(h.bound, "0.0.0.0:8080")
	n := len(h.Calls())
	p := play(h, false, "3", "8080", "0", "0")
	field := p.out[strings.Index(p.out, "New SSH port"):]
	if p.err != nil || p.unread != 0 || !shows(field, "A port the change adds is in use. Pick another.") || asked(p.out) || shows(p.out, applied) || len(writes(h, n)) > 0 {
		t.Errorf("port 8080 in use: Run = %v, wrote %v:\n%s", p.err, writes(h, n), p.out)
	}
}

// TestPhrase: a wrong hostname is refused and asked again; 0 then
// declines, nothing applied.
func TestPhrase(t *testing.T) {
	h := ready(t, "debian13")
	n := len(h.Calls())
	p := play(h, false, "1", "web-02", "0", "0")
	if p.err != nil || p.unread != 0 || shows(p.out, applied) || strings.Count(p.out, typeIt) != 2 || len(writes(h, n)) > 0 {
		t.Errorf("a wrong phrase, then 0: Run = %v, wrote %v:\n%s", p.err, writes(h, n), p.out)
	}
}

// TestHostname: a live change whose hostname can't be read says so before
// asking, the field too, writing nothing; plan only needs none.
func TestHostname(t *testing.T) {
	for _, lines := range [][]string{{"1", "0"}, {"2", "0"}, {"3", "0"}} {
		h := ready(t, "debian13")
		_ = h.Remove(context.Background(), header.HostnameFile)
		n := len(h.Calls())
		p := play(h, false, lines...)
		if p.err != nil || !shows(p.out, cantConfirm) || asked(p.out) || strings.Contains(p.out, "New SSH port") || len(writes(h, n)) > 0 {
			t.Errorf("%q: Run = %v:\n%s", lines, p.err, p.out)
		}
		h = ready(t, "debian13")
		_ = h.Remove(context.Background(), header.HostnameFile)
		if lines[0] == "3" {
			lines = []string{"3", "2230", "0"}
		}
		if p := play(h, true, lines...); p.err != nil || shows(p.out, cantConfirm) || !shows(p.out, planned) {
			t.Errorf("%q, plan only: Run = %v:\n%s", lines, p.err, p.out)
		}
	}
}

// hostnameOnce is h whose hostname reads once and then fails.
type hostnameOnce struct {
	*sshHost
	reads int
}

func (h *hostnameOnce) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if name == header.HostnameFile {
		if h.reads++; h.reads > 1 {
			return nil, fs.ErrPermission
		}
	}
	return h.sshHost.ReadFile(ctx, name)
}

// TestHostnameLost: a hostname read before asking that then fails says so
// in place of the question, and nothing is applied.
func TestHostnameLost(t *testing.T) {
	h := ready(t, "debian13")
	n := len(h.Calls())
	p := playWith(h, false, func(o *sshscreen.Options) { o.System = &hostnameOnce{sshHost: h} }, "1", "0")
	if p.err != nil || !shows(p.out, cantConfirm) || asked(p.out) || shows(p.out, applied) || len(p.runs) > 0 && p.runs[0].Err == nil || len(writes(h, n)) > 0 {
		t.Errorf("hostname lost: Run = %v, runs %+v, wrote %v:\n%s", p.err, p.runs, writes(h, n), p.out)
	}
}

// TestMoved: a change is planned from a fresh reading: root login
// disallowed by hand after the draw, Disallow root login plans nothing, and
// nothing is applied to the old picture.
func TestMoved(t *testing.T) {
	h := ready(t, "debian13")
	p := playWith(h, false, func(o *sshscreen.Options) {
		build := o.NewEngine
		o.NewEngine = func(progress func(engine.Event)) *engine.Engine {
			h.AddFile(sshd.HardnDropIn, []byte("PermitRootLogin no\n"), 0o644, 0, 0)
			return build(progress)
		}
	}, "1", "0")
	if p.err != nil || asked(p.out) || shows(p.out, applied) || p.unread != 0 {
		t.Errorf("moved: Run = %v:\n%s", p.err, p.out)
	}
}

// TestApplyContext: each apply runs under the context ApplyContext makes,
// stopped once it returns; plan only makes none. A cancelled apply is rolled
// back, and the screen goes on.
func TestApplyContext(t *testing.T) {
	for _, tc := range []struct {
		dryRun bool
		made   int
		lines  []string
	}{{false, 1, []string{"1", hostName, "0"}}, {true, 0, []string{"1", "0"}}} {
		h := ready(t, "debian13")
		made, stopped := 0, 0
		p := playWith(h, tc.dryRun, func(o *sshscreen.Options) {
			o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
				made++
				c, cancel := context.WithCancel(ctx)
				return c, func() { stopped++; cancel() }
			}
		}, tc.lines...)
		if p.err != nil || made != tc.made || stopped != made {
			t.Errorf("plan only %v: Run = %v, %d contexts made, %d stopped, want %d:\n%s", tc.dryRun, p.err, made, stopped, tc.made, p.out)
		}
	}
	h := ready(t, "debian13")
	before, _ := h.ReadFile(context.Background(), sshd.HardnDropIn)
	p := playWith(h, false, func(o *sshscreen.Options) {
		o.ApplyContext = func(ctx context.Context) (context.Context, context.CancelFunc) {
			c, cancel := context.WithCancel(ctx)
			cancel()
			return c, cancel
		}
	}, "1", hostName, "0")
	after, _ := h.ReadFile(context.Background(), sshd.HardnDropIn)
	if p.err != nil || len(p.runs) != 1 || p.runs[0].Err == nil || !bytes.Equal(before, after) ||
		!strings.Contains(p.out[strings.LastIndex(p.out, "Disallow root login?"):], "# SSH Login #") {
		t.Errorf("cancelled: Run = %v, runs %+v, %q:\n%s", p.err, p.runs, after, p.out)
	}
}

// TestPlanOnlyWritesNothing: under plan only every item opens and stops at
// its plan, and no file outside the engine's run directory changes, the
// hostname unread.
func TestPlanOnlyWritesNothing(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
	}{
		{"debian13", []string{"1", "2", "3", "2230", "0"}}, {"hardened", []string{"1", "2", "0"}}, {"legacy-main", []string{"4", "0"}},
		{"pve9", []string{"1", "2", "0"}}, {"ubuntu2604", []string{"3", "2230", "0"}}, {"alpine323", []string{"3", "2230", "0"}},
	} {
		h := ready(t, tt.host)
		_ = h.Remove(context.Background(), header.HostnameFile)
		n := len(h.Calls())
		p := play(h, true, tt.lines...)
		if p.err != nil || !shows(p.out, planned) || shows(p.out, cantConfirm) || p.unread != 0 || len(p.runs) == 0 {
			t.Errorf("%s %v: Run = %v, did not stop at the plan:\n%s", tt.host, tt.lines, p.err, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s %v: plan only wrote %v", tt.host, tt.lines, w)
		}
		checkHost(t, tt.host, h)
	}
}

// TestNavigation: 0 returns, q quits, and the end of the input aborts, from
// the screen, each question and the form, writing nothing.
func TestNavigation(t *testing.T) {
	for _, tt := range []struct {
		host  string
		lines []string
		want  error
	}{
		{"debian13", []string{"0"}, nil}, {"debian13", []string{"q"}, nav.ErrQuit}, {"debian13", []string{"1", "q"}, nav.ErrQuit},
		{"debian13", []string{"3", "q"}, nav.ErrQuit}, {"debian13", []string{"3", "2230", "q"}, nav.ErrQuit}, {"legacy-main", []string{"4", "q"}, nav.ErrQuit},
		{"debian13", nil, prompt.ErrAborted}, {"debian13", []string{"1"}, prompt.ErrAborted}, {"debian13", []string{"3"}, prompt.ErrAborted},
		{"debian13", []string{"3", "2230"}, prompt.ErrAborted}, {"debian13", []string{"9", "3", "0", "0"}, nil}, {"debian13", []string{"2", "0", "0"}, nil},
		{"root-session", []string{"1", "0"}, nil},
	} {
		h := ready(t, tt.host)
		n := len(h.Calls())
		p := play(h, false, tt.lines...)
		if !errors.Is(p.err, tt.want) || (tt.want == nil) != (p.err == nil) || p.unread != 0 {
			t.Errorf("%s %q: Run = %v, want %v, %d unread:\n%s", tt.host, tt.lines, p.err, tt.want, p.unread, p.out)
		}
		if w := writes(h, n); len(w) > 0 {
			t.Errorf("%s %q: wrote %v", tt.host, tt.lines, w)
		}
	}
}

// TestRunNeeds: Run refuses, wrapping fs.ErrInvalid and writing nothing, no
// Prompter's In or Out, no System, Getenv or NewEngine. A nil Config is
// none.
func TestRunNeeds(t *testing.T) {
	h := ready(t, "debian13")
	n := len(h.Calls())
	var out bytes.Buffer
	in := prompt.New(strings.NewReader("0\n"), false)
	newEngine := func(func(engine.Event)) *engine.Engine { return nil }
	for _, o := range []sshscreen.Options{
		{}, {System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in}, System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{Out: &out}, System: h, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, Getenv: h.getenv, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, NewEngine: newEngine},
		{Prompter: nav.Prompter{In: in, Out: &out}, System: h, Getenv: h.getenv},
	} {
		if err := sshscreen.Run(context.Background(), o); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Run(%+v) = %v, want fs.ErrInvalid", o, err)
		}
	}
	if len(h.Calls()) > n || out.Len() > 0 {
		t.Errorf("refused options made %d calls, wrote %q", len(h.Calls())-n, out.String())
	}
}

// TestMatchModel: the fake's sshd -T -C applies the Match block for a
// connection it matches, and not for one it does not.
func TestMatchModel(t *testing.T) {
	h := ready(t, "match-allows")
	main, drops := h.files(sshd.MainConfig)
	in, _ := h.sshdT(matched(main, drops, "user=alice,host=192.0.2.10,addr=192.0.2.10"), drops)
	out, _ := h.sshdT(matched(main, drops, "user=alice,host=198.51.100.7,addr=198.51.100.7"), drops)
	if !strings.Contains(in, "permitrootlogin yes\n") || !strings.Contains(out, "permitrootlogin without-password\n") {
		t.Errorf("in:\n%s\nout:\n%s", in, out)
	}
}

// TestHeights (the gallery's, gallery-ssh.md §4): every host's screen is as
// many lines at 80x24 as at 80x40, Unicode and ASCII, no line over 80
// cells; over 24 its top scrolls off.
func TestHeights(t *testing.T) {
	want := map[string]int{"match-allows": 24, "not-read": 24, "legacy-main": 25, "legacy-v032": 26}
	for _, rows := range []int{24, 40} {
		for _, unicode := range []bool{true, false} {
			lang := map[bool]string{true: "LANG=en_US.UTF-8", false: "LANG=C"}[unicode]
			env := term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: rows, Environ: []string{"TERM=xterm-256color", lang}})
			for _, name := range sshHosts {
				h := ready(t, name)
				var out bytes.Buffer
				playWith(h, false, func(o *sshscreen.Options) {
					o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(uitest.NewTypist(&out, "0"), false), Out: env.Writer(&out)}
				})
				plain := escapes.ReplaceAllString(out.String(), "")
				w, ok := want[name]
				if !ok {
					w = 23
				}
				if got := drawn(plain); len(got) != 1 || got[0] != w {
					t.Errorf("%s at 80x%d, unicode %v: %v lines, want %d", name, rows, unicode, got, w)
				}
				if i := slices.IndexFunc(strings.Split(plain, "\n"), func(l string) bool { return len([]rune(l)) > 80 }); i >= 0 {
					t.Errorf("%s at 80x%d, unicode %v: line %d over 80 cells", name, rows, unicode, i)
				}
			}
		}
	}
}

// escapes are the terminal's SGR sequences.
var escapes = regexp.MustCompile("\x1b\\[[0-9;]*m")

// drawn are the heights of the screens out shows, in order: each from its
// title bar to its prompt, which ends " or q]:", the answer echoed after.
func drawn(out string) []int {
	var got []int
	start := 0
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if strings.Contains(l, " or q]:") && strings.Contains(l, "Enter your choice") {
			for start < i && strings.TrimSpace(lines[start]) == "" {
				start++
			}
			got = append(got, i-start+1)
			start = i + 1
		}
	}
	return got
}
