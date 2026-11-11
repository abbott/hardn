package main

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The Firewall screen's hosts (firewall.go; slice 4, since unit (a)). fakeHost.ufw
// answers ufw's status with a deny policy and port rules from anywhere, and
// its enable and a port's allow: enough for the engine's scenarios. The
// screen needs more of ufw 0.36.2, so ufwSim answers ufw's commands on these
// hosts in its place, adding to the fake and changing none of it: another
// default policy, a rule from some sources (status rows as
// pkg/host/ufw's parser and its tests' model shape them; ufw.md §3),
// "ufw default" and "ufw disable" (ufw(8); the words are ufw's own,
// frontend.py), an application's rule (ufw-framework(8)), and the packages
// that bring ufw (apt-get, apk add, ufw.md §3 for Alpine's community
// package; the simulations below). Synthetic values only: 192.0.2.0/24, 198.51.100.0/24,
// 2001:db8::/32, alice, port 2230.

// simRule is one saved rule of ufwSim: action on port/tcp, from anywhere
// in both IP versions, or from src in src's alone; app names the
// application profile it came from; iface the interface it is limited to,
// with no port everything arriving on it ("ufw allow in on <iface>").
type simRule struct{ action, port, src, app, proto, iface string }

// protocol is the rule's, tcp unless it says.
func (r simRule) protocol() string {
	if r.proto != "" {
		return r.proto
	}
	return "tcp"
}

// ufwSim is ufw on one host.
type ufwSim struct {
	h        *fakeHost
	incoming string // deny, allow or reject
	active   bool   // its chains loaded
	enabled  bool   // ENABLED=yes
	noIPv6   bool   // IPV6=no in /etc/default/ufw
	unitOff  bool   // ufw.service disabled, on systemd
	rules    []simRule
	// sim answers the package tool's simulation, where it is not the usual one
	// (fwAPTInstalls, fwAPKInstalls).
	sim *hostfake.Response
}

// The package tools' simulations. No VM has simulated a new ufw: fwAPTInstalls
// is apt's form for another package (deb13-vm-2.log's nftables), its one package
// [I]; fwAPKInstalls is apk's as pkg/host/alpine's tests have it, [I]: the Alpine
// VM has no DNS. fwAPTNotFound is deb13-vm.log's for a package with no such
// name, fwAPTRemoves deb13-vm-2.log's Remv line, in ufw's place, and fwAPKNoIndex
// alpine323-vm.log's, as a stock Alpine says it.
var (
	fwAPTInstalls = hostfake.Response{Stdout: "Reading package lists...\nBuilding dependency tree...\nReading state information...\nThe following NEW packages will be installed:\n  ufw\n" +
		"0 upgraded, 1 newly installed, 0 to remove and 1 not upgraded.\nInst ufw (0.36.2-9 Debian:13.7/stable [all])\nConf ufw (0.36.2-9 Debian:13.7/stable [all])\n"}
	fwAPKInstalls = hostfake.Response{Stdout: "(1/1) Installing ufw (0.36.2-r0)\nOK: 9 MiB in 26 packages\n"}
	fwAPTNotFound = hostfake.Response{Stdout: "Reading package lists...\nBuilding dependency tree...\nReading state information...\n", Stderr: "E: Unable to locate package ufw\n", ExitCode: 100}
	fwAPTRemoves  = hostfake.Response{Stdout: "Remv nftables [1.1.3-1]\nInst ufw (0.36.2-9 Debian:13.7/stable [all])\nConf ufw (0.36.2-9 Debian:13.7/stable [all])\n"}
	fwAPKNoIndex  = hostfake.Response{Stderr: "WARNING: opening from cache https://dl-cdn.alpinelinux.org/alpine/v3.23/main/x86_64/APKINDEX.tar.gz: No such file or directory\n" +
		"WARNING: opening from cache https://dl-cdn.alpinelinux.org/alpine/v3.23/community/x86_64/APKINDEX.tar.gz: No such file or directory\n" +
		"ERROR: unable to select packages:\n  ufw (no such package):\n    required by: world[ufw]\n", ExitCode: 1}
)

// simulation is the package tool's simulation on this host.
func (s *ufwSim) simulation() hostfake.Response {
	switch {
	case s.sim != nil:
		return *s.sim
	case s.h.openRC:
		return fwAPKInstalls
	}
	return fwAPTInstalls
}

// simulateUFW has sim answer ufw's commands on h, the rest left to h, and
// writes ufw's files as sim holds them, where ufw is installed.
func simulateUFW(h *fakeHost, sim ufwSim) *fakeHost {
	sim.h = h
	s := &sim
	h.f.Handler = s.run
	if !h.noUFW {
		s.save()
	}
	return h
}

// ufwOn gives h, as its own model left ufw, to ufwSim: its port rules, and
// whether ufw runs and starts at boot as ufw.conf says.
func ufwOn(h *fakeHost) *fakeHost {
	sim := ufwSim{incoming: "deny", active: !h.inactive && !h.noUFW}
	for _, p := range h.allowed {
		sim.rules = append(sim.rules, simRule{action: "allow", port: fmt.Sprint(p)})
	}
	if !h.noUFW {
		data, err := h.f.ReadFile(context.Background(), ufw.ConfFile)
		sim.enabled = err == nil && strings.Contains(string(data), "ENABLED=yes")
	}
	return simulateUFW(h, sim)
}

// v4, v6: the rule applies in that IP version.
func (r simRule) v4() bool { return !strings.Contains(r.src, ":") }
func (r simRule) v6() bool { return r.src == "" || strings.Contains(r.src, ":") }

// status is ufw status verbose in the forms pkg/host/ufw's parser test models:
// an application's rule "<port>/tcp (<app>)", and for IPv6
// "<port>/tcp (<app> (v6))".
func (s *ufwSim) status() string {
	if !s.active {
		return "Status: inactive\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Status: active\nLogging: on (low)\nDefault: %s (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n", s.incoming)
	if len(s.rules) > 0 {
		fmt.Fprintf(&b, "\n%-26s %-12s%s\n%-26s %-12s%s\n", "To", "Action", "From", "--", "------", "----")
	}
	row := func(r simRule, v6 bool) {
		to, from := r.port+"/"+r.protocol(), r.src
		switch {
		case r.port == "" && v6:
			to = "Anywhere (v6)"
		case r.port == "":
			to = "Anywhere"
		case r.app != "" && v6:
			to += " (" + r.app + " (v6))"
		case r.app != "":
			to += " (" + r.app + ")"
		case v6 && r.src == "":
			to += " (v6)"
		}
		if r.iface != "" {
			to += " on " + r.iface
		}
		if r.src == "" {
			from = "Anywhere"
			if v6 {
				from = "Anywhere (v6)"
			}
		}
		fmt.Fprintf(&b, "%-26s %-12s%s\n", to, strings.ToUpper(r.action)+" IN", from)
	}
	for _, r := range s.rules {
		if r.v4() {
			row(r, false)
		}
	}
	for _, r := range s.rules {
		if r.v6() && !s.noIPv6 {
			row(r, true)
		}
	}
	return b.String()
}

// save writes ufw's defaults, its configuration and its saved rules as ufw
// keeps them (saveRules' forms; an application in the tuple's ninth-field
// form, pkg/host/ufw ParseTuples).
func (s *ufwSim) save() {
	f := s.h.f
	word := map[string]string{"deny": "DROP", "allow": "ACCEPT", "reject": "REJECT"}[s.incoming]
	f.AddDir(ufw.KernelIPv6, 0o555, 0, 0)
	ipv6 := "yes"
	if s.noIPv6 {
		ipv6 = "no"
	}
	f.AddFile(ufw.DefaultsFile, []byte(fmt.Sprintf("IPV6=%s\nDEFAULT_INPUT_POLICY=%q\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n", ipv6, word)), 0o644, 0, 0)
	enabled := "no"
	if s.enabled {
		enabled = "yes"
	}
	f.AddFile(ufw.ConfFile, []byte("ENABLED="+enabled+"\nLOGLEVEL=low\n"), 0o644, 0, 0)
	for _, list := range []struct {
		path, any string
		in        func(simRule) bool
	}{{ufw.UserRules, "0.0.0.0/0", simRule.v4}, {ufw.User6Rules, "::/0", simRule.v6}} {
		var b strings.Builder
		b.WriteString("*filter\n:ufw-user-input - [0:0]\n### RULES ###\n")
		for _, r := range s.rules {
			if !list.in(r) {
				continue
			}
			src := r.src
			if src == "" {
				src = list.any
			}
			dir := "in"
			if r.iface != "" {
				dir += "_" + r.iface
			}
			switch {
			case r.app != "":
				fmt.Fprintf(&b, "\n### tuple ### %s %s %s %s any %s %s - %s\n", r.action, r.protocol(), r.port, list.any, src, r.app, dir)
			case r.port == "":
				fmt.Fprintf(&b, "\n### tuple ### %s any any %s any %s %s\n", r.action, list.any, src, dir)
			default:
				fmt.Fprintf(&b, "\n### tuple ### %s %s %s %s any %s %s\n", r.action, r.protocol(), r.port, list.any, src, dir)
			}
		}
		b.WriteString("\n### END RULES ###\nCOMMIT\n")
		f.AddFile(list.path, []byte(b.String()), 0o640, 0, 0)
	}
}

// run answers what the model here adds, and leaves the rest to the host:
// ufw's commands where ufw is installed, the packages that bring it, and
// its runlevel on OpenRC.
func (s *ufwSim) run(argv []string, in []byte) hostfake.Response {
	cmd := strings.Join(argv, " ")
	switch base := path.Base(argv[0]); {
	case base == "ufw" && !s.h.noUFW:
		if r, ok := s.ufw(argv); ok {
			return r
		}
	case base == "ufw":
		return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}
	case base == "dpkg-query" && argv[len(argv)-1] == "ufw" && !s.h.openRC && s.h.noUFW: // deb13-vm.log's forms
		return hostfake.Response{Stderr: "dpkg-query: no packages found matching ufw\n", ExitCode: 1}
	case base == "dpkg-query" && argv[len(argv)-1] == "ufw" && !s.h.openRC:
		return hostfake.Response{Stdout: "install ok installed\t0.36.2-9\n"}
	case cmd == "apk info -e ufw" && s.h.openRC && s.h.noUFW: // alpine323-vm.log
		return hostfake.Response{ExitCode: 1}
	case cmd == "apk info -e ufw" && s.h.openRC: // alpine323-vm-2.log's form, for python3
		return hostfake.Response{Stdout: "ufw\n"}
	case cmd == "apt-get install -s -y ufw" && !s.h.openRC, cmd == "apk add --simulate ufw" && s.h.openRC:
		return s.simulation()
	case cmd == "apt-get install -y ufw" && !s.h.openRC, cmd == "apk add ufw" && s.h.openRC:
		s.install()
		return hostfake.Response{Stdout: "ufw installed\n"}
	case cmd == "apt-get remove -y ufw" && !s.h.openRC, cmd == "apk del ufw" && s.h.openRC:
		s.h.noUFW = true
		return hostfake.Response{}
	case cmd == "rc-update add ufw default" && s.h.openRC:
		s.h.f.AddSymlink(revert.OpenRCDefault+"/ufw", revert.OpenRCInitDir+"/ufw")
		return hostfake.Response{Stdout: " * service ufw added to runlevel default\n"}
	case cmd == "rc-update del ufw default" && s.h.openRC:
		_ = s.h.f.RemoveAll(context.Background(), revert.OpenRCDefault+"/ufw")
		return hostfake.Response{}
	case cmd == "systemctl is-enabled ufw.service" && !s.unitOff:
		return hostfake.Response{Stdout: "enabled\n"}
	case cmd == "systemctl is-enabled ufw.service":
		return hostfake.Response{Stdout: "disabled\n", ExitCode: 1}
	case cmd == "systemctl enable ufw.service":
		s.unitOff = false
		return hostfake.Response{}
	case cmd == "systemctl disable ufw.service":
		s.unitOff = true
		return hostfake.Response{}
	}
	return s.h.run(argv, in)
}

// install is ufw as its package leaves it: inactive, ENABLED=no, no rule,
// deny incoming (ufw.md §3; Ubuntu's stock state, vmtest.md §4.9, taken for
// Debian's and Alpine's, [I]: neither installed here), and on OpenRC its
// init script, in no runlevel (ufw.md §6).
func (s *ufwSim) install() {
	s.h.noUFW, s.incoming, s.active, s.enabled, s.rules = false, "deny", false, false, nil
	s.save()
	if s.h.openRC {
		s.h.f.AddFile(revert.OpenRCInitDir+"/ufw", []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	}
}

// ufw answers ufw's commands, and reports whether it did. A port's allow says
// "Rule added", or "Rules updated" while inactive, as fakeHost.ufw does
// (vmtest.md §4.9); a delete "Rule deleted" (frontend.py as remembered, not
// run), reached only on a rollback.
func (s *ufwSim) ufw(argv []string) (hostfake.Response, bool) {
	cmd := strings.Join(argv[1:], " ")
	word := "Rule added"
	if !s.active {
		word = "Rules updated"
	}
	both := word + "\n" + word + " (v6)\n"
	gone := "Rule deleted\nRule deleted (v6)\n"
	if !s.active {
		gone = both
	}
	switch cmd {
	case "status verbose":
		return hostfake.Response{Stdout: s.status()}, true
	case "--force enable":
		s.active, s.enabled = true, true
		s.save()
		return hostfake.Response{Stdout: "Firewall is active and enabled on system startup\n"}, true
	case "disable":
		s.active, s.enabled = false, false
		s.save()
		return hostfake.Response{Stdout: "Firewall stopped and disabled on system startup\n"}, true
	}
	switch {
	case len(argv) == 4 && argv[1] == "default" && argv[3] == "incoming":
		s.incoming = argv[2]
		s.save()
		return hostfake.Response{Stdout: fmt.Sprintf("Default incoming policy changed to '%s'\n(be sure to update your rules accordingly)\n", argv[2])}, true
	case len(argv) == 3 && argv[1] == "allow" && strings.HasSuffix(argv[2], "/tcp"):
		s.rules = append(s.rules, simRule{action: "allow", port: strings.TrimSuffix(argv[2], "/tcp")})
		s.save()
		return hostfake.Response{Stdout: both}, true
	case len(argv) == 4 && argv[1] == "delete" && argv[2] == "allow":
		port := strings.TrimSuffix(argv[3], "/tcp")
		s.rules = slices.DeleteFunc(s.rules, func(r simRule) bool { return r.action == "allow" && r.port == port && r.src == "" && r.app == "" })
		s.save()
		return hostfake.Response{Stdout: gone}, true
	case strings.HasPrefix(cmd, "delete allow from any to any app "):
		app := argv[len(argv)-1]
		s.rules = slices.DeleteFunc(s.rules, func(r simRule) bool { return r.app == app })
		s.save()
		return hostfake.Response{Stdout: gone}, true
	case strings.HasPrefix(cmd, "allow from any to any app "):
		app := argv[len(argv)-1]
		rules := s.profileRules(app)
		if len(rules) == 0 {
			return hostfake.Response{Stderr: "ERROR: Could not find a profile matching '" + app + "'\n", ExitCode: 1}, true
		}
		s.rules = append(s.rules, rules...)
		s.save()
		return hostfake.Response{Stdout: both}, true
	case len(argv) == 11 && argv[1] == "allow" && argv[2] == "in" && argv[3] == "on" && argv[9] == "app": // a profile on an interface
		app, iface := argv[10], argv[4]
		rules := s.profileRules(app)
		if len(rules) == 0 {
			return hostfake.Response{Stderr: "ERROR: Could not find a profile matching '" + app + "'\n", ExitCode: 1}, true
		}
		for i := range rules {
			rules[i].iface = iface
		}
		s.rules = append(s.rules, rules...)
		s.save()
		return hostfake.Response{Stdout: both}, true
	case len(argv) == 12 && argv[1] == "delete" && argv[3] == "in" && argv[4] == "on" && argv[10] == "app":
		app, iface := argv[11], argv[5]
		s.rules = slices.DeleteFunc(s.rules, func(r simRule) bool { return r.app == app && r.iface == iface })
		s.save()
		return hostfake.Response{Stdout: gone}, true
	case len(argv) == 5 && argv[1] == "allow" && argv[2] == "in" && argv[3] == "on": // a trusted interface
		s.rules = append(s.rules, simRule{action: "allow", iface: argv[4]})
		s.save()
		return hostfake.Response{Stdout: both}, true
	case len(argv) == 6 && argv[1] == "delete" && argv[3] == "in" && argv[4] == "on":
		iface := argv[5]
		s.rules = slices.DeleteFunc(s.rules, func(r simRule) bool { return r.port == "" && r.iface == iface })
		s.save()
		return hostfake.Response{Stdout: gone}, true
	}
	return hostfake.Response{}, false
}

// profileRules are application app's rules, from hardn's profile file: one per
// protocol, its ports joined by a comma, as "ufw status verbose" prints a
// profile of several ("80,443/tcp (Web)", from memory of such a host, not run;
// ufw-framework(8) has the file's form). None for no such application.
func (s *ufwSim) profileRules(app string) []simRule {
	data, err := s.h.f.ReadFile(context.Background(), ufw.ProfileFile)
	if err != nil {
		return nil
	}
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case line == "["+app+"]":
			in = true
		case in && strings.HasPrefix(line, "ports="):
			var protos []string
			ports := map[string][]string{}
			for _, spec := range strings.Split(strings.TrimPrefix(line, "ports="), "|") {
				port, proto, _ := strings.Cut(spec, "/")
				if !slices.Contains(protos, proto) {
					protos = append(protos, proto)
				}
				ports[proto] = append(ports[proto], port)
			}
			var rules []simRule
			for _, proto := range protos {
				rules = append(rules, simRule{action: "allow", port: strings.Join(ports[proto], ","), app: app, proto: proto})
			}
			return rules
		}
	}
	return nil
}

// The hardn.yml lines of the hosts: each ufw host has one, so that the
// application profiles' changes have a file to save to.
const (
	fwYML        = "username: alice\n"
	fwProfileYML = fwYML + "ufwAppProfiles:\n" +
		"  - name: LabHTTPS\n    title: Lab Web Server (HTTPS)\n    description: Lab Web server secure port\n    ports:\n      - \"30443/tcp\"\n" +
		"  - name: LabDNS\n    title: Lab DNS\n    description: Lab DNS over TLS\n    ports:\n      - \"8853/tcp\"\n"
)

// fwDebian is Debian 13 with hardn.yml, ufw as simulate leaves it: active,
// deny incoming, allowing 22.
func fwDebian() *fakeHost { return ufwOn(statusOn(debian13Platform).withHardnYML(fwYML)) }

// fwNoUFW is p with ufw not installed.
func fwNoUFW(p platform) *fakeHost {
	p.noUFW = true
	return ufwOn(statusOn(p).withHardnYML(fwYML))
}

// fwNoUFWSim is p with ufw not installed, its package tool simulating as sim
// says.
func fwNoUFWSim(p platform, sim hostfake.Response) func() *fakeHost {
	return func() *fakeHost {
		p.noUFW = true
		return simulateUFW(statusOn(p).withHardnYML(fwYML), ufwSim{incoming: "deny", sim: &sim})
	}
}

// fwAlpineStock is the Alpine 3.23 VM's shape (alpine323-vm.log): no ufw, no ss
// and apk's index empty.
func fwAlpineStock() *fakeHost {
	return fwWithoutSS(fwNoUFWSim(alpine323Platform, fwAPKNoIndex)())
}

// fwAlpineLXC is fwAlpineStock in an LXC container, as PID 1's environment
// says it (container=lxc), with no /run/systemd/container: OpenRC writes none.
func fwAlpineLXC() *fakeHost {
	h := fwAlpineStock()
	h.f.AddFile(detect.InitEnviron, []byte("container=lxc\x00HOME=/\x00TERM=linux\x00"), 0o400, 0, 0)
	return h
}

// fwProfilesInactive is Debian 13 with two profiles in hardn.yml, ufw inactive.
func fwProfilesInactive() *fakeHost {
	return simulateUFW(statusOn(debian13Platform).withHardnYML(fwProfileYML), ufwSim{incoming: "deny"})
}

// fwUbuntu is Ubuntu 26.04 as shipped: ssh.socket, ufw inactive.
func fwUbuntu() *fakeHost {
	h := statusOn(ubuntu2604Platform).withHardnYML(fwYML)
	h.ufwAsShipped()
	return ufwOn(h)
}

// fwWith is Debian 13, ufw as sim holds it, ENABLED as it says.
func fwWith(sim ufwSim) func() *fakeHost {
	return func() *fakeHost { return simulateUFW(statusOn(debian13Platform).withHardnYML(fwYML), sim) }
}

// fwEdit is Debian 13 with Port 2230 in hardn's drop-in, sshd not reloaded,
// so still on 22, ufw inactive with no rule: the configured set 2230, what
// accepts connections 22 (check-firewall.md §2.3).
func fwEdit() *fakeHost {
	h := statusOn(debian13Platform).withHardnYML(fwYML)
	h.f.AddFile(sshd.HardnDropIn, []byte("Port 2230\n"), 0o644, 0, 0)
	h.ufwAsShipped()
	return ufwOn(h)
}

// fwAlpineActive is Alpine 3.24 with ufw from community, active, allowing
// 22, enabled by ufw enable, which adds it to no runlevel (ufw.md §6, [I]).
func fwAlpineActive() *fakeHost {
	p := alpine324Platform
	p.noUFW = false
	h := statusOn(p).withHardnYML(fwYML)
	h.f.AddFile(revert.OpenRCInitDir+"/ufw", []byte("#!/sbin/openrc-run\n"), 0o755, 0, 0)
	h.saveRules()
	return ufwOn(h)
}

// fwPVEDrop is a Proxmox VE 9 node with the datacenter firewall on, DROP,
// and the management IP set written (cluster.fw as check-firewall's tests
// write it).
func fwPVEDrop() *fakeHost {
	h := pveNode()
	h.f.AddFile(pve.ClusterFW, []byte("[OPTIONS]\nenable: 1\npolicy_in: DROP\n\n[IPSET management]\n192.0.2.0/24\n"), 0o640, 0, 33)
	return h
}

// fwMoved is Debian 13, ufw active allowing 22, sshd -T saying 22, and sshd
// still bound on 2230: the stale listener the other way round.
func fwMoved() *fakeHost {
	h := fwDebian()
	h.bound = []string{"0.0.0.0:2230", "[::]:2230"}
	return h
}

// fwPVEUFW is a disabled-pve-firewall node where origin/main enabled ufw (V10):
// ufw answers as active, and the screen, which makes no ufw call, cannot say so.
func fwPVEUFW() *fakeHost {
	h := pveNode()
	h.noUFW = false
	return simulateUFW(h, ufwSim{incoming: "deny", active: true, enabled: true, rules: []simRule{{action: "allow", port: "22"}}})
}

// The refusals' hosts, which the menu of hosts leaves out (fwRefusalHosts).

// fwNoSS is Debian 13 with ufw inactive and no ss to read the bound sockets
// with: not found, as on a stock Alpine (alpine323-vm.log).
func fwNoSS() *fakeHost { return fwWithoutSS(fwUbuntu()) }

// fwWithoutSS is h with no ss: iproute2 not installed, as dpkg-query and apk
// say, until Install iproute2 runs its install (the maintainer, 2026-11-07).
func fwWithoutSS(h *fakeHost) *fakeHost {
	run := h.f.Handler
	missing := true
	h.f.Handler = func(argv []string, in []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case path.Base(argv[0]) == "ss" && missing:
			return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}
		case path.Base(argv[0]) == "dpkg-query" && argv[len(argv)-1] == "iproute2" && missing:
			return hostfake.Response{Stderr: "dpkg-query: no packages found matching iproute2\n", ExitCode: 1}
		case path.Base(argv[0]) == "dpkg-query" && argv[len(argv)-1] == "iproute2":
			return hostfake.Response{Stdout: "install ok installed\t6.15.0-1\n"}
		case cmd == "apk info -e iproute2" && missing:
			return hostfake.Response{ExitCode: 1}
		case cmd == "apk info -e iproute2":
			return hostfake.Response{Stdout: "iproute2\n"}
		case cmd == "apt-get install -s -y iproute2":
			return hostfake.Response{Stdout: "Reading package lists...\nThe following NEW packages will be installed:\n  iproute2\nInst iproute2 (6.15.0-1 Debian:13.7/stable [amd64])\nConf iproute2 (6.15.0-1 Debian:13.7/stable [amd64])\n"}
		case cmd == "apk add --simulate iproute2":
			return hostfake.Response{Stdout: "(1/1) Installing iproute2 (6.15.0-r0)\nOK: 12 MiB in 30 packages\n"}
		case cmd == "apt-get install -y iproute2", cmd == "apk add iproute2":
			missing = false
			return hostfake.Response{Stdout: "iproute2 installed\n"}
		case cmd == "apt-get remove -y iproute2", cmd == "apk del iproute2":
			missing = true
			return hostfake.Response{}
		}
		return run(argv, in)
	}
	return h
}

// fwSSFailing is Debian 13 with ufw inactive and an ss that runs and fails.
func fwSSFailing() *fakeHost {
	h := fwUbuntu()
	run := h.f.Handler
	h.f.Handler = func(argv []string, in []byte) hostfake.Response {
		if path.Base(argv[0]) == "ss" {
			return hostfake.Response{Stderr: "ss: netlink error\n", ExitCode: 1}
		}
		return run(argv, in)
	}
	return h
}

// fwMovedInactive is fwMoved with ufw inactive: Enable would refuse 2230.
func fwMovedInactive() *fakeHost {
	h := fwWith(ufwSim{incoming: "deny"})()
	h.bound = []string{"0.0.0.0:2230", "[::]:2230"}
	return h
}

// fwGarbled is Debian 13 whose ufw prints a line ufw does not print.
func fwGarbled() *fakeHost {
	h := fwDebian()
	run := h.f.Handler
	h.f.Handler = func(argv []string, in []byte) hostfake.Response {
		if strings.Join(argv, " ") == "ufw status verbose" {
			return hostfake.Response{Stdout: "Status: active\ngarbage\n"}
		}
		return run(argv, in)
	}
	return h
}

// fwProxmoxUnknown is Debian 13 where dpkg-query fails and pveversion is
// not there: whether it runs Proxmox VE is not established (detect's
// DetectProxmox).
func fwProxmoxUnknown() *fakeHost {
	h := fwDebian()
	run := h.f.Handler
	h.f.Handler = func(argv []string, in []byte) hostfake.Response {
		if path.Base(argv[0]) == "dpkg-query" {
			return hostfake.Response{Stderr: "dpkg-query: error: cannot access the dpkg database\n", ExitCode: 2}
		}
		return run(argv, in)
	}
	return h
}
