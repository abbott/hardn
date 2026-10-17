// Package ufw lets the SSH listeners through UFW before the firewall can
// block them (refactor plan P2, next/p2-ufw; decision §3.17: no "ufw
// disable" followed by "ufw reset"). Proxmox VE is never touched: its packet
// filter is pve-firewall's (brief §2).
//
// Read takes UFW's state over host.System. PlanFor is pure: from the
// effective SSH listener set (sshd.ReadEffective), the ports a change adds
// and that state, it returns the steps, allow rules first, then the default
// policy, then "ufw --force enable", and refuses a plan after which an SSH
// listener that is reachable now would be blocked. Execute runs the steps,
// then checks "ufw status verbose"; Check is the same cross-check for a
// status report.
//
// docs/safety/ufw.md describes the guarantees, the ufw facts they rest on and
// what is written but not yet run on a Linux host.
package ufw

import (
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/host/detect"
)

// The files Read reads besides "ufw status verbose": ufw's defaults, the
// saved rules it loads when it starts, and the kernel's IPv6 directory,
// whose absence turns ufw's IPv6 handling off.
const (
	DefaultsFile = "/etc/default/ufw"
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
	Src    string
	Iface  string // the interface the rule is limited to; "" for any
	Family Family
	Line   string // as read
}
