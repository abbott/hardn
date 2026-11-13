package firewall

import (
	"context"
	"errors"
	"fmt"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Limit SSH (docs/engine/gallery-bruteforce.md, adopted 2026-11-13): ufw's
// limit rule on SSH's ports, which refuses an address that opens 6 or more
// connections in 30 seconds (ufw(8)). It replaces the plain allow of the
// port and nothing else, so the port always has a rule that lets SSH
// through; where that can't be shown, it refuses.

// limit is Limit SSH's plan: refused where what accepts SSH is not read, as
// Enable is (ss missing or failing, a configured listener not bound, sshd's
// listeners not read), where ufw is not read, not installed or not running,
// and where no SSH port has the plain allow a limit replaces while one is
// open with no limit; steps.PlanUFWLimit otherwise, plain. On a host with no
// SSH server, or with every SSH port limited, an empty plan.
func limit(ctx context.Context, sys host.System, in engine.Input) ([]engine.Change, error) {
	sk := ReadSockets(ctx, sys, in.Facts)
	switch {
	case sk.NoSS:
		return nil, &steps.Refusal{Code: codeNoSS, Why: "hardn can't tell which ports SSH uses: ss isn't installed",
			Fix: "install iproute2, then try again", Err: ErrNoSS}
	case sk.SSFailed:
		return nil, &steps.Refusal{Code: codeSSFailed, Why: "hardn can't tell which ports SSH uses: ss failed",
			Fix: "check that it runs, then try again", Err: fmt.Errorf("%w: %v", ErrSSFailed, sk.err)}
	case sk.NoServer:
		return nil, nil
	case len(sk.Unbound) > 0:
		return nil, &steps.Refusal{Code: codeNotInEffect, Why: "sshd isn't listening where its config says, so hardn can't tell which ports SSH uses",
			Fix: "reload sshd or restart ssh.socket, then try again", Err: ErrNotInEffect}
	case !sk.Established():
		return nil, &steps.Refusal{Code: codeListeners, Why: "hardn can't tell which ports SSH uses", Err: fmt.Errorf("ufw: %w", ufw.ErrListenersUnknown)}
	}
	changes, err := steps.PlanUFWLimit(ctx, sys, in, ID, sk.Conf)
	switch {
	case errors.Is(err, ufw.ErrInactive):
		return nil, &steps.Refusal{Code: codeInactive, Why: "ufw isn't running, so it limits nothing",
			Fix: "enable the firewall (Firewall, option 1), then try again", Err: err}
	case errors.Is(err, ufw.ErrNoPlainAllow):
		return nil, &steps.Refusal{Code: codeNoPlainAllow, Why: "ufw's rule for SSH isn't a plain allow of its port, and hardn doesn't change a rule it finds",
			Fix: "set the limit with ufw limit", Err: err}
	}
	return changes, refusal(ctx, sys, in, err)
}

// LimitPorts are the SSH ports Limit SSH would limit on r's host, ascending:
// ufw.LimitPorts over the listeners, where they are established. Nil
// elsewhere: the screen then offers no item.
func (r Reading) LimitPorts() []uint16 {
	if r.OnPVE || r.EL || r.Proxmox != nil || r.StateErr != nil || !r.Sockets.Established() {
		return nil
	}
	return ufw.LimitPorts(r.State, r.Sockets.Conf)
}
