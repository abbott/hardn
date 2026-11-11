// Package firewall holds the firewall area's control: Figma's Firewall row
// ("deny policy"), and the changes the Firewall screen makes (refactor plan
// slice 4, next/s4-control). It no longer only checks.
//
// Controls() is the status list: Incoming with ActionNone, whose Plan is
// engine.ErrNotMigrated, as a check-only control's, so that nothing that plans
// over the status list can start planning a lockout change. A screen plans
// Incoming with the Action it was asked for: Enable, the first lockout plan
// since the SSH primitives, refused wherever what accepts SSH is not read
// (decision 2); Install and Disable, plain; the application profiles' Save and
// Apply.
// Every refusal is a *steps.Refusal in the decided words
// (docs/engine/gallery-firewall.md §6), before anything changes. Every action
// refuses on Proxmox VE before any ufw call (brief 2.5). Read gives the screen
// what it draws, changing nothing.
//
// Check reads the host through the Phase 2 readers, never from the files
// alone (brief §3 principle 3): off Proxmox VE, ufw's state (ufw.Read) and
// whether it lets every SSH listener through (ufw.Needs, ufw.Check); on
// Proxmox VE pve-firewall's (pve.ReadFirewall), ufw not consulted (brief
// 2.5). The SSH listeners are read as SSH Port reads them: the configured
// set (sshd.ReadEffective) checked against the sockets ss shows bound
// (reload.ReadBound, reload.Unbound), never the configured set alone
// (docs/engine/check-ssh.md §6), so without ss they are not established.
// A firewall that refuses an SSH listener, or lets it through from
// loopback only, is the brief's critical cross-check (CodeSSHBlocked), but
// for one refused on IPv6 alone while IPv4 gets in on its port (ipv4Only). A
// read that fails is the check's error, which the engine reports as
// Unknown; what it read but cannot establish a state from is Unknown with
// findings: never a guess.
// docs/engine/check-firewall.md has every status line the row can show.
package firewall

import (
	"context"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
)

// The finding codes of this package (pkg/output's registry holds them).
const (
	// CodeSSHBlocked: an active firewall refuses new connections to an SSH
	// listener, so a new session cannot log in: the lockout risk the
	// brief's cross-check names, which next/p4-risk scores critical. Not
	// for one refused on IPv6 alone while IPv4 gets in on its port
	// (ipv4Only): a new session still logs in over IPv4.
	CodeSSHBlocked = "FIREWALL_SSH_BLOCKED"
	// CodeSSHUnchecked: the SSH listeners are not established, so whether
	// the firewall lets them through is not established either, beside a
	// state that does not depend on it.
	CodeSSHUnchecked = "FIREWALL_SSH_UNCHECKED"
	// CodeBootUnchecked: ufw is active, and whether it starts at the next boot
	// is not read, so Configured would be a guess.
	CodeBootUnchecked = "FIREWALL_BOOT_UNCHECKED"
	// CodeLocalOnly: on Proxmox VE no management IP set is written, so
	// pve-firewall's management rule accepts SSH and the web UI from the
	// cluster's local network only, which is not read.
	CodeLocalOnly = "FIREWALL_PVE_LOCAL_ONLY"
	// CodePortsBlocked: Enable's firewall would block listening ports
	// other than SSH's, which it lets through now (others.go); the
	// finding names them and how to keep one open, and refuses nothing.
	CodePortsBlocked = "FIREWALL_PORTS_BLOCKED"
	// CodePortsUnread: the listening sockets were not all read (ss -Htlnp
	// or ss -Hulnp failed, or printed a line hardn can't read), so which
	// other ports Enable's firewall would block is not established.
	CodePortsUnread = "FIREWALL_PORTS_UNREAD"
)

// Three of the row's Partial details (proposed wording,
// check-firewall.md §5): a firewall that is on and refuses nothing no rule
// denies. Exported so that the risk level (next/p4-risk) tells them apart
// from the other Partial lines without a copy of the wording.
const (
	DetailAllowPolicy  = "allow policy"      // ufw: default allow incoming
	DetailAllPorts     = "all ports allowed" // ufw: a rule that opens every port
	DetailAcceptPolicy = "accept policy"     // pve-firewall: policy_in ACCEPT
)

// ID is the control's ID.
const ID = "firewall.incoming"

// Controls are the firewall controls in Figma's order: Firewall, with
// ActionNone, which plans nothing.
func Controls() []engine.Control { return []engine.Control{Incoming{}} }

// Action is what Incoming's Plan does.
type Action uint8

const (
	// ActionNone is the status list's: Plan is engine.ErrNotMigrated, with
	// no change.
	ActionNone            Action = iota
	ActionInstall                // the ufw package, through apt-get or apk (decision 1)
	ActionEnable                 // let SSH through, deny by default, start ufw, start it at boot
	ActionDisable                // ufw disable (decision 6)
	ActionSaveProfiles           // hardn.yml's ufwAppProfiles, as in.Config has them
	ActionApplyProfiles          // hardn's profile file, and a rule for each profile
	ActionSaveInterfaces         // hardn.yml's ufwTrustedInterfaces, as in.Config has them
	ActionApplyInterfaces        // "ufw allow in on <interface>" for each trusted interface
	ActionInstallSS              // the iproute2 package, which brings ss, through apt-get or apk
)

// Incoming is Figma's Firewall row: the host's firewall for incoming
// connections. Configured is the firewall active, refusing what no rule
// accepts (the default incoming policy deny or reject; DROP or REJECT on
// Proxmox VE), and letting every SSH listener through. Action is what Plan
// does; Check is the same for each.
type Incoming struct{ Action Action }

// ID returns ID.
func (Incoming) ID() string { return ID }

// Title is Figma's row label.
func (Incoming) Title() string { return "Firewall" }

// Applies is true everywhere: detect's facts do not establish a host
// without a packet filter, and NotApplicable would leave the row out of the
// risk level (docs/engine/checks.md §1.6).
func (Incoming) Applies(detect.Facts) (bool, string) { return true, "" }

// Check reads pve-firewall on Proxmox VE, firewalld on Rocky Linux and
// AlmaLinux, and ufw elsewhere; whether the
// host runs Proxmox VE must be established first, since which firewall is
// read depends on it. Off Proxmox VE, in a container, the evidence names the
// signal that established it (contained). docs/engine/check-firewall.md
// lists each status line.
func (Incoming) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	onPVE, err := pve.Applies(f.Proxmox)
	switch {
	case err != nil:
		return engine.Status{State: engine.Unknown, Detail: "can't tell if Proxmox"}, err
	case onPVE:
		return checkPVE(ctx, sys, f)
	case f.Platform.Distro.EL():
		return contained(f)(checkFirewalld(ctx, sys, f))
	}
	return contained(f)(checkUFW(ctx, sys, f))
}

// inContainer is whether detect establishes that hardn runs in a container,
// whose host may filter its traffic too, out of hardn's sight (the
// maintainer, 2026-11-11; check-firewall.md §2.16). Unknown is not.
func inContainer(f detect.Facts) bool { return f.Container.Detected == detect.Yes }

// contained returns a status as it is, its evidence ending with "container: "
// and the signal that fired, in DNS's form, where inContainer holds. The
// state, detail and findings stay as read: the host's filtering is not read.
func contained(f detect.Facts) func(engine.Status, error) (engine.Status, error) {
	return func(st engine.Status, err error) (engine.Status, error) {
		if inContainer(f) {
			st.Evidence = append(slices.Clip(st.Evidence), "container: "+f.Container.Signal)
		}
		return st, err
	}
}

// blocked is CodeSSHBlocked's finding for the listeners of cs.
func blocked(cs []netip.AddrPort, source string) engine.Finding {
	s := make([]string, len(cs))
	for i, c := range cs {
		s[i] = c.String()
	}
	return engine.Finding{Code: CodeSSHBlocked, Detail: strings.Join(s, ", ") + " blocked: lockout risk", Source: source}
}

// portList is ps ascending, joined by ", ".
func portList(ps []uint16) string {
	ps = slices.Sorted(slices.Values(ps))
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = strconv.Itoa(int(p))
	}
	return strings.Join(s, ", ")
}
