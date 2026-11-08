// Package ufw lets the SSH listeners through UFW before the firewall can
// block them (refactor plan P2, next/p2-ufw; decision §3.17: no "ufw
// disable" followed by "ufw reset"). Proxmox VE is never touched: its packet
// filter is pve-firewall's (brief §2).
//
// Read takes UFW's state over host.System. PlanFor is pure: from the
// effective SSH listener set (sshd.ReadEffective), the ports a change adds
// and that state, it returns the steps, allow rules first, then the default
// policy, then "ufw --force enable", and refuses a plan after which an SSH
// listener that is reachable now would be blocked. PlanDisable and PlanApps
// are the Firewall control's other plans, the same Execute running them:
// "ufw disable" (decision 6), and an application profile's rule, with the
// profile file's bytes from ProfileBytes. Execute runs the steps, then checks
// "ufw status verbose"; Check is the same cross-check for a status report. What ufw shows is its saved rules with its chains loaded:
// the proof that new connections pass is the confirmation from a new
// session (brief 2.4).
//
// docs/safety/ufw.md describes the guarantees, the ufw facts they rest on and
// what is written but not yet run on a Linux host.
package ufw

import (
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/host/detect"
)

// The files Read reads besides "ufw status verbose": ufw's defaults and its
// configuration (read in that order, a later key winning: ENABLED is in
// ConfFile), the saved rules it loads when it starts, and the kernel's IPv6
// directory, whose absence turns ufw's IPv6 handling off.
const (
	DefaultsFile = "/etc/default/ufw"
	ConfFile     = "/etc/ufw/ufw.conf"
	UserRules    = "/etc/ufw/user.rules"
	User6Rules   = "/etc/ufw/user6.rules"
	KernelIPv6   = "/proc/sys/net/ipv6"
)

// Errors the package returns, testable with errors.Is.
var (
	ErrNotApplicable    = errors.New("ufw is not used on Proxmox VE: pve-firewall manages its packet filter")
	ErrProxmoxUnknown   = errors.New("whether the host runs Proxmox VE is not established")
	ErrNotInstalled     = errors.New("ufw is not installed")
	ErrListenersUnknown = errors.New("the SSH listener set is not established")
	ErrStateUnknown     = errors.New("ufw's state is not established")
	ErrBlocks           = errors.New("the change would block an SSH listener")
	ErrVerify           = errors.New("ufw status does not show the planned state")
	// ErrEnabledMismatch: ENABLED in ConfFile and whether ufw's chains are
	// loaded disagree, so ufw's commands would not do what the plan
	// expects (State.Enabled).
	ErrEnabledMismatch = errors.New("ufw's ENABLED setting and its loaded chains disagree")
	// ErrInactive: ufw is not running, so an application's rule would only
	// be saved (decision 5: no rule is saved while ufw stays inactive).
	ErrInactive = errors.New("ufw is not running: its rules would only be saved")
	// ErrBadProfile: an application profile ufw would not read
	// (ValidProfile).
	ErrBadProfile = errors.New("the application profile is not one ufw reads")
	// ErrBadInterface: an interface name ufw would not take
	// (ValidInterface), or one named twice, in hardn.yml's trusted
	// interfaces.
	ErrBadInterface = errors.New("the interface is not one ufw takes")
)

// Applies says whether UFW may be used: ErrNotApplicable on Proxmox VE,
// ErrProxmoxUnknown (wrapping detect's reason) when that is not established,
// nil otherwise.
func Applies(pve detect.Proxmox) error {
	switch pve.Installed {
	case detect.No:
		return nil
	case detect.Yes:
		return ErrNotApplicable
	}
	if pve.Err != nil {
		return fmt.Errorf("ufw: %w: %w", ErrProxmoxUnknown, pve.Err)
	}
	return fmt.Errorf("ufw: %w", ErrProxmoxUnknown)
}

// Policy is a default policy as ufw names it; "" is not established.
type Policy string

const (
	PolicyAllow    Policy = "allow"
	PolicyDeny     Policy = "deny"
	PolicyReject   Policy = "reject"
	PolicyDisabled Policy = "disabled" // routed only: forwarding is off
)

// drops: the policy refuses what no rule accepts.
func (p Policy) drops() bool { return p == PolicyDeny || p == PolicyReject }

// Family is an IP version; ufw keeps a rule list for each.
type Family uint8

const (
	IPv4 Family = 4
	IPv6 Family = 6
)

func (f Family) String() string {
	if f == IPv6 {
		return "IPv6"
	}
	return "IPv4"
}

// Rule is one ufw rule, from user.rules or user6.rules (ParseTuples) or from
// a line of "ufw status verbose" (ParseStatus).
type Rule struct {
	Action string // allow, deny, reject, limit
	Dir    string // in, out, fwd
	Proto  string // tcp, udp, any, ...
	Dst    string // destination address or prefix; "" for any
	DPort  string // destination ports: "22", "6000:6007", "80,443"; "" for any
	// Src is whatever restricts the source (address, port, application),
	// as read; "" for any source.
	Src   string
	Iface string // the interface the rule is limited to; "" for any
	// App is the destination application of a saved rule ("" for none):
	// ufw tells "allow OpenSSH" from "allow 22/tcp" by it.
	App    string
	Family Family
	Line   string // as read
}
