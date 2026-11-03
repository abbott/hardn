package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/header"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The SSH Login hosts of the SSH Login item's and -d's tests: the User
// Management host (newUsersHost: Debian 13, george in sudo with
// cloud-init's NOPASSWD rule) with george's key, and a model of sshd that
// reads its files, after pkg/screens/ssh's (host_test.go), which a test of
// another package cannot share, cut to what root login needs: sshd -T
// over sshd_config and its drop-ins (sshd.ReadOrder: the first value
// outside Match blocks, the rest Debian 13's defaults in the model's
// shape, not read from a binary), sshd's log of george's login by key over
// SSH (fwSession's connection), ss, and systemctl, systemd-run and the
// boot unit as the engine runs them. Synthetic values only: george,
// alice, web-01, 192.0.2.x.

// sshTestHost is one host: the User Management host and what sshd logged.
type sshTestHost struct {
	*usrTestHost

	smu        sync.Mutex
	log        string   // journalctl -u ssh -u sshd's lines
	unexpected []string // the commands the model does not answer
}

// sshTestDefaults are sshd -T's keywords on the hosts where no file sets
// them, Debian 13's shape: without-password is what its sshd prints for
// prohibit-password.
var sshTestDefaults = []struct{ k, v string }{
	{"port", "22"}, {"listenaddress", "[::]:22"}, {"listenaddress", "0.0.0.0:22"},
	{"permitrootlogin", "without-password"}, {"pubkeyauthentication", "yes"}, {"passwordauthentication", "yes"},
	{"kbdinteractiveauthentication", "no"}, {"permitemptypasswords", "no"}, {"usepam", "yes"},
	{"hostbasedauthentication", "no"}, {"gssapiauthentication", "no"}, {"kerberosauthentication", "no"},
	{"authorizedkeysfile", ".ssh/authorized_keys .ssh/authorized_keys2"}, {"authenticationmethods", "any"},
	{"pubkeyacceptedalgorithms", "ssh-ed25519,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256"},
	{"strictmodes", "yes"}, {"authorizedkeyscommand", "none"}, {"authorizedkeyscommanduser", "none"},
	{"authorizedprincipalsfile", "none"}, {"authorizedprincipalscommand", "none"}, {"authorizedprincipalscommanduser", "none"},
	{"trustedusercakeys", "none"}, {"revokedkeys", "none"}, {"pubkeyauthoptions", "none"}, {"maxauthtries", "6"},
	{"forcecommand", "none"}, {"chrootdirectory", "none"},
}

// newSSHHost returns the host named name, and hardn.yml as hardn reads it
// there (newDNSHost's: username alice, sshPort 2230):
//
//   - debian13: root logs in by key alone (sshd's default), george by key:
//     Disallow root login's plan is lockout-risk;
//   - disallowed: debian13 with PermitRootLogin no in a drop-in: nothing
//     to change;
//   - no-key: debian13 with george's key gone, so no admin could log in
//     afterwards: refused, SSH_NO_ADMIN_LOGIN;
//   - pve9: Proxmox VE 9, root key-only already: refused,
//     SSH_PVE_ROOT_KEY_ONLY, which -d reads as nothing to change;
//   - no-hostname: debian13 with no kernel hostname to read.
func newSSHHost(t *testing.T, name string) (*sshTestHost, *config.Config) {
	t.Helper()
	u, cfg := newUsersHost(t)
	h := &sshTestHost{usrTestHost: u}
	h.facts.Init.Source = detect.SystemdRunDir
	for _, d := range []string{"/run/sshd", "/usr/local", "/usr/local/bin", "/etc/systemd", revert.SystemdUnitDir, revert.SystemdWants,
		"/usr/lib/systemd", "/usr/lib/systemd/system", "/proc", "/home/george/.ssh"} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddDir("/home/george/.ssh", 0o700, 1000, 1000)
	h.AddFile("/home/george/.ssh/authorized_keys", []byte(usrTestKey(1, "george@laptop-1")+"\n"), 0o600, 1000, 1000)
	h.AddFile(revertTestHardn, []byte("ELF"), 0o755, 0, 0)
	h.AddFile(header.HostnameFile, []byte("web-01\n"), 0o644, 0, 0)
	blob, _ := base64.StdEncoding.DecodeString(strings.Fields(usrTestKey(1, ""))[1])
	sum := sha256.Sum256(blob)
	h.log = "2026-10-20T11:58:00+00:00 web-01 sshd-session[3900]: Accepted publickey for george from 192.0.2.10 port 50022 ssh2: ED25519 SHA256:" +
		base64.RawStdEncoding.EncodeToString(sum[:]) + "\n"
	switch name {
	case "debian13":
	case "disallowed":
		h.AddFile(sshd.HardnDropIn, []byte("PermitRootLogin no\n"), 0o644, 0, 0)
	case "no-key":
		if err := h.Remove(context.Background(), "/home/george/.ssh/authorized_keys"); err != nil {
			t.Fatal(err)
		}
	case "pve9":
		h.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
		h.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	case "no-hostname":
		if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("no host %q", name)
	}
	users := h.Handler
	h.Handler = func(argv []string, in []byte) hostfake.Response {
		if r, ok := h.answer(argv); ok {
			return r
		}
		r := users(argv, in)
		if errors.Is(r.Err, hostfake.ErrUnscripted) {
			h.smu.Lock()
			h.unexpected = append(h.unexpected, host.FormatArgv(argv))
			h.smu.Unlock()
		}
		return r
	}
	t.Cleanup(func() {
		if len(h.unexpected) > 0 {
			t.Errorf("commands the model does not answer: %q", h.unexpected)
		}
	})
	return h, cfg
}

// answer is sshd, its log, ss, systemctl and the revert timer as the model
// has them; false for a command it leaves to the User Management host's.
func (h *sshTestHost) answer(argv []string) (hostfake.Response, bool) {
	h.smu.Lock()
	defer h.smu.Unlock()
	cmd := strings.Join(argv, " ")
	switch {
	case path.Base(argv[0]) == "sshd" && slices.Contains(argv, "-t"):
		return hostfake.Response{}, true
	case path.Base(argv[0]) == "sshd" && slices.Contains(argv, "-T"):
		conf := sshd.MainConfig
		if i := slices.Index(argv, "-f"); i >= 0 && i+1 < len(argv) {
			conf = argv[i+1]
		}
		return hostfake.Response{Stdout: h.sshdT(conf)}, true
	case argv[0] == "journalctl":
		return hostfake.Response{Stdout: h.log}, true
	case cmd == "ss -Htln":
		return hostfake.Response{Stdout: "LISTEN 0      4096                  [::]:22   [::]:*\nLISTEN 0      4096               0.0.0.0:22   0.0.0.0:*\n"}, true
	case cmd == "systemctl is-active ssh.service":
		return hostfake.Response{Stdout: "active\n"}, true
	case cmd == "systemctl is-enabled ssh.service":
		return hostfake.Response{Stdout: "enabled\n"}, true
	case cmd == "systemctl is-active ssh.socket":
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}, true
	case cmd == "systemctl is-enabled ssh.socket":
		return hostfake.Response{Stdout: "disabled\n", ExitCode: 1}, true
	case cmd == "systemctl reload ssh.service":
		return hostfake.Response{}, true
	case cmd == "systemctl --no-reload enable "+revert.BootUnitName+".service":
		h.AddSymlink(revert.SystemdWants+"/"+revert.BootUnitName+".service", revert.SystemdUnitDir+"/"+revert.BootUnitName+".service")
		return hostfake.Response{}, true
	case len(argv) == 6 && argv[0] == "systemctl" && argv[1] == "list-timers":
		return hostfake.Response{Stdout: "[]\n"}, true
	case fwTimerCommand(cmd), argv[0] == "systemd-analyze":
		return hostfake.Response{}, true
	case argv[0] == "ufw" || argv[0] == "dpkg-query" && argv[len(argv)-1] == "ufw":
		return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}, true
	}
	return hostfake.Response{}, false
}

// sshdT is sshd -T over conf (sshd_config, or sshd.Validate's copy with its
// Include mapped back) and the drop-ins beside it: the first value of each
// keyword outside Match blocks, lower-cased as sshd prints it, then
// sshTestDefaults for the rest.
func (h *sshTestHost) sshdT(conf string) string {
	ctx := context.Background()
	work := path.Dir(conf)
	main, _ := h.ReadFile(ctx, conf)
	main = []byte(strings.ReplaceAll(string(main), "Include "+work+"/sshd_config.d/*.conf", sshd.IncludeLine))
	var drops []sshd.File
	entries, _ := h.ReadDir(ctx, work+"/sshd_config.d")
	for _, de := range entries {
		if data, err := h.ReadFile(ctx, work+"/sshd_config.d/"+de.Name()); err == nil {
			drops = append(drops, sshd.File{Path: sshd.DropInDir + "/" + de.Name(), Data: data})
		}
	}
	first := map[string]string{}
	for _, l := range sshd.ReadOrder(main, drops) {
		if _, ok := first[l.Keyword]; !ok && l.Match == nil {
			first[l.Keyword] = strings.ToLower(strings.Join(l.Args, " "))
		}
	}
	var b strings.Builder
	for _, d := range sshTestDefaults {
		v, ok := first[d.k]
		if !ok || d.k == "port" || d.k == "listenaddress" {
			v = d.v
		}
		b.WriteString(d.k + " " + v + "\n")
	}
	return b.String()
}

// rootLogin is PermitRootLogin as sshd -T prints it now.
func (h *sshTestHost) rootLogin() string {
	for _, l := range strings.Split(h.sshdT(sshd.MainConfig), "\n") {
		if v, ok := strings.CutPrefix(l, "permitrootlogin "); ok {
			return v
		}
	}
	return ""
}

// sshdWrites are the writes the run left under /etc/ssh.
func (h *sshTestHost) sshdWrites() []string {
	var out []string
	for _, w := range h.writes() {
		if strings.Contains(w, " /etc/ssh") {
			out = append(out, w)
		}
	}
	return out
}

// useSSHHost points the seams at h as useUsersHost does, the session
// george's over SSH (fwSession's connection, which sshd's log has), the
// user typing lines.
func useSSHHost(t *testing.T, h *sshTestHost, lines ...string) *dnsSeams {
	t.Helper()
	s := useUsersHost(t, h.usrTestHost, lines...)
	engineHost = func() host.System { return h }
	revertGetenv = fwSession
	return s
}

// TestSSHScreen: the main menu's SSH Login item runs the SSH Login screen
// on the host hardn builds, through the engine with engineOptions, its
// session the engine's (george's over SSH, sshd's log read for it) and
// hardn's environment read for the session user (os.Getenv: george,
// through sudo): Disallow root login typed and applied writes
// PermitRootLogin no and arms the revert timer, the countdown and sudo
// hardn confirm said after; under plan only it stops at its plan, nothing
// written or armed; 0 goes back (nil), q quits (nav.ErrQuit), the end of
// the input aborts (prompt.ErrAborted). The signals are registered for the
// apply alone, and never while a prompt waits. No change saves hardn.yml.
func TestSSHScreen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		lines   []string
		want    error
		applied bool
		said    []string
	}{
		{"back", false, []string{"0"}, nil, false, []string{"SSH Login", "george"}},
		{"Disallow root login", false, []string{"1", "web-01", "0"}, nil, true,
			[]string{"Disallow root login?", "Type the hostname", "Operation Successful", "sudo hardn confirm", "Allow root key-only"}},
		{"Disallow under plan only", true, []string{"1", "0"}, nil, false, []string{"No actual changes were made to your system."}},
		{"quit", false, []string{"q"}, nav.ErrQuit, false, nil},
		{"the end of the input", false, nil, prompt.ErrAborted, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newSSHHost(t, "debian13")
			yml := h.content(dnsTestYML)
			s := useSSHHost(t, h, tc.lines...)
			cfg.DryRun = tc.dryRun
			err := runSSHScreen(context.Background(), cfg)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("runSSHScreen = %v, want %v; screen:\n%s", err, tc.want, s.out.String())
			}
			out := style.StripAnsi(s.out.String())
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the screen lacks %q:\n%s", w, out)
			}
			if got := h.rootLogin(); (got == "no") != tc.applied || h.armed() != tc.applied || !tc.applied && len(h.sshdWrites()) > 0 {
				t.Errorf("PermitRootLogin %s, armed %t, wrote %q; want applied %t", got, h.armed(), h.sshdWrites(), tc.applied)
			}
			if want := map[bool]int{true: 1}[tc.applied]; s.applies != want || s.live != 0 || s.atPrompt != 0 {
				t.Errorf("signals registered %d times (want %d), %d left, %d prompts read under them", s.applies, want, s.live, s.atPrompt)
			}
			if got := h.content(dnsTestYML); got != yml {
				t.Errorf("hardn.yml saved:\n%s", got)
			}
		})
	}
}

// armed reports whether the revert timer was armed: systemd-run.
func (h *sshTestHost) armed() bool {
	for _, c := range h.Calls() {
		if c.Op == "run" && strings.HasPrefix(strings.Join(c.Argv, " "), "systemd-run") {
			return true
		}
	}
	return false
}

// TestSSHHook: withSSHScreen sets the SSH Login item's screen on the
// configuration main gives it, plan only set after the hook stopping
// Disallow root login at its plan.
func TestSSHHook(t *testing.T) {
	h, cfg := newSSHHost(t, "debian13")
	s := useSSHHost(t, h, "1", "0")
	set := &hookSet{}
	if got := withSSHScreen(set, cfg); got != set || set.ssh == nil {
		t.Fatal("withSSHScreen did not set the SSH Login item on the menu given")
	}
	cfg.DryRun = true
	if err := set.ssh(); err != nil {
		t.Fatalf("the SSH Login item = %v; screen:\n%s", err, s.out.String())
	}
	if got := h.sshdWrites(); len(got) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") || s.applies != 0 {
		t.Errorf("wrote %q, %d applies; screen:\n%s", got, s.applies, s.out.String())
	}
}

// dLockoutWords are -d's refusal off a terminal (proposed, refactor plan
// slice 6, unit (d)), as output.Human says it.
const dLockoutWords = "Turning root login off here risks a lockout (PLAN_REFUSED)"

// runD runs hardn with args as root (runG) on the SSH Login host h with
// cfg, the session george's over SSH and hardn's environment george's
// sudo; stdout's render environment env when set (onTTY: on a terminal),
// the user typing lines; setup, when set, run once the seams are in place.
func runD(t *testing.T, h *sshTestHost, cfg *config.Config, env func() term.Env, lines []string, setup func(*dnsSeams), args ...string) (result, *dnsSeams) {
	t.Helper()
	return runG(t, h.dnsHost, cfg, func(s *dnsSeams) {
		engineHost = func() host.System { return h }
		revertGetenv = fwSession
		t.Setenv("SUDO_USER", "george")
		in := prompt.New(uitest.NewTypist(&s.out, lines...), false)
		engineInput = func() *prompt.Reader { return in }
		if env != nil {
			stdoutEnv = env
		}
		if setup != nil {
			setup(s)
		}
	}, args...)
}

// TestDisallowRootFlag: -d plans and applies the SSH Login control's
// Disallow root login through the engine (refactor plan slice 6, unit
// (d); gallery-ssh.md §5, decision 8 (a), adopted): on a terminal the
// plan, "Disallow root login?" above the hostname typed, never y/N, the
// apply, PermitRootLogin no written and the revert timer armed, then the
// countdown and sudo hardn confirm, exit status 0; 0 and q declining,
// nothing written, 3; the hostname unreadable said in place of the
// question, nothing asked or written, 1. Off a terminal the plan is
// refused, PLAN_REFUSED, nothing written or armed, 1. -n the plan alone,
// no question, nothing written, 3, on a terminal or off. The control's
// refusals, said before anything is asked: no admin able to log in
// afterwards (george's key gone), SSH_NO_ADMIN_LOGIN, 1, nothing written.
// Root login already off: nothing to change, 0, nothing asked. Proxmox VE
// with root key-only already: the control's SSH_PVE_ROOT_KEY_ONLY read as
// nothing to change, 0. Nothing goes through the legacy provider; the
// signals are registered for an apply alone, an empty plan's too, as
// runControls applies any flag's; hardn.yml is never saved.
func TestDisallowRootFlag(t *testing.T) {
	for _, tc := range []struct {
		name, host  string
		tty, dryRun bool
		lines       []string
		code        int      // the exit status; 0: hardn did not exit
		said        []string // in order
		unsaid      []string
		applied     bool // PermitRootLogin no written, the timer armed
	}{
		{name: "on a terminal", host: "debian13", tty: true, lines: []string{"web-01"}, applied: true,
			said: []string{"Lockout risk", "Disallow root login?", "Type the hostname [web-01, 0 or q]", "Done", "Operation Successful", "sudo hardn confirm"}},
		{name: "0 declines", host: "debian13", tty: true, lines: []string{"0"}, code: 3, said: []string{"Type the hostname", "Operation Cancelled"}},
		{name: "q declines", host: "debian13", tty: true, lines: []string{"q"}, code: 3, said: []string{"Type the hostname"}},
		{name: "the hostname unreadable", host: "no-hostname", tty: true, code: 1,
			said: []string{"Lockout risk", "Can't confirm with the hostname"}, unsaid: []string{"Type the hostname"}},
		{name: "off a terminal", host: "debian13", code: 1,
			said:   []string{"Lockout risk", "No actual changes were made to your system.", dLockoutWords, "To go on: run hardn -d from a terminal, which asks you to confirm."},
			unsaid: []string{"Type the hostname"}},
		{name: "-n on a terminal", host: "debian13", tty: true, dryRun: true, code: 3,
			said: []string{"Lockout risk", "No actual changes were made to your system."}, unsaid: []string{"Type the hostname", dLockoutWords}},
		{name: "-n off a terminal", host: "debian13", dryRun: true, code: 3,
			said: []string{"Lockout risk", "No actual changes were made to your system."}, unsaid: []string{dLockoutWords}},
		{name: "no admin able", host: "no-key", tty: true, code: 1,
			said: []string{"No admin could log in over SSH afterwards", "SSH_NO_ADMIN_LOGIN"}, unsaid: []string{"Type the hostname"}},
		{name: "no admin able, -n", host: "no-key", dryRun: true, code: 1, said: []string{"SSH_NO_ADMIN_LOGIN"}},
		{name: "already disallowed", host: "disallowed", tty: true, said: []string{"Nothing needs changing."}, unsaid: []string{"Type the hostname"}},
		{name: "already disallowed off a terminal", host: "disallowed", said: []string{"Nothing needs changing."}, unsaid: []string{dLockoutWords}},
		{name: "Proxmox VE, root key-only already", host: "pve9", tty: true,
			said: []string{"Nothing needs changing."}, unsaid: []string{"SSH_PVE_ROOT_KEY_ONLY", "Type the hostname"}},
	} {
		args := []string{"-d"}
		if tc.dryRun {
			args = []string{"-n", "-d"}
		}
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newSSHHost(t, tc.host)
			yml, was := h.content(dnsTestYML), h.rootLogin()
			var env func() term.Env
			if tc.tty {
				env = onTTY
			}
			r, s := runD(t, h, cfg, env, tc.lines, nil, args...)
			out := style.StripAnsi(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstdout:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stdout, r.stderr)
			}
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, out)
			}
			for _, w := range append([]string{"y/N", "[SUCCESS]", "[ERROR]", "Script completed", "Root SSH access disabled"}, tc.unsaid...) {
				if strings.Contains(out+r.stdout, w) {
					t.Errorf("printed %q:\n%s%s", w, out, r.stdout)
				}
			}
			if got := h.rootLogin(); tc.applied && got != "no" || !tc.applied && (got != was || len(h.sshdWrites()) > 0) {
				t.Errorf("PermitRootLogin %s (was %s), wrote %q; want applied %t", got, was, h.sshdWrites(), tc.applied)
			}
			if h.armed() != tc.applied {
				t.Errorf("the revert timer armed %t, want %t", h.armed(), tc.applied)
			}
			if len(r.calls) != 0 {
				t.Errorf("-d went through the legacy provider: %v", r.calls)
			}
			if want := map[bool]int{true: 1}[tc.applied || !tc.dryRun && tc.code == 0]; s.applies != want || s.live != 0 {
				t.Errorf("signals registered %d times, want %d; %d left", s.applies, want, s.live)
			}
			if got := h.content(dnsTestYML); got != yml {
				t.Errorf("hardn.yml saved:\n%s", got)
			}
		})
	}
}

// TestDisallowRootPlanOnlyFromFile: hardn.yml's dryRun: true makes -d the
// plan alone, as -n does, on a terminal too: no question, nothing written
// or armed, 3; --dry-run=false over it asks and applies as live.
func TestDisallowRootPlanOnlyFromFile(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		lines []string
		code  int
	}{
		{[]string{"-d"}, nil, 3},
		{[]string{"--dry-run=false", "-d"}, []string{"web-01"}, 0},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h, _ := newSSHHost(t, "debian13")
			h.AddFile(dnsTestYML, []byte("username: alice\ndryRun: true\n"), 0o644, 0, 0)
			cfg, err := config.Read(context.Background(), h, dnsTestYML)
			if err != nil {
				t.Fatal(err)
			}
			r, s := runD(t, h, cfg, onTTY, tc.lines, func(*dnsSeams) {
				fileDryRun = func(string) (string, bool, error) { return dnsTestYML, true, nil }
			}, tc.args...)
			out := style.StripAnsi(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
			}
			live := tc.code == 0
			if strings.Contains(out, "Type the hostname") != live || strings.Contains(out, "No actual changes were made to your system.") == live {
				t.Errorf("asked %t, want %t; report:\n%s", !live, live, out)
			}
			if h.armed() != live || (h.rootLogin() == "no") != live {
				t.Errorf("armed %t, PermitRootLogin %s; want applied %t", h.armed(), h.rootLogin(), live)
			}
		})
	}
}

// TestDisallowRootFlagWithOthers: -d's exit status combines with the
// flags' after it as -c's does (runStatus), each run in Run's order:
// under -n -d's plan (3), then -w's refusal on a host without ufw
// (UFW_NOT_INSTALLED), 1; -d with nothing to change, then -w's refusal,
// 1; -d refused (no admin able), then -g applied, the refusal hardn's
// exit status once -g has run; under -n both plans, 3.
func TestDisallowRootFlagWithOthers(t *testing.T) {
	for _, tc := range []struct {
		name, host string
		args       []string
		code       int
		said       []string
		dns        bool // -g's file written
	}{
		{"-n: -d's plan, then -w refused", "debian13", []string{"-n", "-d", "-w"}, 1, []string{"Lockout risk", "Disallow root login", "UFW_NOT_INSTALLED"}, false},
		{"-d nothing to change, then -w refused", "disallowed", []string{"-d", "-w"}, 1, []string{"Nothing needs changing.", "UFW_NOT_INSTALLED"}, false},
		{"-d refused, then -g", "no-key", []string{"-d", "-g"}, 1, []string{"SSH_NO_ADMIN_LOGIN", "Set the nameservers  Done"}, true},
		{"-n: -d's plan, then -g's", "debian13", []string{"-n", "-d", "-g"}, 3, []string{"Lockout risk", "+nameserver 192.0.2.53"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, cfg := newSSHHost(t, tc.host)
			conf, was := h.content("/etc/resolv.conf"), h.rootLogin()
			r, s := runD(t, h, cfg, nil, nil, nil, tc.args...)
			out := style.StripAnsi(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
			}
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, out)
			}
			if got := h.content("/etc/resolv.conf") != conf; got != tc.dns {
				t.Errorf("-g's file written %t, want %t", got, tc.dns)
			}
			if h.rootLogin() != was || h.armed() || len(h.sshdWrites()) > 0 {
				t.Errorf("PermitRootLogin %s (was %s), armed %t, wrote %q", h.rootLogin(), was, h.armed(), h.sshdWrites())
			}
		})
	}
}

// TestCreateUserThenDisallowRoot: -c with -d is no longer refused (slice
// 6, decision 8 (a)): -c runs first, so -d's proof sees the user -c
// created. Under -n both plans are shown, nothing written, 3; and with
// george's key gone -d is refused, no admin able, as -c's carol is not
// created, 1. Off a terminal -c is applied, carol created with
// hardn.yml's key, and -d refused off a terminal, nothing written to
// sshd's files, 1. On a terminal with george's key gone, -d's proof finds
// carol able by the key -c gave her: typed and applied, 0. Nothing goes
// through the legacy provider.
func TestCreateUserThenDisallowRoot(t *testing.T) {
	key := usrTestKey(5, "carol@laptop")
	for _, tc := range []struct {
		name, host string
		args       []string
		tty        bool
		lines      []string
		code       int // the exit status; 0: hardn did not exit
		said       []string
		made       bool // -c created carol
		applied    bool // -d applied
	}{
		{name: "-n -c -d", host: "debian13", args: []string{"-n", "-c", "-d", "-u", "carol"}, code: 3,
			said: []string{"Create user carol", "No actual changes were made to your system.", "Lockout risk", "Disallow root login", "No actual changes were made to your system."}},
		{name: "-n -c -d, george's key gone", host: "no-key", args: []string{"-n", "-c", "-d", "-u", "carol"}, code: 1,
			said: []string{"Create user carol", "SSH_NO_ADMIN_LOGIN"}},
		{name: "-c -d off a terminal", host: "debian13", args: []string{"-c", "-d", "-u", "carol"}, code: 1, made: true,
			said: []string{"Create user carol", "Operation Successful", "Lockout risk", dLockoutWords}},
		{name: "-c -d on a terminal, george's key gone", host: "no-key", args: []string{"-c", "-d", "-u", "carol"}, tty: true, lines: []string{"web-01"},
			made: true, applied: true, said: []string{"Create user carol", "Disallow root login?", "Type the hostname", "Operation Successful", "sudo hardn confirm"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newSSHHost(t, tc.host)
			cfg := withKeys(t, h.usrTestHost, key)
			was, before := h.rootLogin(), len(h.writes())
			var env func() term.Env
			if tc.tty {
				env = onTTY
			}
			r, s := runD(t, h, cfg, env, tc.lines, nil, tc.args...)
			out := style.StripAnsi(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
			}
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, out)
			}
			if made := h.content("/home/carol/.ssh/authorized_keys") == key+"\n"; made != tc.made {
				t.Errorf("-c created carol %t, want %t: ran %q", made, tc.made, h.commands("useradd"))
			}
			if got := h.rootLogin(); tc.applied != (got == "no") || h.armed() != tc.applied || !tc.applied && (got != was || len(h.sshdWrites()) > 0) {
				t.Errorf("PermitRootLogin %s (was %s), armed %t, wrote %q; want -d applied %t", got, was, h.armed(), h.sshdWrites(), tc.applied)
			}
			if w := h.writes()[before:]; !tc.made && len(w) > 0 {
				t.Errorf("wrote %q", w)
			}
			if len(r.calls) != 0 || strings.Contains(r.stderr, "hardn -c -d") {
				t.Errorf("legacy calls %q; stderr:\n%s", r.calls, r.stderr)
			}
		})
	}
}
