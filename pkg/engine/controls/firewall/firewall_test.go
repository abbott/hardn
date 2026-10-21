package firewall_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
)

// Nothing here runs a real command or reads the machine: every host is
// hostfake's. ufw answers from a model of ufw 0.36.2's status and saved
// files (shaped as pkg/host/ufw's tests shape them, from ufw's sources, not
// captured from a host); sshd -T from the ports a case gives, with the
// listeners sshd derives from them; ss and ssh.socket from what a case
// says is bound and configured; pve-firewall from its files as its parser
// reads them. Synthetic values only: 192.0.2.0/24, port 2230.

// platform is a supported release's stock host.
type platform struct {
	facts  detect.Facts
	bin    string // the sshd the check runs
	socket bool   // ssh.socket listens, on a dual-stack [::]
	noSS   bool   // ss is not installed
}

func debian13() platform {
	return platform{bin: "sshd", facts: detect.Facts{
		OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
			Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
			Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}}
}

func ubuntu2604() platform {
	p := debian13()
	p.socket = true
	p.facts.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04", VersionCodename: "resolute"}
	p.facts.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
	p.facts.SSH = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
		Socket:  detect.UnitState{Enabled: "enabled", Active: "active"},
		Service: detect.UnitState{Enabled: "disabled", Active: "active"}}
	return p
}

func alpine324() platform {
	p := debian13()
	p.bin, p.noSS = "/usr/sbin/sshd", true
	p.facts.OS = detect.OSRelease{ID: "alpine", VersionID: "3.24.0"}
	p.facts.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.24", Support: detect.Supported}
	p.facts.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes}
	p.facts.SSH = detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes, InRunlevel: detect.Yes}}
	return p
}

func pve9() platform {
	p := debian13()
	p.facts.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
	p.facts.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	return p
}

// fw is ufw on a test host.
type fw struct {
	missing   bool // not installed
	active    bool // its chains loaded
	enabled   string
	incoming  string // the default incoming policy: deny (""), allow or reject
	noV6      bool   // IPV6=no
	noKernel6 bool   // no /proc/sys/net/ipv6
	rules     []rule
	garbage   bool               // the status has a line ufw does not print
	fail      *hostfake.Response // ufw status verbose's answer
}

// rule is a saved rule: action on port/tcp from anywhere, in both IP
// versions, or from src, in IPv4 only.
type rule struct{ action, port, src string }

func allow(ports ...string) []rule {
	var rs []rule
	for _, p := range ports {
		rs = append(rs, rule{action: "allow", port: p})
	}
	return rs
}

var policyWord = map[string]string{"allow": "ACCEPT", "deny": "DROP", "reject": "REJECT"}

func (u fw) policy() string {
	if u.incoming == "" {
		return "deny"
	}
	return u.incoming
}

func (u fw) v6() bool { return !u.noV6 && !u.noKernel6 }

// status is ufw status verbose's output, as get_status prints it.
func (u fw) status() string {
	switch {
	case u.garbage:
		return "Status: active\ngarbage\n"
	case !u.active:
		return "Status: inactive\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Status: active\nLogging: on (low)\nDefault: %s (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n", u.policy())
	if len(u.rules) > 0 {
		fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	}
	for _, r := range u.rules {
		from := r.src
		if from == "" {
			from = "Anywhere"
		}
		fmt.Fprintf(&b, "%-26s %-12s%-26s\n", r.port+"/tcp", strings.ToUpper(r.action)+" IN", from)
	}
	for _, r := range u.rules {
		if r.src == "" && u.v6() {
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", r.port+"/tcp (v6)", strings.ToUpper(r.action)+" IN", "Anywhere (v6)")
		}
	}
	return b.String()
}

// files are ufw's settings and saved rules.
func (u fw) files(h *hostfake.System) {
	v6, enabled := "yes", u.enabled
	if u.noV6 {
		v6 = "no"
	}
	if enabled == "" {
		enabled = map[bool]string{true: "yes", false: "no"}[u.active]
	}
	h.AddFile(ufw.DefaultsFile, []byte(fmt.Sprintf("IPV6=%s\nDEFAULT_INPUT_POLICY=%q\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n", v6, policyWord[u.policy()])), 0o644, 0, 0)
	h.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct{ path, any string }{{ufw.UserRules, "0.0.0.0/0"}, {ufw.User6Rules, "::/0"}} {
		var b strings.Builder
		b.WriteString("*filter\n### RULES ###\n")
		for _, r := range u.rules {
			src := r.src
			switch {
			case src != "" && f.any == "::/0":
				continue
			case src == "":
				src = f.any
			}
			fmt.Fprintf(&b, "\n### tuple ### %s tcp %s %s any %s in\n", r.action, r.port, f.any, src)
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
	if !u.noKernel6 {
		h.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	}
}

// pf is pve-firewall on a Proxmox VE test host.
type pf struct {
	cluster, hostfw string // the files' content; "" for none
	state           string // systemctl is-active's answer; "" is active
	unit            string // the unit asked about; "" is pve-firewall.service
}

// site is one test's host.
type site struct {
	plat   platform
	ports  []uint16 // sshd's Port lines (00-hardn.conf); nil is the stock 22
	socket []string // ssh.socket's Listen entries; nil is [::] on each port
	bound  []string // the sockets ss shows; nil is what is configured
	ufw    fw
	pve    pf
	failT  *hostfake.Response // sshd -T's answer
	ssErr  *hostfake.Response // ss's answer
	facts  func(*detect.Facts)
}

const stockMain = "Include /etc/ssh/sshd_config.d/*.conf\n\nKbdInteractiveAuthentication no\nUsePAM yes\n"

// sshdT is sshd -T's output for ports, with no ListenAddress: every port
// on [::] and 0.0.0.0 (sshd_config(5)), and the listeners.
func sshdT(ports []uint16) (out string, ls []string) {
	var b strings.Builder
	for _, p := range ports {
		fmt.Fprintf(&b, "port %d\n", p)
		ls = append(ls, fmt.Sprintf("[::]:%d", p), fmt.Sprintf("0.0.0.0:%d", p))
	}
	for _, l := range ls {
		b.WriteString("listenaddress " + l + "\n")
	}
	b.WriteString("permitrootlogin without-password\npubkeyauthentication yes\npasswordauthentication yes\nkbdinteractiveauthentication no\nusepam yes\n")
	return b.String(), ls
}

// ss is ss -Htln's output for socks; on a socket host [::] is dual-stack,
// which ss prints as "*".
func ss(socks []string, dual bool) string {
	var b strings.Builder
	for _, s := range socks {
		peer := "0.0.0.0:*"
		if strings.HasPrefix(s, "[") {
			peer = "[::]:*"
			if dual && strings.HasPrefix(s, "[::]:") {
				s, peer = "*:"+strings.TrimPrefix(s, "[::]:"), "*:*"
			}
		}
		fmt.Fprintf(&b, "LISTEN 0      4096   %20s   %s\n", s, peer)
	}
	return b.String()
}

func (s site) build(t *testing.T) (*hostfake.System, detect.Facts) {
	t.Helper()
	h := hostfake.New(host.Options{})
	h.AddFile(sshd.MainConfig, []byte(stockMain), 0o644, 0, 0)
	h.AddDir(sshd.DropInDir, 0o755, 0, 0)
	ports := s.ports
	if ports == nil {
		ports = []uint16{22}
	} else {
		var b strings.Builder
		for _, p := range ports {
			fmt.Fprintf(&b, "Port %d\n", p)
		}
		h.AddFile(sshd.DropInDir+"/00-hardn.conf", []byte(b.String()), 0o644, 0, 0)
	}
	out, ls := sshdT(ports)
	socket, bound := s.socket, s.bound
	if socket == nil {
		for _, p := range ports {
			socket = append(socket, fmt.Sprintf("[::]:%d", p))
		}
	}
	if bound == nil {
		bound = ls
		if s.plat.socket {
			bound = socket
		}
	}
	if !s.ufw.missing {
		s.ufw.files(h)
	}
	unit := s.pve.unit
	if unit == "" {
		unit = pve.FirewallUnit
	}
	for name, data := range map[string]string{pve.ClusterFW: s.pve.cluster, pve.HostFW: s.pve.hostfw} {
		if data != "" {
			h.AddFile(name, []byte(data), 0o640, 0, 33)
		}
	}
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case argv[0] == "ufw" && s.ufw.missing:
			return hostfake.Response{Err: fmt.Errorf("host: run ufw: %w", host.ErrNotFound)}
		case cmd == "ufw status verbose" && s.ufw.fail != nil:
			return *s.ufw.fail
		case cmd == "ufw status verbose":
			return hostfake.Response{Stdout: s.ufw.status()}
		case argv[0] == s.plat.bin && slices.Contains(argv, "-T") && s.failT != nil:
			return *s.failT
		case argv[0] == s.plat.bin && slices.Contains(argv, "-T"):
			return hostfake.Response{Stdout: out}
		case cmd == "systemctl show -p Listen ssh.socket":
			var b strings.Builder
			for _, l := range socket {
				fmt.Fprintf(&b, "Listen=%s (Stream)\n", l)
			}
			return hostfake.Response{Stdout: b.String()}
		case cmd == "ss -Htln" && s.plat.noSS:
			return hostfake.Response{Err: fmt.Errorf("host: run ss: %w", host.ErrNotFound)}
		case cmd == "ss -Htln" && s.ssErr != nil:
			return *s.ssErr
		case cmd == "ss -Htln":
			return hostfake.Response{Stdout: ss(bound, s.plat.socket)}
		case cmd == "systemctl is-active "+unit:
			state := s.pve.state
			if state == "" {
				return hostfake.Response{Stdout: "active\n"}
			}
			return hostfake.Response{Stdout: state + "\n", ExitCode: 3}
		}
		t.Errorf("unexpected command %q", cmd)
		return hostfake.Response{Err: hostfake.ErrUnscripted}
	}
	facts := s.plat.facts
	if s.facts != nil {
		s.facts(&facts)
	}
	return h, facts
}

// row is one status the control shows on a host.
type row struct {
	name  string
	site  site
	state engine.State
	want  string   // the detail
	codes []string // the findings' codes, in order
	err   string   // the error's code (pkg/output), "" for none
}

func run(t *testing.T, r row) engine.Status {
	t.Helper()
	h, facts := r.site.build(t)
	st, err := firewall.Incoming{}.Check(context.Background(), h, facts)
	if code := codeOf(err); code != r.err {
		t.Errorf("error %v: code %q, want %q", err, code, r.err)
	}
	if err != nil && st.State != engine.Unknown {
		t.Errorf("an error with %s %q: a failed read is Unknown", st.State, st.Detail)
	}
	if st.State != r.state || st.Detail != r.want {
		t.Errorf("%s %q, want %s %q (evidence %q, findings %v)", st.State, st.Detail, r.state, r.want, st.Evidence, st.Findings)
	}
	var codes []string
	for _, fd := range st.Findings {
		codes = append(codes, fd.Code)
		if e, ok := output.Lookup(fd.Code); !ok || e.Kind != output.KindFinding {
			t.Errorf("finding %s: not a registered finding code", fd.Code)
		}
	}
	if !slices.Equal(codes, r.codes) {
		t.Errorf("findings %v, want codes %v", st.Findings, r.codes)
	}
	readOnly(t, h, r.site.plat)
	return st
}

func codeOf(err error) string {
	if err == nil {
		return ""
	}
	return output.CodeOf(err)
}

// readOnly fails for any call that changes the fake, and any command but
// the reads the check makes; on Proxmox VE, for any ufw command.
func readOnly(t *testing.T, h *hostfake.System, p platform) {
	t.Helper()
	onPVE := p.facts.Proxmox.Installed == detect.Yes
	for _, c := range h.Calls() {
		switch c.Op {
		case "read", "read-nofollow", "stat", "lstat", "readdir", "readlink", "resolve-symlink", "lookup-user", "lookup-group":
			if onPVE && strings.HasPrefix(c.Path, "/etc/ufw") {
				t.Errorf("read %s on Proxmox VE", c.Path)
			}
		case "run":
			switch cmd := strings.Join(c.Argv, " "); {
			case cmd == "ufw status verbose" && !onPVE, strings.HasPrefix(cmd, p.bin+" -T -f "+sshd.MainConfig),
				cmd == "systemctl show -p Listen ssh.socket", cmd == "ss -Htln",
				cmd == "systemctl is-active "+pve.FirewallUnit, cmd == "systemctl is-active "+pve.NFTablesUnit:
			default:
				t.Errorf("ran %q", cmd)
			}
		default:
			t.Errorf("a call that is not a read: %v", c)
		}
	}
}

// The findings' codes.
const (
	blockedF    = firewall.CodeSSHBlocked
	uncheckedF  = firewall.CodeSSHUnchecked
	notAllowed  = "UFW_SSH_NOT_ALLOWED"
	restricted  = "UFW_SSH_RESTRICTED"
	excepted    = "UFW_SSH_ALLOWED_EXCEPT"
	ufwUnknown  = "UFW_UNKNOWN"
	atStart     = "UFW_SSH_NOT_ALLOWED_AT_START"
	notInEffect = "SSH_PORT_NOT_IN_EFFECT"
)

var (
	on        = fw{active: true}
	allow22   = fw{active: true, rules: allow("22")}
	allow2230 = fw{active: true, rules: allow("2230")}
	failed    = &hostfake.Response{Stderr: "ERROR: You need to be root to run this script\n", ExitCode: 1}
	invalid   = &hostfake.Response{Stderr: "/etc/ssh/sshd_config.d/10-site.conf line 1: Bad configuration option: Bogus\r\n" +
		"/etc/ssh/sshd_config.d/10-site.conf: terminating, 1 bad configuration options\r\n", ExitCode: 255}
	noSSHD  = &hostfake.Response{Err: fmt.Errorf("host: run sshd: %w", host.ErrNotFound)}
	stale22 = []string{"[::]:22", "0.0.0.0:22"} // what sshd still holds after an edit to 2230 not reloaded
)

func with(u fw, f func(*fw)) fw {
	f(&u)
	return u
}

// TestUFW: every ufw line of check-firewall.md §5 on Debian 13, where sshd
// holds its own sockets.
func TestUFW(t *testing.T) {
	d := debian13()
	for _, r := range []row{
		{name: "ufw not installed", site: site{plat: d, ufw: fw{missing: true}}, state: engine.NotConfigured, want: "ufw not installed"},
		{name: "stock: inactive", site: site{plat: d}, state: engine.NotConfigured, want: "inactive"},
		{name: "default deny, 22 allowed", site: site{plat: d, ufw: allow22}, state: engine.Configured, want: "deny policy"},
		{name: "default deny, 2230 allowed", site: site{plat: d, ports: []uint16{2230}, ufw: allow2230}, state: engine.Configured, want: "deny policy"},
		{name: "default reject, 22 allowed", site: site{plat: d, ufw: with(allow22, func(u *fw) { u.incoming = "reject" })}, state: engine.Configured, want: "reject policy"},
		{name: "22 rate-limited", site: site{plat: d, ufw: fw{active: true, rules: []rule{{action: "limit", port: "22"}}}}, state: engine.Configured, want: "deny policy"},
		{name: "22 from 192.0.2.0/24 only, no kernel IPv6", site: site{plat: d, ufw: fw{active: true, noKernel6: true, rules: []rule{{"allow", "22", "192.0.2.0/24"}}}},
			state: engine.Configured, want: "deny policy", codes: []string{restricted}},
		{name: "22 from everywhere but 192.0.2.0/24", site: site{plat: d, ufw: fw{active: true, rules: []rule{{"deny", "22", "192.0.2.0/24"}, {action: "allow", port: "22"}}}},
			state: engine.Configured, want: "deny policy", codes: []string{excepted}},
		{name: "default deny, 2230 not allowed", site: site{plat: d, ports: []uint16{2230}, ufw: allow22},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, blockedF}},
		{name: "default deny, nothing allowed", site: site{plat: d, ufw: on}, state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, blockedF}},
		{name: "IPV6=no, sshd on [::]:22", site: site{plat: d, ufw: with(allow22, func(u *fw) { u.noV6 = true })},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, blockedF}},
		{name: "default allow", site: site{plat: d, ufw: fw{active: true, incoming: "allow"}}, state: engine.Partial, want: "allow policy"},
		{name: "default allow, 22 denied", site: site{plat: d, ufw: fw{active: true, incoming: "allow", rules: []rule{{action: "deny", port: "22"}}}},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, blockedF}},
		{name: "active with ENABLED=no", site: site{plat: d, ufw: with(allow22, func(u *fw) { u.enabled = "no" })}, state: engine.Partial, want: "off after restart"},
		{name: "ENABLED=yes, stopped, 22 saved", site: site{plat: d, ufw: fw{enabled: "yes", rules: allow("22")}}, state: engine.NotConfigured, want: "enabled, not running"},
		{name: "ENABLED=yes, stopped, nothing saved for 22", site: site{plat: d, ufw: fw{enabled: "yes"}},
			state: engine.NotConfigured, want: "enabled, not running", codes: []string{atStart, atStart}},
		{name: "a rule hardn cannot read", site: site{plat: d, ufw: fw{active: true, rules: allow("70000", "22")}},
			state: engine.Unknown, want: "can't read", codes: []string{ufwUnknown, ufwUnknown}},
		{name: "a status line ufw does not print", site: site{plat: d, ufw: fw{garbage: true}}, state: engine.Unknown, want: "can't read", codes: []string{ufwUnknown}},
		{name: "ufw status fails", site: site{plat: d, ufw: fw{fail: failed}}, state: engine.Unknown, want: "can't read", err: "COMMAND_FAILED"},
	} {
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

// TestSSHListeners: the cross-check against the sockets ss shows bound,
// and the listeners not established.
func TestSSHListeners(t *testing.T) {
	d, u := debian13(), ubuntu2604()
	moved := []uint16{2230}
	for _, r := range []row{
		{name: "an edit to 2230 not reloaded, 22 allowed", site: site{plat: d, ports: moved, bound: stale22, ufw: allow22},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, notInEffect, blockedF}},
		{name: "an edit to 2230 not reloaded, 2230 allowed: sshd still on 22", site: site{plat: d, ports: moved, bound: stale22, ufw: allow2230},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, notInEffect, blockedF}},
		{name: "an edit to 2230 not reloaded, both allowed", site: site{plat: d, ports: moved, bound: stale22, ufw: fw{active: true, rules: allow("22", "2230")}},
			state: engine.Unknown, want: "ssh config not in effect", codes: []string{notInEffect}},
		{name: "an edit not reloaded, default allow", site: site{plat: d, ports: moved, bound: stale22, ufw: fw{active: true, incoming: "allow"}},
			state: engine.Partial, want: "allow policy", codes: []string{notInEffect, uncheckedF}},
		{name: "22 still bound after the move to 2230, 22 refused", site: site{plat: d, ports: moved, bound: append([]string{"[::]:2230", "0.0.0.0:2230"}, stale22...), ufw: allow2230},
			state: engine.Unknown, want: "can't tell if 22 is ssh", codes: []string{notAllowed, notAllowed, "SSH_PORT_22_BOUND"}},
		{name: "22 still bound after the move to 2230, both allowed", site: site{plat: d, ports: moved, bound: append([]string{"[::]:2230", "0.0.0.0:2230"}, stale22...), ufw: fw{active: true, rules: allow("22", "2230")}},
			state: engine.Configured, want: "deny policy"},
		{name: "ssh.service not running", site: site{plat: d, bound: []string{}, ufw: allow22, facts: func(f *detect.Facts) { f.SSH.Service.Active = "inactive" }},
			state: engine.Unknown, want: "can't tell ssh ports", codes: []string{notInEffect}},
		{name: "no sshd", site: site{plat: d, failT: noSSHD, ufw: allow22}, state: engine.Unknown, want: "sshd not installed", err: "COMMAND_NOT_FOUND"},
		{name: "no sshd, default allow", site: site{plat: d, failT: noSSHD, ufw: fw{active: true, incoming: "allow"}},
			state: engine.Partial, want: "allow policy", codes: []string{uncheckedF}},
		{name: "no sshd, ENABLED=yes and stopped", site: site{plat: d, failT: noSSHD, ufw: fw{enabled: "yes"}},
			state: engine.NotConfigured, want: "enabled, not running", codes: []string{uncheckedF}},
		{name: "a configuration sshd refuses", site: site{plat: d, failT: invalid, ufw: allow22}, state: engine.Unknown, want: "can't tell ssh ports", err: "SSHD_CONFIG_INVALID"},
		{name: "ss fails", site: site{plat: d, ssErr: failed, ufw: allow22}, state: engine.Unknown, want: "can't tell ssh ports", err: "COMMAND_FAILED"},
		{name: "ubuntu: the socket's [::]:22, 22 allowed", site: site{plat: u, ufw: allow22}, state: engine.Configured, want: "deny policy"},
		{name: "ubuntu: socket on 2230, 2230 allowed", site: site{plat: u, ports: moved, ufw: allow2230}, state: engine.Configured, want: "deny policy"},
		{name: "ubuntu: socket on 2230, 22 allowed only", site: site{plat: u, ports: moved, ufw: allow22},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, blockedF}},
		{name: "ubuntu: socket not restarted, *:22 bound, 2230 allowed", site: site{plat: u, ports: moved, bound: []string{"[::]:22"}, ufw: allow2230},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, notInEffect, blockedF}},
		{name: "ubuntu: socket not restarted, both allowed", site: site{plat: u, ports: moved, bound: []string{"[::]:22"}, ufw: fw{active: true, rules: allow("22", "2230")}},
			state: engine.Unknown, want: "ssh config not in effect", codes: []string{notInEffect}},
		{name: "ubuntu: daemon-reload not run, socket and sshd differ", site: site{plat: u, ports: moved, socket: []string{"[::]:22"}, ufw: allow22},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, notInEffect, blockedF}},
		{name: "ubuntu: no ss", site: site{plat: noSS(u), ufw: allow22}, state: engine.Unknown, want: "ss not installed", err: "COMMAND_NOT_FOUND"},
		{name: "ubuntu: no ss, 22 not allowed", site: site{plat: noSS(u), ufw: on}, state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, blockedF}},
	} {
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

func noSS(p platform) platform {
	p.noSS = true
	return p
}

// TestAlpine: ufw is in the community repository, not installed by
// setup-alpine; without ss the configured set stands.
func TestAlpine(t *testing.T) {
	a := alpine324()
	for _, r := range []row{
		{name: "stock: no ufw", site: site{plat: a, ufw: fw{missing: true}}, state: engine.NotConfigured, want: "ufw not installed"},
		{name: "ufw active, 22 allowed, no ss", site: site{plat: a, ufw: allow22}, state: engine.Configured, want: "deny policy"},
		{name: "ufw active, 2230 not allowed, no ss", site: site{plat: a, ports: []uint16{2230}, ufw: allow22},
			state: engine.Partial, want: "ssh blocked", codes: []string{notAllowed, notAllowed, blockedF}},
		{name: "ENABLED=yes after a reboot, not in a runlevel", site: site{plat: a, ufw: fw{enabled: "yes", rules: allow("22")}}, state: engine.NotConfigured, want: "enabled, not running"},
	} {
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
	st := run(t, row{site: site{plat: a, ufw: allow22}, state: engine.Configured, want: "deny policy"})
	if !slices.Contains(st.Evidence, "ss: not installed, so the bound sockets are not read") {
		t.Errorf("evidence %q: does not say the bound sockets were not read", st.Evidence)
	}
}

const (
	pveOn      = "[OPTIONS]\nenable: 1\n\n[IPSET management]\n192.0.2.0/24\n"
	pveDisable = "[OPTIONS]\nenable: 0\n"
)

// TestProxmox: pve-firewall's state, ufw never consulted (readOnly).
func TestProxmox(t *testing.T) {
	p := pve9()
	for _, r := range []row{
		{name: "stock: no firewall files", site: site{plat: p}, state: engine.NotConfigured, want: "disabled"},
		{name: "disabled at the datacenter", site: site{plat: p, pve: pf{cluster: pveDisable}}, state: engine.NotConfigured, want: "disabled"},
		{name: "on, DROP, sshd on 22", site: site{plat: p, pve: pf{cluster: pveOn}}, state: engine.Configured, want: "drop policy"},
		{name: "on, REJECT", site: site{plat: p, pve: pf{cluster: "[OPTIONS]\nenable: 1\npolicy_in: REJECT\n"}}, state: engine.Configured, want: "reject policy"},
		{name: "on, ACCEPT", site: site{plat: p, pve: pf{cluster: "[OPTIONS]\nenable: 1\npolicy_in: ACCEPT\n"}}, state: engine.Partial, want: "accept policy"},
		{name: "ufw installed and active: ignored", site: site{plat: p, ufw: on, pve: pf{cluster: pveOn}}, state: engine.Configured, want: "drop policy"},
		{name: "service stopped", site: site{plat: p, pve: pf{cluster: pveOn, state: "inactive"}}, state: engine.Unknown, want: "service stopped"},
		{name: "pmxcfs not mounted", site: site{plat: p, facts: func(f *detect.Facts) { f.Proxmox.ClusterFS = detect.No }},
			state: engine.Unknown, want: "/etc/pve not mounted", err: "PVE_CLUSTER_FS_DOWN"},
		{name: "a host rule may drop 22", site: site{plat: p, pve: pf{cluster: pveOn, hostfw: "[RULES]\nIN DROP -p tcp -dport 22\n"}},
			state: engine.Unknown, want: "ssh may be blocked", codes: []string{"PVE_RULE_MAY_BLOCK"}},
		{name: "a host rule may drop 8006", site: site{plat: p, pve: pf{cluster: pveOn, hostfw: "[RULES]\nIN DROP -p tcp -dport 8006\n"}},
			state: engine.Unknown, want: "web ui may be blocked", codes: []string{"PVE_RULE_MAY_BLOCK"}},
		{name: "a blacklist", site: site{plat: p, pve: pf{cluster: pveOn + "\n[IPSET blacklist]\n192.0.2.99\n"}},
			state: engine.Unknown, want: "ssh may be blocked", codes: []string{"PVE_BLACKLIST"}},
		{name: "nftables", site: site{plat: p, pve: pf{cluster: pveOn, hostfw: "[OPTIONS]\nnftables: 1\n", unit: pve.NFTablesUnit}},
			state: engine.Unknown, want: "uses nftables", codes: []string{"PVE_FIREWALL_NOT_MODELLED"}},
		{name: "an option pve-firewall skips", site: site{plat: p, pve: pf{cluster: "[OPTIONS]\nenable: 1 # on\n"}},
			state: engine.Unknown, want: "can't read", err: "PVE_FIREWALL_UNKNOWN"},
		{name: "sshd on 2230", site: site{plat: p, ports: []uint16{2230}, pve: pf{cluster: pveOn}},
			state: engine.Unknown, want: "2230 may be blocked", codes: []string{"PVE_PORT_NOT_22"}},
		{name: "sshd still on 2230 after an edit to 22: a stale port other than 22 is not looked for", site: site{plat: p, bound: []string{"0.0.0.0:2230", "[::]:2230"}, pve: pf{cluster: pveOn}},
			state: engine.Unknown, want: "ssh config not in effect", codes: []string{notInEffect}},
		{name: "a configuration sshd refuses", site: site{plat: p, failT: invalid, pve: pf{cluster: pveOn}},
			state: engine.Unknown, want: "can't tell ssh ports", err: "SSHD_CONFIG_INVALID"},
		{name: "whether the host runs Proxmox VE not established", site: site{plat: p, facts: func(f *detect.Facts) { f.Proxmox = detect.Proxmox{Installed: detect.Unknown} }},
			state: engine.Unknown, want: "can't tell if Proxmox", err: "PVE_UNKNOWN"},
	} {
		t.Run(r.name, func(t *testing.T) { run(t, r) })
	}
}

// TestEvidence: what the row was read from, as the commands printed it,
// and the cross-check's one line.
func TestEvidence(t *testing.T) {
	st := run(t, row{site: site{plat: debian13(), ports: []uint16{2230}, ufw: allow2230}, state: engine.Configured, want: "deny policy"})
	want := []string{
		"ufw status verbose: Status: active",
		"ufw status verbose: Default: deny (incoming), allow (outgoing), disabled (routed)",
		"ufw: ENABLED=yes",
		"sshd -T: listenaddress [::]:2230",
		"sshd -T: listenaddress 0.0.0.0:2230",
		"ss -Htln: [::]:2230 0.0.0.0:2230",
		"ufw: listener [::]:2230 (IPv6): allowed by 2230/tcp (v6) ALLOW IN Anywhere (v6)",
		"ufw: listener 0.0.0.0:2230 (IPv4): allowed by 2230/tcp ALLOW IN Anywhere",
	}
	if !slices.Equal(st.Evidence, want) {
		t.Errorf("evidence\n%q\nwant\n%q", st.Evidence, want)
	}
	st = run(t, row{site: site{plat: ubuntu2604(), ports: []uint16{2230}, ufw: allow22}, state: engine.Partial, want: "ssh blocked",
		codes: []string{notAllowed, notAllowed, blockedF}})
	if f := st.Findings[2]; f.Detail != "[::]:2230, 0.0.0.0:2230 blocked: lockout risk" || f.Source != "ufw status verbose" {
		t.Errorf("cross-check finding %+v", f)
	}
	if !slices.Contains(st.Evidence, "systemctl show -p Listen ssh.socket: [::]:2230") || !slices.Contains(st.Evidence, "ss -Htln: *:2230") {
		t.Errorf("evidence %q: no socket listeners or bound sockets", st.Evidence)
	}
	st = run(t, row{site: site{plat: debian13(), ports: []uint16{2230}, bound: stale22, ufw: fw{active: true, incoming: "allow"}},
		state: engine.Partial, want: "allow policy", codes: []string{notInEffect, uncheckedF}})
	if a, b := st.Findings[0], st.Findings[1]; a.Detail != "[::]:2230 0.0.0.0:2230 configured, not bound" || a.Source != "ss -Htln" ||
		b.Detail != "ssh config not in effect" || b.Source != "ss -Htln" {
		t.Errorf("findings %+v", st.Findings)
	}
	st = run(t, row{site: site{plat: debian13(), failT: noSSHD, ufw: fw{active: true, incoming: "allow"}}, state: engine.Partial, want: "allow policy", codes: []string{uncheckedF}})
	if f := st.Findings[0]; !strings.HasPrefix(f.Detail, "sshd not installed: sshd's listeners: ") || f.Source != "sshd -T" {
		t.Errorf("finding %+v", f)
	}
	st = run(t, row{site: site{plat: pve9(), pve: pf{cluster: pveOn}}, state: engine.Configured, want: "drop policy"})
	want = []string{
		"pve-firewall: enabled yes, policy_in DROP, nftables no",
		"systemctl is-active pve-firewall.service: active",
		"pve-firewall: port 22 management, port 8006 management",
		"pve-firewall: management 192.0.2.0/24",
		"sshd -T: listenaddress [::]:22",
		"sshd -T: listenaddress 0.0.0.0:22",
		"ss -Htln: [::]:22 0.0.0.0:22",
	}
	if !slices.Equal(st.Evidence, want) {
		t.Errorf("Proxmox VE evidence\n%q\nwant\n%q", st.Evidence, want)
	}
}

// TestEngineCheck: Engine.Check over Controls(), Figma's one row.
func TestEngineCheck(t *testing.T) {
	h, facts := site{plat: debian13(), ufw: allow22}.build(t)
	e := engine.New(h, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
	reps, err := e.Check(context.Background(), firewall.Controls())
	if err != nil {
		t.Fatal(err)
	}
	if len(reps) != 1 || reps[0].Control != "firewall.incoming" || reps[0].Title != "Firewall" || reps[0].Err != nil ||
		reps[0].Status.State != engine.Configured || reps[0].Status.Detail != "deny policy" {
		t.Fatalf("reports %+v", reps)
	}
	h, facts = site{plat: debian13(), failT: invalid, ufw: allow22}.build(t)
	e = engine.New(h, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
	if reps, _ = e.Check(context.Background(), firewall.Controls()); reps[0].Status.State != engine.Unknown || output.CodeOf(reps[0].Err) != "SSHD_CONFIG_INVALID" {
		t.Errorf("a failed read: %+v", reps[0])
	}
}

// TestCheckOnly: Plan is ErrNotMigrated, and a plan with the control is
// refused with it (CONTROL_NOT_MIGRATED), changing nothing.
func TestCheckOnly(t *testing.T) {
	h, facts := site{plat: debian13(), ufw: allow22}.build(t)
	cs, err := firewall.Incoming{}.Plan(context.Background(), h, engine.Input{Facts: facts})
	if !errors.Is(err, engine.ErrNotMigrated) || cs != nil {
		t.Errorf("Plan %v, %v", cs, err)
	}
	h.AddDir("/run", 0o755, 0, 0)
	e := engine.New(h, engine.Options{Detect: func(context.Context, host.System) (detect.Facts, error) { return facts, nil }})
	p, err := e.Plan(context.Background(), firewall.Controls(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Empty() || !errors.Is(p.Err(), engine.ErrNotMigrated) || len(p.Items) != 1 || output.CodeOf(p.Items[0].Err) != "CONTROL_NOT_MIGRATED" {
		t.Fatalf("plan: %d changes, items %+v", len(p.Changes), p.Items)
	}
	for _, c := range h.Calls() {
		if c.Op != "mkdir" && c.Op != "removeall" && c.Op != "lstat" && c.Op != "stat" {
			t.Errorf("plan: %v", c)
		}
	}
}
