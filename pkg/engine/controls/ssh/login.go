package ssh

import (
	"context"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// Login is SSH Login: whether root may log in over SSH. Off Proxmox VE the
// baseline is PermitRootLogin no; on Proxmox VE it is prohibit-password,
// with root reachable by key from the other nodes, which clustering needs
// (brief 2.5, docs/safety/pve.md §6).
type Login struct{ engine.CheckOnly }

// ID is "ssh.login".
func (Login) ID() string { return "ssh.login" }

// Title is Figma's row label.
func (Login) Title() string { return "SSH Login" }

// Applies is true everywhere: a host without sshd is Unknown, not
// NotApplicable, since detect's units alone do not show that no SSH server
// runs.
func (Login) Applies(detect.Facts) (bool, string) { return true, "" }

// rootKeys are the keywords pve.CheckSSH reads for root's login by key,
// besides PermitRootLogin: the evidence of SSH Login on Proxmox VE.
var rootKeys = []string{"pubkeyauthentication", "allowusers", "denyusers", "allowgroups", "denygroups",
	"authenticationmethods", "authorizedkeysfile", "authorizedkeyscommand", "pubkeyacceptedalgorithms", "listenaddress"}

// Check reads PermitRootLogin as sshd -T prints it and every Match block
// that sets it, and on Proxmox VE pve.CheckSSH's root key-only and root by
// key. A state both baselines give (yes, a Match block that lets root in
// more, forced-commands-only) is reported whether or not Proxmox VE and
// root's groups are established. docs/engine/check-ssh.md lists each
// status line.
func (Login) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	r, err := read(ctx, sys, f)
	if err != nil {
		return failed(f.SSH, err)
	}
	ev := r.said("permitrootlogin")
	if st, ok := r.stale(ctx, sys, f, ev); ok {
		return st, nil
	}
	g := r.eff.PermitRootLogin
	if !g.Known {
		return unknown("can't read", ev, r.unknowns("permitrootlogin")), nil
	}
	more, odd := r.matchAllows("permitrootlogin", g.Value)
	if len(odd) > 0 {
		found, mev := matchFindings(odd, CodeMatchValueUnknown)
		return unknown("can't read", slices.Concat(ev, mev), found), nil
	}
	found, mev := matchFindings(more, CodeMatchAllowsMore)
	st := engine.Status{Evidence: slices.Concat(ev, mev), Findings: append(r.conflicts("permitrootlogin"), found...)}
	both := g.Value == "yes" || g.Value == "forced-commands-only" || len(more) > 0
	onPVE, err := pve.Applies(f.Proxmox)
	switch {
	case err != nil && !both:
		st.State, st.Detail = engine.Unknown, "can't tell if Proxmox"
		return st, err
	case err != nil || !onPVE:
		st.State, st.Detail = loginState(g.Value, len(more) > 0, "no")
		return st, nil
	}
	chk, groupsErr := r.proxmox(ctx, sys, f.Proxmox, true)
	if groupsErr != nil { // root's groups: RootKeyOnly does not need them
		if chk, err = r.proxmox(ctx, sys, f.Proxmox, false); err != nil {
			st.State, st.Detail = engine.Unknown, "can't read"
			return st, err
		}
	}
	st.Findings = pveFindings(st.Findings, chk.Findings, pve.FindingNotKeyOnly, pve.FindingRootUnreachable, pve.FindingProxmoxRootLogin)
	if both || chk.RootKeyOnly == detect.No {
		st.State, st.Detail = loginState(g.Value, len(more) > 0, pve.RootLogin)
		return st, nil
	}
	st.Evidence = append(st.Evidence, r.said(rootKeys...)...)
	switch {
	case groupsErr != nil: // RootByKey needs them
		st.State, st.Detail = engine.Unknown, "can't read"
		return st, groupsErr
	case chk.RootKeyOnly != detect.Yes:
		st.State, st.Detail = engine.Unknown, "can't read"
	case chk.RootByKey == detect.Yes:
		st.State, st.Detail = engine.Configured, "root key-only (Proxmox)"
	case chk.RootByKey == detect.No:
		st.State, st.Detail = engine.Partial, "root key login blocked"
	default:
		st.State, st.Detail = engine.Unknown, "can't tell if root key works"
	}
	return st, nil
}

// loginState is PermitRootLogin's state for the global value v against
// want, the baseline's ("no", or prohibit-password on Proxmox VE); more is
// whether a Match block lets root in more than v. On Proxmox VE a value
// stricter than prohibit-password, globally or in a Match block, is not
// the baseline either: the other nodes log in as root by key.
func loginState(v string, more bool, want string) (engine.State, string) {
	switch {
	case v == "yes":
		return engine.NotConfigured, "root allowed"
	case more:
		return engine.Partial, "match block allows root"
	case v == want:
		if want == "no" {
			return engine.Configured, "root disallowed"
		}
		return engine.Partial, "match block denies root" // prohibit-password, and a Match block stricter
	case v == "no":
		return engine.Partial, "root disallowed"
	case v == "forced-commands-only":
		return engine.Partial, "root commands only"
	}
	return engine.Partial, "root key-only"
}

// pveFindings is found with pve.CheckSSH's findings of kinds, each in
// place of sshd's shadowed finding at its line (Proxmox VE's own
// PermitRootLogin yes, which is not drift: pve.md §2.7).
func pveFindings(found []engine.Finding, pfs []pve.Finding, kinds ...pve.FindingKind) []engine.Finding {
	var add []engine.Finding
	for _, pf := range pfs {
		if slices.Contains(kinds, pf.Kind) {
			add = append(add, steps.PVEFinding(pf))
		}
	}
	shadowed := steps.FindingCode("SSHD", sshd.FindingShadowed.String())
	found = slices.DeleteFunc(found, func(f engine.Finding) bool {
		return f.Code == shadowed && slices.ContainsFunc(add, func(a engine.Finding) bool { return a.Source == f.Source })
	})
	return append(found, add...)
}
