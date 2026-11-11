package firewall_test

import (
	"context"
	"slices"
	"testing"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/output"
)

// The other listeners, in ss -Hlnp's form (deb13-vm, rocky10-test-110,
// 2026-11-11), the programs and addresses synthetic.
const (
	vaultwarden = "LISTEN 0      4096         0.0.0.0:8000 0.0.0.0:* users:((\"vaultwarden\",pid=812,fd=11))\n" +
		"LISTEN 0      4096            [::]:8000    [::]:* users:((\"vaultwarden\",pid=812,fd=12))\n"
	postgres = "LISTEN 0      200        127.0.0.1:5432 0.0.0.0:* users:((\"postgres\",pid=901,fd=6))\n" +
		"LISTEN 0      4096   127.0.0.53%lo:53   0.0.0.0:* users:((\"systemd-resolve\",pid=455,fd=21))\n"
	coturn = "UNCONN 0      0              0.0.0.0:3478 0.0.0.0:* users:((\"turnserver\",pid=930,fd=3))\n"
	// dhcp is the DHCP client's sockets ufw's before.rules accepts, and
	// chronyd's on loopback.
	dhcp = "UNCONN 0      0      192.0.2.20%eth0:68   0.0.0.0:* users:((\"systemd-network\",pid=593,fd=34))\n" +
		"UNCONN 0      0            127.0.0.1:323 0.0.0.0:* users:((\"chronyd\",pid=853,fd=4))\n"
	// docker is a port Docker publishes, which ufw doesn't block.
	docker = "LISTEN 0      4096         0.0.0.0:8080 0.0.0.0:* users:((\"docker-proxy\",pid=1200,fd=4))\n"
	// unnamed is a socket ss names no program for.
	unnamed = "LISTEN 0      4096         0.0.0.0:9000 0.0.0.0:*\n"

	wordsVault = "ufw will block 8000/tcp (vaultwarden). To keep it open, run ufw allow 8000/tcp first."
)

// findingsOf are the findings of the change of id in p.
func findingsOf(p *engine.Plan, id string) []engine.Finding {
	for _, c := range p.Changes {
		if c.ID == id {
			return c.Findings
		}
	}
	return nil
}

// TestEnableOthers: Enable lists the listening ports ufw would block once it
// ran, other than SSH's, with the program ss names and how to keep one open
// (the maintainer, 2026-11-11). Loopback's, sshd's, the session's, a port a
// saved rule lets through, the DHCP client's, and a port ufw blocks already
// are not listed, nor a port Docker publishes. A read that fails is said,
// and refuses nothing. Each finding is Volatile, so a socket that comes or
// goes before Apply doesn't change the plan.
func TestEnableOthers(t *testing.T) {
	for name, tc := range map[string]struct {
		s       site
		session revert.Session
		code    string
		want    string // the detail; "" for no finding
	}{
		"vaultwarden on 8000, inactive": {site{plat: debian13(), ufw: fw{}, tcp: vaultwarden}, console, firewall.CodePortsBlocked, wordsVault},
		"loopback only":                 {site{plat: debian13(), ufw: fw{}, tcp: postgres, udp: dhcp}, console, "", ""},
		"a port Docker publishes":       {site{plat: debian13(), ufw: fw{}, tcp: docker}, console, "", ""},
		"a saved allow for 8000":        {site{plat: debian13(), ufw: fw{rules: allow("8000")}, tcp: vaultwarden}, console, "", ""},
		"active, the policy allow, two ports": {site{plat: debian13(), ufw: fw{active: true, incoming: "allow", rules: allow("22")}, tcp: vaultwarden, udp: coturn + dhcp},
			console, firewall.CodePortsBlocked, "ufw will block 8000/tcp (vaultwarden), 3478/udp (turnserver). To keep one open, run ufw allow 8000/tcp first."},
		"no program named": {site{plat: debian13(), ufw: fw{}, tcp: unnamed}, console, firewall.CodePortsBlocked,
			"ufw will block 9000/tcp. To keep it open, run ufw allow 9000/tcp first."},
		"active, deny: blocked already": {site{plat: debian13(), ufw: on, tcp: vaultwarden}, console, "", ""},
		"sshd's port and the session's": {site{plat: debian13(), ports: []uint16{2230}, bound: []string{"0.0.0.0:2230", "[::]:2230", "0.0.0.0:22", "[::]:22"}, ufw: fw{}},
			remote22, "", ""},
		// The model's restricted rule is IPv4's alone, so vaultwarden listens on IPv4 alone here.
		// 8000 still reached over IPv4 once the policy is deny, as SSH's IPv4-only case is not refused (ipv4Only).
		"one family still reached": {site{plat: debian13(), ufw: fw{active: true, incoming: "allow", rules: append(allow("22"), rule{action: "allow", port: "8000", v4: true})},
			tcp: vaultwarden}, console, "", ""},
		"a restricted allow for 8000": {site{plat: debian13(), ufw: fw{rules: []rule{{action: "allow", port: "8000", src: "192.0.2.0/24"}}},
			tcp: "LISTEN 0      4096         0.0.0.0:8000 0.0.0.0:* users:((\"vaultwarden\",pid=812,fd=11))\n"}, console, "", ""},
		"the read fails": {site{plat: debian13(), ufw: fw{}, tcp: vaultwarden, listenErr: &hostfake.Response{Stderr: "ss: netlink error\n", ExitCode: 1}},
			console, firewall.CodePortsUnread, "hardn can't tell which other ports ufw will block."},
		"a line not read": {site{plat: debian13(), ufw: fw{}, udp: "UNCONN 0 0 nowhere 0.0.0.0:*\n"}, console, firewall.CodePortsUnread,
			"hardn can't tell which other ports ufw will block."},
	} {
		t.Run(name, func(t *testing.T) {
			l := liveOn(t, tc.s)
			_, p := planOn(t, l, firewall.ActionEnable, tc.session, nil)
			if p.Err() != nil || p.Empty() {
				t.Fatalf("plan: %d changes, %v", len(p.Changes), p.Err())
			}
			fs := findingsOf(p, firewall.ID+"/firewall")
			var got []engine.Finding
			for _, f := range fs {
				if f.Code == firewall.CodePortsBlocked || f.Code == firewall.CodePortsUnread {
					got = append(got, f)
				}
			}
			switch {
			case tc.code == "" && len(got) > 0:
				t.Errorf("findings %+v, want none", got)
			case tc.code != "" && (len(got) != 1 || got[0].Code != tc.code || got[0].Detail != tc.want || got[0].Source != "ss -Htlnp, ss -Hulnp"):
				t.Errorf("findings %+v, want %s %q", got, tc.code, tc.want)
			}
			for _, f := range got {
				if e, ok := output.Lookup(f.Code); !ok || e.Kind != output.KindFinding {
					t.Errorf("%s: not a registered finding code", f.Code)
				}
				if !f.Volatile {
					t.Errorf("%s: not Volatile, so a socket that comes or goes before Apply would change the plan", f.Code)
				}
			}
			r := firewall.Read(context.Background(), l, l.facts, nil, tc.session)
			if r.First != firewall.ItemEnable || r.PlanErr != nil {
				t.Errorf("read: item %d, %v", r.First, r.PlanErr)
			}
		})
	}
}

// TestEnableOthersNothing: a host that is set plans nothing, and one whose
// plan only adds ufw's service to the boot services blocks nothing new:
// neither runs the reads.
func TestEnableOthersNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		u       fw
		changes int
	}{"set": {allow22, 0}, "boot alone": {fw{active: true, rules: allow("22"), boot: "disabled"}, 1}} {
		l := liveOn(t, site{plat: debian13(), ufw: tc.u, tcp: vaultwarden})
		_, p := planOn(t, l, firewall.ActionEnable, console, nil)
		if p.Err() != nil || len(commands(p)) != tc.changes {
			t.Fatalf("%s: plan %q, %v", name, commands(p), p.Err())
		}
		for _, r := range l.Runs() {
			if slices.Equal(r, []string{"ss", "-Htlnp"}) || slices.Equal(r, []string{"ss", "-Hulnp"}) {
				t.Errorf("%s: ran %q", name, r)
			}
		}
	}
}

// TestELEnableOthers: on Rocky Linux firewalld lists what its zones don't
// let through: cockpit's 9090 is its service's in public, 546/udp
// dhcpv6-client's, the DHCPv4 client's 68 needs no rule, loopback's is not
// reached; vaultwarden's 8000 is listed
// with firewall-offline-cmd's command.
func TestELEnableOthers(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		tcp, udp string
		want     string
	}{
		"cockpit and dhcpv6-client": {"LISTEN 0      4096         *:9090       *:* users:((\"systemd\",pid=1,fd=86))\n" + postgres,
			"UNCONN 0      0      [fe80::1]%eth0:546 [::]:* users:((\"NetworkManager\",pid=700,fd=20))\n", ""},
		"the DHCP client": {"", "UNCONN 0      0      192.0.2.20%eth0:68   0.0.0.0:* users:((\"NetworkManager\",pid=700,fd=22))\n", ""},
		"vaultwarden": {vaultwarden + "LISTEN 0      4096         *:9090       *:* users:((\"systemd\",pid=1,fd=86))\n", "",
			"firewalld will block 8000/tcp (vaultwarden). To keep it open, run firewall-offline-cmd --add-port=8000/tcp first."},
	} {
		t.Run(name, func(t *testing.T) {
			e := &elHost{installed: true, active: "inactive", enabled: "disabled"}
			s := e.site()
			s.tcp, s.udp = tc.tcp, tc.udp
			h, facts := s.build(t)
			in := engine.Input{Facts: facts, Config: &config.Config{}, Session: remote22}
			cs, err := firewall.Incoming{Action: firewall.ActionEnable}.Plan(ctx, h, in)
			if err != nil || len(cs) != 1 {
				t.Fatalf("plan: %+v, %v", cs, err)
			}
			switch fs := cs[0].Findings; {
			case tc.want == "" && len(fs) > 0:
				t.Errorf("findings %+v, want none", fs)
			case tc.want != "" && (len(fs) != 1 || fs[0].Code != firewall.CodePortsBlocked || fs[0].Detail != tc.want || !fs[0].Volatile):
				t.Errorf("findings %+v, want %q, Volatile", fs, tc.want)
			}
		})
	}
}
