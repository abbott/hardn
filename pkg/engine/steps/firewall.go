package steps

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// UFW is what an SSH change does with ufw.
type UFW uint8

const (
	// UFWKeep lets the ports the change adds through an active ufw before
	// sshd moves (brief 2.3 step 2), and saves no rule in an inactive one
	// (ufw.Desired.NoSaveWhileInactive; P-33 is open on that). Where ufw is
	// not installed, or on Proxmox VE, it has nothing to do.
	UFWKeep UFW = iota
	// UFWEnable also sets the default incoming policy to deny and enables
	// ufw, after its allow rules (ufw.md §2.7).
	UFWEnable
)

// firewallWhy is the why of every ufw undo step the journal holds.
const firewallWhy = "firewall"

// planUFW plans ufw's part of a change: listeners, the ones that accept
// connections now (sshd.ReadEffective's, or a migration's Plan.Listening),
// and the ports the change adds let through before sshd moves, with the
// applying session's own port kept (ufw.md §6). nil when there is nothing
// for ufw to do: UFWKeep with no port added, ufw not installed, or
// Proxmox VE.
func (st sshState) planUFW(ctx context.Context, sys host.System, listeners sshd.Listeners, mode UFW, add []uint16) (*ufw.Plan, error) {
	if mode == UFWKeep && len(add) == 0 {
		return nil, nil
	}
	state, err := ufw.Read(ctx, sys, st.in.Facts.Proxmox)
	switch {
	case errors.Is(err, ufw.ErrNotApplicable) && mode == UFWKeep:
		return nil, nil
	case err != nil:
		return nil, err
	case !state.Installed && mode == UFWKeep:
		return nil, nil
	case !state.Installed:
		return nil, fmt.Errorf("ufw: %w", ufw.ErrNotInstalled)
	}
	in := ufw.Input{Proxmox: st.in.Facts.Proxmox, Listeners: listeners, Add: add, State: state}
	if c, err := revert.ParseConn(st.in.Session.Conn); err == nil {
		in.Keep = []uint16{c.Server.Port()}
	}
	if mode == UFWEnable {
		in.Desired = ufw.Desired{Enable: true, Incoming: ufw.PolicyDeny}
	} else {
		in.Desired = ufw.Desired{NoSaveWhileInactive: true}
	}
	p, err := ufw.PlanFor(in)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ufwTitle is ufw's plan's Title: the ports it allows, then whether it
// sets a default policy and enables ufw ("ufw: allow 22/tcp, 2230/tcp;
// enable"), or how many steps when that is too long (engine.TitleMax).
func ufwTitle(p ufw.Plan) string {
	var ports, rest []string
	for _, s := range p.Steps {
		switch s.Kind {
		case ufw.StepAllow:
			ports = append(ports, fmt.Sprintf("%d/tcp", s.Port))
		case ufw.StepDefault:
			rest = append(rest, "default policy")
		case ufw.StepEnable:
			rest = append(rest, "enable")
		}
	}
	if len(ports) > 0 {
		rest = append([]string{"allow " + strings.Join(ports, ", ")}, rest...)
	}
	return engine.FitTitle("ufw: "+strings.Join(rest, "; "), fmt.Sprintf("ufw: %d steps", len(p.Steps)))
}

// firewallChange is ufw's plan as a change, before sshd moves
// (StageFirewall): its default and enable undos journalled before it runs
// (Plan.PreUndo), and once it ran, error or not, the undos of what it did
// (Report.Undo, from ufw's answers: an allow ufw kept is not deleted), which
// the engine journals under a context without the apply's cancellation.
func firewallChange(id string, p ufw.Plan) engine.Change {
	var argvs [][]string
	for _, s := range p.Steps {
		argvs = append(argvs, s.Argv)
	}
	c := engine.Change{
		ID: id, Summary: "Firewall: " + strings.Join(formatArgvs(argvs), "; "), Title: ufwTitle(p), Target: "ufw", Commands: formatArgvs(argvs),
		StateBefore: activeWord(p, false), StateAfter: activeWord(p, true),
		Risk: engine.RiskLockout, Stage: engine.StageFirewall, Findings: ufwFindings(p.Findings),
		Rollback: engine.Undo{Steps: runSteps(firewallWhy, p.PreUndo())},
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		rep, err := ufw.Execute(ctx, sys, p)
		u := engine.Undo{Steps: runSteps(firewallWhy, rep.Undo)}
		return engine.Ran{Rollback: &u, Detail: fmt.Sprintf("firewall: verified %v%s", rep.Verified, because(rep.Unverified))}, err
	}
	return c
}

// activeWord is ufw's state before or after p, for the plan's view; ""
// when p does not change it.
func activeWord(p ufw.Plan, after bool) string {
	enable := false
	for _, s := range p.Steps {
		enable = enable || s.Kind == ufw.StepEnable
	}
	switch {
	case !enable:
		return ""
	case after:
		return "active"
	}
	return "inactive"
}

// PlanUFWEnable plans ufw enabled with default deny incoming, after allow
// rules for every listener sshd is configured for and for the applying
// session's port (ufw.md §2.7; p2drive's ufw-enable): one lockout-risk
// change, id prefix+"/firewall", or none when ufw is already so. The
// listeners must be established (ufw.ErrListenersUnknown).
func PlanUFWEnable(ctx context.Context, sys host.System, in engine.Input, prefix string) ([]engine.Change, error) {
	st, err := readSSH(ctx, sys, in, "")
	if err != nil {
		return nil, err
	}
	return st.enableUFW(ctx, sys, prefix, UFWEnable)
}

// enableUFW is ufw's enable alone, for mode UFWEnable (PlanUFWEnable, and
// an SSH change with nothing else left to do); none for UFWKeep.
func (st sshState) enableUFW(ctx context.Context, sys host.System, prefix string, mode UFW) ([]engine.Change, error) {
	if mode != UFWEnable {
		return nil, nil
	}
	if !st.before.Listeners.Known {
		return nil, fmt.Errorf("the listeners sshd configures now: %w", ufw.ErrListenersUnknown)
	}
	p, err := st.planUFW(ctx, sys, st.before.Listeners, UFWEnable, nil)
	if err != nil || p.Empty() {
		return nil, err
	}
	return claim([]engine.Change{firewallChange(prefix+"/firewall", *p)}), nil
}
