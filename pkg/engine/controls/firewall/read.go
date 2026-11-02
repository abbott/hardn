package firewall

import (
	"context"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Item is what the screen's item 1 does.
type Item uint8

const (
	ItemNone    Item = iota // Proxmox VE: Return alone
	ItemInstall             // no ufw
	ItemEnable              // anything not set: Enable's plan, or its refusal
	ItemDisable             // active and set
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
	// Profiles are hardn.yml's as loaded, whether ufw reads them or not.
	Profiles []config.UfwAppProfile
	// On Proxmox VE: pve-firewall's reading, and ENABLED from ufw.conf
	// (decision 7's warning row), with no ufw call.
	PVE           pve.Firewall
	PVEErr        error
	PVEUFWEnabled detect.Tri
	PVEUFWErr     error
	// Off Proxmox VE: ufw's state and whether its service starts at boot.
	State    ufw.State
	StateErr error
	Boot     detect.Tri
	Sockets  Sockets
	// First is what item 1 does; DisableBeside, that Disable has an item of its
	// own: ufw is active, and item 1 is not Disable.
	First         Item
	DisableBeside bool
	// Plan and PlanErr are what Enable plans, its refusal, read only where
	// First is ItemEnable; Does, what that plan does.
	Plan    []engine.Change
	PlanErr error
	Does    Does
}

// Read reads what the screen draws of sys, changing nothing and running only
// reads: Proxmox VE first, whichever firewall is read depending on it, with no
// ufw call there (brief 2.5); then the SSH listeners, ufw, its service at boot,
// and, where item 1 is Enable, Enable's plan, for session, as Plan makes it.
func Read(ctx context.Context, sys host.System, f detect.Facts, cfg *config.Config, session revert.Session) Reading {
	var r Reading
	if cfg != nil {
		r.Profiles = cfg.UfwAppProfiles
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
	r.State, r.StateErr = ufw.Read(ctx, sys, f.Proxmox)
	if r.State.Installed {
		r.Boot = ufw.ReadBoot(ctx, sys, f)
	}
	r.First = r.first()
	r.DisableBeside = r.First == ItemEnable && r.State.Status.Known && r.State.Status.Active
	if r.First == ItemEnable {
		in := engine.Input{Facts: f, Config: cfg, Session: session}
		r.Plan, r.PlanErr = Incoming{Action: ActionEnable}.Plan(ctx, sys, in)
		r.Does = does(r.Plan)
	}
	return r
}

// first is item 1: Install with no ufw; Disable once ufw is active and set;
// Enable otherwise.
func (r Reading) first() Item {
	switch {
	case r.StateErr == nil && !r.State.Installed:
		return ItemInstall
	case r.set():
		return ItemDisable
	}
	return ItemEnable
}

// set: ufw is active and set: it refuses what no rule accepts, lets every SSH
// listener, known to be what accepts SSH, through (an allowlist and a blocklist
// count), and starts at the next boot.
func (r Reading) set() bool {
	s := r.State.Status
	if r.StateErr != nil || !s.Known || len(s.Bad) > 0 || !s.Active || !r.State.Enabled || r.Boot != detect.Yes || !r.Sockets.Established() ||
		s.Incoming != ufw.PolicyDeny && s.Incoming != ufw.PolicyReject {
		return false
	}
	return !slices.ContainsFunc(r.Coverage(), func(c ufw.Coverage) bool { return c.Verdict == ufw.Blocked || c.Verdict == ufw.Denied })
}

// Coverage is ufw's verdict on each SSH listener: while active under what ufw
// shows, else under what its saved rules do once it starts.
func (r Reading) Coverage() []ufw.Coverage { return verdicts(r.State, r.Sockets) }

// Applied reports whether ufw's saved rules hold application name's rule.
func (r Reading) Applied(name string) bool {
	return slices.ContainsFunc(r.State.Rules, func(rl ufw.Rule) bool {
		return rl.App == name || rl.App == strings.ReplaceAll(name, " ", "%20")
	})
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
