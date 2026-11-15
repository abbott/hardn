package ufw_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The carried plan's tests (docs/engine/design-ssh-port-sources.md §3, §5,
// §6, §8). The model answers a "from" rule as ufw 0.36.2's sources read, not
// as a host answered.

const (
	// office and lab are the two sources of the host the bug was seen on.
	office = "allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 in"
	lab    = "allow tcp 22 0.0.0.0/0 any 172.20.8.0/24 in"
)

func v4(tuples ...string) []modelRule {
	var rs []modelRule
	for _, t := range tuples {
		rs = append(rs, saved(ufw.IPv4, t))
	}
	return rs
}

func v6(tuples ...string) []modelRule {
	var rs []modelRule
	for _, t := range tuples {
		rs = append(rs, saved(ufw.IPv6, t))
	}
	return rs
}

// sourced is an active ufw, default deny, with rules.
func sourced(t *testing.T, rules ...[]modelRule) *model {
	m := newModel(t)
	m.active = true
	m.rules = slices.Concat(rules...)
	return m
}

// session is a remote session from client, to 10.2.8.5 (or 2001:db8:8::5),
// its route back over dev.
func session(client, dev string) ufw.Session {
	c := netip.MustParseAddr(client)
	server := netip.MustParseAddr("10.2.8.5")
	if c.Is6() {
		server = netip.MustParseAddr("2001:db8:8::5")
	}
	return ufw.Session{Remote: true, Client: c, Server: server, Dev: dev}
}

func carryPlan(t *testing.T, m *model, s ufw.Session) (ufw.Plan, error) {
	t.Helper()
	return ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: stock22(), Add: []uint16{2230}, State: read(t, m), Session: s,
		Desired: ufw.Desired{NoSaveWhileInactive: true}})
}

// TestCarryRows: one case per row of the page's two tables, each IP version
// on its own, and what is not carried.
func TestCarryRows(t *testing.T) {
	to := " to any port 2230 proto tcp"
	for _, tc := range []struct {
		name    string
		rules   [][]modelRule
		want    []string
		finding []string
	}{
		{"a rule from anywhere, source rules beside it: as today", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in", office), v6("allow tcp 22 ::/0 any ::/0 in")},
			[]string{"ufw allow 2230/tcp"}, nil},
		{"source rules alone, nothing on IPv6: each carried, IPv6 left", [][]modelRule{v4(office, lab)},
			[]string{"ufw allow from 10.2.8.0/24" + to, "ufw allow from 172.20.8.0/24" + to}, nil},
		{"a source on an interface", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 in_eth0")},
			[]string{"ufw allow in on eth0 from 10.2.8.0/24" + to}, nil},
		{"anywhere on an interface, both versions: one command", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in_eth0"), v6("allow tcp 22 ::/0 any ::/0 in_eth0")},
			[]string{"ufw allow in on eth0" + to}, nil},
		{"anywhere on an interface, one version: pinned", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in_eth0")},
			[]string{"ufw allow in on eth0 from 0.0.0.0/0" + to}, nil},
		{"anywhere on an interface, IPv6 alone: pinned", [][]modelRule{v6("allow tcp 22 ::/0 any ::/0 in_eth0")},
			[]string{"ufw allow in on eth0 from ::/0" + to}, nil},
		{"anywhere on an interface in both, IPv6's there already: IPv4's pinned", [][]modelRule{
			v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in_eth0"), v6("allow tcp 22 ::/0 any ::/0 in_eth0", "allow tcp 2230 ::/0 any ::/0 in_eth0")},
			[]string{"ufw allow in on eth0 from 0.0.0.0/0" + to}, nil},
		{"an IPv6 source", [][]modelRule{v6("allow tcp 22 ::/0 any 2001:db8:8::/64 in")},
			[]string{"ufw allow from 2001:db8:8::/64" + to}, nil},
		{"a limit: the action is kept", [][]modelRule{v4("limit tcp 22 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw limit from 10.2.8.0/24" + to}, nil},
		{"a to address", [][]modelRule{v4("allow tcp 22 10.2.8.5 any 10.2.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24 to 10.2.8.5 port 2230 proto tcp"}, nil},
		{"a to address and no source", [][]modelRule{v4("allow tcp 22 10.2.8.5 any 0.0.0.0/0 in")},
			[]string{"ufw allow to 10.2.8.5 port 2230 proto tcp"}, nil},
		{"a range", [][]modelRule{v4("allow tcp 20:30 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"a list", [][]modelRule{v4("allow tcp 22,80 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"a range that holds the new port too: nothing", [][]modelRule{v4("allow tcp 22:3000 0.0.0.0/0 any 10.2.8.0/24 in")},
			nil, nil},
		{"any protocol: tcp", [][]modelRule{v4("allow any 22 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"no port: it lets the new port in already", [][]modelRule{v4("allow any any 0.0.0.0/0 any 10.2.8.0/24 in")},
			nil, nil},
		{"a comment and a log type", [][]modelRule{v4("allow_log tcp 22 0.0.0.0/0 any 10.2.8.0/24 in_eth0 comment=6f6666696365")},
			[]string{"ufw allow in on eth0 log from 10.2.8.0/24" + to + " comment office"}, nil},
		{"log-all, a comment of two words", [][]modelRule{v4("limit_log-all tcp 22 0.0.0.0/0 any 10.2.8.0/24 in comment=6d79206f6666696365")},
			[]string{"ufw limit log-all from 10.2.8.0/24" + to + " comment my office"}, nil},
		{"an application profile: a plain port rule", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 OpenSSH - in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"22 from anywhere on IPv4, sources alone on IPv6: IPv4 pinned, IPv6 never open", [][]modelRule{
			v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in"), v6("allow tcp 22 ::/0 any 2001:db8:8::/64 in")},
			[]string{"ufw allow from 0.0.0.0/0" + to, "ufw allow from 2001:db8:8::/64" + to}, nil},
		{"sources alone on IPv4, from anywhere on IPv6", [][]modelRule{v4(office), v6("allow tcp 22 ::/0 any ::/0 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to, "ufw allow from ::/0" + to}, nil},
		{"from anywhere on IPv6 and the new port there already: IPv4's alone", [][]modelRule{v4(office), v6("allow tcp 22 ::/0 any ::/0 in", "allow tcp 2230 ::/0 any ::/0 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"every source there already, as on the host it was seen on: no step", [][]modelRule{v4(office, lab,
			"allow tcp 2230 0.0.0.0/0 any 10.2.8.0/24 in", "allow tcp 2230 0.0.0.0/0 any 172.20.8.0/24 in")},
			nil, nil},
		{"one of two there: the missing one", [][]modelRule{v4(office, lab, "allow tcp 2230 0.0.0.0/0 any 172.20.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"there under any protocol: counts", [][]modelRule{v4(office, "allow any 2230 0.0.0.0/0 any 10.2.8.0/24 in")},
			nil, nil},
		{"there as a limit: counts, never changed", [][]modelRule{v4(office, "limit tcp 2230 0.0.0.0/0 any 10.2.8.0/24 in")},
			nil, nil},
		{"a udp rule for the source is not it", [][]modelRule{v4(office, "allow udp 2230 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"a rule in the other IP version is not it, and warns there: 22 has no rule in it", [][]modelRule{v4(office), v6("allow tcp 2230 ::/0 any 2001:db8:8::/64 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, []string{"ssh-new-port-wider IPv6 2230"}},
		{"the same source on two ports sshd has: one rule", [][]modelRule{v4(office, "allow tcp 2222 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"the new port open from anywhere already: left, with a warning", [][]modelRule{v4(office, "allow tcp 2230 0.0.0.0/0 any 0.0.0.0/0 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, []string{"ssh-new-port-wider IPv4 2230"}},
		{"the new port let in from a source 22 doesn't have: left, with a warning", [][]modelRule{v4(office, "allow tcp 2230 0.0.0.0/0 any 198.51.100.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, []string{"ssh-new-port-wider IPv4 2230"}},
		{"a deny naming 22 before its allow: not carried, a warning", [][]modelRule{v4("deny tcp 22 0.0.0.0/0 any 10.2.8.9 in", office)},
			[]string{"ufw allow from 10.2.8.0/24" + to}, []string{"ssh-new-port-wider IPv4 2230"}},
		{"a narrower source than 22's on the new port: no warning", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 10.0.0.0/8 in", "allow tcp 2230 0.0.0.0/0 any 10.1.0.0/16 in")},
			[]string{"ufw allow from 10.0.0.0/8" + to}, nil},
		{"22's source on a range that holds the new port: no warning", [][]modelRule{v4(office, "allow tcp 2000:3000 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"22's source on one interface, the new port's on every one: a warning", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 in_eth0", "allow tcp 2230 0.0.0.0/0 any 10.2.8.0/24 in")},
			[]string{"ufw allow in on eth0 from 10.2.8.0/24" + to}, []string{"ssh-new-port-wider IPv4 2230"}},
		{"no rule for 22 on IPv6, the new port open there already: a warning", [][]modelRule{v4(office), v6("allow tcp 2230 ::/0 any ::/0 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, []string{"ssh-new-port-wider IPv6 2230"}},
		{"IPv4 open with a deny above it, IPv6 carried: pinned, a warning", [][]modelRule{
			v4("deny tcp 22 0.0.0.0/0 any 192.0.2.99 in", "allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in"), v6("allow tcp 22 ::/0 any 2001:db8:8::/64 in")},
			[]string{"ufw allow from 0.0.0.0/0" + to, "ufw allow from 2001:db8:8::/64" + to}, []string{"ssh-allowed-except IPv4 22", "ssh-new-port-wider IPv4 2230"}},
		{"IPv4 open with a deny below it, IPv6 carried: no warning", [][]modelRule{
			v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in", "deny tcp 22 0.0.0.0/0 any 192.0.2.99 in"), v6("allow tcp 22 ::/0 any 2001:db8:8::/64 in")},
			[]string{"ufw allow from 0.0.0.0/0" + to, "ufw allow from 2001:db8:8::/64" + to}, nil},
		{"a deny naming 22 after its allow stops nothing the new port lets in: no warning", [][]modelRule{v4(office, "deny tcp 22 0.0.0.0/0 any 0.0.0.0/0 in")},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
		{"a deny with no port covers the new port as it does 22", [][]modelRule{v4("deny any any 0.0.0.0/0 any 10.2.8.9 in", office)},
			[]string{"ufw allow from 10.2.8.0/24" + to}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := sourced(t, tc.rules...)
			p, err := carryPlan(t, m, ufw.Session{})
			if err != nil {
				t.Fatal(err)
			}
			checkPlan(t, p, tc.want, tc.finding)
			for _, s := range p.Steps {
				if s.Kind != ufw.StepAllowFrom {
					continue
				}
				undo := strings.Join(s.Undo, " ")
				add, _, _ := strings.Cut(strings.Join(s.Argv[1:], " "), " comment ")
				if undo != "ufw delete "+add {
					t.Errorf("undo %q of %q", undo, s.Argv)
				}
			}
		})
	}
}

// TestCarryWiderWords: the two warnings name the rule in plain words.
func TestCarryWiderWords(t *testing.T) {
	for _, tc := range []struct {
		rules []modelRule
		want  string
	}{
		{v4(office, "allow tcp 2230 0.0.0.0/0 any 0.0.0.0/0 in"), `ssh-new-port-wider: new port (IPv4 2230, allowed): ufw has "allow 2230/tcp from anywhere" already, which port 22 has not: left as it is`},
		{v4(office, "limit tcp 2230 0.0.0.0/0 any 198.51.100.0/24 in_eth0"), `ssh-new-port-wider: new port (IPv4 2230, restricted): ufw has "limit 2230/tcp from 198.51.100.0/24 on eth0" already, which port 22 has not: left as it is`},
		{v4("deny tcp 22 0.0.0.0/0 any 10.2.8.9 in", office), `ssh-new-port-wider: new port (IPv4 2230, restricted): "deny 22/tcp from 10.2.8.9" is not copied from port 22: port 2230 lets that in`},
	} {
		p, err := carryPlan(t, sourced(t, tc.rules), ufw.Session{})
		if err != nil || len(p.Findings) != 1 || p.Findings[0].String() != tc.want {
			t.Errorf("%v, findings %q, want %q", err, p.Findings, tc.want)
		}
	}
}

// TestCarryTwoListeners: sshd on 22 and 2222, each with its own source: both
// carried, each once.
func TestCarryTwoListeners(t *testing.T) {
	m := sourced(t, v4(office, "allow tcp 2222 0.0.0.0/0 any 172.20.8.0/24 in"))
	ls := stock22()
	ls.SSHD = append(ls.SSHD, listener("0.0.0.0", 2222))
	ls.All = ls.SSHD
	p, err := ufw.PlanFor(ufw.Input{Proxmox: notPVE, Listeners: ls, Add: []uint16{2230}, State: read(t, m), Desired: ufw.Desired{NoSaveWhileInactive: true}})
	want := []string{"ufw allow from 10.2.8.0/24 to any port 2230 proto tcp", "ufw allow from 172.20.8.0/24 to any port 2230 proto tcp"}
	if err != nil || !slices.Equal(argvs(p.Steps), want) {
		t.Errorf("%v, %q, want %q", err, argvs(p.Steps), want)
	}
}

// TestCarryInactive: no rule is saved while ufw is inactive, as before.
func TestCarryInactive(t *testing.T) {
	m := sourced(t, v4(office))
	m.active = false
	p, err := carryPlan(t, m, session("10.9.0.4", "eth0"))
	if err != nil || !p.Empty() {
		t.Errorf("%v, %q", err, argvs(p.Steps))
	}
}

// TestCarryRefuses: a rule that cannot be copied is refused and named; a
// deny on the new port for a carried source is refused as before.
func TestCarryRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules []modelRule
		words string // CopyError's
	}{
		{"a source port", v4(office, "allow tcp 22 0.0.0.0/0 1024 10.2.8.0/24 in"), "22/tcp from 10.2.8.0/24 port 1024"},
		{"a source application", v4("allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 - Bastion in"), "22/tcp from 10.2.8.0/24 app Bastion"},
		{"a source port on an interface, any protocol", v4("allow any 22 0.0.0.0/0 1024 0.0.0.0/0 in_eth0"), "22 on eth0 from port 1024"},
	} {
		_, err := carryPlan(t, sourced(t, tc.rules), ufw.Session{})
		var ce *ufw.CopyError
		if !errors.As(err, &ce) || !errors.Is(err, ufw.ErrCantCopy) || ce.Words() != tc.words || ce.Port != 22 {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.words)
		}
		if err := ufw.CheckSession(read(t, sourced(t, tc.rules)), []uint16{22}, ufw.Session{}); !errors.As(err, &ce) {
			t.Errorf("%s: CheckSession = %v", tc.name, err)
		}
	}
	// A source port beside a rule from anywhere: the version is open, so
	// nothing is copied and nothing refused.
	if p, err := carryPlan(t, sourced(t, v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in", "allow tcp 22 0.0.0.0/0 1024 10.2.8.0/24 in"),
		v6("allow tcp 22 ::/0 any ::/0 in")), ufw.Session{}); err != nil || !slices.Equal(argvs(p.Steps), []string{"ufw allow 2230/tcp"}) {
		t.Errorf("open beside a source port: %v, %q", err, argvs(p.Steps))
	}
	for _, tc := range []struct {
		name  string
		rules []modelRule
	}{
		{"a deny of the new port for the source", v4(office, "deny tcp 2230 0.0.0.0/0 any 10.2.8.0/24 in")},
		{"a deny of the new port for one address of the source", v4(office, "deny tcp 2230 0.0.0.0/0 any 10.2.8.9 in")},
		{"a deny of the new port from anywhere", v4(office, "deny tcp 2230 0.0.0.0/0 any 0.0.0.0/0 in")},
		{"a deny with no port after 22's allow: it would stop the new port's", v4(office, "deny any any 0.0.0.0/0 any 10.2.8.9 in")},
	} {
		_, err := carryPlan(t, sourced(t, tc.rules), ufw.Session{})
		var be *ufw.BlockError
		if !errors.As(err, &be) || !errors.Is(err, ufw.ErrBlocks) {
			t.Errorf("%s: %v, want a BlockError", tc.name, err)
		}
	}
}

// TestCarrySession: the session's own address must be let in on the new
// port by the rules as they will be; loopback and a console have no check;
// a rule on an interface counts by the route back.
func TestCarrySession(t *testing.T) {
	eth0 := v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in_eth0")
	for _, tc := range []struct {
		name    string
		rules   [][]modelRule
		sess    ufw.Session
		refused bool
		unread  bool
	}{
		{"inside a carried rule", [][]modelRule{v4(office, lab)}, session("10.2.8.77", "eth0"), false, false},
		{"outside every carried rule", [][]modelRule{v4(office, lab)}, session("10.9.0.4", "eth0"), true, false},
		{"a console", [][]modelRule{v4(office)}, ufw.Session{}, false, false},
		{"a remote session whose address is not read", [][]modelRule{v4(office)}, ufw.Session{Remote: true}, true, true},
		{"loopback: ufw accepts lo first", [][]modelRule{v4(office)}, session("127.0.0.1", ""), false, false},
		{"IPv6 loopback", [][]modelRule{v4(office)}, session("::1", ""), false, false},
		{"the route back is over lo", [][]modelRule{v4(office)}, session("10.2.8.5", "lo"), false, false},
		{"a rule on the interface of the route back", [][]modelRule{eth0}, session("10.9.0.4", "eth0"), false, false},
		{"a rule on another interface", [][]modelRule{eth0}, session("10.9.0.4", "eth1"), true, false},
		{"a rule on an interface, the route not read", [][]modelRule{eth0}, session("10.9.0.4", ""), true, false},
		{"a rule to another address of the host", [][]modelRule{v4("allow tcp 22 10.2.9.5 any 10.2.8.0/24 in")}, session("10.2.8.77", "eth0"), true, false},
		{"a rule to the address the session is on", [][]modelRule{v4("allow tcp 22 10.2.8.5 any 10.2.8.0/24 in")}, session("10.2.8.77", "eth0"), false, false},
		{"let in by a rule with no port", [][]modelRule{v4(office, "allow any any 0.0.0.0/0 any 10.9.0.0/16 in")}, session("10.9.0.4", "eth0"), false, false},
		{"a deny before the rule that carries it", [][]modelRule{v4("deny any any 0.0.0.0/0 any 10.2.8.77 in", office)}, session("10.2.8.77", "eth0"), true, false},
		{"IPv4 open, IPv6 carried, from IPv4", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in"), v6("allow tcp 22 ::/0 any 2001:db8:8::/64 in")}, session("10.9.0.4", "eth0"), false, false},
		{"IPv4 open, IPv6 carried, from outside IPv6's source", [][]modelRule{v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in"), v6("allow tcp 22 ::/0 any 2001:db8:8::/64 in")}, session("2001:db8:9::4", "eth0"), true, false},
		{"IPv4 carried, no rule on IPv6, from IPv6", [][]modelRule{v4(office)}, session("2001:db8:9::4", "eth0"), true, false},
	} {
		m := sourced(t, tc.rules...)
		_, err := carryPlan(t, m, tc.sess)
		check := ufw.CheckSession(read(t, m), []uint16{22}, tc.sess)
		var se *ufw.SessionError
		switch {
		case !tc.refused && (err != nil || check != nil):
			t.Errorf("%s: PlanFor %v, CheckSession %v", tc.name, err, check)
		case tc.refused && (!errors.As(err, &se) || !errors.Is(check, ufw.ErrSessionSource) || se.Unread != tc.unread):
			t.Errorf("%s: PlanFor %v, CheckSession %v, want a SessionError (unread %v)", tc.name, err, check, tc.unread)
		case tc.refused && !tc.unread && (se.Client != tc.sess.Client || se.Port != 22):
			t.Errorf("%s: %+v", tc.name, se)
		}
	}
	// Covered by 22's rules, but denied on the new port itself: refused as a
	// block, in the planner alone.
	m := sourced(t, v4(office, "deny tcp 2230 0.0.0.0/0 any 10.2.8.77 in"))
	if err := ufw.CheckSession(read(t, m), []uint16{22}, session("10.2.8.77", "eth0")); err != nil {
		t.Errorf("CheckSession, before a port is named: %v", err)
	}
	if _, err := carryPlan(t, m, session("10.2.8.77", "eth0")); !errors.Is(err, ufw.ErrBlocks) {
		t.Errorf("a deny of the new port for the session: %v", err)
	}
	// Nothing carried: no check at all, the session's address read or not.
	open := sourced(t, v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in"), v6("allow tcp 22 ::/0 any ::/0 in"))
	if _, err := carryPlan(t, open, ufw.Session{Remote: true}); err != nil {
		t.Errorf("nothing carried: %v", err)
	}
	if ufw.Carries(read(t, open), []uint16{22}) || !ufw.Carries(read(t, sourced(t, v4(office))), []uint16{22}) {
		t.Error("Carries")
	}
}

// TestRouteDev: the dev of "ip route get", a link-local address's zone, ""
// where ip fails.
func TestRouteDev(t *testing.T) {
	ctx := context.Background()
	answer := "10.9.0.4 via 10.2.8.1 dev eth0 src 10.2.8.5 uid 0 \n    cache \n"
	h := newModel(t).host()
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		if strings.Join(argv, " ") == "ip route get 10.9.0.4" {
			return hostfake.Response{Stdout: answer}
		}
		return hostfake.Response{ExitCode: 2, Stderr: "RTNETLINK answers: Network is unreachable\n"}
	}
	if got := ufw.RouteDev(ctx, h, netip.MustParseAddr("10.9.0.4")); got != "eth0" {
		t.Errorf("dev %q", got)
	}
	answer = "local 10.2.8.5 dev lo src 10.2.8.5 uid 0 \n    cache <local> \n"
	if got := ufw.RouteDev(ctx, h, netip.MustParseAddr("10.9.0.4")); got != "lo" {
		t.Errorf("local: dev %q", got)
	}
	if got := ufw.RouteDev(ctx, h, netip.MustParseAddr("198.51.100.1")); got != "" {
		t.Errorf("ip failed: dev %q", got)
	}
	if got := ufw.RouteDev(ctx, h, netip.MustParseAddr("fe80::1%eth1")); got != "eth1" {
		t.Errorf("zone: dev %q", got)
	}
	if got := ufw.RouteDev(ctx, h, netip.Addr{}); got != "" {
		t.Errorf("no address: dev %q", got)
	}
}

// TestParseTuplesCarried: the comment is hex, decoded whole (non-ASCII, a
// quote); the log type is read; a comment that is not hex is a line not
// read.
func TestParseTuplesCarried(t *testing.T) {
	data := "### tuple ### allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 in comment=6f6666696365\n" +
		"### tuple ### allow_log tcp 22 0.0.0.0/0 any 10.2.8.0/24 in_eth0 comment=62c3bc726f\n" +
		"### tuple ### limit_log-all tcp 22 0.0.0.0/0 any 10.2.8.0/24 in comment=626f62277320226f666669636522\n" +
		"### tuple ### allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 in\n" +
		"### tuple ### allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 in comment=office\n"
	rs, bad := ufw.ParseTuples([]byte(data), ufw.IPv4)
	if len(rs) != 4 || len(bad) != 1 || !strings.HasSuffix(bad[0], "comment=office") {
		t.Fatalf("%d rules, bad %q", len(rs), bad)
	}
	for i, want := range []struct{ comment, log, action, iface string }{
		{"office", "", "allow", ""}, {"büro", "log", "allow", "eth0"}, {`bob's "office"`, "log-all", "limit", ""}, {"", "", "allow", ""},
	} {
		if r := rs[i]; r.Comment != want.comment || r.Log != want.log || r.Action != want.action || r.Iface != want.iface || r.Src != "10.2.8.0/24" {
			t.Errorf("rule %d: %+v", i, r)
		}
	}
	// A log type that is not ufw's would reach ufw's command as a word of
	// its own ("allow out ..."): the line is not read.
	for _, tuple := range []string{"allow_out tcp 22 0.0.0.0/0 any 10.2.8.0/24 in", "allow_log_all tcp 22 0.0.0.0/0 any 10.2.8.0/24 in"} {
		if rs, bad := ufw.ParseTuples([]byte("### tuple ### "+tuple+"\n"), ufw.IPv4); len(rs) != 0 || len(bad) != 1 {
			t.Errorf("%q: %d rules, bad %q", tuple, len(rs), bad)
		}
	}
	// A comment with a quote and one that is not ASCII are each one argument.
	m := sourced(t, v4("allow tcp 22 0.0.0.0/0 any 10.2.8.0/24 in comment=626f62277320226f666669636522", "allow tcp 22 0.0.0.0/0 any 172.20.8.0/24 in comment=62c3bc726f"))
	p, err := carryPlan(t, m, ufw.Session{})
	if err != nil || len(p.Steps) != 2 || !slices.Equal(p.Steps[0].Argv[len(p.Steps[0].Argv)-2:], []string{"comment", `bob's "office"`}) ||
		p.Steps[1].Argv[len(p.Steps[1].Argv)-1] != "büro" || slices.Contains(p.Steps[0].Undo, "comment") {
		t.Errorf("%v, %q", err, argvs(p.Steps))
	}
}

// TestCarryExecute: the host the bug was seen on, with nothing on 2230 yet.
// The two rules are added, 22's are never touched, ufw shows the plan's
// state, the second plan is empty, and the undo deletes exactly the two.
func TestCarryExecute(t *testing.T) {
	ctx := context.Background()
	m := sourced(t, v4(office, lab))
	h := m.host()
	before := slices.Clone(m.rules)
	p, err := carryPlan(t, m, session("10.2.8.77", "eth0"))
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err != nil || !rep.Verified {
		t.Fatalf("Execute: %v, Verified %t (%s)", err, rep.Verified, rep.Unverified)
	}
	want := []string{"ufw allow from 10.2.8.0/24 to any port 2230 proto tcp", "ufw allow from 172.20.8.0/24 to any port 2230 proto tcp", "ufw status verbose"}
	if got := runs(h); !slices.Equal(got, want) {
		t.Errorf("runs\n got %q\nwant %q", got, want)
	}
	wantUndo := []string{"ufw delete allow from 172.20.8.0/24 to any port 2230 proto tcp", "ufw delete allow from 10.2.8.0/24 to any port 2230 proto tcp"}
	if got := undos(rep); !slices.Equal(got, wantUndo) {
		t.Errorf("undo\n got %q\nwant %q", got, wantUndo)
	}
	if len(m.rules) != 4 {
		t.Fatalf("%d rules, want 4", len(m.rules))
	}
	if again, err := carryPlan(t, m, session("10.2.8.77", "eth0")); err != nil || !again.Empty() {
		t.Errorf("second plan %v, %q, want empty", err, argvs(again.Steps))
	}
	if err := ufw.Rollback(ctx, h, rep); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(m.rules, before) {
		t.Errorf("after the rollback %+v, want %+v", m.rules, before)
	}
}

// TestCarryUndo: the undo from each of ufw's answers. A rule that was there
// is never deleted; a limit and a log type are kept in the delete, the
// comment dropped; the rollback removes the rules hardn added and no other.
func TestCarryUndo(t *testing.T) {
	ctx := context.Background()
	// One of two there by hand, and a rule of the user's for another source.
	m := sourced(t, v4("limit_log tcp 22 0.0.0.0/0 any 10.2.8.0/24 in_eth0 comment=6f6666696365", lab,
		"allow tcp 2230 0.0.0.0/0 any 172.20.8.0/24 in", "allow tcp 2230 0.0.0.0/0 any 198.51.100.0/24 in"))
	h := m.host()
	before := slices.Clone(m.rules)
	p, err := carryPlan(t, m, ufw.Session{})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err != nil || !rep.Verified {
		t.Fatalf("Execute: %v, Verified %t (%s)", err, rep.Verified, rep.Unverified)
	}
	add := "ufw limit in on eth0 log from 10.2.8.0/24 to any port 2230 proto tcp comment office"
	del := "ufw delete limit in on eth0 log from 10.2.8.0/24 to any port 2230 proto tcp"
	if got := runs(h); !slices.Equal(got, []string{add, "ufw status verbose"}) {
		t.Errorf("runs %q", got)
	}
	if got := undos(rep); !slices.Equal(got, []string{del}) {
		t.Errorf("undo %q, want %q", got, del)
	}
	if err := ufw.Rollback(ctx, h, rep); err != nil || !slices.Equal(m.rules, before) {
		t.Errorf("rollback %v: %+v, want %+v", err, m.rules, before)
	}

	// ufw's other answers: skipped (the rule appeared since the plan), and
	// words that do not show it added the rule alone. Neither is undone.
	for _, tc := range []struct{ name, answer, note string }{
		{"skipped", "Skipping adding existing rule\n", "ufw kept an existing rule"},
		{"updated", "Rule updated\n", "not undone"},
		{"added and skipped", "Rule added\nSkipping adding existing rule (v6)\n", "not undone"},
	} {
		m := sourced(t, v4(office, "allow tcp 2230 0.0.0.0/0 any 10.2.8.0/24 in"))
		p, err := carryPlan(t, sourced(t, v4(office)), ufw.Session{})
		if err != nil {
			t.Fatal(err)
		}
		h := m.host()
		inner := h.Handler
		h.Handler = func(argv []string, in []byte) hostfake.Response {
			if len(argv) > 1 && argv[1] == "allow" {
				return hostfake.Response{Stdout: tc.answer}
			}
			return inner(argv, in)
		}
		rep, err := ufw.Execute(ctx, h, p)
		if err != nil || len(rep.Undo) != 0 || len(rep.Steps) != 1 || !strings.Contains(rep.Steps[0].Note, tc.note) {
			t.Errorf("%s: %v, undo %q, steps %+v", tc.name, err, rep.Undo, rep.Steps)
		}
	}

	// The command for both IP versions: both answers, one delete for both.
	m = sourced(t, v4("allow tcp 22 0.0.0.0/0 any 0.0.0.0/0 in_eth0"), v6("allow tcp 22 ::/0 any ::/0 in_eth0"))
	h = m.host()
	p, err = carryPlan(t, m, ufw.Session{})
	if err != nil {
		t.Fatal(err)
	}
	rep, err = ufw.Execute(ctx, h, p)
	if err != nil || !rep.Verified || !slices.Equal(undos(rep), []string{"ufw delete allow in on eth0 to any port 2230 proto tcp"}) || len(m.rules) != 4 {
		t.Errorf("both versions: %v, verified %v, undo %q, %d rules", err, rep.Verified, undos(rep), len(m.rules))
	}
	if err := ufw.Rollback(ctx, h, rep); err != nil || len(m.rules) != 2 {
		t.Errorf("both versions, rollback: %v, %d rules", err, len(m.rules))
	}
}

// TestCarryFailsBetweenSteps: the second rule fails. The first is undone by
// its own delete, the failed one is not, and 22's rules were never named.
func TestCarryFailsBetweenSteps(t *testing.T) {
	ctx := context.Background()
	m := sourced(t, v4(office, lab))
	m.fail = "ufw allow from 172.20.8.0/24 to any port 2230 proto tcp"
	h := m.host()
	p, err := carryPlan(t, m, ufw.Session{})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := ufw.Execute(ctx, h, p)
	if err == nil || rep.Verified {
		t.Fatalf("Execute: %v", err)
	}
	if got := undos(rep); !slices.Equal(got, []string{"ufw delete allow from 10.2.8.0/24 to any port 2230 proto tcp"}) {
		t.Errorf("undo %q", got)
	}
	for _, r := range runs(h) {
		if strings.Contains(r, "port 22 ") || strings.Contains(r, " 22/tcp") {
			t.Errorf("ran %q", r)
		}
	}
	if err := ufw.Rollback(ctx, h, rep); err != nil {
		t.Fatal(err)
	}
	st := read(t, m)
	if cs, _ := ufw.Check(st, ufw.Needs(stock22(), nil, nil)); len(m.rules) != 2 || cs[0].Verdict != ufw.Restricted {
		t.Errorf("after the rollback: %d rules, 22 is %v", len(m.rules), cs)
	}
}
