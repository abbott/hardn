package main

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/screens/mainmenu"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Firewall hosts of the Firewall item's and -w's tests: hostfake on
// Debian 13, sshd on 22, with a model of ufw that does what it is told,
// after pkg/screens/firewall's (host_test.go), which a test of another
// package cannot share; ufw's status and files are that model's, from
// ufw's sources, not captured from a host. Synthetic values only: web-01,
// 192.0.2.0/24, alice.

// fwTestHost is one host: the DNS host's fake and facts, and ufw's state.
type fwTestHost struct {
	*dnsHost
	t *testing.T

	mu      sync.Mutex
	active  bool     // ufw is running
	allowed []string // the ports ufw lets through, "22"
	unit    string   // what systemctl is-enabled ufw.service prints
	during  func()   // run as ufw starts, before it answers
	ran     []string // the commands run, as host.FormatArgv gives them
}

// newFWHost returns the host named name:
//
//   - debian13: ufw installed and inactive, no rule saved, its service
//     enabled: Enable's plan is lockout-risk;
//   - enabled: ufw running, deny by default, 22 let through: nothing to
//     change;
//   - boot-off: ufw running, deny by default, 22 let through, its service
//     not started at boot: Enable's plan is plain, "Start ufw at boot"
//     alone;
//   - pve9: Proxmox VE 9, where the control refuses (UFW_NOT_APPLICABLE);
//   - rocky: Rocky Linux 10, where -w is refused before its plan
//     (FIREWALL_EL_READ_ONLY);
//   - rhel: RHEL 10, Supported: refused as on Rocky Linux, with its fix;
//   - rhel8: RHEL 8, which the matrix doesn't name: refused as on Rocky
//     Linux, with no fix (the Firewall screen refuses there too);
//   - no-hostname: debian13 with no kernel hostname to read.
func newFWHost(t *testing.T, name string) *fwTestHost {
	t.Helper()
	h := &fwTestHost{dnsHost: &dnsHost{System: hostfake.New(host.Options{})}, t: t, unit: "enabled"}
	h.facts = detect.Facts{
		OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
			Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
			Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}
	for _, d := range []string{"/etc", "/etc/hardn", "/etc/ufw", "/etc/ufw/applications.d", "/etc/default", "/etc/systemd/system", "/etc/systemd/system/multi-user.target.wants",
		"/usr/lib/systemd/system", "/var", "/var/lib", "/var/backups", "/run", "/var/log", "/usr", "/usr/local", "/usr/local/bin",
		"/proc", "/proc/sys", "/proc/sys/kernel", "/etc/ssh", sshd.DropInDir} {
		h.AddDir(d, 0o755, 0, 0)
	}
	h.AddFile(revertTestHardn, []byte("ELF"), 0o755, 0, 0)
	h.AddFile(header.HostnameFile, []byte("web-01\n"), 0o644, 0, 0)
	h.AddFile(sshd.MainConfig, []byte("Include /etc/ssh/sshd_config.d/*.conf\n\nUsePAM yes\n"), 0o644, 0, 0)
	h.AddFile(sshd.DropInDir+"/00-hardn.conf", []byte("Port 22\n"), 0o644, 0, 0)
	switch name {
	case "debian13":
	case "enabled":
		h.active, h.allowed = true, []string{"22"}
	case "boot-off":
		h.active, h.allowed, h.unit = true, []string{"22"}, "disabled"
	case "pve9":
		h.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
		h.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	case "rocky":
		h.facts.OS = detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.2"}
		h.facts.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
		h.facts.SSH.SSHDUnits = true
	case "rhel":
		h.facts.OS = detect.OSRelease{ID: "rhel", IDLike: []string{"centos", "fedora"}, VersionID: "10.3"}
		h.facts.Platform = detect.Platform{Distro: detect.DistroRHEL, Version: "10", Support: detect.Supported}
		h.facts.SSH.SSHDUnits = true
	case "rhel8":
		h.facts.OS = detect.OSRelease{ID: "rhel", IDLike: []string{"fedora"}, VersionID: "8.10"}
		h.facts.Platform = detect.Platform{Distro: detect.DistroRHEL, Version: "8", Support: detect.Unlisted}
		h.facts.SSH.SSHDUnits = true
	case "no-hostname":
		if err := h.Remove(context.Background(), header.HostnameFile); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("no host %q", name)
	}
	h.sync()
	h.Handler = h.answer
	return h
}

// sync writes ufw's settings and saved rules as the model has them.
func (h *fwTestHost) sync() {
	enabled := map[bool]string{true: "yes", false: "no"}[h.active]
	h.AddFile(ufw.DefaultsFile, []byte("IPV6=yes\nDEFAULT_INPUT_POLICY=\"DROP\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	h.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct{ path, any string }{{ufw.UserRules, "0.0.0.0/0"}, {ufw.User6Rules, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n### RULES ###\n")
		for _, p := range h.allowed {
			fmt.Fprintf(&b, "\n### tuple ### allow tcp %s %s any %s in\n", p, f.any, f.any)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
	h.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
}

// status is ufw status verbose's output.
func (h *fwTestHost) status() string {
	if !h.active {
		return "Status: inactive\n"
	}
	var b strings.Builder
	b.WriteString("Status: active\nLogging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	if len(h.allowed) > 0 {
		fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	}
	for _, v6 := range []string{"", " (v6)"} {
		for _, p := range h.allowed {
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", p+"/tcp"+v6, "ALLOW IN", "Anywhere"+v6)
		}
	}
	return b.String()
}

// answer is the host's commands: sshd, ss, systemctl and ufw as the model
// has them, and the revert timer's, which a lockout-risk apply arms. A
// command it does not know fails the test.
func (h *fwTestHost) answer(argv []string, _ []byte) hostfake.Response {
	cmd := host.FormatArgv(argv)
	h.mu.Lock()
	h.ran = append(h.ran, cmd)
	during := h.during
	h.mu.Unlock()
	defer h.sync()
	switch {
	case argv[0] == "sshd" && slices.Contains(argv, "-T"):
		return hostfake.Response{Stdout: "port 22\nlistenaddress [::]:22\nlistenaddress 0.0.0.0:22\npermitrootlogin without-password\npubkeyauthentication yes\npasswordauthentication yes\nusepam yes\n"}
	case cmd == "ss -Htln", cmd == "ss -Htlnp":
		return hostfake.Response{Stdout: "LISTEN 0      4096                  [::]:22   [::]:*\nLISTEN 0      4096               0.0.0.0:22   0.0.0.0:*\n"}
	case cmd == "ss -Hulnp":
		return hostfake.Response{}
	case cmd == "ufw status verbose":
		return hostfake.Response{Stdout: h.status()}
	case cmd == "ufw default deny incoming":
		return hostfake.Response{Stdout: "Default incoming policy changed to 'deny'\n(be sure to update your rules accordingly)\n"}
	case len(argv) == 3 && argv[0] == "ufw" && argv[1] == "allow":
		h.allowed = append(h.allowed, strings.TrimSuffix(argv[2], "/tcp"))
		return hostfake.Response{Stdout: "Rule added\nRule added (v6)\n"}
	case len(argv) == 4 && argv[0] == "ufw" && argv[1] == "delete":
		port := strings.TrimSuffix(argv[3], "/tcp")
		h.allowed = slices.DeleteFunc(h.allowed, func(p string) bool { return p == port })
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}
	case cmd == "ufw --force enable":
		if during != nil {
			during()
		}
		h.active = true
		return hostfake.Response{Stdout: "Firewall is active and enabled on system startup\n"}
	case cmd == "ufw disable":
		h.active = false
		return hostfake.Response{Stdout: "Firewall stopped and disabled on system startup\n"}
	case cmd == "systemctl enable ufw.service":
		h.unit = "enabled"
		return hostfake.Response{}
	case cmd == "systemctl is-enabled ufw.service":
		return hostfake.Response{Stdout: h.unit + "\n", ExitCode: map[bool]int{true: 0, false: 1}[h.unit == "enabled"]}
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == "ufw":
		return hostfake.Response{Stdout: "install ok installed\t0.36.2-9\n"}
	case cmd == "systemctl --no-reload enable hardn-boot.service":
		h.AddSymlink("/etc/systemd/system/multi-user.target.wants/hardn-boot.service", "/etc/systemd/system/hardn-boot.service")
		return hostfake.Response{}
	case fwTimerCommand(cmd):
		return hostfake.Response{}
	}
	h.t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// fwTimerCommand: cmd is the revert timer's or hardn's boot unit's, which
// the engine runs for a lockout-risk plan alone.
func fwTimerCommand(cmd string) bool {
	return strings.HasPrefix(cmd, "systemd-run ") || strings.Contains(cmd, revert.UnitPrefix) || strings.Contains(cmd, revert.BootUnitName) ||
		strings.Contains(cmd, revert.SleeperScript) || cmd == "systemctl daemon-reload"
}

// armed reports whether the revert timer was armed: systemd-run, or a
// detached sleeper.
func (h *fwTestHost) armed() bool {
	for _, c := range h.Calls() {
		if c.Op == "start-detached" || c.Op == "run" && strings.HasPrefix(strings.Join(c.Argv, " "), "systemd-run") {
			return true
		}
	}
	return false
}

// changed are the ufw commands run that change it.
func (h *fwTestHost) changed() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, c := range h.ran {
		if strings.HasPrefix(c, "ufw ") && c != "ufw status verbose" {
			out = append(out, c)
		}
	}
	return out
}

// fwSession is alice's SSH session to 22, as revert.ReadSession reads it.
func fwSession(k string) string {
	return map[string]string{"SSH_CONNECTION": "192.0.2.10 50022 192.0.2.20 22"}[k]
}

// onTTY is stdout's render environment on a terminal, stdin one too, 80
// columns, no colour.
func onTTY() term.Env { return ttyEnv(true, true) }

// ttyEnv is stdout's render environment, stdout a terminal when out and
// stdin one when in, 80 columns, no colour.
func ttyEnv(out, in bool) term.Env {
	return term.Resolve(term.Options{}, term.Facts{StdoutTTY: out, StdinTTY: in, Width: 80, Environ: []string{"TERM=xterm", "NO_COLOR=1"}})
}

// useFWHost points the seams at h as useDNSHost does, the session
// alice's, the user typing lines.
func useFWHost(t *testing.T, h *fwTestHost, lines ...string) *dnsSeams {
	t.Helper()
	s := useDNSHost(t, h.dnsHost, lines...)
	revertGetenv = fwSession
	return s
}

// TestFirewallScreen: the main menu's Firewall item runs the Firewall
// screen on the host hardn builds, through the engine with engineOptions:
// Enable on Debian 13 plans, asks for the hostname typed, applies ufw's
// rule, policy and start, and arms the revert timer, the countdown and
// sudo hardn confirm said after; under plan only it stops at the plan,
// nothing run or armed; 0 goes back (nil), q quits (nav.ErrQuit), the end
// of the input aborts (prompt.ErrAborted). The signals are registered for
// the apply alone, and never while a prompt waits. The screen plans with
// the engine's session: one over SSH whose port can't be read refuses
// Enable on the screen, said before anything is asked or run, where no
// session at all would plan it, the refusal then the engine's alone.
func TestFirewallScreen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dryRun  bool
		lines   []string
		want    error
		applied bool
		said    []string
		getenv  func(string) string // the session's environment, when not alice's
		unsaid  []string
	}{
		{"enable, then return", false, []string{"1", "web-01", "0"}, nil, true,
			[]string{"Enable the firewall?", "Type the hostname", "Operation Successful", "sudo hardn confirm"}, nil, nil},
		{"enable under plan only", true, []string{"1", "0"}, nil, false, []string{"No actual changes were made to your system."}, nil, nil},
		{"enable declined", false, []string{"1", "0", "0"}, nil, false, []string{"Operation Cancelled"}, nil, nil},
		{"quit", false, []string{"q"}, nav.ErrQuit, false, nil, nil, nil},
		{"the end of the input", false, nil, prompt.ErrAborted, false, nil, nil, nil},
		{"the session's port unreadable", false, []string{"1", "0"}, nil, false,
			[]string{"Warning", "hardn can't tell which port this session uses"},
			func(k string) string { return map[string]string{"SSH_CONNECTION": "192.0.2.10 50022 192.0.2.20"}[k] },
			[]string{"Lockout risk", "Type the hostname", "Operation Failed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newFWHost(t, "debian13")
			s := useFWHost(t, h, tc.lines...)
			if tc.getenv != nil {
				revertGetenv = tc.getenv
			}
			cfg := config.DefaultConfig()
			cfg.DryRun = tc.dryRun
			err := runFirewallScreen(context.Background(), cfg)
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("runFirewallScreen = %v, want %v; screen:\n%s", err, tc.want, s.out.String())
			}
			out := stripANSI(s.out.String())
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the screen lacks %q:\n%s", w, out)
			}
			for _, w := range tc.unsaid {
				if strings.Contains(out, w) {
					t.Errorf("the screen says %q:\n%s", w, out)
				}
			}
			if got := h.changed(); tc.applied != slices.Contains(got, "ufw --force enable") || !tc.applied && len(got) > 0 {
				t.Errorf("ufw changed by %q, want applied %t", got, tc.applied)
			}
			if h.armed() != tc.applied {
				t.Errorf("the revert timer armed %t, want %t", h.armed(), tc.applied)
			}
			if want := map[bool]int{true: 1}[tc.applied]; s.applies != want || s.live != 0 || s.atPrompt != 0 {
				t.Errorf("signals registered %d times (want %d), %d left, %d prompts read under them", s.applies, want, s.live, s.atPrompt)
			}
		})
	}
}

// TestFirewallHook: withFirewallScreen sets the Firewall item's screen on
// the configuration main gives it, plan only set after the hook stopping
// Enable at its plan, and the item's description Proxmox VE's on Proxmox
// VE alone (decision 7), firewalld's on Rocky Linux alone, as detect reads
// it.
func TestFirewallHook(t *testing.T) {
	h := newFWHost(t, "debian13")
	s := useFWHost(t, h, "1", "0")
	cfg := config.DefaultConfig()
	set := withFirewallScreen(mainmenu.Options{}, cfg)
	if set.Firewall == nil || set.FirewallPVE == nil {
		t.Fatal("withFirewallScreen did not set the Firewall item on the menu given")
	}
	cfg.DryRun = true
	if err := set.Firewall(); err != nil {
		t.Fatalf("the Firewall item = %v; screen:\n%s", err, s.out.String())
	}
	if got := h.changed(); len(got) > 0 || !strings.Contains(s.out.String(), "No actual changes were made to your system.") || s.applies != 0 {
		t.Errorf("ufw changed by %q, %d applies; screen:\n%s", got, s.applies, s.out.String())
	}
	if set.FirewallPVE() || set.FirewallEL() {
		t.Error("Debian 13 is taken for Proxmox VE, or Rocky Linux")
	}
	pve := newFWHost(t, "pve9")
	useFWHost(t, pve)
	if set := withFirewallScreen(mainmenu.Options{}, cfg); !set.FirewallPVE() || set.FirewallEL() {
		t.Error("Proxmox VE 9 is not taken for Proxmox VE, or is taken for Rocky Linux")
	}
	rocky := newFWHost(t, "rocky")
	useFWHost(t, rocky)
	if set := withFirewallScreen(mainmenu.Options{}, cfg); set.FirewallPVE() || !set.FirewallEL() {
		t.Error("Rocky Linux 10 is taken for Proxmox VE, or not for Rocky Linux")
	}
}

// fwLockoutWords are -w's refusal off a terminal (proposed, refactor plan
// slice 4, unit (d)), as output.Human says it.
const fwLockoutWords = "Enabling the firewall here risks a lockout (PLAN_REFUSED)"

// fwELWords are -w's refusal on Rocky Linux and AlmaLinux (proposed,
// el-firewalld.md §1.7), as output.Human says it.
const fwELWords = "hardn doesn't configure the firewall from -w on Rocky Linux, AlmaLinux or RHEL (FIREWALL_EL_READ_ONLY)"

// runW runs hardn with args as root (runG) on the Firewall host h, its
// configuration hardn's defaults, the session alice's; stdout's render
// environment env when set (onTTY: on a terminal), the user typing lines;
// setup, when set, run once the seams are in place.
func runW(t *testing.T, h *fwTestHost, env func() term.Env, lines []string, setup func(*dnsSeams), args ...string) (result, *dnsSeams) {
	t.Helper()
	return runG(t, h.dnsHost, config.DefaultConfig(), func(s *dnsSeams) {
		revertGetenv = fwSession
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

// TestConfigureFirewallFlag: -w plans and applies the Firewall control's
// Enable through the engine (refactor plan slice 4, unit (d);
// gallery-firewall.md §5, decision 4, adopted): on a terminal the plan,
// "Enable the firewall?" above the hostname typed, never y/N, the apply,
// the revert timer armed, then the countdown and sudo hardn confirm, exit
// status 0; a wrong phrase asked again; 0 and q declining, nothing run,
// 3; the hostname unreadable said in place of the question, nothing
// asked or run, 1; the end of the input 130. Off a terminal the plan is
// refused, PLAN_REFUSED, nothing run or armed, 1, and so it is where
// stdout alone, or stdin alone, is a terminal; with --yes too. A plain plan
// ("Start ufw at boot" alone) is applied unasked, nothing armed, 0, on a
// terminal, and off one with --yes; off one without it, shown and not
// applied, CONFIRMATION_REQUIRED, 2.
// -n the plan alone, no
// question, nothing run, 3, on a terminal or off. Proxmox VE refused
// (UFW_NOT_APPLICABLE), 1. Rocky Linux refused before anything is planned
// or run (FIREWALL_EL_READ_ONLY), 1, on a terminal or off, -n too. A host already as asked: nothing to change, 0,
// nothing asked. Nothing goes through the legacy provider, and the
// signals are registered for an apply alone, an empty plan's too, as
// runControls applies any flag's.
func TestConfigureFirewallFlag(t *testing.T) {
	for _, tc := range []struct {
		name, host       string
		tty, dryRun, yes bool
		env              func() term.Env // stdout's render environment, when not tty's
		lines            []string
		code             int      // the exit status; 0: hardn did not exit
		said             []string // in order
		unsaid           []string
		applied          bool // Enable's lockout-risk plan applied: ufw started, the timer armed
		plain            bool // the plain plan applied: ufw's service started at boot, nothing armed
	}{
		{name: "on a terminal", host: "debian13", tty: true, lines: []string{"web-01"}, applied: true,
			said: []string{"Lockout risk", "Enable the firewall?", "Type the hostname [web-01, 0 or q]", "Done", "Operation Successful", "sudo hardn confirm"}},
		{name: "a wrong phrase, then the hostname", host: "debian13", tty: true, lines: []string{"y", "web-01"}, applied: true,
			said: []string{"Type the hostname", "Type the hostname", "Operation Successful"}},
		{name: "0 declines", host: "debian13", tty: true, lines: []string{"0"}, code: 3, said: []string{"Type the hostname", "Operation Cancelled"}},
		{name: "q declines", host: "debian13", tty: true, lines: []string{"q"}, code: 3, said: []string{"Type the hostname"}},
		{name: "the end of the input", host: "debian13", tty: true, code: 130, said: []string{"Type the hostname"}},
		{name: "the hostname unreadable", host: "no-hostname", tty: true, code: 1,
			said: []string{"Lockout risk", "Can't confirm with the hostname"}, unsaid: []string{"Type the hostname"}},
		{name: "off a terminal", host: "debian13", code: 1,
			said: []string{"Lockout risk", "No actual changes were made to your system.", fwLockoutWords, "To go on: run hardn -w from a terminal, which asks you to confirm."}},
		{name: "stdout a terminal, stdin not", host: "debian13", env: func() term.Env { return ttyEnv(true, false) }, code: 1,
			said: []string{"Lockout risk", fwLockoutWords}, unsaid: []string{"Type the hostname"}},
		{name: "stdin a terminal, stdout not", host: "debian13", env: func() term.Env { return ttyEnv(false, true) }, code: 1,
			said: []string{"Lockout risk", fwLockoutWords}, unsaid: []string{"Type the hostname"}},
		{name: "a plain plan on a terminal", host: "boot-off", tty: true, plain: true,
			said: []string{"Start ufw at boot"}, unsaid: []string{"Lockout risk", "Type the hostname", "Enable the firewall?"}},
		{name: "a plain plan off a terminal", host: "boot-off", code: 2,
			said: []string{"Start ufw at boot", confirmationWords, "CONFIRMATION_REQUIRED"}, unsaid: []string{"Lockout risk", "Type the hostname", fwLockoutWords}},
		{name: "a plain plan off a terminal, --yes", host: "boot-off", yes: true, plain: true,
			said: []string{"Start ufw at boot"}, unsaid: []string{"Lockout risk", "Type the hostname", fwLockoutWords, "CONFIRMATION_REQUIRED"}},
		{name: "off a terminal, --yes", host: "debian13", yes: true, code: 1,
			said: []string{"Lockout risk", "No actual changes were made to your system.", fwLockoutWords}, unsaid: []string{"CONFIRMATION_REQUIRED"}},
		{name: "on a terminal, --yes", host: "debian13", tty: true, yes: true, lines: []string{"web-01"}, applied: true,
			said: []string{"Lockout risk", "Enable the firewall?", "Type the hostname", "Operation Successful", "sudo hardn confirm"}},
		{name: "-n off a terminal, --yes", host: "debian13", yes: true, dryRun: true, code: 3, said: []string{"Lockout risk", "No actual changes were made to your system."},
			unsaid: []string{fwLockoutWords, "CONFIRMATION_REQUIRED"}},
		{name: "-n on a terminal", host: "debian13", tty: true, dryRun: true, code: 3, said: []string{"Lockout risk", "No actual changes were made to your system."}},
		{name: "-n off a terminal", host: "debian13", dryRun: true, code: 3, said: []string{"Lockout risk", "No actual changes were made to your system."}},
		{name: "Proxmox VE", host: "pve9", tty: true, code: 1, said: []string{"UFW_NOT_APPLICABLE"}},
		{name: "Proxmox VE off a terminal", host: "pve9", code: 1, said: []string{"UFW_NOT_APPLICABLE"}},
		{name: "Rocky Linux", host: "rocky", tty: true, code: 1, said: []string{fwELWords, "To go on: use the Firewall screen."}, unsaid: []string{"Lockout risk", "Type the hostname"}},
		{name: "Rocky Linux off a terminal, --yes", host: "rocky", yes: true, code: 1, said: []string{fwELWords}, unsaid: []string{"Lockout risk", fwLockoutWords}},
		{name: "-n on Rocky Linux", host: "rocky", dryRun: true, code: 1, said: []string{fwELWords}, unsaid: []string{"Lockout risk"}},
		{name: "RHEL 10, supported", host: "rhel", tty: true, code: 1, said: []string{fwELWords, "To go on: use the Firewall screen."}, unsaid: []string{"Lockout risk", "Type the hostname"}},
		{name: "RHEL 8, unlisted", host: "rhel8", tty: true, code: 1, said: []string{fwELWords}, unsaid: []string{"To go on: use the Firewall screen.", "Lockout risk", "Type the hostname"}},
		{name: "nothing to change", host: "enabled", tty: true, said: []string{"Nothing needs changing."}},
	} {
		args := []string{"-w"}
		if tc.dryRun {
			args = []string{"-n", "-w"}
		}
		if tc.yes {
			args = append(args, "--yes")
		}
		t.Run(tc.name, func(t *testing.T) {
			h := newFWHost(t, tc.host)
			env := tc.env
			if tc.tty {
				env = onTTY
			}
			r, s := runW(t, h, env, tc.lines, nil, args...)
			out := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstdout:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stdout, r.stderr)
			}
			if w := inOrder(out, tc.said...); w != "" {
				t.Errorf("the report lacks %q after the text before it:\n%s", w, out)
			}
			for _, w := range append([]string{"y/N", "[SUCCESS]", "[ERROR]", "Script completed", "Firewall configured"}, tc.unsaid...) {
				if strings.Contains(out+r.stdout, w) {
					t.Errorf("printed %q:\n%s%s", w, out, r.stdout)
				}
			}
			if got := h.changed(); tc.applied != slices.Contains(got, "ufw --force enable") || !tc.applied && len(got) > 0 {
				t.Errorf("ufw changed by %q, want applied %t", got, tc.applied)
			}
			if h.armed() != tc.applied {
				t.Errorf("the revert timer armed %t, want %t", h.armed(), tc.applied)
			}
			if (tc.host == "rocky" || strings.HasPrefix(tc.host, "rhel")) && len(h.ran) > 0 {
				t.Errorf("-w planned on %s: ran %q", tc.host, h.ran)
			}
			if tc.host == "boot-off" && (h.unit == "enabled") != tc.plain {
				t.Errorf("ufw's service at boot %q, want the plain plan applied %t", h.unit, tc.plain)
			}
			if want := map[bool]int{true: 1}[tc.applied || !tc.dryRun && tc.code == 0]; s.applies != want || s.live != 0 {
				t.Errorf("signals registered %d times, want %d; %d left", s.applies, want, s.live)
			}
		})
	}
}

// TestConfigureFirewallFlagInterrupted: a signal during -w's apply on a
// terminal (engineSignals' context cancelled as ufw starts) rolls the
// change back and ends hardn at once with 130, as runControls ends -g, the
// flags after it not run.
func TestConfigureFirewallFlagInterrupted(t *testing.T) {
	h := newFWHost(t, "debian13")
	ran := withLog(t, false)
	var cancel context.CancelFunc
	r, s := runW(t, h, onTTY, []string{"web-01"}, func(s *dnsSeams) {
		engineSignals = func(ctx context.Context) (context.Context, context.CancelFunc) {
			s.applies++
			c, cc := context.WithCancel(ctx)
			cancel = cc
			return c, cc
		}
		h.during = func() { cancel() }
	}, "-w", "-p")
	out := stripANSI(s.out.String())
	if !r.ended || r.code != 130 {
		t.Fatalf("exit status %d (ended %v), want 130; report:\n%s\nstderr:\n%s", r.code, r.ended, out, r.stderr)
	}
	if s.applies != 1 || s.held == 0 {
		t.Errorf("signals registered %d times, held %d: want one apply, rolled back under the hold", s.applies, s.held)
	}
	if h.active {
		t.Errorf("ufw left running after the rollback; ran %q", h.changed())
	}
	if ran() {
		t.Errorf("-p ran after -w was interrupted:\n%s", out)
	}
}

// TestConfigureFirewallPlanOnlyFromFile: hardn.yml's dryRun: true makes -w
// the plan alone, as -n does, on a terminal too: no question, nothing run
// or armed, 3 with changes to make; --dry-run=false over it asks and
// applies as live (parity-firewall.md §6, adopted 2026-11-02).
func TestConfigureFirewallPlanOnlyFromFile(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		lines []string
		code  int
	}{
		{[]string{"-w"}, nil, 3},
		{[]string{"--dry-run=false", "-w"}, []string{"web-01"}, 0},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			h := newFWHost(t, "debian13")
			h.AddFile(dnsTestYML, []byte("username: alice\ndryRun: true\n"), 0o644, 0, 0)
			cfg, err := config.Read(context.Background(), h, dnsTestYML)
			if err != nil {
				t.Fatal(err)
			}
			r, s := runG(t, h.dnsHost, cfg, func(s *dnsSeams) {
				revertGetenv = fwSession
				in := prompt.New(uitest.NewTypist(&s.out, tc.lines...), false)
				engineInput = func() *prompt.Reader { return in }
				stdoutEnv = onTTY
			}, tc.args...)
			out := stripANSI(s.out.String())
			if got := map[bool]int{true: r.code}[r.ended]; got != tc.code {
				t.Fatalf("exit status %d (ended %v), want %d; report:\n%s\nstderr:\n%s", r.code, r.ended, tc.code, out, r.stderr)
			}
			live := tc.code == 0
			if strings.Contains(out, "Type the hostname") != live || strings.Contains(out, "No actual changes were made to your system.") == live {
				t.Errorf("asked %t, want %t; report:\n%s", !live, live, out)
			}
			if h.armed() != live || slices.Contains(h.changed(), "ufw --force enable") != live {
				t.Errorf("armed %t, ufw changed by %q; want applied %t", h.armed(), h.changed(), live)
			}
		})
	}
}
