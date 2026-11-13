package ufw

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// ErrNoPlainAllow: no SSH port has, in every IP version ufw writes, the rule
// "ufw allow <port>/tcp" saves and nothing else deciding it, so there is no
// rule a limit would replace without changing one of the administrator's.
var ErrNoPlainAllow = errors.New("no SSH port has a plain ufw allow rule for a limit to replace")

// plainTuple is the saved rule "ufw <action> <port>/tcp" writes in family f:
// from anywhere, to anywhere, no interface, application, log type or comment.
func plainTuple(action string, port uint16, f Family) string {
	wild := "0.0.0.0/0"
	if f == IPv6 {
		wild = "::/0"
	}
	return "### tuple ### " + action + " tcp " + strconv.Itoa(int(port)) + " " + wild + " any " + wild + " in"
}

// tuple is a saved rule's line with its white space as single spaces.
func tuple(r Rule) string { return strings.Join(strings.Fields(r.Line), " ") }

// LimitPorts are the SSH ports of ls, ascending, that "ufw limit <port>/tcp"
// would limit by replacing hardn's kind of rule alone: ufw is active, and in
// every IP version it writes (IPv4 and, with its IPv6 on, IPv6) the port's
// connections from anywhere are Allowed by the plain rule "ufw allow
// <port>/tcp" saves. A port whose rule has a comment, a log type, a source,
// an interface or an application ("limit OpenSSH"), which another rule
// decides first, or which has the rule in one IP version alone, is not one:
// the limit would change a rule of the administrator's, or add one (ufw.md
// §2.8). A port already limited is not one either. Nil where st is not read
// whole, ufw is not active, or ENABLED disagrees with it.
func LimitPorts(st State, ls sshd.Listeners) []uint16 {
	if !st.Installed || !st.Known() || !st.Status.Active || !st.Enabled {
		return nil
	}
	fw := Firewall{Active: true, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Incoming}
	var ports []uint16
	for _, port := range ls.Ports() {
		plain := true
		for _, f := range writes(st) {
			c := Evaluate(st.Rules, fw, []Need{{Port: port, Family: f, Addr: anywhere(f)}})[0]
			want := plainTuple("allow", port, f)
			if c.Verdict != Allowed || strings.Join(strings.Fields(c.By), " ") != want ||
				!slices.ContainsFunc(st.Rules, func(r Rule) bool { return r.Family == f && tuple(r) == want }) {
				plain = false
			}
		}
		if plain {
			ports = append(ports, port)
		}
	}
	return ports
}

// PlanLimit plans, with no I/O, "ufw limit <port>/tcp" for each of
// LimitPorts, its undo "ufw allow <port>/tcp": ufw replaces the allow with
// the limit in its place, and the undo puts the allow back the same way
// (ufw.md §2.8, §3: UFWRule.match below 0). A limit accepts, so no port is
// ever left without a rule that lets SSH through: each listener let through
// before is let through after (a *BlockError otherwise), and Execute checks
// that ufw then shows each limited port Limited. Every other rule is left as
// it is. With every SSH port limited already, or none to limit and none of
// them open without a limit, the plan is empty. Errors: ErrProxmoxUnknown,
// ErrListenersUnknown, ErrNotInstalled, ErrStateUnknown, ErrEnabledMismatch,
// ErrInactive, and ErrNoPlainAllow where a port is let through with no limit
// and none has the plain rule.
func PlanLimit(st State, ls sshd.Listeners, pve detect.Proxmox) (Plan, error) {
	if err := Applies(pve); errors.Is(err, ErrNotApplicable) {
		return Plan{NotApplicable: true}, nil
	} else if err != nil {
		return Plan{}, err
	}
	switch {
	case !ls.Known || len(ls.All) == 0:
		return Plan{}, fmt.Errorf("ufw: %w", ErrListenersUnknown)
	case !st.Installed:
		return Plan{}, fmt.Errorf("ufw: %w", ErrNotInstalled)
	case !st.Known():
		return Plan{}, fmt.Errorf("ufw: %w (unread lines %q)", ErrStateUnknown, st.Bad)
	case st.Enabled != st.Status.Active:
		return Plan{}, fmt.Errorf("ufw: %w", ErrEnabledMismatch)
	case !st.Status.Active:
		return Plan{}, fmt.Errorf("ufw: %w", ErrInactive)
	}
	p := Plan{Active: true, Incoming: st.Incoming, Outgoing: st.Outgoing, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6}
	fw := p.firewall()
	ports := LimitPorts(st, ls)
	p.Needs = Needs(ls, nil, nil)
	for _, port := range ports {
		for _, f := range writes(st) {
			p.Needs = append(p.Needs, Need{Port: port, Family: f, Addr: anywhere(f), Why: "limited port"})
		}
	}
	p.Before = Evaluate(st.Rules, fw, p.Needs)
	if len(ports) == 0 {
		if slices.ContainsFunc(p.Before, func(c Coverage) bool { return c.Verdict == Allowed && !c.Need.Addr.IsLoopback() }) {
			return Plan{}, fmt.Errorf("ufw: %w", ErrNoPlainAllow)
		}
		return Plan{}, nil
	}
	rules := slices.Clone(st.Rules)
	for _, port := range ports {
		spec := strconv.Itoa(int(port)) + "/tcp"
		for _, f := range writes(st) {
			i := slices.IndexFunc(rules, func(r Rule) bool { return r.Family == f && tuple(r) == plainTuple("allow", port, f) })
			rules[i].Action, rules[i].Line = "limit", plainTuple("limit", port, f)
		}
		p.Steps = append(p.Steps, Step{Kind: StepLimit, Port: port, Argv: []string{"ufw", "limit", spec},
			Undo: []string{"ufw", "allow", spec}, Why: "limit SSH port " + spec})
	}
	p.After = Evaluate(rules, fw, p.Needs)
	var blocked []Coverage
	for i, c := range p.After {
		if p.Before[i].Verdict.OK() && !c.Verdict.OK() {
			blocked = append(blocked, c)
		}
	}
	if len(blocked) > 0 {
		return Plan{}, &BlockError{Coverage: blocked}
	}
	return p, nil
}
