package firewall_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The gallery's web-01 (gallery-ports.md §2), in ss -Hlnp's form: nginx on 80
// and 443 on every address, PostgreSQL on loopback alone, prometheus on one
// address, node_exporter dual-stack, avahi-daemon's mDNS. The site builder
// adds sshd's own sockets on 22, which name no program.
const (
	webTCP = "LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:((\"nginx\",pid=700,fd=6),(\"nginx\",pid=701,fd=6))\n" +
		"LISTEN 0 511 [::]:80 [::]:* users:((\"nginx\",pid=700,fd=7))\n" +
		"LISTEN 0 511 0.0.0.0:443 0.0.0.0:* users:((\"nginx\",pid=700,fd=8))\n" +
		"LISTEN 0 511 [::]:443 [::]:* users:((\"nginx\",pid=700,fd=9))\n" +
		"LISTEN 0 244 127.0.0.1:5432 0.0.0.0:* users:((\"postgres\",pid=801,fd=6))\n" +
		"LISTEN 0 244 [::1]:5432 [::]:* users:((\"postgres\",pid=801,fd=7))\n" +
		"LISTEN 0 4096 192.0.2.20:9090 0.0.0.0:* users:((\"prometheus\",pid=820,fd=7))\n" +
		"LISTEN 0 4096 *:9100 *:* users:((\"node_exporter\",pid=733,fd=3))\n"
	nine100 = "LISTEN 0 4096 *:9100 *:* users:((\"node_exporter\",pid=733,fd=3))\n"
	webUDP  = "UNCONN 0 0 0.0.0.0:5353 0.0.0.0:* users:((\"avahi-daemon\",pid=590,fd=12))\n"
)

// portsOf are a reading's ports as "spec/proto reach process".
func portsOf(p firewall.Ports) []string {
	words := map[firewall.Reach]string{firewall.ReachUnknown: "unknown", firewall.ReachOpen: "open", firewall.ReachSome: "some", firewall.ReachBlocked: "blocked",
		firewall.ReachLocal: "local", firewall.ReachUnused: "unused", firewall.ReachUnfiltered: "unfiltered"}
	var out []string
	for _, port := range p.List {
		out = append(out, strings.TrimSpace(fmt.Sprintf("%s/%s %s %s", port.Spec, port.Proto, words[port.Reach], port.Process)))
	}
	return out
}

// TestPortsUFW: each listening port against ufw's rules, one row for each
// port and protocol, in port order.
func TestPortsUFW(t *testing.T) {
	web := allow("22", "80", "443", "8080")
	some := "LISTEN 0 4096 0.0.0.0:9090 0.0.0.0:* users:((\"prometheus\",pid=820,fd=7))\n"
	for name, tc := range map[string]struct {
		s    site
		want []string
		flag func(firewall.Ports) bool
	}{
		// The mock "ufw": open, blocked, local only, and 8080 let in with nothing listening. mDNS's port is
		// one before.rules accepts for its multicast group, so it is let in for some.
		"active, deny": {s: site{plat: debian13(), ufw: fw{active: true, rules: web}, tcp: webTCP, udp: webUDP},
			want: []string{"22/tcp open", "80/tcp open nginx", "443/tcp open nginx", "5353/udp some avahi-daemon", "5432/tcp local postgres",
				"8080/tcp unused", "9090/tcp blocked prometheus", "9100/tcp blocked node_exporter"}},
		// The mock "ufw-inactive": nothing is filtered, and a saved rule's unused port has no row.
		"inactive": {s: site{plat: debian13(), ufw: fw{rules: web}, tcp: webTCP, udp: webUDP},
			want: []string{"22/tcp unfiltered", "80/tcp unfiltered nginx", "443/tcp unfiltered nginx", "5353/udp unfiltered avahi-daemon", "5432/tcp local postgres",
				"9090/tcp unfiltered prometheus", "9100/tcp unfiltered node_exporter"}},
		"not installed": {s: site{plat: debian13(), ufw: fw{missing: true}, tcp: webTCP, edit: noInstallPlan},
			want: []string{"22/tcp unfiltered", "80/tcp unfiltered nginx", "443/tcp unfiltered nginx", "5432/tcp local postgres", "9090/tcp unfiltered prometheus",
				"9100/tcp unfiltered node_exporter"}},
		// The mocks "ufw-no-ss" and an ss that fails: nothing is listed.
		"no ss":    {s: site{plat: noSS(debian13()), ufw: fw{active: true, rules: web}}, flag: func(p firewall.Ports) bool { return p.NoSS && !p.Failed }},
		"ss fails": {s: site{plat: debian13(), ufw: fw{active: true, rules: web}, listenErr: &hostfake.Response{Stderr: "ss: netlink error\n", ExitCode: 1}}, flag: func(p firewall.Ports) bool { return p.Failed && !p.NoSS }},
		// A line not read: what was read is listed, and no port is said to have nothing listening.
		"a line not read": {s: site{plat: debian13(), ufw: fw{active: true, rules: web}, udp: "UNCONN 0 0 nowhere 0.0.0.0:*\n"},
			want: []string{"22/tcp open"}, flag: func(p firewall.Ports) bool { return p.Partial }},
		// ufw's status not read whole: no port's standing is established, loopback's but.
		"status garbled": {s: site{plat: debian13(), ufw: fw{active: true, rules: web, garbage: true}, tcp: webTCP},
			want: []string{"22/tcp unknown", "80/tcp unknown nginx", "443/tcp unknown nginx", "5432/tcp local postgres", "9090/tcp unknown prometheus", "9100/tcp unknown node_exporter"}},
		"status fails": {s: site{plat: debian13(), ufw: fw{active: true, fail: &hostfake.Response{Stderr: "ERROR: problem running iptables\n", ExitCode: 1}}},
			want: []string{"22/tcp unknown"}},
		// Let in from some sources; from all but some; rate limited; a deny ahead of the allow.
		"an allowlist": {s: site{plat: debian13(), ufw: fw{active: true, rules: append(allow("22"), rule{action: "allow", port: "9090", src: "192.0.2.0/24"})}, tcp: some},
			want: []string{"22/tcp open", "9090/tcp some prometheus"}},
		"a blocklist": {s: site{plat: debian13(), ufw: fw{active: true, rules: append(allow("22"), rule{action: "deny", port: "9090", src: "198.51.100.0/24"}, rule{action: "allow", port: "9090"})}, tcp: some},
			want: []string{"22/tcp open", "9090/tcp some prometheus"}},
		"a deny first": {s: site{plat: debian13(), ufw: fw{active: true, rules: append(allow("22"), rule{action: "deny", port: "9090"}, rule{action: "allow", port: "9090"})}, tcp: some},
			want: []string{"22/tcp open", "9090/tcp blocked prometheus"}},
		// The widest reach of a port's sockets counts: IPv4 let in, IPv6 not.
		"one family let in": {s: site{plat: debian13(), ufw: fw{active: true, rules: append(allow("22"), rule{action: "allow", port: "80", v4: true})}, tcp: webTCP[:strings.Index(webTCP, "LISTEN 0 511 0.0.0.0:443")]},
			want: []string{"22/tcp open", "80/tcp open nginx"}},
		// The policy allow: every port is open, and no rule's port is unused.
		"the policy allow": {s: site{plat: debian13(), ufw: fw{active: true, incoming: "allow"}, tcp: some},
			want: []string{"22/tcp open", "9090/tcp open prometheus"}},
		// A port Docker publishes is forwarded ahead of ufw's rules: not established.
		"docker-proxy": {s: site{plat: debian13(), ufw: fw{active: true, rules: allow("22")}, tcp: docker},
			want: []string{"22/tcp open", "8080/tcp unknown docker-proxy"}},
		// A program ss does not name; the DHCP client's port, which before.rules accepts from a server.
		"no program, and DHCP": {s: site{plat: debian13(), ufw: fw{active: true, rules: allow("22")}, tcp: unnamed, udp: dhcp},
			want: []string{"22/tcp open", "68/udp some systemd-network", "323/udp local chronyd", "9000/tcp blocked"}},
		// A range and a list with nothing listening are rows as ufw writes them; a restricted rule's port, a rule
		// on an interface and a port with a listener on loopback are not.
		"unused ranges": {s: site{plat: debian13(), ufw: fw{active: true, rules: append(allow("22", "6000:6007", "5432"), rule{action: "allow", port: "8443", src: "192.0.2.0/24"},
			rule{action: "allow", iface: "eth1"})}, tcp: postgres},
			want: []string{"22/tcp open", "53/tcp local systemd-resolve", "5432/tcp local postgres", "6000:6007/tcp unused"}},
		// A rule of both protocols, as "ufw allow 8080" makes: one row, the port alone, where nothing listens on
		// either; none where something listens on one.
		"both protocols": {s: site{plat: debian13(), ufw: fw{active: true, table: "22/tcp                     ALLOW IN    Anywhere\n8080                       ALLOW IN    Anywhere\n" +
			"9000                       ALLOW IN    Anywhere\n8080 (v6)                  ALLOW IN    Anywhere (v6)\n"}, tcp: unnamed},
			want: []string{"22/tcp open", "8080/ unused", "9000/tcp open"}},
	} {
		h, facts := tc.s.build(t)
		p := firewall.Read(context.Background(), h, facts, &config.Config{}, console).Ports
		if got := portsOf(p); !slices.Equal(got, tc.want) || tc.flag != nil && !tc.flag(p) {
			t.Errorf("%s:\n got %q\nwant %q\n%+v", name, got, tc.want, p)
		}
		if tc.s.edit == nil { // Install's plan simulates, which readOnly does not list
			readOnly(t, h, tc.s.plat)
		}
	}
}

// noInstallPlan answers what Install's plan asks of a host with no ufw, which
// the site builder does not script: the plan is refused, and no part of the
// ports' reading.
func noInstallPlan(h *hostfake.System) {
	next := h.Handler
	h.Handler = func(argv []string, stdin []byte) hostfake.Response {
		if argv[0] == "dpkg-query" || argv[0] == "apt-get" {
			return hostfake.Response{ExitCode: 1}
		}
		return next(argv, stdin)
	}
}

// TestPortsFirewalld: each listening port against a running firewalld's
// zones, and what a stopped, missing or unread one says.
func TestPortsFirewalld(t *testing.T) {
	zone := func(head, extra string) string {
		return head + "\n  target: default\n" + extra + "  rich rules: \n\n"
	}
	// The mock "firewalld": public on eth0 lets in ssh, http, https and 8080; internal, bound to a source, those and 9090.
	web := zone("internal (active)", "  interfaces: \n  sources: 192.0.2.0/24\n  services: ssh\n  ports: 80/tcp 443/tcp 8080/tcp 9090/tcp\n") +
		zone("public (default, active)", "  interfaces: eth0\n  sources: \n  services: ssh\n  ports: 80/tcp 443/tcp 8080/tcp\n")
	active := "internal\n  sources: 192.0.2.0/24\npublic (default)\n  interfaces: eth0\n"
	for name, tc := range map[string]struct {
		e           elHost
		active      string
		tcp, udp    string
		noSS        bool
		want        []string
		flag        func(firewall.Ports) bool
		stateFailed bool
	}{
		"running": {e: elHost{installed: true, active: "active", enabled: "enabled", zones: web}, active: active, tcp: webTCP, udp: webUDP,
			want: []string{"22/tcp open", "80/tcp open nginx", "443/tcp open nginx", "5353/udp blocked avahi-daemon", "5432/tcp local postgres",
				"8080/tcp unused", "9090/tcp some prometheus", "9100/tcp blocked node_exporter"}},
		// The stock zone: cockpit's port is let in with nothing listening; the DHCPv6 client's is not listed.
		"stock": {e: elHost{installed: true, active: "active", enabled: "enabled"}, tcp: unnamed,
			want: []string{"22/tcp open", "9000/tcp blocked", "9090/tcp unused"}},
		"no ss": {e: elHost{installed: true, active: "active", enabled: "enabled", zones: web}, active: active, noSS: true,
			flag: func(p firewall.Ports) bool { return p.NoSS }},
		"stopped": {e: elHost{installed: true, active: "inactive", enabled: "disabled"}, tcp: webTCP,
			want: []string{"22/tcp unfiltered", "80/tcp unfiltered nginx", "443/tcp unfiltered nginx", "5432/tcp local postgres", "9090/tcp unfiltered prometheus",
				"9100/tcp unfiltered node_exporter"}},
		"not installed": {e: elHost{}, tcp: unnamed, want: []string{"22/tcp unfiltered", "9000/tcp unfiltered"}},
		"failed":        {e: elHost{installed: true, active: "failed", enabled: "enabled"}, tcp: unnamed, want: []string{"22/tcp unknown", "9000/tcp unknown"}},
		// A rich rule hardn does not read may accept or refuse any port.
		"a rich rule": {e: elHost{installed: true, active: "active", enabled: "enabled",
			zones: "public (default, active)\n  target: default\n  interfaces: eth0\n  services: ssh\n  ports: \n  rich rules: \n\trule family=\"ipv4\" source address=\"192.0.2.0/24\" accept\n"},
			tcp: postgres + unnamed, want: []string{"22/tcp unknown", "53/tcp local systemd-resolve", "5432/tcp local postgres", "9000/tcp unknown"}},
		"zones not read": {e: elHost{installed: true, active: "active", enabled: "enabled", zones: "Traceback (most recent call last):\n"}, tcp: unnamed,
			want: []string{"22/tcp unknown", "9000/tcp unknown"}},
		// A zone that accepts everything on the only interface: every port is open.
		"target ACCEPT": {e: elHost{installed: true, active: "active", enabled: "enabled",
			zones: "trusted (default, active)\n  target: ACCEPT\n  interfaces: eth0\n  services: \n  ports: \n  rich rules: \n"}, active: "trusted (default)\n  interfaces: eth0\n",
			tcp: unnamed, want: []string{"22/tcp open", "9000/tcp open"}},
		// Two zones with an interface, one letting the port in: some. A range with nothing listening.
		"two interfaces": {e: elHost{installed: true, active: "active", enabled: "enabled",
			zones: zone("dmz (active)", "  interfaces: eth1\n  services: ssh\n  ports: 9000/tcp 6000-6007/udp\n") + zone("public (default, active)", "  interfaces: eth0\n  services: ssh\n  ports: 6000-6007/udp\n")},
			active: "dmz\n  interfaces: eth1\npublic (default)\n  interfaces: eth0\n", tcp: unnamed + docker,
			want: []string{"22/tcp open", "6000-6007/udp unused", "8080/tcp unknown docker-proxy", "9000/tcp some"}},
		// A zone bound to sources takes their connections first: a port it lacks is not open from anywhere,
		// and one the interface's zone lets in with nothing listening is not listed.
		"a source zone without the port": {e: elHost{installed: true, active: "active", enabled: "enabled",
			zones: zone("internal (active)", "  interfaces: \n  sources: 192.0.2.0/24\n  services: \n  ports: 9000/tcp\n") +
				zone("public (default, active)", "  interfaces: eth0\n  sources: \n  services: ssh\n  ports: 8080/tcp\n")},
			active: "internal\n  sources: 192.0.2.0/24\npublic (default)\n  interfaces: eth0\n", tcp: unnamed,
			want: []string{"22/tcp some", "9000/tcp some"}},
		// The default zone lists no interface while another zone has one: whether it takes any is not read,
		// so a port the two differ on is not established, and none is listed as let in with nothing listening.
		"default zone lists no interface": {e: elHost{installed: true, active: "active", enabled: "enabled",
			zones: zone("drop (active)", "  interfaces: eth0\n  sources: \n  services: ssh\n  ports: \n") +
				zone("public (default)", "  interfaces: \n  sources: \n  services: ssh\n  ports: 9000/tcp 8080/tcp\n")},
			active: "drop\n  interfaces: eth0\n", tcp: unnamed + nine100,
			want: []string{"22/tcp open", "9000/tcp unknown", "9100/tcp blocked node_exporter"}},
		// A port forward: the port it takes and the one it lands on here are not established; the rest are.
		"a port forward": {e: elHost{installed: true, active: "active", enabled: "enabled",
			zones: "public (default, active)\n  target: default\n  interfaces: eth0\n  sources: \n  services: ssh\n  ports: 80/tcp\n" +
				"  forward-ports: \n\tport=80:proto=tcp:toport=9000:toaddr=\n\tport=8443:proto=tcp:toport=443:toaddr=192.0.2.9\n  source-ports: \n  icmp-blocks: \n  rich rules: \n"},
			tcp: unnamed + nine100, want: []string{"22/tcp open", "9000/tcp unknown", "9100/tcp blocked node_exporter"}},
		// A forward that is not read: nothing is established.
		"a forward not read": {e: elHost{installed: true, active: "active", enabled: "enabled",
			zones: "public (default, active)\n  target: default\n  interfaces: eth0\n  services: ssh\n  ports: \n  forward-ports: \n\tport=http:proto=tcp\n  rich rules: \n"},
			tcp: unnamed, want: []string{"22/tcp unknown", "9000/tcp unknown"}},
	} {
		e := tc.e
		st := e.site()
		st.tcp, st.udp, st.plat.noSS = tc.tcp, tc.udp, tc.noSS
		h, facts := st.build(t)
		if e.active == "active" {
			zones, def := tc.active, "public"
			if zones == "" {
				zones = "public (default)\n  interfaces: eth0\n"
			}
			if strings.HasPrefix(zones, "trusted") {
				def = "trusted"
			}
			h.Respond(hostfake.Response{Stdout: "running\n"}, "firewall-cmd", "--state")
			h.Respond(hostfake.Response{Stdout: def + "\n"}, "firewall-cmd", "--get-default-zone")
			h.Respond(hostfake.Response{Stdout: zones}, "firewall-cmd", "--get-active-zones")
		}
		p := firewall.Read(context.Background(), h, facts, &config.Config{}, console).Ports
		if got := portsOf(p); !slices.Equal(got, tc.want) || tc.flag != nil && !tc.flag(p) {
			t.Errorf("%s:\n got %q\nwant %q\n%+v", name, got, tc.want, p)
		}
		for _, cmd := range e.ran {
			if strings.Contains(cmd, "-add-") || strings.Contains(cmd, "-remove-") || strings.HasPrefix(cmd, "systemctl st") {
				t.Errorf("%s: the read ran %q", name, cmd)
			}
		}
	}
}

// TestPortsProxmox: on Proxmox VE the Ports section is not read: no ss -p
// runs.
func TestPortsProxmox(t *testing.T) {
	h, facts := site{plat: pve9(), ufw: fw{missing: true}}.build(t)
	r := firewall.Read(context.Background(), h, facts, &config.Config{}, console)
	if !r.OnPVE || r.Ports.NoSS || r.Ports.Failed || len(r.Ports.List) != 0 {
		t.Errorf("%+v", r.Ports)
	}
	for _, argv := range h.Runs() {
		if argv[0] == "ss" && !slices.Equal(argv, []string{"ss", "-Htln"}) {
			t.Errorf("ran %q", argv)
		}
	}
}
