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

// Desired is the state the plan leaves besides the SSH rules.
type Desired struct {
	Enable   bool   // start ufw when it is inactive
	Incoming Policy // the default incoming policy; "" leaves it
	Outgoing Policy // the default outgoing policy; "" leaves it
	// NoSaveWhileInactive: with ufw inactive and staying so, plan no allow
	// rule. By default the rules are saved (ufw writes them to its rule
	// files without loading them), so that a later "ufw enable" keeps SSH
	// open; whether hardn adds rules the user did not ask for is V8's open
	// approach, proposal P-33.
	NoSaveWhileInactive bool
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
	StepAllow    StepKind = iota + 1 // ufw allow <port>/tcp
	StepDefault                      // ufw default <policy> incoming|outgoing
	StepEnable                       // ufw --force enable
	StepDisable                      // ufw disable (PlanDisable)
	StepAllowApp                     // ufw allow from any to any app <name> (PlanApps)
)

var stepNames = [...]string{"unknown", "allow", "default", "enable", "disable", "allow-app"}

func (k StepKind) String() string {
	if int(k) >= len(stepNames) {
		return "unknown"
	}
	return stepNames[k]
}

// allows: the step adds a rule, so its undo depends on ufw's answer.
func (k StepKind) allows() bool { return k == StepAllow || k == StepAllowApp }

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
	// What ufw shows once the steps ran: Execute checks it.
	Active             bool
	Incoming, Outgoing Policy
	IPv6, NoKernelIPv6 bool
	// Findings are the Needs that are not OK now and stay so, which the
	// plan does not cause and does not hide; with ufw staying inactive,
	// those its saved rules would not let through once it starts
	// (FindingAtStart); and a new port opened wider than a listener now
	// (FindingWidened).
	Findings []Finding
}

// Empty: nothing to run (the second, idempotent plan).
func (p Plan) Empty() bool { return len(p.Steps) == 0 }

func (p Plan) firewall() Firewall {
	return Firewall{Active: p.Active, IPv6: p.IPv6, NoKernelIPv6: p.NoKernelIPv6, Incoming: p.Incoming}
}

// PreUndo are the undos of p's StepDefault, StepEnable and StepDisable
// steps, last first: safe to journal before Execute runs (p2-revert), as each puts back
// the state before the plan and does no harm when its step never ran. An
// allow's undo depends on ufw's answer (Report.Undo); an allow left behind
// never locks anyone out.
func (p Plan) PreUndo() [][]string {
	var u [][]string
	for _, s := range p.Steps {
		if s.Kind == StepDefault || s.Kind == StepEnable || s.Kind == StepDisable {
			u = append([][]string{s.Undo}, u...)
		}
	}
	return u
}

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
// is not: an explicit deny, a restricted allow or deny, IPv6 off in ufw, or
// a rule it cannot read. Errors: ErrProxmoxUnknown, ErrListenersUnknown,
// ErrNotInstalled, ErrStateUnknown, ErrEnabledMismatch, fs.ErrInvalid for a
// bad Desired or port.
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
	case st.Enabled != st.Status.Active:
		// Enabled but stopped, each allow would fail after saving its rule
		// ("Could not update running firewall"), and the next boot loads
		// the saved rules; loaded but not enabled, an allow is saved, not
		// loaded, while the status shows it.
		enabled, status := "no", "inactive"
		if st.Enabled {
			enabled = "yes"
		}
		if st.Status.Active {
			status = "active"
		}
		return Plan{}, fmt.Errorf("ufw: %w: ENABLED=%s (%s), status %s", ErrEnabledMismatch, enabled, ConfFile, status)
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
	p := Plan{Needs: needs, Active: active || in.Desired.Enable, Incoming: st.Incoming, Outgoing: st.Outgoing,
		IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6}
	if in.Desired.Incoming != "" {
		p.Incoming = in.Desired.Incoming
	}
	if in.Desired.Outgoing != "" {
		p.Outgoing = in.Desired.Outgoing
	}
	p.Before = Evaluate(st.Rules, Firewall{Active: active, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Incoming}, needs)
	started := p.firewall() // once ufw is active, with the planned policy
	started.Active = true
	ports, handsOff := allowPorts(st, started, needs)
	if !p.Active && in.Desired.NoSaveWhileInactive {
		ports = nil
	}
	rules := slices.Clone(st.Rules)
	for _, port := range ports {
		spec := strconv.Itoa(int(port)) + "/tcp"
		for _, f := range writes(st) {
			rules = append(rules, Rule{Action: "allow", Dir: "in", Proto: "tcp", DPort: strconv.Itoa(int(port)), Family: f, Line: "ufw allow " + spec})
		}
		p.Steps = append(p.Steps, Step{Kind: StepAllow, Port: port, Argv: []string{"ufw", "allow", spec},
			Undo: []string{"ufw", "delete", "allow", spec}, Why: "let SSH port " + spec + " through"})
	}
	p.After = note(Evaluate(rules, p.firewall(), needs), handsOff)
	var blocked []Coverage
	for i, c := range p.After {
		if !c.Verdict.OK() && (c.Need.New || p.Before[i].Verdict.OK()) {
			blocked = append(blocked, c)
		}
	}
	if len(blocked) > 0 {
		return Plan{}, &BlockError{Coverage: blocked}
	}
	later := note(Evaluate(rules, started, needs), handsOff)
	p.Findings = findings(p.After, p.Active, p.Incoming)
	if !p.Active {
		p.Findings = append(p.Findings, atStart(later, p.Incoming)...)
	}
	p.Findings = append(p.Findings, widened(later, ports, p.Incoming)...)
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

// PlanDisable plans, with no I/O, "ufw disable", with "ufw --force enable"
// its undo: decision 6's exception to PlanFor's "never ufw disable"
// (docs/safety/ufw.md §2.7). Its rules stay saved, and it never resets. An
// inactive ufw plans nothing. It reads the status line alone, so that a ufw
// whose rule files hold a line not read still has a way off. Errors:
// ErrProxmoxUnknown, ErrNotInstalled, ErrStateUnknown for a status not read.
func PlanDisable(st State, pve detect.Proxmox) (Plan, error) {
	if err := Applies(pve); errors.Is(err, ErrNotApplicable) {
		return Plan{NotApplicable: true}, nil
	} else if err != nil {
		return Plan{}, err
	}
	switch {
	case !st.Installed:
		return Plan{}, fmt.Errorf("ufw: %w", ErrNotInstalled)
	case !st.Status.Known:
		return Plan{}, fmt.Errorf("ufw: %w (unread lines %q)", ErrStateUnknown, st.Status.Bad)
	case !st.Status.Active:
		return Plan{}, nil
	}
	return Plan{Steps: []Step{{Kind: StepDisable, Argv: []string{"ufw", "disable"},
		Undo: []string{"ufw", "--force", "enable"}, Why: "stop ufw, its rules left saved"}}}, nil
}

// writes are the IP versions "ufw allow <port>/tcp" writes a rule for:
// IPv4 and, with ufw's IPv6 on, IPv6, whichever the listeners use.
func writes(st State) []Family {
	if st.IPv6 {
		return []Family{IPv4, IPv6}
	}
	return []Family{IPv4}
}

// allowPorts are the ports that get "ufw allow <port>/tcp": those a need is
// Blocked on once ufw is active (fw), even while it stays inactive, so that
// a later enable keeps them open. Not a port on which the allow would
// change a saved rule (handsOff, with that rule's line).
func allowPorts(st State, fw Firewall, needs []Need) (ports []uint16, handsOff map[uint16]string) {
	handsOff = map[uint16]string{}
	for _, c := range Evaluate(st.Rules, fw, needs) {
		port := c.Need.Port
		if _, done := handsOff[port]; done || c.Verdict != Blocked || slices.Contains(ports, port) ||
			c.Need.Family == IPv6 && !st.IPv6 {
			continue
		}
		if why := changes(st, fw, port); why != "" {
			handsOff[port] = why
			continue
		}
		ports = append(ports, port)
	}
	slices.Sort(ports)
	return ports, handsOff
}

// changes is the line of a saved rule that "ufw allow <port>/tcp" would
// change, in any family it writes, or "". A port Denied, Restricted or
// unreadable from anywhere would be widened or turned round for the sake of
// the other family; and ufw replaces a saved rule of the same tuple that
// differs in its action, log type or comment (a limit, deny or reject of
// <port>/tcp from anywhere) with the allow, instead of adding one.
func changes(st State, fw Firewall, port uint16) string {
	dport := strconv.Itoa(int(port))
	for _, f := range writes(st) {
		c := Evaluate(st.Rules, fw, []Need{{Port: port, Family: f, Addr: anywhere(f)}})[0]
		switch c.Verdict {
		case Denied, Restricted, VerdictUnknown:
			return c.By
		}
		wild := "0.0.0.0/0"
		if f == IPv6 {
			wild = "::/0"
		}
		same := "### tuple ### allow tcp " + dport + " " + wild + " any " + wild + " in"
		for _, r := range st.Rules {
			if r.Family == f && r.Dir == "in" && r.Proto == "tcp" && r.DPort == dport && r.Dst == "" &&
				r.Src == "" && r.Iface == "" && r.App == "" && strings.Join(strings.Fields(r.Line), " ") != same {
				return r.Line
			}
		}
	}
	return ""
}

// note says, on a need left Blocked on a hands-off port, why no rule was
// added.
func note(cs []Coverage, handsOff map[uint16]string) []Coverage {
	for i, c := range cs {
		if why, ok := handsOff[c.Need.Port]; ok && c.Verdict == Blocked {
			cs[i].By = fmt.Sprintf("%s; no rule added: \"ufw allow %d/tcp\" would change %q", c.By, c.Need.Port, why)
		}
	}
	return cs
}

// widened is a FindingWidened for each new port the plan lets through from
// anywhere (later: once ufw is active) while a listener of its IP version
// is Restricted or Excepted.
func widened(later []Coverage, ports []uint16, incoming Policy) []Finding {
	var fs []Finding
	for _, c := range later {
		if !c.Need.New || !c.Verdict.OK() || !slices.Contains(ports, c.Need.Port) {
			continue
		}
		for _, l := range later {
			if !l.Need.New && l.Need.Family == c.Need.Family && (l.Verdict == Restricted || l.Verdict == Excepted) {
				c.By = fmt.Sprintf("\"ufw allow %d/tcp\" lets it through from anywhere, while %s is %s by %q",
					c.Need.Port, l.Need.Why, l.Verdict, l.By)
				fs = append(fs, Finding{Kind: FindingWidened, Coverage: c, Incoming: incoming})
				break
			}
		}
	}
	return fs
}
