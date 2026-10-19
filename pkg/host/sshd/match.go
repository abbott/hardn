package sshd

import (
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// criterion is one attribute of a Match line with its argument, paired as
// sshd's match_cfg_line pairs them: attr in lower case, arg "" for All and
// Invalid-User, name as written.
type criterion struct {
	attr, arg, name string
}

// eqAttrs are the attributes OpenSSH 10 also accepts as attr=value
// (servconf.c V_10_3_P1, match_cfg_line).
var eqAttrs = []string{"user", "group", "host", "address", "localaddress", "localport", "rdomain", "version"}

// criteria10 pairs argv (splitArgs of the line) as OpenSSH 10.0 and 10.3
// do. problem names a criterion with no argument.
func criteria10(argv []string) (cs []criterion, problem string) {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if strings.HasPrefix(a, "#") {
			break
		}
		attr := strings.ToLower(a)
		if attr == "all" || attr == "invalid-user" {
			cs = append(cs, criterion{attr, "", a})
			continue
		}
		name, arg, eq := strings.Cut(a, "=")
		if !eq || !slices.Contains(eqAttrs, strings.ToLower(name)) {
			name, arg = a, ""
			if i+1 < len(argv) {
				i++
				arg = argv[i]
			}
		}
		if arg == "" || arg[0] == '#' {
			return cs, name + " has no value"
		}
		cs = append(cs, criterion{strings.ToLower(name), arg, name})
	}
	return cs, ""
}

// criteria9 pairs the line as OpenSSH 9.2 and 9.6 do: their match_cfg_line
// reads it with strdelim, which knows double quotes only and splits at '='
// (servconf.c V_9_2_P1 and V_9_6_P1). ok is false where they reject it.
func criteria9(line string) (cs []criterion, ok bool) {
	c := cursor{s: line}
	for {
		a, more := c.next()
		if !more || a == "" || a[0] == '#' {
			return cs, true
		}
		if strings.EqualFold(a, "all") {
			cs = append(cs, criterion{"all", "", a})
			continue
		}
		arg, more := c.next()
		if !more || arg == "" || arg[0] == '#' {
			return cs, false
		}
		cs = append(cs, criterion{strings.ToLower(a), arg, a})
	}
}

// target returns base changed so that it satisfies the criteria of a Match
// line (line is the text after the keyword), and the reasons it may not.
// No reason means that, by the rules of sshd's matching mirrored here
// (match.c, addrmatch.c), base as changed matches every criterion.
//
// Each attribute's pattern lists (one per occurrence: sshd ANDs them) get
// the first candidate every list accepts: base's value, then, from each
// pattern that is not negated, '*' and '?' filled in, and for a CIDR block
// its first and last address. A list of negations alone never matches.
// With UseDNS no, sshd compares Host with the client's address, so Host
// takes addresses only. Group, Version and Invalid-User depend on the host
// or on sshd, not on the spec, and a line OpenSSH 9.x and 10.x pair
// differently has no single answer.
func target(line string, base Conn, useDNS bool) (c Conn, reasons []string) {
	c = base
	argv, _ := splitArgs(line) // unbalanced quotes fail sshd -t first
	cs, problem := criteria10(argv)
	if problem != "" {
		reasons = append(reasons, problem)
	}
	same := func(a, b criterion) bool { return a.attr == b.attr && a.arg == b.arg }
	if cs9, ok := criteria9(line); ok != (problem == "") || ok && !slices.EqualFunc(cs, cs9, same) {
		reasons = append(reasons, "OpenSSH 9.2 and 9.6 read this line differently from 10.x (quotes, '=' or a backslash)")
	}
	var order []string
	lists := map[string][]criterion{}
	for _, cr := range cs {
		switch cr.attr {
		case "all":
			continue
		case "invalid-user":
			reasons = append(reasons, "Invalid-User needs the test spec of OpenSSH 10")
			continue
		case "group":
			reasons = append(reasons, "Group is decided by the host's group database")
			continue
		case "user", "host", "address", "localaddress", "localport", "rdomain":
		default:
			reasons = append(reasons, cr.name+" cannot be set in a test spec")
			continue
		}
		if lists[cr.attr] == nil {
			order = append(order, cr.attr)
		}
		lists[cr.attr] = append(lists[cr.attr], cr)
	}
	for _, attr := range order {
		var label, ls []string
		for _, cr := range lists[attr] {
			label, ls = append(label, cr.name+" "+cr.arg), append(ls, cr.arg)
		}
		var dst *string
		var v, why string
		switch attr {
		case "user":
			dst = &c.User
			v, why = pickName(ls, []string{base.User}, false, nameFills, nil)
		case "rdomain":
			dst = &c.RDomain
			v, why = pickName(ls, []string{base.RDomain}, false, nameFills, nil)
		case "host":
			dst = &c.Host
			if useDNS {
				v, why = pickName(ls, []string{base.Host}, true, nameFills, nil)
			} else if v, why = pickName(ls, []string{base.Host, base.Addr}, true, addrFills, isAddr); why != "" {
				why += " (UseDNS is no, so sshd matches Host against the client's address)"
			}
		case "address":
			dst = &c.Addr
			v, why = pickAddr(ls, base.Addr)
		case "localaddress":
			dst = &c.LAddr
			v, why = pickAddr(ls, base.LAddr)
		case "localport":
			dst = &c.LPort
			v, why = pickPort(ls)
		}
		if why != "" {
			reasons = append(reasons, strings.Join(label, ", ")+": "+why)
			continue
		}
		*dst = v
	}
	return c, reasons
}

// Fills for '*' (and, by their first byte, '?') in a pattern. The last
// address fill, from TEST-NET-2 (RFC 5737), is for a '*' that stands alone.
var (
	nameFills = []string{"x", "y"}
	addrFills = []string{"1", "2", "1.1", "1.1.1", ":1", "198.51.100.1"}
)

const onlyNegated = "only negated patterns, which never match"

// pickName returns the first of bases, then of the patterns' fills, that is
// not empty, passes valid (nil: any) and every list accepts; fold is
// match_hostname's lower-casing.
func pickName(lists, bases []string, fold bool, fills []string, valid func(string) bool) (string, string) {
	cands := slices.Clone(bases)
	for _, l := range lists {
		pos := 0
		for p := range strings.SplitSeq(l, ",") {
			if strings.HasPrefix(p, "!") {
				continue
			}
			pos++
			for _, f := range fills {
				if v := fill(p, f); fold {
					cands = append(cands, lower(v))
				} else {
					cands = append(cands, v)
				}
			}
		}
		if pos == 0 {
			return "", onlyNegated
		}
	}
	for _, v := range cands {
		if v == "" || valid != nil && !valid(v) {
			continue
		}
		if !slices.ContainsFunc(lists, func(l string) bool { return matchList(v, l, fold) != 1 }) {
			return v, ""
		}
	}
	return "", "no value hardn tried matches every pattern"
}

// pickAddr is pickName for address lists (addr_match_list).
func pickAddr(lists []string, base string) (string, string) {
	cands := []string{base}
	for _, l := range lists {
		pos := 0
		for e := range strings.SplitSeq(l, ",") {
			if e == "" || e[0] == '!' {
				continue
			}
			pos++
			switch p, kind := cidr(e); kind {
			case isCIDR:
				cands = append(cands, p.Addr().String(), lastAddr(p).String())
			case notCIDR:
				for _, f := range addrFills {
					cands = append(cands, fill(e, f))
				}
			}
		}
		if pos == 0 {
			return "", onlyNegated
		}
	}
	for _, v := range cands {
		if !isAddr(v) {
			continue
		}
		if !slices.ContainsFunc(lists, func(l string) bool { return matchAddrList(v, l) != 1 }) {
			return v, ""
		}
	}
	return "", "no address hardn tried matches every pattern"
}

// pickPort: LocalPort takes one number (no list); repeated, all must agree.
// sshd's a2port also takes a service name, which hardn does not resolve.
func pickPort(ls []string) (string, string) {
	var port int
	for _, v := range ls {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 65535 || strings.Trim(v, "0123456789") != "" {
			return "", v + " is not a port number a connection can have"
		}
		if port != 0 && n != port {
			return "", "different ports, which no connection has at once"
		}
		port = n
	}
	return strconv.Itoa(port), ""
}

// fill replaces each '*' in p with f and each '?' with f's first byte.
func fill(p, f string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '*':
			b.WriteString(f)
		case '?':
			b.WriteByte(f[0])
		default:
			b.WriteByte(p[i])
		}
	}
	return b.String()
}

// lower is C's tolower over ASCII, as match_pattern_list applies it.
func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

func isAddr(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}

// matchPattern is match.c's match_pattern: '*' matches any run of bytes,
// '?' one byte.
func matchPattern(s, p string) bool {
	for {
		if p == "" {
			return s == ""
		}
		if p[0] == '*' {
			p = strings.TrimLeft(p, "*")
			if p == "" {
				return true
			}
			for i := 0; i < len(s); i++ {
				if (p[0] == '?' || s[i] == p[0]) && matchPattern(s[i:], p) {
					return true
				}
			}
			return false
		}
		if s == "" || p[0] != '?' && p[0] != s[0] {
			return false
		}
		s, p = s[1:], p[1:]
	}
}

// matchList is match.c's match_pattern_list: -1 when a negated pattern
// matches s, 1 when a positive one does, else 0; a subpattern of 1023 bytes
// or more is no match at all. fold lower-cases both, as match_hostname does.
func matchList(s, list string, fold bool) int {
	if fold {
		s, list = lower(s), lower(list)
	}
	got := 0
	for i := 0; i < len(list); {
		neg := list[i] == '!'
		if neg {
			i++
		}
		sub := list[i:]
		if j := strings.IndexByte(sub, ','); j >= 0 {
			sub = sub[:j]
			i++
		}
		i += len(sub)
		if len(sub) >= 1023 {
			return 0
		}
		if matchPattern(s, sub) {
			if neg {
				return -1
			}
			got = 1
		}
	}
	return got
}

// Kinds of an address list entry, as addr_pton_cidr sorts them.
const (
	notCIDR = iota // a wildcard pattern
	isCIDR
	badCIDR // host bits set: sshd rejects the list
)

func cidr(e string) (netip.Prefix, int) {
	if strings.Contains(e, "/") {
		p, err := netip.ParsePrefix(e)
		switch {
		case err != nil:
			return p, notCIDR
		case p.Masked() != p:
			return p, badCIDR
		}
		return p, isCIDR
	}
	a, err := netip.ParseAddr(e)
	if err != nil {
		return netip.Prefix{}, notCIDR
	}
	return netip.PrefixFrom(a, a.BitLen()), isCIDR
}

// matchAddrList is addrmatch.c's addr_match_list: CIDR entries by
// containment, others as patterns over the address as written; -1 for a
// matching negation, 1 for a positive match, 0 for none, -2 for a bad list.
func matchAddrList(addr, list string) int {
	a, err := netip.ParseAddr(addr)
	if err != nil {
		return 0
	}
	ret := 0
	for e := range strings.SplitSeq(list, ",") {
		neg := strings.HasPrefix(e, "!")
		if neg {
			e = e[1:]
		}
		if e == "" {
			return -2
		}
		hit := false
		switch p, kind := cidr(e); kind {
		case badCIDR:
			return -2
		case isCIDR:
			hit = p.Contains(a)
		default:
			hit = matchPattern(addr, e)
		}
		if hit {
			if neg {
				return -1
			}
			ret = 1
		}
	}
	return ret
}

// lastAddr is the last address of p.
func lastAddr(p netip.Prefix) netip.Addr {
	b := p.Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 0x80 >> (i % 8)
	}
	a, _ := netip.AddrFromSlice(b)
	return a
}

// useDNS reads UseDNS from sshd -T's output; anything but "usedns yes" is
// no, sshd's default.
func useDNS(effective []byte) bool {
	for l := range strings.SplitSeq(string(effective), "\n") {
		if f := strings.Fields(l); len(f) == 2 && strings.EqualFold(f[0], "usedns") {
			return strings.EqualFold(f[1], "yes")
		}
	}
	return false
}
