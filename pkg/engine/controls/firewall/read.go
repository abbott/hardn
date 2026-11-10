package firewall

import (
	"context"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Item is what the screen's item 1 does.
type Item uint8

const (
	ItemNone      Item = iota // Proxmox VE: Return alone
	ItemInstall               // no ufw (no firewalld on Rocky Linux and AlmaLinux)
	ItemEnable                // anything not set: Enable's plan, or its refusal
	ItemDisable               // active and set (firewalld running there)
	ItemInstallSS             // ufw installed, ss not: Install iproute2 (the maintainer, 2026-11-07)
)

// Does is what Enable's plan does, so that a screen says item 1's words without
// reading commands.
type Does struct {
	Allows    bool // lets SSH through
	SetsDeny  bool // sets the default incoming policy
	StartsUFW bool // starts ufw
	Boot      bool // adds ufw's service to the boot services
}

// Reading is what the Firewall screen draws of a host, as facts: no words, no
// rows.
type Reading struct {
	OnPVE bool
	// Proxmox is why it is not established whether the host runs Proxmox VE;
	// nothing below it is read then.
	Proxmox error
	// Profiles are hardn.yml's as loaded, whether ufw reads them or not;
	// Interfaces, its trusted interfaces, likewise.
	Profiles   []config.UfwAppProfile
	Interfaces []string
	// On Proxmox VE: pve-firewall's reading, and ENABLED from ufw.conf
	// (decision 7's warning row), with no ufw call.
	PVE           pve.Firewall
	PVEErr        error
	PVEUFWEnabled detect.Tri
	PVEUFWErr     error
	// On Rocky Linux and AlmaLinux: firewalld's state, read in ufw's
	// place (el-firewalld.md); State, Boot and BootUnit are not read.
	EL           bool
	Firewalld    firewalld.State
	FirewalldErr error
	// Off Proxmox VE: ufw's state and whether its service starts at boot.
	State    ufw.State
	StateErr error
	Boot     detect.Tri
	// BootUnit is the state is-enabled printed on systemd, "" where none was
	// read: masked and not-found are what Enable refuses (ufw.ReadBootUnit).
	BootUnit string
	Sockets  Sockets
	// First is what item 1 does; DisableBeside, that Disable has an item of its
	// own: ufw is active, and item 1 is Enable or Install iproute2.
	First         Item
	DisableBeside bool
	// Plan and PlanErr are what item 1 plans, its refusal, read only where
	// First is ItemInstall, ItemInstallSS or ItemEnable; Does, what Enable's
	// plan does.
	Plan    []engine.Change
	PlanErr error
	Does    Does
}

// Read reads what the screen draws of sys, changing nothing and running only
// reads: Proxmox VE first, whichever firewall is read depending on it, with no
// ufw call there (brief 2.5); then the SSH listeners, ufw, its service at boot,
// and, where item 1 is Install or Enable, its plan, for session, as Plan makes
// it: Install's simulates, which changes nothing.
func Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config, session revert.Session) Reading {
	var r Reading
	if cfg != nil {
		r.Profiles, r.Interfaces = cfg.UfwAppProfiles, cfg.UfwTrustedInterfaces
	}
	if r.OnPVE, r.Proxmox = pve.Applies(f.Proxmox); r.Proxmox != nil {
		r.First, r.PlanErr = ItemEnable, applies(f.Proxmox)
		return r
	}
	r.Sockets = ReadSockets(ctx, sys, f)
	if r.OnPVE {
		r.PVE, r.PVEErr = pve.ReadFirewall(ctx, sys, f.Proxmox)
		r.PVEUFWEnabled, r.PVEUFWErr = ufw.ReadEnabled(ctx, sys)
		return r
	}
	if f.Platform.Distro.EL() {
		r.readEL(ctx, sys, engine.Input{Facts: f, Config: cfg, Session: session})
		return r
	}
	r.State, r.StateErr = ufw.Read(ctx, sys, f.Proxmox)
	if r.State.Installed {
		r.Boot, r.BootUnit = ufw.ReadBootUnit(ctx, sys, f)
	}
	r.First = r.first()
	r.DisableBeside = (r.First == ItemEnable || r.First == ItemInstallSS) && r.State.Status.Known && r.State.Status.Active
	in := engine.Input{Facts: f, Config: cfg, Session: session}
	switch r.First {
	case ItemInstall:
		r.Plan, r.PlanErr = Incoming{Action: ActionInstall}.Plan(ctx, sys, in)
	case ItemEnable:
		r.Plan, r.PlanErr = Incoming{Action: ActionEnable}.Plan(ctx, sys, in)
		r.Does = does(r.Plan)
		// Set but for a listener refused on IPv6 alone, with nothing for Enable
		// to change (where it would let IPv6 through, item 1 stays Enable):
		// item 1 is Disable, as on a set host.
		if r.PlanErr == nil && len(r.Plan) == 0 && r.setBut(true) {
			r.First, r.DisableBeside, r.Plan, r.Does = ItemDisable, false, nil, Does{}
		}
	case ItemInstallSS:
		r.Plan, r.PlanErr = Incoming{Action: ActionInstallSS}.Plan(ctx, sys, in)
	}
	return r
}

// readEL reads firewalld, and item 1 from it: Install where it isn't
// installed (not planned here: dnf's simulation may refresh its metadata
// over the network, el-firewalld.md §1.6); Disable where it runs, which
// needs no ss; Install iproute where ss is missing, likewise not planned;
// Enable otherwise, its plan read. Disable is beside the last two where
// is-enabled says enabled, which is what Disable then undoes. A read that
// fails is item 1 Enable with the refusal.
func (r *Reading) readEL(ctx context.Context, sys host.System, in engine.Input) {
	r.EL = true
	r.Firewalld, r.FirewalldErr = firewalld.Read(ctx, sys)
	switch {
	case r.FirewalldErr != nil:
		r.First, r.PlanErr = ItemEnable, firewalldUnknown(r.FirewalldErr)
		return
	case r.Firewalld.Installed == detect.No:
		r.First = ItemInstall
		return
	case r.Firewalld.Running == detect.Yes:
		r.First = ItemDisable
		return
	case r.Sockets.NoSS:
		r.First, r.DisableBeside = ItemInstallSS, r.Firewalld.Unit == "enabled"
		return
	default:
		r.First, r.DisableBeside = ItemEnable, r.Firewalld.Unit == "enabled"
	}
	if r.First == ItemEnable {
		r.Plan, r.PlanErr = Incoming{Action: ActionEnable}.Plan(ctx, sys, in)
	}
}

// first is item 1: Install with no ufw; Install iproute2 with ufw and no ss,
// which Enable needs to read the bound sockets (Sockets.NoSS); Disable once
// ufw is active and set; Enable otherwise.
func (r Reading) first() Item {
	switch {
	case r.StateErr == nil && !r.State.Installed:
		return ItemInstall
	case r.Sockets.NoSS:
		return ItemInstallSS
	case r.set():
		return ItemDisable
	}
	return ItemEnable
}

// set: ufw is active and set: it refuses what no rule accepts, lets every SSH
// listener, known to be what accepts SSH, through (an allowlist and a blocklist
// count), or there is none (Sockets.NoServer), and starts at the next boot.
func (r Reading) set() bool { return r.setBut(false) }

// setBut is set, with ipv6 a listener refused on IPv6 alone while IPv4 gets
// in (ipv4Only) not counting against it.
func (r Reading) setBut(ipv6 bool) bool {
	s := r.State.Status
	if r.StateErr != nil || !s.Known || len(s.Bad) > 0 || !s.Active || !r.State.Enabled || r.Boot != detect.Yes || !r.Sockets.Established() && !r.Sockets.NoServer ||
		s.Incoming != ufw.PolicyDeny && s.Incoming != ufw.PolicyReject {
		return false
	}
	cs := r.Coverage()
	return !slices.ContainsFunc(cs, func(c ufw.Coverage) bool {
		return (c.Verdict == ufw.Blocked || c.Verdict == ufw.Denied) && !(ipv6 && ipv4Only(cs, c))
	})
}

// Coverage is ufw's verdict on each SSH listener: while active under what ufw
// shows, else under what its saved rules do once it starts.
func (r Reading) Coverage() []ufw.Coverage { return verdicts(r.State, r.Sockets) }

// InterfaceApplied reports whether ufw's saved rules let everything on iface
// through (ufw.InterfaceAllowed).
func (r Reading) InterfaceApplied(iface string) bool {
	return ufw.InterfaceAllowed(r.State, iface)
}

// Applied reports whether ufw's saved rules hold profile p's rule, on its
// interface or on every one as it says (ufw.AppApplied).
func (r Reading) Applied(p config.UfwAppProfile) bool {
	return ufw.AppApplied(r.State, p.Name, p.Interface)
}

// does says what Enable's plan does, from its commands.
func does(plan []engine.Change) Does {
	var d Does
	for _, c := range plan {
		for _, cmd := range c.Commands {
			d.Allows = d.Allows || strings.HasPrefix(cmd, "ufw allow ")
			d.SetsDeny = d.SetsDeny || strings.HasPrefix(cmd, "ufw default ")
			d.StartsUFW = d.StartsUFW || cmd == "ufw --force enable"
		}
		d.Boot = d.Boot || c.ID == ID+"/boot"
	}
	return d
}
