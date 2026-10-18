package pve_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/pve"
)

var isActive = func(unit string) []string { return []string{"systemctl", "is-active", unit} }

// firewallHost is the fixture with clusterFW and hostFW as the firewall
// files ("-" leaves the fixture's; "" removes the file) and unit answering
// is-active with state.
func firewallHost(t *testing.T, clusterFW, hostFW, unit, state string) *hostfake.System {
	t.Helper()
	h := pve9(t)
	for name, data := range map[string]string{pve.ClusterFW: clusterFW, "/etc/pve/nodes/web-01/host.fw": hostFW} {
		switch data {
		case "-":
		case "":
			_ = h.Remove(context.Background(), name)
		default:
			h.AddFile(name, []byte(data), 0o640, 0, wwwData)
		}
	}
	r := hostfake.Response{Stdout: state + "\n"}
	if state != "active" {
		r.ExitCode = 3
	}
	h.Respond(r, isActive(unit)[0], isActive(unit)[1:]...)
	return h
}

// TestReadFirewallFixture: the fixture's firewall is on with policy DROP,
// so SSH and the web UI are allowed from the management IP set; its rules
// for other ports, other protocols, OUT and a disabled one do not change
// that.
func TestReadFirewallFixture(t *testing.T) {
	h := firewallHost(t, "-", "-", pve.FirewallUnit, "active")
	f, err := pve.ReadFirewall(context.Background(), h, pveUp)
	if err != nil {
		t.Fatal(err)
	}
	if f.Enabled != detect.Yes || f.PolicyIn != "DROP" || f.NFTables != detect.No || f.Unit != pve.FirewallUnit ||
		f.Service.IsActive() != detect.Yes || !slices.Equal(f.Management, []string{"192.0.2.0/24"}) ||
		f.SSH != pve.ReachManagement || f.Web != pve.ReachManagement || len(f.Findings) != 0 || f.Err != nil {
		t.Errorf("ReadFirewall = %+v", f)
	}
	checkReadOnly(t, h)
	if runs := h.Runs(); !slices.EqualFunc(runs, [][]string{isActive(pve.FirewallUnit)}, slices.Equal) {
		t.Errorf("runs = %q", runs)
	}
}

func TestReadFirewall(t *testing.T) {
	on := "[OPTIONS]\nenable: 1\n"
	tests := []struct {
		name            string
		cluster, hostfw string
		unit, state     string
		enabled         detect.Tri
		policy          string
		ssh, web        pve.Reach
		finding         pve.FindingKind
		findingLine     int
		unknown         bool // Err set
	}{
		{name: "stock: no firewall files", cluster: "", hostfw: "", enabled: detect.No, policy: "DROP", ssh: pve.ReachNotFiltered, web: pve.ReachNotFiltered},
		{name: "cluster enable 0", cluster: "[OPTIONS]\nenable: 0\n", hostfw: "", enabled: detect.No, policy: "DROP", ssh: pve.ReachNotFiltered, web: pve.ReachNotFiltered},
		{name: "host enable 0", cluster: on, hostfw: "[OPTIONS]\nenable: 0\n", enabled: detect.No, policy: "DROP", ssh: pve.ReachNotFiltered, web: pve.ReachNotFiltered},
		{name: "on, no host.fw: DROP by default", cluster: on, hostfw: "", enabled: detect.Yes, policy: "DROP", ssh: pve.ReachManagement, web: pve.ReachManagement},
		{name: "a cluster enable above 1", cluster: "[OPTIONS]\nenable: 2\n", hostfw: "", enabled: detect.Yes, policy: "DROP", ssh: pve.ReachManagement, web: pve.ReachManagement},
		{name: "policy ACCEPT", cluster: on + "policy_in: accept\n", hostfw: "", enabled: detect.Yes, policy: "ACCEPT", ssh: pve.ReachAnywhere, web: pve.ReachAnywhere},
		{name: "policy REJECT", cluster: on + "policy_in: REJECT\n", hostfw: "", enabled: detect.Yes, policy: "REJECT", ssh: pve.ReachManagement, web: pve.ReachManagement},
		{name: "a host rule drops 22", cluster: on, hostfw: "[RULES]\n\nIN DROP -p tcp -dport 22\n", enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachUnknown, web: pve.ReachManagement, finding: pve.FindingRuleMayBlock, findingLine: 3},
		{name: "a cluster rule rejects a range with 8006", cluster: on + "[RULES]\nIN REJECT --dport 8000:8010\n", hostfw: "", enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachManagement, web: pve.ReachUnknown, finding: pve.FindingRuleMayBlock, findingLine: 4},
		{name: "a macro that drops", cluster: on + "[RULES]\nIN SSH(DROP)\n", hostfw: "", enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachUnknown, web: pve.ReachUnknown, finding: pve.FindingRuleMayBlock},
		{name: "a macro that accepts", cluster: on + "[RULES]\nIN SSH(ACCEPT) -i vmbr0\n", hostfw: "", enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachManagement, web: pve.ReachManagement},
		{name: "a security group", cluster: on + "[group admins]\nIN DROP\n[RULES]\nGROUP admins\n", hostfw: "", enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachUnknown, web: pve.ReachUnknown, finding: pve.FindingRuleMayBlock, findingLine: 6},
		{name: "a port by name", cluster: on + "[RULES]\nIN DROP -p tcp -dport ssh\n", hostfw: "", enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachUnknown, web: pve.ReachUnknown, finding: pve.FindingRuleMayBlock},
		{name: "a disabled drop", cluster: on + "[RULES]\n|IN DROP\n", hostfw: "", enabled: detect.Yes, policy: "DROP", ssh: pve.ReachManagement, web: pve.ReachManagement},
		{name: "a drop rule's comment", cluster: on + "[RULES]\nIN ACCEPT # IN DROP\n", hostfw: "", enabled: detect.Yes, policy: "DROP", ssh: pve.ReachManagement, web: pve.ReachManagement},
		{name: "nftables", cluster: on, hostfw: "[OPTIONS]\nnftables: 1\n", unit: pve.NFTablesUnit, enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachUnknown, web: pve.ReachUnknown, finding: pve.FindingNotModelled},
		{name: "the service stopped", cluster: on, hostfw: "", state: "inactive", enabled: detect.Yes, policy: "DROP", ssh: pve.ReachUnknown, web: pve.ReachUnknown, unknown: true},
		{name: "the service stopped, firewall off", cluster: "", hostfw: "", state: "inactive", enabled: detect.No, policy: "DROP", ssh: pve.ReachUnknown, web: pve.ReachUnknown, unknown: true},
		{name: "the service failed", cluster: on, hostfw: "", state: "failed", enabled: detect.Yes, policy: "DROP", ssh: pve.ReachUnknown, web: pve.ReachUnknown, unknown: true},
		{name: "cluster enable not a number", cluster: "[OPTIONS]\nenable: yes\n", hostfw: "", enabled: detect.Unknown, policy: "DROP", ssh: pve.ReachUnknown, web: pve.ReachUnknown, unknown: true},
		{name: "host enable not 0 or 1", cluster: on, hostfw: "[OPTIONS]\nenable: true\n", enabled: detect.Unknown, policy: "DROP", ssh: pve.ReachUnknown, web: pve.ReachUnknown, unknown: true},
		{name: "policy_in unknown", cluster: on + "policy_in: allow\n", hostfw: "", enabled: detect.Yes, ssh: pve.ReachUnknown, web: pve.ReachUnknown, unknown: true},
		{name: "a blacklist", cluster: on + "[IPSET blacklist]\n192.0.2.99\n", hostfw: "", enabled: detect.Yes, policy: "DROP",
			ssh: pve.ReachManagement, web: pve.ReachManagement, finding: pve.FindingBlacklist},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unit, state := tt.unit, tt.state
			if unit == "" {
				unit = pve.FirewallUnit
			}
			if state == "" {
				state = "active"
			}
			h := firewallHost(t, tt.cluster, tt.hostfw, unit, state)
			before := len(h.Calls())
			f, err := pve.ReadFirewall(context.Background(), h, pveUp)
			if err != nil {
				t.Fatal(err)
			}
			if f.Enabled != tt.enabled || f.PolicyIn != tt.policy || f.SSH != tt.ssh || f.Web != tt.web || f.Unit != unit || f.Service.Active != state {
				t.Errorf("ReadFirewall = enabled %v policy %q ssh %v web %v unit %s %q (%v)", f.Enabled, f.PolicyIn, f.SSH, f.Web, f.Unit, f.Service.Active, f.Err)
			}
			if (f.Err != nil) != tt.unknown || f.Err != nil && !errors.Is(f.Err, pve.ErrStateUnknown) {
				t.Errorf("Err = %v", f.Err)
			}
			var kinds []pve.FindingKind
			for _, fd := range f.Findings {
				kinds = append(kinds, fd.Kind)
				if tt.findingLine != 0 && fd.Line != tt.findingLine {
					t.Errorf("finding at line %d, want %d: %+v", fd.Line, tt.findingLine, fd)
				}
			}
			if tt.finding == 0 && len(kinds) != 0 || tt.finding != 0 && (len(kinds) == 0 || slices.ContainsFunc(kinds, func(k pve.FindingKind) bool { return k != tt.finding })) {
				t.Errorf("findings = %+v, want %v", f.Findings, tt.finding)
			}
			checkReadOnly(t, h, before)
		})
	}
}

func TestReadFirewallNotEstablished(t *testing.T) {
	h := pve9(t)
	f, err := pve.ReadFirewall(context.Background(), h, pveDown)
	if err != nil || !errors.Is(f.Err, pve.ErrClusterFSDown) || f.Enabled != detect.Unknown || f.SSH != pve.ReachUnknown || f.Web != pve.ReachUnknown {
		t.Errorf("pmxcfs down: %+v, %v", f, err)
	}
	if len(h.Calls()) != 0 {
		t.Errorf("pmxcfs down: calls %v", h.Calls())
	}
	if _, err := pve.ReadFirewall(context.Background(), h, debian); !errors.Is(err, pve.ErrNotApplicable) {
		t.Errorf("not Proxmox VE: %v", err)
	}
	if _, err := pve.ReadFirewall(context.Background(), h, unknown); !errors.Is(err, pve.ErrProxmoxUnknown) {
		t.Errorf("unknown: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pve.ReadFirewall(cancelled, h, pveUp); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	// A file that cannot be read is Unknown, not "absent".
	h = pve9(t)
	_ = h.Remove(context.Background(), pve.ClusterFW)
	h.AddDir(pve.ClusterFW, 0o755, 0, wwwData)
	h.Respond(hostfake.Response{Stdout: "active\n"}, "systemctl", "is-active", pve.FirewallUnit)
	f, err = pve.ReadFirewall(context.Background(), h, pveUp)
	if err != nil || f.Enabled != detect.Unknown || f.SSH != pve.ReachUnknown || f.Err == nil || !strings.Contains(f.Err.Error(), pve.ClusterFW) {
		t.Errorf("unreadable cluster.fw: %+v, %v", f, err)
	}
	// systemctl printing something unknown.
	h = firewallHost(t, "[OPTIONS]\nenable: 1\n", "", pve.FirewallUnit, "activating")
	if f, _ := pve.ReadFirewall(context.Background(), h, pveUp); f.SSH != pve.ReachUnknown || f.Err == nil {
		t.Errorf("activating: %+v", f)
	}
}
