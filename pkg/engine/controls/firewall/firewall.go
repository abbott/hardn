// Package firewall holds the firewall area's control (refactor plan Phase
// 4, next/p4-check-firewall): Figma's Firewall row ("deny policy"). In v0.4
// it is check-only: Check reads the host, and Plan returns
// engine.ErrNotMigrated (engine.CheckOnly) until slice 4 migrates it.
//
// Check reads the host through the Phase 2 readers, never from the files
// alone (brief §3 principle 3): off Proxmox VE, ufw's state (ufw.Read) and
// whether it lets every SSH listener through (ufw.Needs, ufw.Check); on
// Proxmox VE pve-firewall's (pve.ReadFirewall), ufw not consulted (brief
// 2.5). The SSH listeners are read as SSH Port reads them: the configured
// set (sshd.ReadEffective) checked against the sockets ss shows bound
// (reload.ReadBound, reload.Unbound), never the configured set alone
// (docs/engine/check-ssh.md §6). A firewall that refuses an SSH listener is
// the brief's critical cross-check (CodeSSHBlocked). A read that fails is
// the check's error, which the engine reports as Unknown; what it read but
// cannot establish a state from is Unknown with findings: never a guess.
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
	// brief's cross-check names, which next/p4-risk scores critical.
	CodeSSHBlocked = "FIREWALL_SSH_BLOCKED"
	// CodeSSHUnchecked: the SSH listeners are not established, so whether
	// the firewall lets them through is not established either, beside a
	// state that does not depend on it.
	CodeSSHUnchecked = "FIREWALL_SSH_UNCHECKED"
)

// Controls are the firewall controls in Figma's order: Firewall.
func Controls() []engine.Control { return []engine.Control{Incoming{}} }

// Incoming is Figma's Firewall row: the host's firewall for incoming
// connections. Configured is the firewall active, refusing what no rule
// accepts (the default incoming policy deny or reject; DROP or REJECT on
// Proxmox VE), and letting every SSH listener through.
type Incoming struct{ engine.CheckOnly }

// ID is "firewall.incoming".
func (Incoming) ID() string { return "firewall.incoming" }

// Title is Figma's row label.
func (Incoming) Title() string { return "Firewall" }

// Applies is true everywhere: detect's facts do not establish a host
// without a packet filter, and NotApplicable would leave the row out of the
// risk level (docs/engine/checks.md §1.6).
func (Incoming) Applies(detect.Facts) (bool, string) { return true, "" }

// Check reads pve-firewall on Proxmox VE and ufw elsewhere; whether the
// host runs Proxmox VE must be established first, since which firewall is
// read depends on it. docs/engine/check-firewall.md lists each status line.
func (Incoming) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	onPVE, err := pve.Applies(f.Proxmox)
	switch {
	case err != nil:
		return engine.Status{State: engine.Unknown, Detail: "can't tell if Proxmox"}, err
	case onPVE:
		return checkPVE(ctx, sys, f)
	}
	return checkUFW(ctx, sys, f)
}

// blocked is CodeSSHBlocked's finding for the listeners of cs.
func blocked(cs []netip.AddrPort, source string) engine.Finding {
	s := make([]string, len(cs))
	for i, c := range cs {
		s[i] = c.String()
	}
	return engine.Finding{Code: CodeSSHBlocked, Detail: strings.Join(s, ", ") + " blocked: lockout risk", Source: source}
}

// unchecked is CodeSSHUnchecked's finding for sockets not established:
// why, with the error of a read that failed.
func unchecked(sk sockets) engine.Finding {
	d := sk.why
	if sk.err != nil {
		d += ": " + sk.err.Error()
	}
	return engine.Finding{Code: CodeSSHUnchecked, Detail: d, Source: sk.source()}
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
