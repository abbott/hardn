package firewall

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
)

// checkPVE is the row on Proxmox VE, from pve.ReadFirewall; ufw is not
// consulted (brief 2.5). pve-firewall disabled (its service running) is
// NotConfigured, policy ACCEPT Partial; DROP or REJECT is Configured when
// pve-firewall's management rule lets SSH (22) and the web UI (8006)
// through, nothing may drop them first, every SSH listener is on 22, the
// one SSH port that rule accepts (decision §3.18), and the management IP
// set is written: without it the rule accepts the cluster's local network
// alone, which is not read (CodeLocalOnly). What ReadFirewall
// leaves Unknown stays Unknown, with its error where a read failed
// (pmxcfs down, a file or an option not read) and with its findings where
// a rule, the blacklist or nftables keep the state from being established.
func checkPVE(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	fw, err := pve.ReadFirewall(ctx, sys, f.Proxmox)
	switch {
	case err != nil:
		return engine.Status{State: engine.Unknown, Detail: "can't read"}, err
	case errors.Is(fw.Err, pve.ErrClusterFSDown): // its files are not there; nothing was read
		return engine.Status{State: engine.Unknown, Detail: "/etc/pve not mounted", Evidence: []string{"detect: pmxcfs not mounted on /etc/pve"}}, fw.Err
	}
	out := engine.Status{Evidence: pveEvidence(fw)}
	for _, x := range fw.Findings {
		out.Findings = append(out.Findings, steps.PVEFinding(x))
	}
	set := func(s engine.State, d string) { out.State, out.Detail = s, d }
	switch {
	case fw.Enabled == detect.Unknown || fw.NFTables == detect.Unknown || fw.Service.IsActive() == detect.Unknown:
		set(engine.Unknown, "can't read")
		return out, fw.Err
	case fw.Service.IsActive() == detect.No: // what it loaded before is not read (pve.md §2.16)
		set(engine.Unknown, strings.TrimSuffix(fw.Unit, ".service")+" stopped")
	case fw.Enabled == detect.No:
		set(engine.NotConfigured, "disabled")
	case fw.NFTables == detect.Yes:
		set(engine.Unknown, "uses nftables")
	case fw.PolicyIn == "":
		set(engine.Unknown, "can't read")
		return out, fw.Err
	case fw.PolicyIn == "ACCEPT":
		set(engine.Partial, "accept policy")
	case fw.SSH != pve.ReachManagement:
		set(engine.Unknown, "ssh may be blocked")
	case fw.Web != pve.ReachManagement:
		set(engine.Unknown, "web ui may be blocked")
	default:
		return pveSSH(ctx, sys, f, fw, out)
	}
	return out, nil
}

// pveSSH is the row under policy DROP or REJECT with 22 and 8006 accepted
// from the management IP set: Configured when every SSH listener is on 22
// and the set is written. A listener on another port is Unknown, not
// blocked: pve-firewall's rules for that port, which may accept it, are
// not read (pve.md §6).
func pveSSH(ctx context.Context, sys host.System, f detect.Facts, fw pve.Firewall, out engine.Status) (engine.Status, error) {
	sk := readSockets(ctx, sys, f)
	out.Evidence = append(out.Evidence, sk.ev...)
	var other []uint16
	if ls, ok := sk.listeners(); ok {
		for _, l := range ls.All {
			if l.Port != pve.SSHPort && !slices.Contains(other, l.Port) {
				other = append(other, l.Port)
			}
		}
	}
	out.Findings = append(out.Findings, sk.notInEffect()...)
	switch {
	case len(other) > 0:
		x := steps.PVEFinding(pve.Finding{Kind: pve.FindingPortNot22,
			Detail: fmt.Sprintf("ssh on %s: pve-firewall's management rule accepts 22 only", portList(other))})
		x.Source = "ss -Htln"
		if !slices.ContainsFunc(other, func(p uint16) bool { return !slices.Contains(sk.conf.Ports(), p) }) {
			x.Source = "sshd -T"
		}
		out.Findings = append(out.Findings, x)
		out.State, out.Detail = engine.Unknown, portList(other)+" may be blocked"
		if len(out.Detail) > maxUnknown {
			out.Detail = "ssh ports may be blocked"
		}
		return out, nil
	case !sk.established():
		out.State, out.Detail = engine.Unknown, sk.why
		return out, sk.err
	case len(fw.Management) == 0:
		out.Findings = append(out.Findings, engine.Finding{Code: CodeLocalOnly, Source: pve.ClusterFW,
			Detail: "no management IP set: pve-firewall's management rule accepts 22 and 8006 from the local network only"})
		out.State, out.Detail = engine.Unknown, "no management ipset"
		return out, nil
	}
	out.State, out.Detail = engine.Configured, strings.ToLower(fw.PolicyIn)+" policy"
	return out, nil
}

// maxUnknown is the room for a detail beside "Unknown" at 64 columns
// (design-system README §9.3); a longer one is cut.
const maxUnknown = 29

// pveEvidence is what ReadFirewall read: the options, the unit and its
// state, how far 22 and 8006 are let through, and the management IP set as
// written.
func pveEvidence(fw pve.Firewall) []string {
	tri := map[detect.Tri]string{detect.Yes: "yes", detect.No: "no", detect.Unknown: "unknown"}
	policy := fw.PolicyIn
	if policy == "" {
		policy = "unknown"
	}
	ev := []string{
		fmt.Sprintf("pve-firewall: enabled %s, policy_in %s, nftables %s", tri[fw.Enabled], policy, tri[fw.NFTables]),
		fmt.Sprintf("systemctl is-active %s: %s", fw.Unit, fw.Service.Active),
		fmt.Sprintf("pve-firewall: port %d %s, port %d %s", pve.SSHPort, fw.SSH, pve.WebPort, fw.Web),
	}
	m := "not written"
	if len(fw.Management) > 0 {
		m = strings.Join(fw.Management, " ")
	}
	return append(ev, "pve-firewall: management "+m)
}
