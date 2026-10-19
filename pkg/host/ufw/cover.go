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
				put(Need{Port: p, Family: f, Addr: anywhere(f), New: ps.why == "new port", Why: ps.why})
			}
		}
	}
	return ns
}

// anywhere is f's unspecified address: every address of the host.
func anywhere(f Family) netip.Addr {
	if f == IPv6 {
		return netip.IPv6Unspecified()
	}
	return netip.IPv4Unspecified()
}

// Verdict is what the firewall does with new connections to a Need.
type Verdict uint8

const (
	VerdictUnknown Verdict = iota // a rule or the policy could not be read
	Allowed                       // an unrestricted allow rule, the policy, or loopback accepts every source
	Limited                       // an unrestricted limit rule accepts it, rate-limited per source
	Restricted                    // accepted for some sources, addresses or interfaces only
	Blocked                       // no rule accepts it, and the default policy drops or rejects it
	Denied                        // an unrestricted deny or reject rule matches first
	// Excepted: accepted from everywhere but some sources, addresses or
	// interfaces, which a restricted deny or reject rule matches first (a
	// blocklist).
	Excepted
	// Unreachable: IPv6 on a kernel without IPv6; no connection can arrive.
	Unreachable
)

var verdictNames = [...]string{"unknown", "allowed", "limited", "restricted", "blocked", "denied", "excepted", "unreachable"}

func (v Verdict) String() string {
	if int(v) >= len(verdictNames) {
		return "unknown"
	}
	return verdictNames[v]
}

// OK: nothing to let through. Every new connection from anywhere is
// accepted (rate-limited when Limited), or none can arrive (Unreachable).
// Excepted is not OK: the session's own address may be among the
// exceptions, which this package cannot tell.
func (v Verdict) OK() bool { return v == Allowed || v == Limited || v == Unreachable }

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

// Firewall is what decides a Verdict besides the rules.
type Firewall struct {
	Active bool // ufw's chains are loaded; stopped, ufw accepts everything
	// IPv6: ufw's IPv6 handling is on (State.IPv6); without it, started,
	// ufw drops all IPv6 but loopback. NoKernelIPv6: the kernel has no
	// IPv6, so an IPv6 need is Unreachable.
	IPv6, NoKernelIPv6 bool
	Incoming           Policy // the default incoming policy
}

// Evaluate gives each need's Verdict under the rules (each family's in
// order, the first match winning, as ufw's chains do) and fw. A loopback
// listener is Allowed while ufw is active: before.rules accepts lo before
// any user rule.
func Evaluate(rules []Rule, fw Firewall, needs []Need) []Coverage {
	cs := make([]Coverage, len(needs))
	for i, n := range needs {
		v, by := verdict(rules, fw, n)
		cs[i] = Coverage{Need: n, Verdict: v, By: by}
	}
	return cs
}

func verdict(rules []Rule, fw Firewall, n Need) (Verdict, string) {
	switch {
	case !fw.Active:
		return Allowed, "ufw is inactive"
	case n.Family == IPv6 && fw.NoKernelIPv6:
		return Unreachable, "the kernel has no IPv6 (" + KernelIPv6 + " is missing)"
	case n.Addr.IsLoopback():
		return Allowed, "loopback: ufw's before.rules accepts lo first"
	case n.Family == IPv6 && !fw.IPv6:
		return Blocked, "IPV6 is not yes in " + DefaultsFile + " or " + ConfFile + ": once started, ufw drops all IPv6"
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
				return Excepted, denySome
			case r.Action == "limit":
				return Limited, r.Line
			}
			return Allowed, r.Line
		}
	}
	switch incoming := fw.Incoming; {
	case incoming == PolicyAllow && denySome != "":
		return Excepted, denySome
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
	// FindingExcepted: active, and a listener is accepted from everywhere
	// but some sources, addresses or interfaces (Excepted).
	FindingExcepted
	// FindingAtStart: ufw is inactive, and once it starts (at boot with
	// ENABLED=yes, or a later "ufw enable") its saved rules would not let
	// the listener through; Coverage is the verdict then.
	FindingAtStart
	// FindingWidened: the plan lets a new port through from anywhere while
	// a listener of the same IP version is Restricted or Excepted now
	// (proposal P-33).
	FindingWidened
)

var findingNames = [...]string{"", "ssh-not-allowed", "ssh-restricted", "ufw-unknown", "ssh-allowed-except",
	"ssh-not-allowed-at-start", "ssh-new-port-wider"}

func (k FindingKind) String() string {
	if int(k) >= len(findingNames) {
		return "unknown"
	}
	return findingNames[k]
}

// Finding is one cross-check result.
type Finding struct {
	Kind     FindingKind
	Coverage Coverage
	Incoming Policy // the default incoming policy the verdicts were made under
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s (%s %d, %s): %s", f.Kind, f.Coverage.Need.Why, f.Coverage.Need.Family,
		f.Coverage.Need.Port, f.Coverage.Verdict, f.Coverage.By)
}

// Check is the cross-check of what ufw shows: each need's Verdict under
// "ufw status verbose" (st.Status: the saved rules and policies, with the
// chains loaded), and a Finding for each that is not OK while ufw is
// active. Inactive with ENABLED=yes, ufw starts at boot: each need its
// saved rules would not let through then is a FindingAtStart. A status that
// was not read whole is one FindingUnknown. ufw not installed (!Installed)
// is neither: no coverage and no finding, so the caller reports it as not
// configured.
func Check(st State, needs []Need) ([]Coverage, []Finding) {
	if !st.Installed {
		return nil, nil
	}
	if !st.Status.Known || len(st.Status.Bad) > 0 {
		return nil, []Finding{{Kind: FindingUnknown, Coverage: Coverage{By: "ufw status verbose was not read whole"}}}
	}
	fw := Firewall{Active: st.Status.Active, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Status.Incoming}
	cs := Evaluate(st.Status.Rules, fw, needs)
	fs := findings(cs, fw.Active, fw.Incoming)
	if !fw.Active && st.Enabled && st.Known() {
		fw.Active, fw.Incoming = true, st.Incoming
		fs = append(fs, atStart(Evaluate(st.Rules, fw, needs), fw.Incoming)...)
	}
	return cs, fs
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
		case Excepted:
			k = FindingExcepted
		case VerdictUnknown:
			k = FindingUnknown
		}
		fs = append(fs, Finding{Kind: k, Coverage: c, Incoming: incoming})
	}
	return fs
}

// atStart is a FindingAtStart for each coverage, under an active ufw,
// that is not OK.
func atStart(cs []Coverage, incoming Policy) []Finding {
	var fs []Finding
	for _, c := range cs {
		if !c.Verdict.OK() {
			fs = append(fs, Finding{Kind: FindingAtStart, Coverage: c, Incoming: incoming})
		}
	}
	return fs
}
