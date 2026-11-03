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
