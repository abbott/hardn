package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/details"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/ufw"
	fwscreen "github.com/abbott/hardn/pkg/screens/firewall"
	sdscreen "github.com/abbott/hardn/pkg/screens/systemdetails"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The listening ports' mocks (docs/roadmap.md, "Listening ports";
// docs/engine/gallery-ports.md, adopted 2026-11-13; docs/engine/screen-ports.md).
// "uigallery ports" lists them: the Firewall screen with a Ports section,
// which sets each listening port against the rules, on a ufw host and a
// firewalld host; and System Details with a Listening Ports section. The two
// sections are hardn's own: a simulated host answers ss and firewall-cmd, the
// real readers read it (firewall.ReadPorts, details.ReadPorts) and the
// screens' mappings draw it (fwscreen.PortRows, sdscreen.PortRows). The other
// sections' rows are the mock's own words. play's Firewall and System
// Details screens have the sections too, on play's hosts.

// portsPrefix names the listing and its goldens.
const portsPrefix = "ports"

// portsLabel is the source label every mock's note opens with.
const portsLabel = "Proposed: no Figma frame."

// The rows' short forms.
func okRow(key, value string) component.KeyValue {
	return component.KeyValue{Key: key, Value: value, State: component.ValueOK}
}

func plainRow(key, value string) component.KeyValue {
	return component.KeyValue{Key: key, Value: value}
}

// portsStatus is the Firewall row.
func portsStatus(state component.StatusState, detail string) component.StatusList {
	return component.StatusList{Rows: []component.StatusRow{{Label: "Firewall", State: state, Detail: detail}}}
}

// The mocks' host's listening sockets, as ss -Htlnp and ss -Hulnp print them
// to root: sshd and nginx on every address, PostgreSQL on loopback alone,
// prometheus on one address, node_exporter dual-stack, and an mDNS responder.
const (
	portsTCP = "LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((\"sshd\",pid=612,fd=3))\n" +
		"LISTEN 0 128 [::]:22 [::]:* users:((\"sshd\",pid=612,fd=4))\n" +
		"LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=700,fd=6),(\"nginx\",pid=701,fd=6))\n" +
		"LISTEN 0 511 [::]:80 [::]:* users:((\"nginx\",pid=700,fd=7))\n" +
		"LISTEN 0 511 0.0.0.0:443 0.0.0.0:* users:((\"nginx\",pid=700,fd=8))\n" +
		"LISTEN 0 511 [::]:443 [::]:* users:((\"nginx\",pid=700,fd=9))\n" +
		"LISTEN 0 244 127.0.0.1:5432 0.0.0.0:* users:((\"postgres\",pid=801,fd=6))\n" +
		"LISTEN 0 244 [::1]:5432 [::]:* users:((\"postgres\",pid=801,fd=7))\n" +
		"LISTEN 0 4096 192.0.2.20:9090 0.0.0.0:* users:((\"prometheus\",pid=820,fd=7))\n" +
		"LISTEN 0 4096 *:9100 *:* users:((\"node_exporter\",pid=733,fd=3))\n"
	portsUDP = "UNCONN 0 0 0.0.0.0:5353 0.0.0.0:* users:((\"avahi-daemon\",pid=590,fd=12))\n"
)

// portsZones is the firewalld host's firewall-cmd --list-all-zones: public,
// the default zone on eth0, lets in ssh, http, https and 8080; internal takes
// 192.0.2.0/24 alone and lets in those and 9090.
const portsZones = "internal (active)\n  target: default\n  interfaces: \n  sources: 192.0.2.0/24\n  services: http https ssh\n  ports: 8080/tcp 9090/tcp\n  rich rules: \n\n" +
	"public (default, active)\n  target: default\n  interfaces: eth0\n  sources: \n  services: http https ssh\n  ports: 8080/tcp\n  rich rules: \n"

// portsHost is the mocks' host: it answers ss's two tables, with each
// socket's program or, as ss prints them to a user with no process of her
// own, without; with noSS it has no ss. It answers firewall-cmd as the
// firewalld host does.
func portsHost(noSS, user bool) *hostfake.System {
	h := hostfake.New(host.Options{})
	table := func(out string) hostfake.Response {
		if !user {
			return hostfake.Response{Stdout: out}
		}
		var b strings.Builder
		for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
			line, _, _ := strings.Cut(l, " users:")
			b.WriteString(line + "\n")
		}
		return hostfake.Response{Stdout: b.String()}
	}
	services := map[string]string{"ssh": "22/tcp", "http": "80/tcp", "https": "443/tcp"}
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		cmd := strings.Join(argv, " ")
		switch {
		case argv[0] == "ss" && noSS:
			return hostfake.Response{Err: fmt.Errorf("host: %q: not in %s: %w", "ss", host.SearchPath, host.ErrNotFound)}
		case cmd == host.FormatArgv(reload.ListenTCPArgv):
			return table(portsTCP)
		case cmd == host.FormatArgv(reload.ListenUDPArgv):
			return table(portsUDP)
		case cmd == "firewall-cmd --list-all-zones":
			return hostfake.Response{Stdout: portsZones}
		}
		if svc, ok := strings.CutPrefix(cmd, "firewall-cmd --permanent --service="); ok {
			if ports, ok := services[strings.TrimSuffix(svc, " --get-ports")]; ok {
				return hostfake.Response{Stdout: ports + "\n"}
			}
		}
		return hostfake.Response{Err: fmt.Errorf("%s: %w", argv[0], hostfake.ErrUnscripted)}
	}
	return h
}

// portsUFWState is ufw as its status says it on the mocks' host: denying by
// default and letting in 22, 80, 443 and 8080, which nothing listens on;
// inactive, installed and not running.
func portsUFWState(active bool) ufw.State {
	sim := ufwSim{incoming: "deny", active: active}
	for _, p := range []string{"22", "80", "443", "8080"} {
		sim.rules = append(sim.rules, simRule{action: "allow", port: p})
	}
	return ufw.State{Installed: true, IPv6: true, Status: ufw.ParseStatus([]byte(sim.status()))}
}

// portsUFWRows are the Ports section's rows on the ufw host, read and drawn
// as hardn does.
func portsUFWRows(active, noSS bool) []component.KeyValue {
	r := firewall.Reading{State: portsUFWState(active)}
	return fwscreen.PortRows(firewall.ReadPorts(context.Background(), portsHost(noSS, false), r))
}

// portsFirewalldRows are the Ports section's rows on the firewalld host.
func portsFirewalldRows(noSS bool) []component.KeyValue {
	r := firewall.Reading{EL: true, Firewalld: firewalld.State{Installed: detect.Yes, Running: detect.Yes, Enabled: detect.Yes, Default: "public", Active: []string{"internal", "public"}}}
	return fwscreen.PortRows(firewall.ReadPorts(context.Background(), portsHost(noSS, false), r))
}

// portsDetailsRows are System Details' Listening Ports rows on the same host:
// as root, or as alice, to whom ss names no program.
func portsDetailsRows(noSS, user bool) []component.KeyValue {
	root := detect.Yes
	if user {
		root = detect.No
	}
	return sdscreen.PortRows(details.ReadPorts(context.Background(), portsHost(noSS, user), root))
}

// portsSSHRows are the Firewall screen's SSH section, sshd on 22: value for
// each address.
func portsSSHRows(value string, state component.ValueState) []component.KeyValue {
	return []component.KeyValue{{Key: "[::]:22", Value: value, State: state}, {Key: "0.0.0.0:22", Value: value, State: state}}
}

// portsUFWRules are the ufw host's rules, as the Rules section writes them.
func portsUFWRules(note string) []component.KeyValue {
	var rows []component.KeyValue
	for _, k := range []string{"22/tcp", "80/tcp", "443/tcp", "8080/tcp", "22/tcp (v6)", "80/tcp (v6)", "443/tcp (v6)", "8080/tcp (v6)"} {
		rows = append(rows, component.KeyValue{Key: k, Value: "Allow", Note: note, State: component.ValueOK})
	}
	return rows
}

// portsUFW is the Firewall screen on the ufw host: ufw active, denying by
// default, with p's Ports section.
func portsUFW(ports []component.KeyValue) screen.Firewall {
	return screen.Firewall{
		Status: portsStatus(component.StatusConfigured, "deny policy"),
		UFW:    []component.KeyValue{okRow("Installed", "Yes"), okRow(screen.FirewallStatusKey, "Active"), okRow(screen.FirewallPolicyKey, "Deny"), okRow(screen.FirewallBootKey, "Yes")},
		SSH:    portsSSHRows("Allowed", component.ValueOK),
		Rules:  portsUFWRules(""), Ports: ports,
		First: screen.FirewallDisableFirst, Installed: true,
	}
}

// portsUFWInactive is the same host with ufw stopped: nothing is filtered, and
// its saved rules are not in effect.
func portsUFWInactive() screen.Firewall {
	return screen.Firewall{
		Status: portsStatus(component.StatusNotConfigured, "inactive"),
		UFW: []component.KeyValue{okRow("Installed", "Yes"), {Key: screen.FirewallStatusKey, Value: "Inactive", State: component.ValueWarn},
			{Key: screen.FirewallBootKey, Value: "No", State: component.ValueWarn}},
		SSH:   portsSSHRows("Not filtered", component.ValuePlain),
		Rules: portsUFWRules("Saved"), Ports: portsUFWRows(false, false),
		First: screen.FirewallEnable, Risk: true, Installed: true,
	}
}

// portsUFWNoSS is the ufw host without ss: the SSH section says so already,
// item 1 installs iproute2, and the Ports section is the one row.
func portsUFWNoSS() screen.Firewall {
	v := portsUFW(portsUFWRows(true, true))
	v.SSH = append(portsSSHRows("Allowed", component.ValueOK), component.KeyValue{Key: "Bound sockets", Value: "ss not installed", State: component.ValueUnknown})
	v.First, v.DisableBeside = screen.FirewallInstallSS, true
	return v
}

// portsFirewalld is the Firewall screen on the firewalld host: firewalld
// running, the default zone public, with the Ports section.
func portsFirewalld(ports []component.KeyValue) screen.Firewalld {
	return screen.Firewalld{
		Status: portsStatus(component.StatusConfigured, "firewalld running"),
		Rows: []component.KeyValue{okRow(screen.FirewalldInstalledKey, "Yes"), okRow(screen.FirewalldRunningKey, "Yes"), okRow(screen.FirewallBootKey, "Yes"),
			plainRow(screen.FirewalldDefaultKey, "public"), plainRow(screen.FirewalldActiveKey, "internal, public")},
		Ports: ports, First: screen.FirewalldDisable,
	}
}

// portsDetails is System Details on the ufw host, with the Listening Ports
// section; the other sections a few rows each.
func portsDetails(ports []component.KeyValue) screen.SystemDetails {
	return screen.SystemDetails{
		OS:     []component.KeyValue{plainRow("OS", "Debian 13 (Trixie)"), plainRow("Kernel", "6.12.113+deb13-cloud-amd64"), plainRow("Uptime", "3 days, 5 hours, 12 minutes")},
		Host:   []component.KeyValue{plainRow("Hostname", "web-01"), plainRow("eth0", "192.0.2.20/24")},
		Ports:  ports,
		Users:  []component.KeyValue{plainRow("alice", "sudo")},
		CPU:    []component.KeyValue{plainRow("CPUs", "2"), plainRow("Load", "0.08, 0.03, 0.01")},
		Disk:   []component.KeyValue{plainRow("/", "4.1 of 19.6 GiB (21%)")},
		Memory: []component.KeyValue{plainRow("RAM", "412 of 1968 MiB (21%)")},
		Logins: []component.KeyValue{plainRow("alice", "2 hours ago from 192.0.2.10")},
	}
}

// portsMock is one mock: its name, what it shows, and its screen.
type portsMock struct {
	name, note string
	view       func() screen.SubMenu
}

// portsMocks are the mocks, in the listing's order.
func portsMocks() []portsMock {
	const host = hostFQDN + " listens on 22, 80 and 443 on every address, 5353/udp, 5432 on loopback alone, 9090 and 9100. "
	return []portsMock{
		{name: "ufw", view: func() screen.SubMenu { return portsUFW(portsUFWRows(true, false)).SubMenu() },
			note: host + "ufw is active, denying by default, and lets in 22, 80, 443 and 8080. The Ports section, after Rules: open, blocked, local only, 8080, let in with nothing listening, and 5353/udp, which ufw's own rules let in for mDNS alone."},
		{name: "ufw-inactive", view: func() screen.SubMenu { return portsUFWInactive().SubMenu() },
			note: host + "ufw is installed and not running, so nothing is filtered: every port but the loopback one says so, and a saved rule's port with nothing listening has no row."},
		{name: "ufw-no-ss", view: func() screen.SubMenu { return portsUFWNoSS().SubMenu() },
			note: "The ufw host without ss (iproute2): the Ports section is one row that says so. Item 1 installs iproute2, as it does today."},
		{name: "firewalld", view: func() screen.SubMenu { return portsFirewalld(portsFirewalldRows(false)).SubMenu() },
			note: host + "firewalld is running, its zone public letting in the same ports, and its zone internal 9090 from 192.0.2.0/24 alone. The same Ports section, after firewalld's, with a port let in from some sources."},
		{name: "firewalld-no-ss", view: func() screen.SubMenu { return portsFirewalld(portsFirewalldRows(true)).SubMenu() },
			note: "The firewalld host without ss (iproute): the Ports section is one row that says so."},
		{name: "details", view: func() screen.SubMenu { return portsDetails(portsDetailsRows(false, false)).SubMenu() },
			note: "System Details as root on the same host: the Listening Ports section, after Host and IPs. A row per socket, as ss lists them, in port order: port and protocol, address, process. Read only, nothing about the firewall."},
		{name: "details-not-root", view: func() screen.SubMenu { return portsDetails(portsDetailsRows(false, true)).SubMenu() },
			note: "hardn system-details as alice: ss names only her own processes, so no row has one, and the last line says why."},
		{name: "details-no-ss", view: func() screen.SubMenu { return portsDetails(portsDetailsRows(true, false)).SubMenu() },
			note: "System Details without ss: the section is one row that says so."},
	}
}

// portsListMocks are the listing's mocks: each mock's screen.
func portsListMocks() []listMock {
	var out []listMock
	for _, m := range portsMocks() {
		out = append(out, listMock{name: m.name, note: m.note, draw: func(_ context.Context, c component.Ctx) (string, error) {
			return m.view().Render(c), nil
		}})
	}
	return out
}

// portsUsage is the listing's usage line.
const portsUsage = "usage: go run ./internal/tools/uigallery ports [flags] [mock | all]..."

// portsListing is the listening ports' listing.
var portsListing = listing{prefix: portsPrefix, usage: portsUsage, label: portsLabel, mocks: portsListMocks}
