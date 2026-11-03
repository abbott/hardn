package firewall

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	firewallctl "github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/risk"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/ufw"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The rows' words, the host's types mapped to what pkg/ui draws: decided
// (gallery-firewall.md §6), but "Listeners" and "Finding", the mocks', and
// decision 7's "ufw" row, "Left on", "Not read" and "Run ufw disable", proposed
// (screen-firewall.md).
const (
	keyInstalled = "Installed"
	keyIPv6      = "IPv6"
	keyEnabled   = "Enabled"
	keyService   = "Service"
	keyBound     = "Bound sockets"
	keySSH       = "SSH (22)"
	keyWeb       = "Web UI (8006)"
	keyListeners = "Listeners"
	keyUFW       = "ufw"

	noRules    = "No firewall rules configured"
	noProfiles = "No application profiles configured"
	saved      = "Saved"
)

// statusRow is the Firewall row of sys, the main menu's: Incoming's Check,
// writing nothing, through the main menu's own mapping, with no engine.
func statusRow(ctx context.Context, sys host.System, f detect.Facts) component.StatusList {
	c := firewallctl.Incoming{}
	st, err := c.Check(ctx, sys, f)
	view := output.StatusViewOf([]engine.Report{{Control: c.ID(), Title: c.Title(), Status: st, Err: err}}, risk.Level{}).Status
	view.Rows = slices.DeleteFunc(view.Rows, func(row component.StatusRow) bool { return row.Label != c.Title() })
	return view
}

// mainView is r as ufw's screen draws it, under status. Whether Proxmox VE runs
// here not established, nothing is read: item 1, which says so when selected,
// under the row saying the same.
func mainView(r firewallctl.Reading, status component.StatusList, f detect.Facts) screen.Firewall {
	v := screen.Firewall{Status: status, First: first(r), Risk: risky(r.Plan), Installed: r.State.Installed, DisableBeside: r.DisableBeside,
		KeepsReject: !r.Does.SetsDeny && r.State.Status.Incoming == ufw.PolicyReject}
	if r.Proxmox != nil {
		return v
	}
	v.UFW, v.SSH = ufwRows(r, f), sshRows(r)
	if r.State.Installed {
		v.Rules = ruleRows(r)
		if len(r.Profiles) > 0 {
			v.Profiles = profileRows(r)
		}
	}
	return v
}

// first is what item 1 does on r: on Enable, what its plan does, starting ufw
// only while it is not running, so the words hold wherever they are drawn; an
// active ufw whose plan refuses, a repair that says why.
func first(r firewallctl.Reading) screen.FirewallFirst {
	switch r.First {
	case firewallctl.ItemInstall:
		return screen.FirewallInstall
	case firewallctl.ItemDisable:
		return screen.FirewallDisableFirst
	case firewallctl.ItemNone:
		return screen.FirewallNoFirst
	}
	if !r.State.Status.Active {
		return screen.FirewallEnable
	}
	switch d := r.Does; {
	case r.PlanErr != nil:
		return screen.FirewallRepair
	case d.SetsDeny:
		return screen.FirewallSetDeny
	case d.Allows && d.Boot:
		return screen.FirewallLetSSHBoot
	case d.Allows:
		return screen.FirewallLetSSH
	}
	return screen.FirewallStartBoot
}

// risky is whether a plan's changes are lockout-risk.
func risky(plan []engine.Change) bool {
	return slices.ContainsFunc(plan, func(c engine.Change) bool { return c.Risk == engine.RiskLockout })
}

// verdictRow is a verdict's value and state while ufw is active.
func verdictRow(v ufw.Verdict) (string, component.ValueState) {
	switch v {
	case ufw.Allowed:
		return "Allowed", component.ValueOK
	case ufw.Limited:
		return "Rate limited", component.ValueOK
	case ufw.Restricted:
		return "Some sources", component.ValueWarn
	case ufw.Excepted:
		return "Blocked for some", component.ValueWarn
	case ufw.Blocked, ufw.Denied:
		return "Refused", component.ValueBad
	case ufw.Unreachable:
		return "No IPv6", component.ValuePlain
	}
	return "", component.ValueUnknown
}

// sshRows are the section SSH's rows: each listener, as sshd accepts on it or is
// configured to, and ufw's verdict for it, never hardn.yml's sshPort. Not
// active, ufw filters nothing: "Not filtered"; ENABLED=yes, "Refused at start"
// for what its saved rules would refuse. A listener configured and not bound, or
// bound and not configured, says so beside it.
func sshRows(r firewallctl.Reading) []component.KeyValue {
	if !r.Sockets.Read {
		return []component.KeyValue{{Key: keyListeners, State: component.ValueUnknown}}
	}
	active := r.State.Status.Known && r.State.Status.Active
	var unbound, configured []string
	for _, l := range r.Sockets.Unbound {
		unbound = append(unbound, l.String())
	}
	for _, l := range r.Sockets.Conf.All {
		configured = append(configured, l.String())
	}
	var rows []component.KeyValue
	for _, c := range r.Coverage() {
		key := strings.TrimPrefix(c.Need.Why, "listener ")
		row := component.KeyValue{Key: key}
		switch {
		case active:
			row.Value, row.State = verdictRow(c.Verdict)
		case r.State.Installed && r.State.Enabled && r.State.Known() && !c.Verdict.OK():
			row.Value, row.State = "Refused at start", component.ValueBad
		default:
			row.Value = "Not filtered"
		}
		base := strings.TrimSuffix(key, " (dual-stack)")
		switch {
		case slices.Contains(unbound, base):
			row.Note = "Not bound"
		case !slices.Contains(configured, base):
			row.Note = "Not configured"
		}
		if row.Note != "" && (row.State == component.ValuePlain || row.State == component.ValueOK) {
			row.State = component.ValueWarn
		}
		rows = append(rows, row)
	}
	switch {
	case r.Sockets.NoSS:
		rows = append(rows, component.KeyValue{Key: keyBound, Value: "ss not installed", State: component.ValueWarn})
	case r.Sockets.SSFailed:
		rows = append(rows, component.KeyValue{Key: keyBound, Value: "ss failed", State: component.ValueWarn})
	case r.Sockets.NoneBound():
		rows = append(rows, component.KeyValue{Key: keyBound, Value: "None on SSH's ports", State: component.ValueWarn})
	}
	return rows
}

// ufwRows are the section ufw's rows; ufw's state not read, Installed Unknown,
// which item 1 says when selected.
func ufwRows(r firewallctl.Reading, f detect.Facts) []component.KeyValue {
	if r.StateErr != nil {
		return []component.KeyValue{{Key: keyInstalled, State: component.ValueUnknown}}
	}
	if !r.State.Installed {
		return []component.KeyValue{{Key: keyInstalled, Value: "No", State: component.ValueWarn}}
	}
	rows := []component.KeyValue{{Key: keyInstalled, Value: "Yes", State: component.ValueOK}}
	s := r.State.Status
	status := component.KeyValue{Key: screen.FirewallStatusKey, Value: "Inactive", State: component.ValueWarn}
	switch {
	case !s.Known || len(s.Bad) > 0:
		status = component.KeyValue{Key: screen.FirewallStatusKey, State: component.ValueUnknown}
	case s.Active:
		status = component.KeyValue{Key: screen.FirewallStatusKey, Value: "Active", State: component.ValueOK}
	}
	in := r.State.Incoming
	if s.Active {
		in = s.Incoming
	}
	policy := component.KeyValue{Key: screen.FirewallPolicyKey, State: component.ValueUnknown}
	switch in {
	case ufw.PolicyDeny:
		policy.Value, policy.State = "Deny", component.ValueOK
	case ufw.PolicyReject:
		policy.Value, policy.State = "Reject", component.ValueOK
	case ufw.PolicyAllow:
		policy.Value, policy.State = "Allow", component.ValueWarn
	}
	if !s.Active && policy.State == component.ValueOK {
		policy.State = component.ValuePlain // it filters nothing: not a state to mark good
	}
	rows = append(rows, status, policy, bootRow(r, f))
	if !r.State.IPv6 && !r.State.NoKernelIPv6 {
		rows = append(rows, component.KeyValue{Key: keyIPv6, Value: "Off", State: component.ValueWarn})
	}
	return rows
}

// bootRow is At boot: Yes only with ENABLED=yes and the service read as starting
// at boot; a service off, masked, missing or not read says so.
func bootRow(r firewallctl.Reading, f detect.Facts) component.KeyValue {
	boot := component.KeyValue{Key: screen.FirewallBootKey, Value: "Yes", State: component.ValueOK}
	switch {
	case !r.State.Enabled:
		boot.Value, boot.State = "No", component.ValueWarn
	case r.Boot == detect.Unknown:
		boot = component.KeyValue{Key: screen.FirewallBootKey, State: component.ValueUnknown}
	case r.Boot == detect.No && f.Init.Kind == detect.OpenRC:
		boot.Value, boot.State = "In no runlevel", component.ValueWarn
	case ufw.BootMasked(r.BootUnit):
		boot.Value, boot.State = "Service masked", component.ValueWarn
	case r.BootUnit == "not-found":
		boot.Value, boot.State = "No service", component.ValueWarn
	case r.Boot == detect.No:
		boot.Value, boot.State = "Service disabled", component.ValueWarn
	}
	return boot
}

// ruleRows are the section Rules' rows: as ufw status verbose prints them while
// active, and while not, from its rule files, said as saved; each rule's state
// by a value state's marker, never colour alone: saved, an allow marks nothing
// good, ufw filtering nothing. None is a muted item (P-30 (b), as DNS decided).
func ruleRows(r firewallctl.Reading) []component.KeyValue {
	rules, note := r.State.Rules, saved
	if r.State.Status.Active {
		rules, note = r.State.Status.Rules, ""
	}
	if len(rules) == 0 {
		return []component.KeyValue{{Key: noRules, State: component.ValueNone}}
	}
	rows := make([]component.KeyValue, len(rules))
	for i, rl := range rules {
		rows[i] = component.KeyValue{Key: ruleKey(rl), Value: actionWord(rl.Action), State: component.ValueBad, Note: note}
		if rl.Action == "allow" || rl.Action == "limit" {
			rows[i].State = component.ValueOK
			if note != "" {
				rows[i].State = component.ValuePlain
			}
		}
	}
	return rows
}

// actionWord is a rule's action, capitalised.
func actionWord(a string) string {
	if a == "" {
		return a
	}
	return strings.ToUpper(a[:1]) + a[1:]
}

// ruleKey is a rule as the row names it: its port, or Anywhere, then who from,
// in ufw's own forms.
func ruleKey(r ufw.Rule) string {
	to := "Anywhere"
	if r.DPort != "" {
		to = r.DPort
		if r.Proto != "any" && r.Proto != "" {
			to += "/" + r.Proto
		}
	}
	if r.Dst != "" {
		to = r.Dst + " " + to
	}
	if r.Iface != "" {
		to += " on " + r.Iface
	}
	switch {
	case r.Src != "":
		to += " from " + r.Src
	case r.Family == ufw.IPv6 && r.Dst == "":
		to += " (v6)"
	}
	if r.Dir != "" && r.Dir != "in" {
		to += " " + r.Dir
	}
	return to
}

// profileRows are the profiles' rows: each one's name and ports, and whether it
// is in ufw. None is a muted item.
func profileRows(r firewallctl.Reading) []component.KeyValue {
	if len(r.Profiles) == 0 {
		return []component.KeyValue{{Key: noProfiles, State: component.ValueNone}}
	}
	var rows []component.KeyValue
	for _, p := range r.Profiles {
		row := component.KeyValue{Key: p.Name, Value: strings.Join(p.Ports, ", "), Note: "Not applied", State: component.ValueWarn}
		if r.Applied(p.Name) {
			row.Note, row.State = "Applied", component.ValueOK
		}
		rows = append(rows, row)
	}
	return rows
}

// pveView is r as the screen on Proxmox VE draws it, under status: whether
// pve-firewall is on, its default incoming policy, how far SSH and the web UI are
// let through, what the reading found, and ufw left on (decision 7). A reading
// that failed leaves each row Unknown, the Firewall row saying the control's
// words for it.
func pveView(r firewallctl.Reading, status component.StatusList) screen.FirewallPVE {
	enabled := component.KeyValue{Key: keyEnabled, State: component.ValueUnknown}
	switch r.PVE.Enabled {
	case detect.Yes:
		enabled.Value, enabled.State = "Yes", component.ValueOK
	case detect.No:
		enabled.Value, enabled.State = "No", component.ValueWarn
	}
	policy := component.KeyValue{Key: screen.FirewallPolicyKey, State: component.ValueUnknown}
	switch r.PVE.PolicyIn {
	case "DROP":
		policy.Value, policy.State = "Drop", component.ValueOK
	case "REJECT":
		policy.Value, policy.State = "Reject", component.ValueOK
	case "ACCEPT":
		policy.Value, policy.State = "Accept", component.ValueWarn
	}
	service := component.KeyValue{Key: keyService, State: component.ValueUnknown}
	switch r.PVE.Service.IsActive() {
	case detect.Yes:
		service.Value, service.State = "Running", component.ValueOK
	case detect.No:
		service.Value, service.State = "Stopped", component.ValueWarn
	}
	ssh, web := reachRow(keySSH, r.PVE.SSH), reachRow(keyWeb, r.PVE.Web)
	// Disabled or stopped it filters nothing that is read: not a state to mark
	// good, whatever its files say.
	if r.PVE.Enabled != detect.Yes || service.State != component.ValueOK {
		for _, kv := range []*component.KeyValue{&enabled, &policy, &ssh, &web} {
			if kv.State == component.ValueOK {
				kv.State = component.ValuePlain
			}
		}
	}
	if ls, ok := r.Sockets.Listeners(); ok {
		var others []string
		for _, p := range ls.Ports() {
			if p != pve.SSHPort {
				others = append(others, strconv.Itoa(int(p)))
			}
		}
		if len(others) > 0 {
			ssh.Note = "sshd on " + strings.Join(others, ", ")
			if ssh.State == component.ValueOK || ssh.State == component.ValuePlain {
				ssh.State = component.ValueWarn
			}
		}
	}
	rows := []component.KeyValue{service, enabled, policy, ssh, web}
	for _, x := range r.PVE.Findings {
		rows = append(rows, findingRow(x))
	}
	switch {
	case r.PVEUFWErr != nil:
		rows = append(rows, component.KeyValue{Key: keyUFW, Value: "Not read", State: component.ValueWarn})
	case r.PVEUFWEnabled == detect.Yes:
		rows = append(rows, component.KeyValue{Key: keyUFW, Value: "Left on", Note: "Run ufw disable", State: component.ValueWarn})
	}
	return screen.FirewallPVE{Status: status, Rows: rows}
}

// reachRow is how far pve-firewall lets a port through, in the row's words.
func reachRow(key string, reach pve.Reach) component.KeyValue {
	switch reach {
	case pve.ReachNotFiltered:
		return component.KeyValue{Key: key, Value: "Not filtered"}
	case pve.ReachAnywhere:
		return component.KeyValue{Key: key, Value: "From anywhere"}
	case pve.ReachManagement:
		return component.KeyValue{Key: key, Value: "Management network", State: component.ValueOK}
	}
	return component.KeyValue{Key: key, State: component.ValueUnknown}
}

// findingRow is what pve-firewall's reading found, as a row.
func findingRow(f pve.Finding) component.KeyValue {
	row := component.KeyValue{Key: "Finding", Value: f.Kind.String(), State: component.ValueWarn}
	switch f.Kind {
	case pve.FindingRuleMayBlock:
		row.Key, row.Value = "Rules", "One may block"
	case pve.FindingBlacklist:
		row.Key, row.Value = "Blacklist", "Has entries"
	case pve.FindingNotModelled:
		row.Key, row.Value = "proxmox-firewall", "Not read"
	}
	return row
}
