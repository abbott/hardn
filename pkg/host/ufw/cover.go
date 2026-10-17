package ufw

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/sshd"
)

// Need is one SSH port and IP version that must be let through.
type Need struct {
	Port   uint16
	Family Family
	// Addr is the listener's address; an unspecified one is every address
	// of Family.
	Addr netip.Addr
	// New: a port the change adds, which nothing listens on yet.
	New bool
	Why string // "listener 0.0.0.0:22", "new port", "kept port"
}

// Needs lists what must be let through: every listener of ls, sshd's and
// ssh.socket's (a socket's [::] is dual-stack, systemd's default, so it
// needs IPv4 too), then add and keep in each IP version those listeners
// use. Keep is for ports the configuration does not show, such as the
// current session's.
func Needs(ls sshd.Listeners, add, keep []uint16) []Need {
	var ns []Need
	seen := map[string]bool{}
	put := func(n Need) {
		k := fmt.Sprint(n.Port, n.Family, n.Addr)
		if !seen[k] {
			seen[k] = true
			ns = append(ns, n)
		}
	}
	listener := func(l sshd.Listener, dual bool) {
		a := l.Addr.Unmap()
		fam := IPv4
		if a.Is6() {
			fam = IPv6
		}
		put(Need{Port: l.Port, Family: fam, Addr: a, Why: "listener " + l.String()})
		if dual && a == netip.IPv6Unspecified() {
			put(Need{Port: l.Port, Family: IPv4, Addr: netip.IPv4Unspecified(), Why: "listener " + l.String() + " (dual-stack)"})
		}
	}
	for _, l := range ls.SSHD {
		listener(l, false)
	}
	for _, l := range ls.Socket {
		listener(l, true)
	}
	fams := []Family{}
	for _, n := range ns {
		if !slices.Contains(fams, n.Family) {
			fams = append(fams, n.Family)
		}
	}
	if len(fams) == 0 {
		fams = []Family{IPv4, IPv6}
	}
	slices.Sort(fams)
	for _, ps := range []struct {
		ports []uint16
		why   string
	}{{add, "new port"}, {keep, "kept port"}} {
		for _, p := range ps.ports {
			for _, f := range fams {
				a := netip.IPv4Unspecified()
				if f == IPv6 {
					a = netip.IPv6Unspecified()
				}
				put(Need{Port: p, Family: f, Addr: a, New: ps.why == "new port", Why: ps.why})
			}
		}
	}
	return ns
}

// Verdict is what the firewall does with new connections to a Need.
type Verdict uint8

const (
	VerdictUnknown Verdict = iota // a rule or the policy could not be read
	Allowed                       // an unrestricted allow rule, or the policy, accepts every source
	Limited                       // an unrestricted limit rule accepts it, rate-limited per source
	Restricted                    // accepted for some sources, addresses or interfaces only
	Blocked                       // no rule accepts it, and the default policy drops or rejects it
	Denied                        // an unrestricted deny or reject rule matches first
)

func (v Verdict) String() string {
	return [...]string{"unknown", "allowed", "limited", "restricted", "blocked", "denied"}[v]
}

// OK: every new connection from anywhere is accepted (rate-limited when
// Limited).
func (v Verdict) OK() bool { return v == Allowed || v == Limited }

// Coverage is a Need's Verdict and what decided it.
type Coverage struct {
	Need    Need
	Verdict Verdict
	By      string // the deciding rule's line, or why no rule decided
}

type match uint8

const (
	noMatch match = iota
	partial       // matches some of the Need's connections
	full          // matches every one of them
	unreadable
)

// matches says which of n's connections (TCP from anywhere) r matches.
func matches(r Rule, n Need) match {
	if r.Family != n.Family || r.Dir != "in" || r.Proto != "tcp" && r.Proto != "any" {
		return noMatch
	}
	if in, ok := portIn(r.DPort, n.Port); !ok {
		return unreadable
	} else if !in {
		return noMatch
	}
	m := full
	if r.Dst != "" {
		p, err := netip.ParsePrefix(r.Dst)
		if err != nil {
			a, aerr := netip.ParseAddr(r.Dst)
			if aerr != nil {
				return unreadable
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		switch {
		case n.Addr.IsUnspecified():
			m = partial // some of the host's addresses only
		case !p.Contains(n.Addr):
			return noMatch
		}
	}
	if r.Src != "" || r.Iface != "" {
		m = partial
	}
	return m
}

// portIn: spec ("" any, "22", "6000:6007", "80,443:450") includes p.
func portIn(spec string, p uint16) (in, ok bool) {
	if spec == "" {
		return true, true
	}
	for _, item := range strings.Split(spec, ",") {
		lo, hi, isRange := strings.Cut(item, ":")
		a, err := strconv.ParseUint(lo, 10, 16)
		b := a
		if err == nil && isRange {
			b, err = strconv.ParseUint(hi, 10, 16)
		}
		if err != nil {
			return false, false
		}
		if a <= uint64(p) && uint64(p) <= b {
			in = true
		}
	}
	return in, true
}

// Evaluate gives each need's Verdict under the rules (each family's in
// order, the first match winning, as ufw's chains do), the incoming
// policy, whether ufw is active (stopped, it accepts everything) and
// whether its IPv6 handling is on (without it, ufw drops all IPv6).
func Evaluate(rules []Rule, active, ipv6 bool, incoming Policy, needs []Need) []Coverage {
	cs := make([]Coverage, len(needs))
	for i, n := range needs {
		v, by := verdict(rules, active, ipv6, incoming, n)
		cs[i] = Coverage{Need: n, Verdict: v, By: by}
	}
	return cs
}

func verdict(rules []Rule, active, ipv6 bool, incoming Policy, n Need) (Verdict, string) {
	if !active {
		return Allowed, "ufw is inactive"
	}
	if n.Family == IPv6 && !ipv6 {
		return Blocked, "IPv6 is off in " + DefaultsFile + ": ufw drops all IPv6"
	}
	var allowSome, denySome string // the first restricted rules of each kind
	for _, r := range rules {
		switch matches(r, n) {
		case unreadable:
			return VerdictUnknown, r.Line
		case partial:
			if r.Action == "allow" || r.Action == "limit" {
				allowSome = first(allowSome, r.Line)
			} else {
				denySome = first(denySome, r.Line)
			}
		case full:
			switch {
			case r.Action != "allow" && r.Action != "limit":
				if allowSome != "" {
					return Restricted, allowSome
				}
				return Denied, r.Line
			case denySome != "":
				return Restricted, denySome
			case r.Action == "limit":
				return Limited, r.Line
			}
			return Allowed, r.Line
		}
	}
	switch {
	case incoming == PolicyAllow && denySome != "":
		return Restricted, denySome
	case incoming == PolicyAllow:
		return Allowed, "default allow (incoming)"
	case incoming.drops() && allowSome != "":
		return Restricted, allowSome
	case incoming.drops():
		return Blocked, "default " + string(incoming) + " (incoming)"
	}
	return VerdictUnknown, "the default incoming policy is not established"
}

// first keeps the first non-empty.
func first(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// FindingKind is a stable code for a cross-check finding.
type FindingKind uint8

const (
	// FindingNotAllowed: ufw is active and an SSH listener is Blocked or
	// Denied. With the default incoming policy deny, p4-risk scores it
	// critical.
	FindingNotAllowed FindingKind = iota + 1
	// FindingRestricted: active, and a listener is accepted for some
	// sources, addresses or interfaces only.
	FindingRestricted
	// FindingUnknown: the verdict, or ufw's status, could not be read.
	FindingUnknown
)

func (k FindingKind) String() string {
	return [...]string{"", "ssh-not-allowed", "ssh-restricted", "ufw-unknown"}[k]
}

// Finding is one cross-check result.
type Finding struct {
	Kind     FindingKind
	Coverage Coverage
	Incoming Policy // the default incoming policy in force
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s (%s %d, %s): %s", f.Kind, f.Coverage.Need.Why, f.Coverage.Need.Family,
		f.Coverage.Need.Port, f.Coverage.Verdict, f.Coverage.By)
}

// Check is the cross-check of what is in force: each need's Verdict under
// "ufw status verbose" (st.Status), and a Finding for each that is not OK
// while ufw is active. A status that was not read whole is one
// FindingUnknown.
func Check(st State, needs []Need) ([]Coverage, []Finding) {
	if !st.Status.Known || len(st.Status.Bad) > 0 {
		return nil, []Finding{{Kind: FindingUnknown, Coverage: Coverage{By: "ufw status verbose was not read whole"}}}
	}
	cs := Evaluate(st.Status.Rules, st.Status.Active, st.IPv6, st.Status.Incoming, needs)
	return cs, findings(cs, st.Status.Active, st.Status.Incoming)
}

func findings(cs []Coverage, active bool, incoming Policy) []Finding {
	var fs []Finding
	for _, c := range cs {
		if !active || c.Verdict.OK() {
			continue
		}
		k := FindingNotAllowed
		switch c.Verdict {
		case Restricted:
			k = FindingRestricted
		case VerdictUnknown:
			k = FindingUnknown
		}
		fs = append(fs, Finding{Kind: k, Coverage: c, Incoming: incoming})
	}
	return fs
}
