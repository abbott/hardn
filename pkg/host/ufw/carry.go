package ufw

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// Carrying a port's sources (docs/engine/design-ssh-port-sources.md, adopted
// 2026-11-15). Where an active ufw lets a port sshd listens on in from some
// sources alone, a port the change adds gets each of those rules for itself,
// "ufw allow from <source> to any port <new> proto tcp", instead of "ufw
// allow <new>/tcp", which would open it to anyone. Each IP version is planned
// on its own, as ufw keeps its rules.
//
// Not run on a host yet: ufw's answer to a "from" rule, that "from 0.0.0.0/0"
// writes IPv4 alone, where ufw takes the log word, and its delete of a rule
// with a log type are read from ufw 0.36.2's sources and man page.

// Errors of a carried plan, testable with errors.Is.
var (
	// ErrCantCopy: a rule that lets the current port in from some sources
	// names a source port or a source application, which no copy keeps
	// (*CopyError).
	ErrCantCopy = errors.New("a ufw rule for the SSH port cannot be copied to a new port")
	// ErrSessionSource: the rules as they will be would not let the
	// applying session's address in on the new port, or that address is not
	// read (*SessionError).
	ErrSessionSource = errors.New("ufw would not let this session's address in on a new port")
)

// CopyError names the rule that cannot be copied. Is(ErrCantCopy).
type CopyError struct {
	Rule Rule
	Port uint16 // the port sshd listens on that Rule lets in
}

func (e *CopyError) Error() string {
	return fmt.Sprintf("ufw: %v: %s", ErrCantCopy, e.Words())
}

func (e *CopyError) Unwrap() error { return ErrCantCopy }

// Words is the rule in a few words: "22/tcp from 10.2.8.0/24 port 1024".
func (e *CopyError) Words() string {
	r := e.Rule
	w := r.DPort
	if r.Proto != "any" {
		w += "/" + r.Proto
	}
	if r.Iface != "" {
		w += " on " + r.Iface
	}
	if r.Dst != "" {
		w += " to " + r.Dst
	}
	return w + " from " + r.Src
}

// SessionError is the session's address that would not be let in, or
// Unread where a remote session's address is not read. Port is the port
// sshd listens on whose rules were weighed. Is(ErrSessionSource).
type SessionError struct {
	Client netip.Addr
	Port   uint16
	Unread bool
}

func (e *SessionError) Error() string {
	if e.Unread {
		return fmt.Sprintf("ufw: %v: the session's address is not read", ErrSessionSource)
	}
	return fmt.Sprintf("ufw: %v: no rule for port %d covers %s", ErrSessionSource, e.Port, e.Client)
}

func (e *SessionError) Unwrap() error { return ErrSessionSource }

// Session is the applying session's connection, for a carried plan: the
// rules as they will be must let Client in on the new port, to Server.
type Session struct {
	Remote bool // over SSH; a console session has no check
	// Client and Server are the connection's two addresses
	// (SSH_CONNECTION's); the zero Client is not read.
	Client, Server netip.Addr
	// Dev is the interface of the route back to Client (RouteDev), which a
	// rule limited to an interface is matched against; "" where it is not
	// read, and such a rule then lets nothing of the session in.
	Dev string
}

// RouteDev is the interface of the route back to addr: the "dev" of "ip
// route get <addr>", or a link-local address's zone. "" where it is not
// read. The interface a packet arrived on is not recorded anywhere, and
// under asymmetric routing it is not this one.
func RouteDev(ctx context.Context, sys host.System, addr netip.Addr) string {
	if !addr.IsValid() {
		return ""
	}
	if z := addr.Zone(); z != "" {
		return z
	}
	res, err := sys.Run(ctx, "ip", "route", "get", addr.String())
	if err != nil {
		return ""
	}
	f := strings.Fields(string(res.Stdout))
	if i := slices.Index(f, "dev"); i >= 0 && i+1 < len(f) {
		return f[i+1]
	}
	return ""
}

// srcAddr is the address or prefix r's source is limited to ("" for any),
// and whether something more limits it: a source port or application.
func (r Rule) srcAddr() (addr string, more bool) {
	f := strings.Fields(r.Src)
	switch {
	case len(f) == 0:
		return "", false
	case isAddr(f[0]):
		return f[0], len(f) > 1
	}
	return "", true
}

// prefixOf reads an address or a prefix.
func prefixOf(s string) (netip.Prefix, bool) {
	if p, err := netip.ParsePrefix(s); err == nil {
		return netip.PrefixFrom(p.Addr().WithZone(""), p.Bits()).Masked(), true
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, false
	}
	a = a.WithZone("")
	return netip.PrefixFrom(a, a.BitLen()), true
}

// samePrefix: a and b are the same address or prefix, or both any.
func samePrefix(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	pa, oka := prefixOf(a)
	pb, okb := prefixOf(b)
	return oka && okb && pa == pb
}

// lets: r is a saved rule that accepts TCP connections coming in.
func (r Rule) lets() bool {
	return r.Dir == "in" && (r.Action == "allow" || r.Action == "limit") && (r.Proto == "tcp" || r.Proto == "any")
}

// names: one of ports is among r's ports; that port, the first where r
// names none.
func (r Rule) names(ports []uint16) (uint16, bool) {
	for _, p := range ports {
		if in, ok := portIn(r.DPort, p); ok && in {
			return p, true
		}
	}
	return 0, false
}

// carriedRule is a source rule and the port sshd listens on that it names.
type carriedRule struct {
	rule Rule
	like uint16
	at   int // its place in State.Rules
}

// sourceRules reads f's rules for ports, the ports sshd listens on now:
// whether one lets a port in from anywhere (no source, interface or "to"
// address), the source rules (those with one of the three), in order, and
// the first of them that cannot be copied, for its source port or source
// application. A rule with no port counts as the others do, and is never
// copied: it lets a new port in already.
func sourceRules(st State, f Family, ports []uint16) (open bool, srcs []carriedRule, bad *CopyError) {
	for i, r := range st.Rules {
		like, ok := r.names(ports)
		if r.Family != f || !r.lets() || !ok {
			continue
		}
		switch _, more := r.srcAddr(); {
		case r.Src == "" && r.Iface == "" && r.Dst == "":
			open = true
		case more && r.DPort == "": // lets a new port in as it does this one
		case more && bad == nil:
			bad = &CopyError{Rule: r, Port: like}
		case !more:
			srcs = append(srcs, carriedRule{rule: r, like: like, at: i})
		}
	}
	return open, srcs, bad
}

// carries: in f, the ports are let in by source rules alone.
func carries(st State, f Family, ports []uint16) bool {
	open, srcs, bad := sourceRules(st, f, ports)
	return !open && (len(srcs) > 0 || bad != nil)
}

// Carries reports whether a port added beside ports, the ports sshd listens
// on now, would be given their source rules: ufw is active, and in an IP
// version it writes they are let in by source rules alone.
func Carries(st State, ports []uint16) bool {
	return st.Status.Active && slices.ContainsFunc(writes(st), func(f Family) bool { return carries(st, f, ports) })
}

// famClass is what one IP version's rules for the current ports are.
type famClass uint8

const (
	famNone     famClass = iota // no rule names them: nothing added, nothing judged
	famOpen                     // one lets them in from anywhere: so the new port
	famCarrying                 // source rules alone: each copied
)

// carryPlan is one new port's part of a carried plan.
type carryPlan struct {
	class    map[Family]famClass
	steps    []Step
	rules    []Rule // the rules the steps add, as they will be
	needs    []Need // one per source rule copied
	findings []Finding
}

// wild is f's address for every source.
func wild(f Family) string {
	if f == IPv6 {
		return "::/0"
	}
	return "0.0.0.0/0"
}

// copyOf is r, a source rule, for port to alone, over TCP.
func copyOf(r Rule, to uint16) Rule {
	addr, _ := r.srcAddr()
	return Rule{Action: r.Action, Dir: "in", Proto: "tcp", DPort: strconv.Itoa(int(to)), Dst: r.Dst, Src: addr,
		Iface: r.Iface, Family: r.Family, Log: r.Log, Comment: r.Comment}
}

// sameTuple: r is the rule ufw itself would take c, a copy, for (ufw's
// UFWRule.match): c's IP version, port, source, "to" address and interface,
// coming in, over TCP or any protocol, with no source port and no
// application. Its action, log type and comment are not compared: ufw
// replaces such a rule in place, so it is never written over.
func sameTuple(r, c Rule) bool {
	addr, more := r.srcAddr()
	return r.Family == c.Family && r.Dir == "in" && (r.Proto == "tcp" || r.Proto == "any") && r.DPort == c.DPort &&
		!more && samePrefix(addr, c.Src) && samePrefix(r.Dst, c.Dst) && r.Iface == c.Iface && r.App == ""
}

// copyStep is the command that adds c, with the delete that undoes it: the
// add's own words, the comment alone dropped. both: no address is said, so
// ufw writes the rule in both IP versions; otherwise the source, the "to"
// address or the pinned "from 0.0.0.0/0" (or ::/0) says the one.
func copyStep(c Rule, like uint16, both bool) Step {
	words := []string{c.Action}
	if c.Iface != "" {
		words = append(words, "in", "on", c.Iface)
	}
	if c.Log != "" {
		words = append(words, c.Log)
	}
	switch {
	case c.Src != "":
		words = append(words, "from", c.Src)
	case !both && c.Dst == "":
		words = append(words, "from", wild(c.Family))
	}
	to := "any"
	if c.Dst != "" {
		to = c.Dst
	}
	words = append(words, "to", to, "port", c.DPort, "proto", "tcp")
	port, _ := strconv.ParseUint(c.DPort, 10, 16)
	s := Step{Kind: StepAllowFrom, Port: uint16(port), From: like, Undo: append([]string{"ufw", "delete"}, words...),
		Why: fmt.Sprintf("let SSH port %s/tcp through as port %d is", c.DPort, like)}
	s.Argv = append([]string{"ufw"}, words...)
	if c.Comment != "" {
		s.Argv = append(s.Argv, "comment", c.Comment)
	}
	return s
}

// carry plans port to from the rules of from, the ports sshd listens on
// now, in each IP version ufw writes. Where a version's source rules are
// copied, each gets its command unless the rule is there already
// (sameTuple) or names to itself, and a Need. Where a version lets from in
// from anywhere while another copies, to gets a rule for that version
// alone where no rule lets it in yet. A *CopyError where a version that
// copies has a rule that cannot be copied.
func carry(st State, fw Firewall, from []uint16, to uint16) (carryPlan, error) {
	cp := carryPlan{class: map[Family]famClass{}}
	srcs := map[Family][]carriedRule{}
	for _, f := range writes(st) {
		open, rs, bad := sourceRules(st, f, from)
		switch {
		case open:
			cp.class[f] = famOpen
		case bad != nil:
			return cp, bad
		case len(rs) > 0:
			cp.class[f], srcs[f] = famCarrying, rs
		}
	}
	there := func(c Rule) bool {
		return slices.ContainsFunc(st.Rules, func(r Rule) bool { return sameTuple(r, c) }) ||
			slices.ContainsFunc(cp.rules, func(r Rule) bool { return sameTuple(r, c) })
	}
	add := func(s Step, c Rule) {
		c.Line = host.FormatArgv(s.Argv)
		cp.steps, cp.rules = append(cp.steps, s), append(cp.rules, c)
	}
	twinned := map[int]bool{} // IPv6's source rules written by IPv4's command
	for _, f := range writes(st) {
		switch cp.class[f] {
		case famOpen:
			n := Need{Port: to, Family: f, Addr: anywhere(f)}
			if v, _ := verdict(st.Rules, fw, n); v == Blocked {
				c := Rule{Action: "allow", Dir: "in", Proto: "tcp", DPort: strconv.Itoa(int(to)), Family: f}
				s := copyStep(c, 0, false)
				s.Why = fmt.Sprintf("let SSH port %d/tcp through from anywhere, %s alone", to, f)
				add(s, c)
			}
		case famCarrying:
			for i, cr := range srcs[f] {
				r := cr.rule
				if in, _ := portIn(r.DPort, to); in {
					continue // the rule names the new port too
				}
				c := copyOf(r, to)
				n := Need{Port: to, Family: f, New: true, Iface: c.Iface, Like: cr.like, Why: "port " + strconv.Itoa(int(cr.like)) + "'s rule " + copyWords(c)}
				n.From, _ = prefixOf(c.Src)
				n.To, _ = prefixOf(c.Dst)
				if !slices.ContainsFunc(cp.needs, func(o Need) bool {
					return o.Family == n.Family && o.From == n.From && o.To == n.To && o.Iface == n.Iface && o.Like == n.Like
				}) {
					cp.needs = append(cp.needs, n)
				}
				if f == IPv6 && twinned[i] || there(c) {
					continue
				}
				both := false
				if f == IPv4 && c.Src == "" && c.Dst == "" && cp.class[IPv6] == famCarrying {
					j := slices.IndexFunc(srcs[IPv6], func(o carriedRule) bool {
						oc := copyOf(o.rule, to)
						in, _ := portIn(o.rule.DPort, to)
						return !in && oc.Src == "" && oc.Dst == "" && oc.Iface == c.Iface && oc.Action == c.Action && oc.Log == c.Log && oc.Comment == c.Comment && !there(oc)
					})
					if j >= 0 && !twinned[j] {
						both, twinned[j] = true, true
					}
				}
				s := copyStep(c, cr.like, both)
				add(s, c)
				if both {
					c.Family = IPv6
					c.Line = host.FormatArgv(s.Argv)
					cp.rules = append(cp.rules, c)
				}
			}
		}
	}
	if to != 0 {
		cp.findings = wider(st, cp, from, to, fw.Incoming)
	}
	return cp, nil
}

// copyWords is a copy in a few words, for a Need's Why: "from 10.2.8.0/24
// on eth0".
func copyWords(c Rule) string {
	w := "from anywhere"
	if c.Src != "" {
		w = "from " + c.Src
	}
	if c.Dst != "" {
		w += " to " + c.Dst
	}
	if c.Iface != "" {
		w += " on " + c.Iface
	}
	return w
}

// wider are the FindingWidened of a carried plan, each IP version judged on
// its own. Where the version has no rule from anywhere for from: a rule
// there already that lets port to in from something no rule of from covers
// (it is left: the rule is the user's). In every version: a deny or reject
// that names a port of from and not to, so is not copied, while to lets in
// some of what it stops: a rule of from after it, or the default policy
// where that is allow.
func wider(st State, cp carryPlan, from []uint16, to uint16, incoming Policy) []Finding {
	var fs []Finding
	for _, f := range writes(st) {
		var olds []carriedRule // the rules that let from in, as copies for to
		for i, r := range st.Rules {
			like, ok := r.names(from)
			if _, more := r.srcAddr(); r.Family == f && r.lets() && ok && !more {
				olds = append(olds, carriedRule{rule: copyOf(r, to), like: like, at: i})
			}
		}
		n := Need{Port: to, Family: f, Addr: anywhere(f), New: true, Why: "new port"}
		for _, r := range st.Rules {
			in, _ := portIn(r.DPort, to)
			_, old := r.names(from)
			if cp.class[f] == famOpen || r.Family != f || r.DPort == "" || !in || old || !r.lets() {
				continue
			}
			rn := ruleNeed(r, to)
			if slices.ContainsFunc(olds, func(c carriedRule) bool { return cover(c.rule, rn, to) == full }) {
				continue // it lets in nothing that from's rules do not
			}
			v := Restricted
			if r.Src == "" && r.Iface == "" && r.Dst == "" {
				v = Allowed
			}
			fs = append(fs, Finding{Kind: FindingWidened, Incoming: incoming, Coverage: Coverage{Need: n, Verdict: v,
				By: fmt.Sprintf("ufw has %q already, which port %s has not: left as it is", ruleText(r), portList(from))}})
		}
		v := Restricted
		if cp.class[f] != famCarrying {
			v = Allowed
		}
		for at, r := range st.Rules {
			in, _ := portIn(r.DPort, to)
			like, old := r.names(from)
			if r.Family != f || !old || in || r.Dir != "in" || r.Action != "deny" && r.Action != "reject" || r.Proto != "tcp" && r.Proto != "any" {
				continue
			}
			dn := ruleNeed(r, to)
			if incoming != PolicyAllow && !slices.ContainsFunc(olds, func(c carriedRule) bool {
				m := cover(c.rule, dn, to)
				return c.at > at && (m == partial || m == full)
			}) {
				continue
			}
			fs = append(fs, Finding{Kind: FindingWidened, Incoming: incoming, Coverage: Coverage{Need: n, Verdict: v,
				By: fmt.Sprintf("%q is not copied from port %d: port %d lets that in", ruleText(r), like, to)}})
		}
	}
	return fs
}

// ruleNeed is what r matches, as a need on port: its source and "to"
// addresses and its interface. An address not read is left as any.
func ruleNeed(r Rule, port uint16) Need {
	n := Need{Port: port, Family: r.Family, Iface: r.Iface}
	addr, _ := r.srcAddr()
	n.From, _ = prefixOf(addr)
	n.To, _ = prefixOf(r.Dst)
	return n
}

// ruleText is a saved rule in a few words, for a finding: "allow 2230/tcp
// from anywhere", "deny 22/tcp from 192.0.2.99 on eth0", "allow any port
// from 10.0.0.0/8", "allow any tcp port from fd00::/8".
func ruleText(r Rule) string {
	w := r.Action + " " + r.DPort
	switch {
	case r.DPort == "" && r.Proto != "any":
		w = r.Action + " any " + r.Proto + " port"
	case r.DPort == "":
		w = r.Action + " any port"
	case r.Proto != "any":
		w += "/" + r.Proto
	}
	if r.Dst != "" {
		w += " to " + r.Dst
	}
	if r.Src == "" {
		w += " from anywhere"
	} else {
		w += " from " + r.Src
	}
	if r.Iface != "" {
		w += " on " + r.Iface
	}
	return w
}

// portList is ports joined by ", ".
func portList(ports []uint16) string {
	s := make([]string, len(ports))
	for i, p := range ports {
		s[i] = strconv.Itoa(int(p))
	}
	return strings.Join(s, ", ")
}

// sessionNeed is the session's own need on port: its client address, to
// its server address, on the route's interface. ok false where there is
// none to judge: a console session, and a client on loopback, which ufw
// accepts before any rule.
func sessionNeed(s Session, port uint16) (n Need, ok bool) {
	if !s.Remote || !s.Client.IsValid() || s.Client.IsLoopback() || s.Dev == "lo" {
		return n, false
	}
	c := s.Client.Unmap().WithZone("")
	n = Need{Port: port, Family: IPv4, New: true, Session: true, Iface: s.Dev, Why: "this session, from " + c.String(),
		From: netip.PrefixFrom(c, c.BitLen())}
	if c.Is6() {
		n.Family = IPv6
	}
	if a := s.Server.Unmap().WithZone(""); a.IsValid() {
		n.To = netip.PrefixFrom(a, a.BitLen())
	}
	return n, true
}

// CheckSession is why a port added beside from, the ports sshd listens on
// now, would not be reachable for the session, before any port is named: a
// *CopyError, or a *SessionError where the rules as they would be for a
// port no rule names yet do not let the session's address in, or a remote
// session's address is not read. nil where nothing is carried (Carries),
// and for a console or loopback session.
func CheckSession(st State, from []uint16, s Session) error {
	if !Carries(st, from) {
		return nil
	}
	fw := Firewall{Active: true, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Incoming}
	cp, err := carry(st, fw, from, 0)
	if err != nil {
		return err
	}
	return checkSession(st, fw, cp, from, s)
}

// checkSession is CheckSession over cp, the plan for port 0, which no rule
// names.
func checkSession(st State, fw Firewall, cp carryPlan, from []uint16, s Session) error {
	if !s.Remote {
		return nil
	}
	if !s.Client.IsValid() {
		return &SessionError{Unread: true}
	}
	n, ok := sessionNeed(s, 0)
	if !ok {
		return nil
	}
	if v, _ := verdict(slices.Concat(st.Rules, cp.rules), fw, n); !v.OK() {
		e := &SessionError{Client: s.Client}
		if len(from) > 0 {
			e.Port = from[0]
		}
		return e
	}
	return nil
}

// cover says which of n's connections to port r matches: n's source, "to"
// address and interface against r's. A rule with a source port or a source
// application matches some of them at most, and so does a rule on an
// interface where n's is not said.
func cover(r Rule, n Need, port uint16) match {
	if r.Family != n.Family || r.Dir != "in" || r.Proto != "tcp" && r.Proto != "any" {
		return noMatch
	}
	if in, ok := portIn(r.DPort, port); !ok {
		return unreadable
	} else if !in {
		return noMatch
	}
	m := full
	addr, more := r.srcAddr()
	for _, side := range []struct {
		rule string
		need netip.Prefix
	}{{addr, n.From}, {r.Dst, n.To}} {
		if side.rule == "" {
			continue
		}
		p, ok := prefixOf(side.rule)
		switch {
		case !ok:
			return unreadable
		case !side.need.IsValid():
			m = partial
		case p.Bits() <= side.need.Bits() && p.Contains(side.need.Addr()):
		case p.Overlaps(side.need):
			m = partial
		default:
			return noMatch
		}
	}
	if more {
		m = partial
	}
	switch {
	case r.Iface == "" || r.Iface == n.Iface:
	case n.Iface == "":
		m = partial
	default:
		return noMatch
	}
	return m
}

// walked is one pass over the rules for a sourced need on one port: the
// denies and rejects that match some of it before the rule that decides,
// by index, and that rule's verdict.
type walked struct {
	denies  []int
	verdict Verdict
	by      string
}

// walk is the first match, as ufw's chains: an allow or limit decides
// where it matches all of n, a deny or reject where it matches all of it
// too; a deny that matches some of it is kept, an allow that does is not
// counted.
func walk(rules []Rule, fw Firewall, n Need, port uint16) walked {
	var w walked
	for i, r := range rules {
		m := cover(r, n, port)
		lets := r.Action == "allow" || r.Action == "limit"
		switch {
		case m == unreadable:
			w.verdict, w.by = VerdictUnknown, r.Line
			return w
		case m == noMatch, m == partial && lets:
		case lets:
			w.verdict, w.by = Allowed, r.Line
			if r.Action == "limit" {
				w.verdict = Limited
			}
			return w
		default:
			w.denies = append(w.denies, i)
			if m == full {
				w.verdict, w.by = Denied, r.Line
				return w
			}
		}
	}
	switch {
	case fw.Incoming == PolicyAllow:
		w.verdict, w.by = Allowed, "default allow (incoming)"
	case fw.Incoming.drops():
		w.verdict, w.by = Blocked, "default "+string(fw.Incoming)+" (incoming)"
	default:
		w.by = "the default incoming policy is not established"
	}
	return w
}

// verdictFrom judges a sourced need under an active ufw. The session's:
// an allow must match all of it before any deny matches some. A source
// rule's copy: the same, but for the denies that match it on Like before
// its rule there too, which stop on the new port what they stop on Like.
func verdictFrom(rules []Rule, fw Firewall, n Need) (Verdict, string) {
	w := walk(rules, fw, n, n.Port)
	if !w.verdict.OK() {
		return w.verdict, w.by
	}
	var same []int
	if n.Like != 0 {
		same = walk(rules, fw, n, n.Like).denies
	}
	for _, i := range w.denies {
		if !slices.Contains(same, i) {
			return Denied, rules[i].Line
		}
	}
	return w.verdict, w.by
}
