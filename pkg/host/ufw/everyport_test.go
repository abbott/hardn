package ufw_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// A rule that names no port beside an SSH port open to anyone
// (docs/engine/design-ssh-port-sources.md §10): the host it was seen on had
// "22/tcp ALLOW IN Anywhere", "Anywhere ALLOW IN 10.0.0.0/8" and "22/tcp
// (v6) ALLOW IN Anywhere (v6)".

const (
	open22   = "allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in"
	open22v6 = "allow tcp 22 ::/0 any ::/0 in"
	tenNet   = "allow any any 0.0.0.0/0 any 10.0.0.0/8 in" // no port
)

// TestEveryPortAdds: the new port gets its plain allow, and is let in.
func TestEveryPortAdds(t *testing.T) {
	only4 := []sshd.Listener{listener("0.0.0.0", 22)}
	for _, tc := range []struct {
		name   string
		rules  [][]modelRule
		noIPv6 bool
	}{
		{"the host it was seen on", [][]modelRule{v4(open22, tenNet), v6(open22v6)}, false},
		{"the rule with no port first", [][]modelRule{v4(tenNet, open22), v6(open22v6)}, false},
		{"22 limited from anywhere: counts as open", [][]modelRule{v4("limit tcp 22 0.0.0.0/0 any 0.0.0.0/0 in", tenNet), v6("limit tcp 22 ::/0 any ::/0 in")}, false},
		{"a limit with no port, on an interface, in both versions", [][]modelRule{
			v4(open22, "limit any any 0.0.0.0/0 any 0.0.0.0/0 in_eth1"), v6(open22v6, "allow tcp any ::/0 any fd00::/8 in")}, false},
		{"IPv6 off", [][]modelRule{v4(open22, tenNet)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sourced(t, tc.rules...)
			ls := stock22()
			if tc.noIPv6 {
				m.ipv6, ls = false, sshd.Listeners{SSHD: only4, All: only4, Known: true}
			}
			p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: ls, Add: []uint16{2230}, State: read(t, m)})
			if err != nil {
				t.Fatal(err)
			}
			checkPlan(t, p, []string{"ufw allow 2230/tcp"}, nil)
			for _, c := range p.After {
				if c.Verdict != ufw.Allowed && c.Verdict != ufw.Limited {
					t.Errorf("%s (%s %d) is %s by %q", c.Need.Why, c.Need.Family, c.Need.Port, c.Verdict, c.By)
				}
			}
		})
	}
}

// TestEveryPortExecute: the rule is added and checked, the second plan is
// empty, and the undo deletes it alone.
func TestEveryPortExecute(t *testing.T) {
	ctx := context.Background()
	m := sourced(t, v4(open22, tenNet), v6(open22v6))
	h := m.host()
	before := slices.Clone(m.rules)
	in := func() ufw.Input {
		return ufw.Input{Proxmox: notPVE, Listeners: stock22(), Add: []uint16{2230}, State: read(t, m)}
	}
	p, err := ufw.PlanFor(in())
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err != nil || !rep.Verified {
		t.Fatalf("Execute: %v, Verified %t (%s)", err, rep.Verified, rep.Unverified)
	}
	if got, want := undos(rep), []string{"ufw delete allow 2230/tcp"}; !slices.Equal(got, want) {
		t.Errorf("undo %q, want %q", got, want)
	}
	if len(m.rules) != len(before)+2 {
		t.Errorf("%d rules, want %d", len(m.rules), len(before)+2)
	}
	if again, err := ufw.PlanFor(in()); err != nil || !again.Empty() {
		t.Errorf("second plan %v, %q, want empty", err, argvs(again.Steps))
	}
	if err := ufw.Rollback(ctx, h, rep); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.rules, before) {
		t.Errorf("after the rollback %+v, want %+v", m.rules, before)
	}
}

// TestEveryPortHandsOff: what still keeps hardn's hands off the new port,
// and the refusal names that rule, not the one with no port.
func TestEveryPortHandsOff(t *testing.T) {
	tenOnly := `new port (IPv4 2230) would be restricted by "allow any port from 10.0.0.0/8; no rule added: \"ufw allow 2230/tcp\" would change \"`
	for _, tc := range []struct {
		name  string
		rules [][]modelRule
		words []string // in the refusal
	}{
		{"a rule for the new port from one source", [][]modelRule{v4(open22, "allow tcp 2230 0.0.0.0/0 any 10.0.0.0/8 in"), v6(open22v6)},
			[]string{`(IPv4 2230) would be restricted by "allow 2230/tcp from 10.0.0.0/8";`, `would change \"allow 2230/tcp from 10.0.0.0/8\""`}},
		{"that rule beside one with no port", [][]modelRule{v4(open22, tenNet, "allow tcp 2230 0.0.0.0/0 any 10.0.0.0/8 in"), v6(open22v6)},
			[]string{tenOnly + `allow 2230/tcp from 10.0.0.0/8\"";`, `(IPv6 2230) would be blocked by "default deny (incoming); no rule added: \"ufw allow 2230/tcp\" would change \"allow 2230/tcp from 10.0.0.0/8\""`}},
		{"a range that holds the new port", [][]modelRule{v4(open22, tenNet, "allow tcp 2000:3000 0.0.0.0/0 any 10.0.0.0/8 in"), v6(open22v6)},
			[]string{tenOnly + `allow 2000:3000/tcp from 10.0.0.0/8\"";`}},
		{"an application's rule for the new port", [][]modelRule{v4(open22, tenNet, "allow tcp 2230 0.0.0.0/0 any 10.0.0.0/8 Bastion - in"), v6(open22v6)},
			[]string{tenOnly + `allow 2230/tcp from 10.0.0.0/8\"";`}},
		{"a deny with no port, from one address", [][]modelRule{v4(open22, tenNet, "deny any any 0.0.0.0/0 any 192.0.2.9 in"), v6(open22v6)},
			[]string{tenOnly + `deny any port from 192.0.2.9\"";`}},
		{"a deny with no port, from anywhere", [][]modelRule{v4(open22, tenNet, "deny any any 0.0.0.0/0 any 0.0.0.0/0 in"), v6(open22v6)},
			[]string{tenOnly + `deny any port from anywhere\"";`}},
		{"a reject of the new port", [][]modelRule{v4(open22, tenNet, "reject tcp 2230 0.0.0.0/0 any 0.0.0.0/0 in"), v6(open22v6)},
			[]string{tenOnly + `reject 2230/tcp from anywhere\"";`}},
		{"a rule not read", [][]modelRule{v4(open22, tenNet, "allow tcp 2230: 0.0.0.0/0 any 10.0.0.0/8 in"), v6(open22v6)},
			[]string{`(IPv4 2230) would be unknown by "allow 2230:/tcp from 10.0.0.0/8";`, `would change \"allow 2230:/tcp from 10.0.0.0/8\""`}},
		{"22 open on IPv4 alone, IPv6's rule with no port only", [][]modelRule{v4(open22), v6("allow any any ::/0 any fd00::/8 in")}, nil},
		{"a tcp rule with no port, and a deny", [][]modelRule{v4(open22), v6(open22v6, "allow tcp any ::/0 any fd00::/8 in", "deny any any ::/0 any 2001:db8::9 in")},
			[]string{`(IPv6 2230) would be restricted by "allow any tcp port from fd00::/8; no rule added`, `would change \"deny any port from 2001:db8::9\""`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := carryPlan(t, sourced(t, tc.rules...), ufw.Session{})
			if slices.Contains(argvs(p.Steps), "ufw allow 2230/tcp") {
				t.Fatalf("steps %q", argvs(p.Steps))
			}
			if tc.words == nil {
				return
			}
			var be *ufw.BlockError
			if !errors.As(err, &be) || strings.Contains(err.Error(), "### tuple ### allow any any") {
				t.Fatalf("PlanFor: %v, want a BlockError in words", err)
			}
			for _, w := range tc.words {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("PlanFor: %v\nwant %s", err, w)
				}
			}
		})
	}
}

// TestEveryPortCurrent: a current port not open to anyone in that IP
// version is false, whatever the new port's rules; so is no current port.
func TestEveryPortCurrent(t *testing.T) {
	fw := ufw.Firewall{Active: true, IPv6: true, Incoming: ufw.PolicyDeny}
	for _, tc := range []struct {
		name   string
		rules  []modelRule
		beside []uint16
		want   bool
	}{
		{"22 open", v4(open22, tenNet), []uint16{22}, true},
		{"22 from one source", v4(office, tenNet), []uint16{22}, false},
		{"22 by the rule with no port alone", v4(tenNet), []uint16{22}, false},
		{"22 denied", v4("deny tcp 22 0.0.0.0/0 any 0.0.0.0/0 in", tenNet), []uint16{22}, false},
		{"one of two open", v4(open22, tenNet), []uint16{22, 2222}, false},
		{"no current port", v4(open22, tenNet), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := read(t, sourced(t, tc.rules))
			if got, by := ufw.EveryPort(st, fw, ufw.IPv4, 2230, tc.beside); got != tc.want || by != "" {
				t.Errorf("everyPort %t by %q, want %t", got, by, tc.want)
			}
		})
	}
}

// TestEveryPortCarries: where 22 itself is let in from some sources alone,
// its rules are copied as before, the rule with no port left.
func TestEveryPortCarries(t *testing.T) {
	p, err := carryPlan(t, sourced(t, v4(office, tenNet)), ufw.Session{})
	if err != nil {
		t.Fatal(err)
	}
	checkPlan(t, p, []string{"ufw allow from 10.2.8.0/24 to any port 2230 proto tcp"}, nil)
}

// TestEveryPortOtherCallers: a plan that adds no port keeps the guard:
// there the allow would open SSH itself to anyone.
func TestEveryPortOtherCallers(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active bool
		add    []uint16
	}{
		{"ufw enabled, a rule with no port only", false, nil},
		{"ufw enabled with a port added: inactive, so not judged", false, []uint16{2230}},
		{"active, no port added", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sourced(t, v4(tenNet))
			m.active = tc.active
			p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: stock22(), Add: tc.add, State: read(t, m), Desired: hardening})
			for _, s := range p.Steps {
				if s.Kind == ufw.StepAllow {
					t.Errorf("step %q", s.Argv)
				}
			}
			if tc.active {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ufw.ErrBlocks) || !strings.Contains(err.Error(), `would change \"allow any port from 10.0.0.0/8\"`) {
				t.Errorf("PlanFor: %v", err)
			}
		})
	}
}

// TestEveryPortInactive: with ufw inactive nothing changes: 22 open, the
// rule with no port, a port added, and no allow is saved for it.
func TestEveryPortInactive(t *testing.T) {
	m := sourced(t, v4(open22, tenNet), v6(open22v6))
	m.active = false
	p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: stock22(), Add: []uint16{2230}, State: read(t, m)})
	if err != nil {
		t.Fatal(err)
	}
	if got := argvs(p.Steps); len(got) != 0 {
		t.Errorf("steps %q, want none", got)
	}
}
