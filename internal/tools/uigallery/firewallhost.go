package main

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The Firewall mocks' hosts (firewall.go; slice 4, unit (a)). fakeHost.ufw
// answers ufw's status with a deny policy and port rules from anywhere, and
// its enable and a port's allow: enough for the engine's scenarios. The
// mocks need more of ufw 0.36.2, so ufwSim answers ufw's commands on these
// hosts in its place, adding to the fake and changing none of it: another
// default policy, a rule from some sources (status rows as
// pkg/host/ufw's parser and its tests' model shape them; ufw.md §3),
// "ufw default" and "ufw disable" (ufw(8); the words are ufw's own,
// frontend.py), an application's rule (ufw-framework(8)), and the packages
// that bring ufw (apt-get, apk add, ufw.md §3 for Alpine's community
// package). Synthetic values only: 192.0.2.0/24, 198.51.100.0/24,
// 2001:db8::/32, alice, port 2230.

// simRule is one saved rule of ufwSim: action on port/tcp, from anywhere
// in both IP versions, or from src in src's alone; app names the
// application profile it came from.
type simRule struct{ action, port, src, app string }

// ufwSim is ufw on one host.
type ufwSim struct {
	h        *fakeHost
	incoming string // deny, allow or reject
	active   bool   // its chains loaded
	enabled  bool   // ENABLED=yes
	rules    []simRule
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

// status is ufw status verbose as ufw 0.36.2 prints it: an application's
// rule in the verbose form, "<port>/tcp (<app>)".
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
		to, from := r.port+"/tcp", r.src
		if r.app != "" {
			to += " (" + r.app + ")"
		}
		if r.src == "" {
			from = "Anywhere"
			if v6 {
				to, from = to+" (v6)", "Anywhere (v6)"
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
		if r.v6() {
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
	f.AddFile(ufw.DefaultsFile, []byte(fmt.Sprintf("IPV6=yes\nDEFAULT_INPUT_POLICY=%q\nDEFAULT_OUTPUT_POLICY=\"ACCEPT\"\n", word)), 0o644, 0, 0)
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
			if r.app != "" {
				fmt.Fprintf(&b, "\n### tuple ### %s tcp %s %s any %s %s - in\n", r.action, r.port, list.any, src, r.app)
				continue
			}
			fmt.Fprintf(&b, "\n### tuple ### %s tcp %s %s any %s in\n", r.action, r.port, list.any, src)
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

// ufw answers ufw's commands, and reports whether it did. A port's allow or
// delete says "Rule added", or "Rules updated" while inactive, as
// fakeHost.ufw does (vmtest.md §4.9).
func (s *ufwSim) ufw(argv []string) (hostfake.Response, bool) {
	cmd := strings.Join(argv[1:], " ")
	word := "Rule added"
	if !s.active {
		word = "Rules updated"
	}
	both := word + "\n" + word + " (v6)\n"
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
		return hostfake.Response{Stdout: both}, true
	case strings.HasPrefix(cmd, "allow from any to any app "):
		app := argv[len(argv)-1]
		if port := s.profilePort(app); port != "" {
			s.rules = append(s.rules, simRule{action: "allow", port: port, app: app})
			s.save()
			return hostfake.Response{Stdout: both}, true
		}
		return hostfake.Response{Stderr: "ERROR: Could not find a profile matching '" + app + "'\n", ExitCode: 1}, true
	}
	return hostfake.Response{}, false
}

// profilePort is the first port of application app in hardn's profile file,
// as ufw reads it (ufw-framework(8)), "" for no such application.
func (s *ufwSim) profilePort(app string) string {
	data, err := s.h.f.ReadFile(context.Background(), fwProfileFile)
	if err != nil {
		return ""
	}
	in := false
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case line == "["+app+"]":
			in = true
		case in && strings.HasPrefix(line, "ports="):
			first, _, _ := strings.Cut(strings.TrimPrefix(line, "ports="), "|")
			return strings.TrimSuffix(first, "/tcp")
		}
	}
	return ""
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

// The refusals' hosts, which the menu of hosts leaves out (refusalsMock).

// fwNoSS is Debian 13 with ufw inactive and no ss to read the bound sockets
// with: not found, as on a stock Alpine, said to ship none (reload.md §3,
// [unverified]).
func fwNoSS() *fakeHost {
	h := fwUbuntu()
	run := h.f.Handler
	h.f.Handler = func(argv []string, in []byte) hostfake.Response {
		if path.Base(argv[0]) == "ss" {
			return hostfake.Response{Err: host.ErrNotFound, ExitCode: -1}
		}
		return run(argv, in)
	}
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
