package firewall

import (
	"context"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
)

// The Firewall row's lines on Rocky Linux and AlmaLinux, read from
// firewalld (proposed words; el-ports.md).
const (
	DetailFirewalld    = "firewalld running"           // Configured
	DetailFirewalldOff = "firewalld not running"       // NotConfigured
	DetailNoFirewall   = "no firewall installed"       // NotConfigured: neither firewalld nor ufw
	DetailZoneAccepts  = "zone accepts everything"     // NotConfigured: a zone's target ACCEPT
	DetailNotAtBoot    = "not started at boot"         // Partial: running, and systemctl is-enabled says disabled or masked
	DetailStartsAtBoot = "starts at boot, not running" // NotConfigured: stopped, and it starts at the next boot
)

// checkFirewalld is the row on Rocky Linux and AlmaLinux: firewalld running
// with no zone a connection can arrive in (active or default) accepting
// everything is Configured;
// installed and stopped ("starts at boot, not running" where it starts at
// the next boot), or with such a zone's target ACCEPT, Not Configured;
// running and not started at boot, Partial; not installed, ufw's line where ufw is installed (a host
// someone gave EPEL's ufw), else "no firewall installed". What reaches SSH
// is the port change's to plan (firewalld.PlanFor), not the row's.
func checkFirewalld(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	st, err := firewalld.Read(ctx, sys)
	switch {
	case err != nil:
		return engine.Status{State: engine.Unknown, Detail: "can't read"}, err
	case st.Installed == detect.No:
		out, err := checkUFW(ctx, sys, f)
		if err == nil && out.Detail == "ufw not installed" {
			out.Detail, out.Evidence = DetailNoFirewall, []string{"firewalld.service: not found", "ufw: not installed"}
		}
		return out, err
	case st.Running != detect.Yes:
		out := engine.Status{State: engine.NotConfigured, Detail: DetailFirewalldOff, Evidence: []string{"systemctl is-active: not active", "systemctl is-enabled: " + st.Unit}}
		if st.Unit == "enabled" {
			out.Detail = DetailStartsAtBoot
		}
		return out, nil
	}
	ev := []string{"firewall-cmd --state: running", "systemctl is-enabled: " + st.Unit, "default zone: " + st.Default, "active zones: " + strings.Join(st.Active, ", ")}
	accepts := false
	for _, z := range st.Zones() {
		res, err := sys.Run(ctx, firewalld.Cmd, "--permanent", "--zone="+z, "--get-target")
		if err != nil {
			return engine.Status{State: engine.Unknown, Detail: "can't read", Evidence: ev}, err
		}
		target := strings.TrimSpace(string(res.Stdout))
		ev = append(ev, z+"'s target: "+target)
		accepts = accepts || target == "ACCEPT"
	}
	if accepts {
		return engine.Status{State: engine.NotConfigured, Detail: DetailZoneAccepts, Evidence: ev}, nil
	}
	if st.Enabled != detect.Yes {
		return engine.Status{State: engine.Partial, Detail: DetailNotAtBoot, Evidence: ev}, nil
	}
	return engine.Status{State: engine.Configured, Detail: DetailFirewalld, Evidence: ev}, nil
}
