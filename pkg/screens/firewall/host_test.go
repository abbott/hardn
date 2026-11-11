package firewall_test

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The new path's host: hostfake, ufw answering from a model that does what it
// is told, in the shape of pkg/engine/controls/firewall's plan_test.go (site,
// liveOn, platforms), which a test here cannot import. ufw's status and files
// are that model's, from ufw's sources, not captured from a host; the
// simulations are deb13-vm-2.log's form and pkg/host/alpine's tests' [I].

const (
	hardnBinary = "/usr/local/bin/hardn"
	hostName    = "host-01"
	aptSim      = "Reading package lists...\nThe following NEW packages will be installed:\n  ufw\nInst ufw (0.36.2-9 Debian:13.7/stable [all])\nConf ufw (0.36.2-9 Debian:13.7/stable [all])\n"
	apkSim      = "(1/1) Installing ufw (0.36.2-r0)\nOK: 9 MiB in 26 packages\n"
	apkNoSuch   = "ERROR: unable to select packages:\n  ufw (no such package):\n    required by: world[ufw]\n"
)

// fwHost is a host and ufw as a run has left them.
type fwHost struct {
	*hostfake.System
	facts  detect.Facts
	bin    string // the sshd the check runs
	socket bool   // ssh.socket listens, on a dual-stack [::]
	noSS   bool   // ss is not installed
	ports  []uint16

	missing     bool
	active      bool
	rules       []fwRule
	fail        string // the command that exits 1 and does nothing
	noIndex     bool   // apk's index holds no ufw (Alpine as shipped)
	inBoot      bool   // ufw's service starts at boot
	statusFails bool

	// The screen's states (helpers_test.go); each zero value is the parity
	// table's host.
	incoming  string             // ufw's default incoming policy: deny, allow or reject; "" is deny
	stale     bool               // ENABLED=yes while ufw is not running
	noIPv6    bool               // IPV6=no in ufw's defaults
	unit      string             // what systemctl is-enabled ufw.service prints, "" enabled; "fails" fails
	bound     []uint16           // the ports sshd is bound on, as ss shows them; nil is ports
	statusOut string             // what ufw status verbose prints, "" the model's
	ssFails   bool               // ss runs and fails
	sim       *hostfake.Response // apt-get's simulation, nil aptSim
	ssSim     *hostfake.Response // apt-get's simulation of iproute2's install, nil the stock one
	pveDown   bool               // pve-firewall's service stopped
	// tcp and udp are the lines ss -Htlnp and ss -Hulnp print besides
	// sshd's: the other listeners Enable may block.
	tcp, udp string

	fwd *fwdModel // firewalld on Rocky Linux, nil elsewhere
}

// fwdModel is firewalld on Rocky Linux 10 as the Firewall item reads and
// changes it, after pkg/engine/controls/firewall's el_test.go (elHost):
// installed or not, running or not, dnf's simulation without weak
// dependencies (rocky10-test-110, probe 3b-2), NetworkManager without zones,
// no other firewall.
type fwdModel struct {
	installed       bool
	active, enabled string // what systemctl is-active and is-enabled print
	masked          bool
	noRepos         bool // dnf has no enabled repository (an unregistered RHEL host)
}

// elSimulate is dnf's simulation of firewalld's install, el_test.go's.
const elSimulate = "Dependencies resolved.\n" +
	"================================================================================\n" +
	" Package                   Arch        Version                Repository   Size\n" +
	"================================================================================\n" +
	"Installing:\n firewalld                 noarch      2.4.3-4.el10_2         baseos      471 k\n" +
	"Installing dependencies:\n firewalld-filesystem      noarch      2.4.3-4.el10_2         baseos      7.7 k\n" +
	" nftables                  x86_64      1:1.1.5-6.el10_2       baseos      447 k\n" +
	"\nTransaction Summary\n================================================================================\nInstall  3 Packages\n"

// elNoRepos is dnf's line where it has no enabled repository
// (rhel10-test-114, el-rhel.md §5).
const elNoRepos = "Error: There are no enabled repositories in \"/etc/yum.repos.d\", \"/etc/yum/repos.d\", \"/etc/distro.repos.d\".\n"

// answer is firewalld's answer to cmd, and whether it is one of its
// commands: systemctl's of firewalld.service and of the other firewall
// units, rpm, dnf, firewall-offline-cmd, nmcli, and firewall-cmd while it
// runs.
func (m *fwdModel) answer(argv []string) (hostfake.Response, bool) {
	cmd := strings.Join(argv, " ")
	word := func(w string, ok bool) hostfake.Response {
		if ok {
			return hostfake.Response{Stdout: w + "\n"}
		}
		return hostfake.Response{Stdout: w + "\n", ExitCode: 3}
	}
	running := m.installed && m.active == "active"
	switch cmd {
	case "systemctl is-active firewalld.service":
		if !m.installed {
			return word("inactive", false), true
		}
		return word(m.active, m.active == "active"), true
	case "systemctl is-enabled firewalld.service":
		switch {
		case m.masked:
			return word("masked", false), true
		case !m.installed:
			return word("not-found", false), true
		}
		return word(m.enabled, m.enabled == "enabled"), true
	case "systemctl mask firewalld.service":
		m.masked = true
	case "systemctl unmask firewalld.service":
		m.masked = false
	case "systemctl start firewalld.service":
		m.active = "active"
	case "systemctl stop firewalld.service":
		m.active = "inactive"
	case "systemctl enable firewalld.service":
		m.enabled = "enabled"
	case "systemctl disable firewalld.service":
		m.enabled = "disabled"
	case "rpm -q ufw":
		return hostfake.Response{Stdout: "package ufw is not installed\n", ExitCode: 1}, true
	case "rpm -q firewalld":
		if m.installed {
			return hostfake.Response{Stdout: "firewalld-2.4.3-4.el10_2.noarch\n"}, true
		}
		return hostfake.Response{Stdout: "package firewalld is not installed\n", ExitCode: 1}, true
	case "dnf install --assumeno --setopt=install_weak_deps=False --setopt=exit_on_lock=True firewalld":
		if m.noRepos {
			return hostfake.Response{Stderr: elNoRepos, ExitCode: 1}, true
		}
		return hostfake.Response{Stdout: elSimulate, Stderr: "Operation aborted.\n", ExitCode: 1}, true
	case "dnf install -y --setopt=install_weak_deps=False --setopt=exit_on_lock=True firewalld":
		m.installed, m.active, m.enabled = true, "inactive", "enabled" // the preset
		if m.masked {
			m.enabled = "disabled"
		}
	case "dnf remove -y --noautoremove --setopt=exit_on_lock=True firewalld":
		m.installed = false
	case "firewall-offline-cmd --get-default-zone":
		return hostfake.Response{Stdout: "public\n"}, true
	case "firewall-offline-cmd --list-all-zones":
		return hostfake.Response{Stdout: "public (default)\n  target: default\n  interfaces: \n  sources: \n  services: cockpit dhcpv6-client ssh\n  ports: \n\n"}, true
	case "firewall-offline-cmd --service=ssh --get-ports":
		return hostfake.Response{Stdout: "22/tcp\n"}, true
	case "firewall-offline-cmd --service=cockpit --get-ports":
		return hostfake.Response{Stdout: "9090/tcp\n"}, true
	case "firewall-offline-cmd --service=dhcpv6-client --get-ports":
		return hostfake.Response{Stdout: "546/udp\n"}, true
	case "nmcli -t -f UUID,DEVICE connection show --active":
		return hostfake.Response{Stdout: "1dd9a779-d327-56e1-8454-c65e2556c12c:eth0\n"}, true
	case "nmcli -g connection.zone connection show 1dd9a779-d327-56e1-8454-c65e2556c12c":
		return hostfake.Response{Stdout: "\n"}, true
	case "firewall-cmd --state":
		if running {
			return hostfake.Response{Stdout: "running\n"}, true
		}
		return hostfake.Response{Stdout: "not running\n", ExitCode: 252}, true
	case "firewall-cmd --get-default-zone":
		return hostfake.Response{Stdout: "public\n"}, running
	case "firewall-cmd --get-active-zones":
		return hostfake.Response{Stdout: "public (default)\n  interfaces: eth0\n"}, running
	case "firewall-cmd --permanent --zone=public --get-target":
		return hostfake.Response{Stdout: "default\n"}, running
	default:
		if len(argv) == 3 && argv[0] == "systemctl" && (argv[1] == "is-active" || argv[1] == "is-enabled") {
			if argv[1] == "is-active" {
				return word("inactive", false), true
			}
			return hostfake.Response{Stdout: "not-found\n", ExitCode: 4}, true
		}
		return hostfake.Response{}, false
	}
	return hostfake.Response{}, true
}

// fwRule is a rule in ufw: what ufw's status prints it for, and the words a
// run's end state gives it.
type fwRule struct {
	port, app string
	iface     string // a trusted interface's rule, ufw allow in on <iface>: every port
	start     bool   // a rule of the start
	// action is allow where ""; src is the source, anywhere where "", one IP
	// version's alone where set.
	action, src string
}

// verb is the rule's action.
func (r fwRule) verb() string {
	if r.action == "" {
		return "allow"
	}
	return r.action
}

// in reports whether the rule applies in the IP version v6 says.
func (r fwRule) in(v6 bool) bool { return r.src == "" || strings.Contains(r.src, ":") == v6 }

func (r fwRule) words() string {
	switch {
	case r.start:
		return "the start's " + r.port + "/tcp"
	case r.app != "":
		return "app " + r.app
	case r.iface != "":
		return "in on " + r.iface
	}
	return r.port + "/tcp"
}

// platformFacts are d's as pkg/engine/controls/firewall's tests give their platforms.
func platformFacts(d string) (f detect.Facts, bin string, socket bool) {
	f = detect.Facts{
		OS:       detect.OSRelease{ID: "debian", VersionID: "13", VersionCodename: "trixie"},
		Platform: detect.Platform{Distro: detect.DistroDebian, Version: "13", Support: detect.Supported},
		Init:     detect.Init{Kind: detect.Systemd, Running: detect.Yes, Source: detect.SystemdRunDir},
		SSH: detect.SSH{Mode: detect.SSHService, Generator: detect.No,
			Socket:  detect.UnitState{Enabled: "disabled", Active: "inactive"},
			Service: detect.UnitState{Enabled: "enabled", Active: "active"}},
		Proxmox:   detect.Proxmox{Installed: detect.No, ClusterFS: detect.No, EtcPVE: detect.No},
		Container: detect.Container{Detected: detect.No},
	}
	bin = "sshd"
	switch d {
	case "ubuntu":
		socket = true
		f.OS = detect.OSRelease{ID: "ubuntu", VersionID: "26.04", VersionCodename: "resolute"}
		f.Platform = detect.Platform{Distro: detect.DistroUbuntu, Version: "26.04", Support: detect.Supported}
		f.SSH = detect.SSH{Mode: detect.SSHSocket, Generator: detect.Yes, GeneratorPath: detect.SocketGenerator,
			Socket:  detect.UnitState{Enabled: "enabled", Active: "active"},
			Service: detect.UnitState{Enabled: "disabled", Active: "active"}}
	case "alpine":
		bin = "/usr/sbin/sshd"
		f.OS = detect.OSRelease{ID: "alpine", VersionID: "3.23.0"}
		f.Platform = detect.Platform{Distro: detect.DistroAlpine, Version: "3.23", Support: detect.Supported}
		f.Init = detect.Init{Kind: detect.OpenRC, Running: detect.Yes}
		f.SSH = detect.SSH{Mode: detect.SSHOpenRC, OpenRC: detect.OpenRCService{Started: detect.Yes, InRunlevel: detect.Yes}}
	case "rocky":
		f.OS = detect.OSRelease{ID: "rocky", IDLike: []string{"rhel", "centos", "fedora"}, VersionID: "10.2"}
		f.Platform = detect.Platform{Distro: detect.DistroRocky, Version: "10", Support: detect.Supported}
		f.SSH.SSHDUnits = true
	case "proxmox":
		f.Platform = detect.Platform{Distro: detect.DistroProxmox, Version: "9", Support: detect.Supported}
		f.Proxmox = detect.Proxmox{Installed: detect.Yes, Version: "9.0.10", ClusterFS: detect.Yes, EtcPVE: detect.Yes}
	}
	return f, bin, socket
}

// newHost builds st on the distribution named d.
func newHost(t *testing.T, d string, st start) *fwHost {
	t.Helper()
	f, bin, socket := platformFacts(d)
	h := &fwHost{System: hostfake.New(host.Options{}), facts: f, bin: bin, socket: socket, noSS: st.noSS, ports: st.sshdPorts(),
		missing: st.missing, active: st.active, fail: st.fails, noIndex: st.noIndex, statusFails: st.statusFails, inBoot: f.Init.Kind != detect.OpenRC}
	if st.ruleOn22 {
		h.rules = []fwRule{{port: "22", start: true}}
	}
	for _, dir := range []string{"/var/lib", "/var/backups", "/run", "/var/log", "/usr/local/bin", "/etc/ufw/applications.d", "/usr/lib/systemd/system", "/etc/systemd/system", "/etc/init.d", "/etc/runlevels/default", sshd.DropInDir, "/etc/hardn"} {
		h.AddDir(dir, 0o755, 0, 0)
	}
	h.AddFile(hardnBinary, []byte("ELF"), 0o755, 0, 0)
	h.AddFile(sshd.MainConfig, []byte("Include /etc/ssh/sshd_config.d/*.conf\n\nUsePAM yes\n"), 0o644, 0, 0)
	var conf strings.Builder
	for _, p := range h.ports {
		fmt.Fprintf(&conf, "Port %d\n", p)
	}
	h.AddFile(sshd.DropInDir+"/00-hardn.conf", []byte(conf.String()), 0o644, 0, 0)
	if f.Init.Kind == detect.OpenRC {
		h.AddFile("/etc/init.d/ufw", []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	}
	if f.Platform.Distro.EL() {
		h.AddDir("/etc/sysconfig", 0o755, 0, 0)
		h.AddFile(reload.SysconfigSSHD, []byte("OPTIONS=\"\"\n"), 0o644, 0, 0)
		// The package's unit, which tells a masked firewalld from a mask of
		// none (firewalld.UnitFile); read only where is-enabled says masked.
		h.AddFile(firewalld.UnitFile, []byte("[Unit]\n"), 0o644, 0, 0)
		h.fwd = &fwdModel{}
	}
	if !h.missing {
		h.syncFiles()
	}
	h.Handler = func(argv []string, _ []byte) hostfake.Response { return h.answer(t, argv) }
	return h
}

// session is alice's connection to the port sshd is bound on first.
func (h *fwHost) session() revert.Session {
	return revert.Session{Remote: true, Conn: fmt.Sprintf("192.0.2.10 50022 192.0.2.20 %d", h.ports[0]), Source: "SSH_CONNECTION"}
}

// syncFiles writes ufw's settings and saved rules as the model has them.
func (h *fwHost) syncFiles() {
	enabled := map[bool]string{true: "yes", false: "no"}[h.active || h.stale]
	ipv6 := map[bool]string{true: "no", false: "yes"}[h.noIPv6]
	policy := map[string]string{"": "DROP", "deny": "DROP", "allow": "ACCEPT", "reject": "REJECT"}[h.incoming]
	h.AddFile(ufw.DefaultsFile, []byte("IPV6="+ipv6+"\nDEFAULT_INPUT_POLICY=\""+policy+"\"\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n"), 0o644, 0, 0)
	h.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, f := range []struct {
		path, any string
		v6        bool
	}{{ufw.UserRules, "0.0.0.0/0", false}, {ufw.User6Rules, "::/0", true}} {
		var b strings.Builder
		b.WriteString("*filter\n### RULES ###\n")
		for _, r := range h.rules {
			src := f.any
			switch {
			case !r.in(f.v6):
				continue
			case r.src != "":
				src = r.src
			}
			switch {
			case r.iface != "":
				fmt.Fprintf(&b, "\n### tuple ### allow any any %s any %s in_%s\n", f.any, src, r.iface)
			case r.app != "":
				fmt.Fprintf(&b, "\n### tuple ### allow tcp %s %s any %s %s - in\n", r.port, f.any, src, r.app)
			default:
				fmt.Fprintf(&b, "\n### tuple ### %s tcp %s %s any %s in\n", r.verb(), r.port, f.any, src)
			}
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		h.AddFile(f.path, []byte(b.String()), 0o640, 0, 0)
	}
	h.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
}

// status is ufw status verbose's output, as get_status prints it.
func (h *fwHost) status() string {
	if !h.active {
		return "Status: inactive\n"
	}
	var b strings.Builder
	incoming := h.incoming
	if incoming == "" {
		incoming = "deny"
	}
	b.WriteString("Status: active\nLogging: on (low)\nDefault: " + incoming + " (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
	if len(h.rules) > 0 {
		fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	}
	for _, v6 := range []string{"", " (v6)"} {
		for _, r := range h.rules {
			if !r.in(v6 != "") || v6 != "" && h.noIPv6 {
				continue
			}
			to, from := r.port+"/tcp"+v6, "Anywhere"+v6
			switch {
			case r.iface != "":
				to = "Anywhere" + v6 + " on " + r.iface
			case r.app != "":
				to = r.port + "/tcp (" + r.app + v6 + ")"
			case r.src != "":
				to, from = r.port+"/tcp", r.src
			}
			fmt.Fprintf(&b, "%-26s %-12s%-26s\n", to, strings.ToUpper(r.verb())+" IN", from)
		}
	}
	return b.String()
}

// listeners are sshd's: every port on [::] and 0.0.0.0.
func (h *fwHost) listeners() []string { return listenersOn(h.ports) }

// listenersOn are every port of ports on [::] and 0.0.0.0.
func listenersOn(ports []uint16) []string {
	var ls []string
	for _, p := range ports {
		ls = append(ls, fmt.Sprintf("[::]:%d", p), fmt.Sprintf("0.0.0.0:%d", p))
	}
	return ls
}

// sshdT is sshd -T's output (sshd_config(5)).
func (h *fwHost) sshdT() string {
	var b strings.Builder
	for _, p := range h.ports {
		fmt.Fprintf(&b, "port %d\n", p)
	}
	for _, l := range h.listeners() {
		b.WriteString("listenaddress " + l + "\n")
	}
	b.WriteString("permitrootlogin without-password\npubkeyauthentication yes\npasswordauthentication yes\nusepam yes\n")
	return b.String()
}

// ss is ss -Htln's output; on a socket host [::] is dual-stack, printed "*".
func (h *fwHost) ss() string {
	var b strings.Builder
	bound := h.listeners()
	if h.bound != nil {
		bound = listenersOn(h.bound)
	}
	for _, s := range bound {
		peer := "0.0.0.0:*"
		if strings.HasPrefix(s, "[") {
			peer = "[::]:*"
		}
		if h.socket {
			if !strings.HasPrefix(s, "[") {
				continue
			}
			s, peer = "*:"+strings.TrimPrefix(s, "[::]:"), "*:*"
		}
		fmt.Fprintf(&b, "LISTEN 0      4096   %20s   %s\n", s, peer)
	}
	return b.String()
}

// answer is the host's answer to argv. A command it does not know fails the
// test, so that nothing the new path runs goes unread.
func (h *fwHost) answer(t *testing.T, argv []string) hostfake.Response {
	cmd := strings.Join(argv, " ")
	defer func() {
		if argv[0] == "ufw" && !h.missing {
			h.syncFiles()
		}
	}()
	if h.fwd != nil {
		if r, ok := h.fwd.answer(argv); ok {
			return r
		}
	}
	switch {
	case cmd == h.fail && cmd != "":
		return hostfake.Response{Stderr: "ERROR: failed\n", ExitCode: 1}
	case argv[0] == "ufw" && h.missing:
		return hostfake.Response{Err: fmt.Errorf("host: run ufw: %w", host.ErrNotFound)}
	case cmd == "ufw status verbose" && h.statusFails:
		return hostfake.Response{Stderr: "ERROR: failed\n", ExitCode: 1}
	case cmd == "ufw status verbose" && h.statusOut != "":
		return hostfake.Response{Stdout: h.statusOut}
	case len(argv) == 4 && argv[1] == "default" && argv[3] == "incoming":
		h.incoming = argv[2]
		return hostfake.Response{Stdout: fmt.Sprintf("Default incoming policy changed to '%s'\n(be sure to update your rules accordingly)\n", argv[2])}
	case cmd == "ufw status verbose":
		return hostfake.Response{Stdout: h.status()}
	case cmd == "ufw --force enable":
		h.active = true
		return hostfake.Response{Stdout: "Firewall is active and enabled on system startup\n"}
	case cmd == "ufw disable":
		h.active, h.stale = false, false
		return hostfake.Response{Stdout: "Firewall stopped and disabled on system startup\n"}
	case len(argv) == 3 && argv[1] == "allow":
		h.rules = append(h.rules, fwRule{port: strings.TrimSuffix(argv[2], "/tcp")})
		return hostfake.Response{Stdout: "Rule added\nRule added (v6)\n"}
	case len(argv) == 4 && argv[1] == "delete":
		port := strings.TrimSuffix(argv[3], "/tcp")
		h.rules = slices.DeleteFunc(h.rules, func(r fwRule) bool { return r.port == port && r.app == "" && !r.start })
		return hostfake.Response{Stdout: "Rule deleted\nRule deleted (v6)\n"}
	case len(argv) == 5 && argv[1] == "allow" && argv[2] == "in" && argv[3] == "on": // a trusted interface
		h.rules = append(h.rules, fwRule{iface: argv[4]})
		return hostfake.Response{Stdout: "Rule added\nRule added (v6)\n"}
	case len(argv) == 8 && argv[1] == "allow" && argv[6] == "app":
		port := h.appPorts(argv[7])
		if port == "" {
			return hostfake.Response{Stderr: "ERROR: Could not find a profile matching '" + argv[7] + "'\n", ExitCode: 1}
		}
		h.rules = append(h.rules, fwRule{port: port, app: argv[7]})
		return hostfake.Response{Stdout: "Rule added\nRule added (v6)\n"}
	case argv[0] == h.bin && slices.Contains(argv, "-T"):
		return hostfake.Response{Stdout: h.sshdT()}
	case cmd == "systemctl show -p Listen ssh.socket":
		var b strings.Builder
		for _, p := range h.ports {
			fmt.Fprintf(&b, "Listen=[::]:%d (Stream)\n", p)
		}
		return hostfake.Response{Stdout: b.String()}
	case argv[0] == "ss" && h.noSS:
		return hostfake.Response{Err: fmt.Errorf("host: run ss: %w", host.ErrNotFound)}
	case argv[0] == "ss" && h.ssFails:
		return hostfake.Response{Stderr: "ss: netlink error\n", ExitCode: 1}
	case cmd == "ss -Htln":
		return hostfake.Response{Stdout: h.ss()}
	case cmd == "ss -Htlnp": // sshd's sockets, then the other listeners Enable may block
		return hostfake.Response{Stdout: h.ss() + h.tcp}
	case cmd == "ss -Hulnp":
		return hostfake.Response{Stdout: h.udp}
	case cmd == "systemctl is-enabled ufw.service" && h.missing:
		return hostfake.Response{Stdout: "not-found\n", ExitCode: 1}
	case cmd == "systemctl is-enabled ufw.service" && h.unit == "fails":
		return hostfake.Response{Stderr: "failed\n", ExitCode: 2}
	case cmd == "systemctl is-enabled ufw.service" && h.unit != "" && h.unit != "enabled":
		return hostfake.Response{Stdout: h.unit + "\n", ExitCode: 1}
	case cmd == "systemctl is-enabled ufw.service":
		return hostfake.Response{Stdout: "enabled\n"}
	case cmd == "systemctl enable ufw.service":
		h.unit = "enabled"
		return hostfake.Response{}
	case cmd == "systemctl disable ufw.service":
		h.unit = "disabled"
		return hostfake.Response{}
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == "iproute2" && h.noSS: // ss's package, as ss is missing or not
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching iproute2\n", ExitCode: 1}
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == "iproute2":
		return hostfake.Response{Stdout: "install ok installed\t6.15.0-1\n"}
	case cmd == "apk info -e iproute2" && h.noSS:
		return hostfake.Response{ExitCode: 1}
	case cmd == "apk info -e iproute2":
		return hostfake.Response{Stdout: "iproute2\n"}
	case cmd == "apt-get install -s -y iproute2" && h.ssSim != nil:
		return *h.ssSim
	case cmd == "apt-get install -s -y iproute2":
		return hostfake.Response{Stdout: "Reading package lists...\nThe following NEW packages will be installed:\n  iproute2\nInst iproute2 (6.15.0-1 Debian:13.7/stable [amd64])\nConf iproute2 (6.15.0-1 Debian:13.7/stable [amd64])\n"}
	case cmd == "apk add --simulate iproute2":
		return hostfake.Response{Stdout: "(1/1) Installing iproute2 (6.15.0-r0)\nOK: 12 MiB in 30 packages\n"}
	case cmd == "apt-get install -y iproute2", cmd == "apk add iproute2":
		h.noSS = false
		return hostfake.Response{Stdout: "iproute2 installed\n"}
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == "ufw" && h.missing:
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching ufw\n", ExitCode: 1}
	case argv[0] == "dpkg-query" && argv[len(argv)-1] == "ufw":
		return hostfake.Response{Stdout: "install ok installed\t0.36.2-9\n"}
	case cmd == "apk info -e ufw" && h.missing:
		return hostfake.Response{ExitCode: 1}
	case cmd == "apk info -e ufw":
		return hostfake.Response{Stdout: "ufw\n"}
	case cmd == "apt-get install -s -y ufw" && h.sim != nil:
		return *h.sim
	case cmd == "apt-get install -s -y ufw":
		return hostfake.Response{Stdout: aptSim}
	case cmd == "apk add --simulate ufw" && h.noIndex:
		return hostfake.Response{Stderr: apkNoSuch, ExitCode: 1}
	case cmd == "apk add --simulate ufw":
		return hostfake.Response{Stdout: apkSim}
	case cmd == "apt-get install -y ufw", cmd == "apk add ufw":
		h.missing = false
		h.syncFiles()
		return hostfake.Response{Stdout: "ufw installed\n"}
	case cmd == "apt-get remove -y ufw", cmd == "apk del ufw":
		h.missing = true
		return hostfake.Response{}
	case cmd == "rc-update add ufw default":
		h.AddFile(detect.OpenRCRunlevels+"/default/ufw", nil, 0o644, 0, 0)
		h.inBoot = true
		return hostfake.Response{}
	case cmd == "rc-update del ufw default":
		_ = h.Remove(context.Background(), detect.OpenRCRunlevels+"/default/ufw")
		h.inBoot = false
		return hostfake.Response{}
	case cmd == "systemctl --no-reload enable hardn-boot.service":
		h.AddSymlink("/etc/systemd/system/multi-user.target.wants/hardn-boot.service", "/etc/systemd/system/hardn-boot.service")
		return hostfake.Response{}
	case cmd == "rc-update add hardn-boot default":
		h.AddSymlink(detect.OpenRCRunlevels+"/default/hardn-boot", "/etc/init.d/hardn-boot")
		return hostfake.Response{}
	case cmd == "systemctl is-active "+pve.FirewallUnit && h.pveDown:
		return hostfake.Response{Stdout: "inactive\n", ExitCode: 3}
	case cmd == "systemctl is-active "+pve.FirewallUnit:
		return hostfake.Response{Stdout: "active\n"}
	case timerCommand(host.FormatArgv(argv)):
		return hostfake.Response{}
	}
	t.Errorf("unexpected command %q", cmd)
	return hostfake.Response{Err: hostfake.ErrUnscripted, ExitCode: -1}
}

// appPorts are application app's ports in hardn's profile file, comma-joined as
// ufw's status prints several of one protocol, without it ("80,443"); "" for no
// such application, or no file. The profile's protocol is tcp in every fixture.
func (h *fwHost) appPorts(app string) string {
	data, err := h.ReadFile(context.Background(), ufw.ProfileFile)
	if err != nil {
		return ""
	}
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "[") {
			in = line == "["+app+"]"
		}
		if v, ok := strings.CutPrefix(line, "ports="); ok && in {
			return strings.ReplaceAll(strings.ReplaceAll(v, "/tcp", ""), "|", ",")
		}
	}
	return ""
}

// timerForm is each command of the revert timer and the boot unit a lockout-risk
// plan arms, which the engine runs for any such plan, as host.FormatArgv gives
// it, and no other.
var timerForm = regexp.MustCompile(`^(?:` + strings.Join([]string{
	`systemd-run --unit=` + revert.UnitPrefix + `\S+ .* ` + hardnBinary + ` rollback \S+`,
	`systemctl (?:stop|is-active) ` + revert.UnitPrefix + `\S+`,
	`systemctl list-timers --all --output=json --no-pager ` + revert.UnitPrefix + `\S+`,
	`/bin/sh -c ` + regexp.QuoteMeta(host.FormatArgv([]string{revert.SleeperScript})) + ` ` + hardnBinary + ` \d+ \S+`,
	`systemd-analyze verify --recursive-errors=no \S*/` + revert.BootUnitName + `\.service`,
	`sh -n \S*/` + revert.BootUnitName,
	`systemctl --no-reload (?:enable|disable) ` + revert.BootUnitName + `\.service`,
	`rc-update (?:add|del) ` + revert.BootUnitName + ` default`,
	`systemctl daemon-reload`,
}, "|") + `)$`)

// timerCommand: cmd, as host.FormatArgv gives it, is the timer's or the boot
// unit's; the row "asks" says them, and the boot unit's files are in "files".
func timerCommand(cmd string) bool { return timerForm.MatchString(cmd) }
