package ufw

import (
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// Desired is what the plan leaves in force besides the SSH rules.
type Desired struct {
	Enable   bool   // start ufw when it is inactive
	Incoming Policy // the default incoming policy; "" leaves it
	Outgoing Policy // the default outgoing policy; "" leaves it
}

// Input is what PlanFor plans from.
type Input struct {
	Proxmox detect.Proxmox // detect's; Proxmox VE is NotApplicable
	// Listeners is sshd.ReadEffective's, before the change; it must be
	// Known.
	Listeners sshd.Listeners
	// Add are the ports the change adds (the new SSH port), let through
	// before sshd listens on them. Keep are ports to keep reachable that
	// the configuration does not show, such as the current session's.
	Add, Keep []uint16
	State     State // Read's
	Desired   Desired
}

// StepKind is what a Step does.
type StepKind uint8

const (
	StepAllow   StepKind = iota + 1 // ufw allow <port>/tcp
	StepDefault                     // ufw default <policy> incoming|outgoing
	StepEnable                      // ufw --force enable
)

func (k StepKind) String() string {
	return [...]string{"unknown", "allow", "default", "enable"}[min(int(k), 3)]
}

// Step is one command of a Plan, with the command that undoes it.
type Step struct {
	Kind StepKind
	Argv []string
	Undo []string
	Port uint16 // StepAllow's
	Why  string
}

// Plan is what PlanFor decided.
type Plan struct {
	NotApplicable bool // Proxmox VE: nothing runs
	Steps         []Step
	Needs         []Need
	// Before and After are each Need's Verdict now and once the steps ran.
	Before, After []Coverage
	// What is in force once the steps ran: Execute checks it.
	Active   bool
	Incoming Policy
	IPv6     bool
	// Findings are the Needs that are not OK now and stay so: the plan
	// does not cause them, and does not hide them.
	Findings []Finding
}

// Empty: nothing to run (the second, idempotent plan).
func (p Plan) Empty() bool { return len(p.Steps) == 0 }

// BlockError lists the Needs a plan would leave blocked: a listener
// reachable now, or a new port. Is(ErrBlocks).
type BlockError struct{ Coverage []Coverage }

func (e *BlockError) Error() string {
	var b strings.Builder
	b.WriteString("ufw: " + ErrBlocks.Error() + ":")
	for _, c := range e.Coverage {
		fmt.Fprintf(&b, " %s (%s %d) would be %s by %q;", c.Need.Why, c.Need.Family, c.Need.Port, c.Verdict, c.By)
	}
	return strings.TrimSuffix(b.String(), ";")
}

func (e *BlockError) Unwrap() error { return ErrBlocks }

// PlanFor plans, with no I/O, the commands that leave every Need let
// through: "ufw allow <port>/tcp" for each port no rule lets through (rules
// already there are never repeated, changed or removed), then the default
// policies, then "ufw --force enable". Never "ufw disable" or "ufw reset",
// except as the undo of its own enable. It refuses (a *BlockError) a plan
// after which a listener that is let through now, or a port the change adds,
// is not: an explicit deny, a restricted allow, IPv6 off in ufw, or a rule
// it cannot read. Errors: ErrProxmoxUnknown, ErrListenersUnknown,
// ErrNotInstalled, ErrStateUnknown, fs.ErrInvalid for a bad Desired or port.
func PlanFor(in Input) (Plan, error) {
	if err := Applies(in.Proxmox); errors.Is(err, ErrNotApplicable) {
		return Plan{NotApplicable: true}, nil
	} else if err != nil {
		return Plan{}, err
	}
	st := in.State
	switch {
	case !in.Listeners.Known:
		return Plan{}, fmt.Errorf("ufw: %w", ErrListenersUnknown)
	case !st.Installed:
		return Plan{}, fmt.Errorf("ufw: %w", ErrNotInstalled)
	case !st.Known():
		return Plan{}, fmt.Errorf("ufw: %w (unread lines %q)", ErrStateUnknown, st.Bad)
	}
	for _, p := range []Policy{in.Desired.Incoming, in.Desired.Outgoing} {
		if p != "" && p != PolicyAllow && !p.drops() {
			return Plan{}, fmt.Errorf("ufw: policy %q: %w", p, fs.ErrInvalid)
		}
	}
	if slices.Contains(in.Add, 0) || slices.Contains(in.Keep, 0) {
		return Plan{}, fmt.Errorf("ufw: port 0: %w", fs.ErrInvalid)
	}
	needs := Needs(in.Listeners, in.Add, in.Keep)
	if len(needs) == 0 {
		return Plan{}, fmt.Errorf("ufw: %w: no listener", ErrListenersUnknown)
	}
	active := st.Status.Active
	p := Plan{Needs: needs, Active: active || in.Desired.Enable, Incoming: st.Incoming, IPv6: st.IPv6}
	if in.Desired.Incoming != "" {
		p.Incoming = in.Desired.Incoming
	}
	p.Before = Evaluate(st.Rules, active, st.IPv6, st.Incoming, needs)
	// A port gets a rule when nothing would let it through once ufw is
	// active, even if it stays inactive: an enable later keeps it open. Not
	// a port with a deny, a restricted rule or an unread rule in either
	// family: "ufw allow <port>/tcp" applies to both, and ufw turns a
	// saved "deny <port>/tcp" into the allow instead of adding one.
	var ports, handsOff []uint16
	for _, c := range Evaluate(st.Rules, true, st.IPv6, p.Incoming, needs) {
		switch c.Verdict {
		case Blocked:
			if c.Need.Family == IPv4 || st.IPv6 {
				ports = append(ports, c.Need.Port)
			}
		case Denied, Restricted, VerdictUnknown:
			handsOff = append(handsOff, c.Need.Port)
		}
	}
	ports = slices.DeleteFunc(ports, func(port uint16) bool { return slices.Contains(handsOff, port) })
	slices.Sort(ports)
	ports = slices.Compact(ports)
	rules := slices.Clone(st.Rules)
	for _, port := range ports {
		spec := strconv.Itoa(int(port)) + "/tcp"
		rules = append(rules, Rule{Action: "allow", Dir: "in", Proto: "tcp", DPort: strconv.Itoa(int(port)), Family: IPv4, Line: "ufw allow " + spec})
		if st.IPv6 {
			rules = append(rules, Rule{Action: "allow", Dir: "in", Proto: "tcp", DPort: strconv.Itoa(int(port)), Family: IPv6, Line: "ufw allow " + spec})
		}
		p.Steps = append(p.Steps, Step{Kind: StepAllow, Port: port, Argv: []string{"ufw", "allow", spec},
			Undo: []string{"ufw", "delete", "allow", spec}, Why: "let SSH port " + spec + " through"})
	}
	p.After = Evaluate(rules, p.Active, st.IPv6, p.Incoming, needs)
	var blocked []Coverage
	for i, c := range p.After {
		if !c.Verdict.OK() && (c.Need.New || p.Before[i].Verdict.OK()) {
			blocked = append(blocked, c)
		}
	}
	if len(blocked) > 0 {
		return Plan{}, &BlockError{Coverage: blocked}
	}
	p.Findings = findings(p.After, p.Active, p.Incoming)
	for _, d := range []struct {
		want, have Policy
		dir        string
	}{{in.Desired.Incoming, st.Incoming, "incoming"}, {in.Desired.Outgoing, st.Outgoing, "outgoing"}} {
		if d.want != "" && d.want != d.have {
			p.Steps = append(p.Steps, Step{Kind: StepDefault, Argv: []string{"ufw", "default", string(d.want), d.dir},
				Undo: []string{"ufw", "default", string(d.have), d.dir}, Why: "default " + d.dir + " policy " + string(d.want)})
		}
	}
	if in.Desired.Enable && !active {
		p.Steps = append(p.Steps, Step{Kind: StepEnable, Argv: []string{"ufw", "--force", "enable"},
			Undo: []string{"ufw", "disable"}, Why: "start ufw, after its rules"})
	}
	return p, nil
}
