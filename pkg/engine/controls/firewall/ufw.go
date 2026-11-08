package firewall

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// codeUFWUnknown is ufw's FindingUnknown as steps.UFWFinding codes it.
var codeUFWUnknown = steps.FindingCode("UFW", ufw.FindingUnknown.String())

// checkUFW is the row off Proxmox VE, from ufw.Read: not installed and
// inactive are NotConfigured; active, the default incoming policy and the
// SSH listeners decide (ufw.Check over ufw.Needs). An SSH listener ufw
// refuses, or lets through from loopback only, is Partial with
// CodeSSHBlocked, whatever else is not established, except one refused on
// IPv6 alone while an IPv4 listener on its port gets in (ipv4Only): that is
// Partial "ssh IPv4 only" where no allowlist or blocklist says more; an allow policy, a
// rule that opens every port, or ufw not started at the next boot (ENABLED=no,
// or ufw.ReadBoot No: its service in no runlevel, or ufw.service not enabled)
// is Partial. Under deny or reject, every listener let through from
// everywhere (rate-limited included) is Configured, and from some sources
// only, or from all but some, Partial: whether the user's own address is
// let through is not established (ufw.Verdict.OK). Unknown while the
// listeners are not established (but where the host has no SSH server,
// Sockets.NoServer: then there is none to let through, and an active
// default-deny ufw that starts at boot is Configured), while ufw refuses a socket on 22 that no
// configured listener names, for an incoming policy ufw does not set, and in
// place of Configured while whether ufw starts at boot is not read
// (CodeBootUnchecked).
func checkUFW(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	st, err := ufw.Read(ctx, sys, f.Proxmox)
	switch {
	case err != nil:
		return engine.Status{State: engine.Unknown, Detail: "can't read"}, err
	case !st.Installed:
		return engine.Status{State: engine.NotConfigured, Detail: "ufw not installed", Evidence: []string{"ufw: not installed"}}, nil
	}
	out := engine.Status{Evidence: ufwEvidence(st)}
	if !st.Status.Known || len(st.Status.Bad) > 0 {
		_, fs := ufw.Check(st, nil)
		out.Findings = ufwFindings(fs)
		out.State, out.Detail = engine.Unknown, "can't read"
		return out, nil
	}
	active := st.Status.Active
	switch in := st.Status.Incoming; {
	case !active && !st.Enabled:
		out.State, out.Detail = engine.NotConfigured, "inactive"
		return out, nil
	case active && in != ufw.PolicyAllow && in != ufw.PolicyDeny && in != ufw.PolicyReject: // "disabled" is routed's only (ufw.md §3)
		out.Findings = []engine.Finding{{Code: codeUFWUnknown, Source: "ufw status verbose",
			Detail: "default incoming policy " + string(in) + ": not one ufw sets"}}
		out.State, out.Detail = engine.Unknown, "can't read"
		return out, nil
	}
	sk := ReadSockets(ctx, sys, f)
	out.Evidence = append(out.Evidence, sk.ev...)
	var refused, stray []netip.AddrPort // refused (loopback only included); a stray socket not let through from everywhere
	var unread, some, except bool       // a verdict not read; let through from some sources only; from all but some
	var only4 bool                      // refused on IPv6 alone while IPv4 gets in (ipv4Only)
	if ls, ok := sk.Listeners(); ok {
		cs, fs := ufw.Check(st, ufw.Needs(ls, nil, nil))
		if active {
			out.Evidence = append(out.Evidence, coverage(cs)...)
		}
		out.Findings = ufwFindings(fs)
		fw := ufw.Firewall{Active: active, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Status.Incoming}
		seen := slices.Clone(cs) // loopback only counted as refused, as verdicts counts it
		for i, c := range seen {
			if c.Verdict == ufw.Restricted && loopbackOnly(st.Status.Rules, fw, c.Need) {
				seen[i].Verdict = ufw.Blocked
			}
		}
		for _, x := range fs {
			n, k := x.Coverage.Need, x.Kind
			if k == ufw.FindingRestricted && loopbackOnly(st.Status.Rules, fw, n) {
				k = ufw.FindingNotAllowed
			}
			a := netip.AddrPortFrom(n.Addr, n.Port)
			switch {
			case (k == ufw.FindingNotAllowed || k == ufw.FindingRestricted || k == ufw.FindingExcepted) && sk.Stray(n.Port):
				stray = append(stray, a)
			case k == ufw.FindingNotAllowed && slices.ContainsFunc(seen, func(c ufw.Coverage) bool { return c.Need == n && ipv4Only(seen, c) }):
				only4 = true
			case k == ufw.FindingNotAllowed && !slices.Contains(refused, a):
				refused = append(refused, a)
			case k == ufw.FindingRestricted:
				some = true
			case k == ufw.FindingExcepted:
				except = true
			case k == ufw.FindingUnknown:
				unread = true
			}
		}
	}
	out.Findings = append(out.Findings, sk.notInEffect()...)
	if len(refused) > 0 {
		out.Findings = append(out.Findings, blocked(refused, "ufw status verbose"))
	}
	if len(stray) > 0 {
		out.Findings = append(out.Findings, sk.bound22())
	}
	open, allPorts := allOpen(st.Status.Rules)
	noBoot, bootUnread := false, false
	if active {
		boot := ufw.ReadBoot(ctx, sys, f)
		switch noBoot, bootUnread = boot == detect.No, boot == detect.Unknown; {
		case noBoot:
			out.Evidence = append(out.Evidence, bootEvidence(f))
		case bootUnread:
			out.Findings = append(out.Findings, engine.Finding{Code: CodeBootUnchecked, Detail: "whether ufw starts at boot not read", Source: bootSource(f)})
		}
	}
	switch {
	case !active: // ENABLED=yes: ufw.Check said what its saved rules refuse at start
		out.State, out.Detail = engine.NotConfigured, "enabled, not running"
		if sk.Established() && !st.Known() {
			out.Evidence, out.Findings = append(out.Evidence, savedEvidence(st)...), append(out.Findings, savedUnread(st))
		}
	case len(refused) > 0:
		out.State, out.Detail = engine.Partial, "ssh blocked"
		return out, nil
	case st.Status.Incoming == ufw.PolicyAllow:
		out.State, out.Detail = engine.Partial, DetailAllowPolicy
	case allPorts:
		out.Evidence = append(out.Evidence, "ufw: every port allowed by "+open)
		out.State, out.Detail = engine.Partial, DetailAllPorts
	case !st.Enabled || noBoot:
		out.State, out.Detail = engine.Partial, "off after restart"
	case !sk.Established() && !sk.NoServer:
		out.State, out.Detail = engine.Unknown, sk.Why
		return out, sk.err
	case unread:
		out.State, out.Detail = engine.Unknown, "can't read"
		return out, nil
	case len(stray) > 0: // ufw refuses it; whether that refuses sshd is not established
		out.State, out.Detail = engine.Unknown, "can't tell if 22 is ssh"
		return out, nil
	case some:
		out.State, out.Detail = engine.Partial, "ssh allowlist"
		return out, nil
	case except:
		out.State, out.Detail = engine.Partial, "ssh blocklist"
		return out, nil
	case only4:
		out.State, out.Detail = engine.Partial, "ssh IPv4 only"
		return out, nil
	case bootUnread:
		out.State, out.Detail = engine.Unknown, "can't tell boot state"
		return out, nil
	default: // deny or reject
		out.State, out.Detail = engine.Configured, string(st.Status.Incoming)+" policy"
		return out, nil
	}
	if !sk.Established() && !sk.NoServer {
		out = sk.unchecked(out)
	}
	return out, nil
}

// bootEvidence says what ufw.ReadBoot read when it read No: the runlevel or
// the unit.
func bootEvidence(f detect.Facts) string {
	if f.Init.Kind == detect.OpenRC {
		return "OpenRC: ufw in no runlevel"
	}
	return "systemctl is-enabled ufw.service: not enabled"
}

// bootSource is what whether ufw starts at boot is read from.
func bootSource(f detect.Facts) string {
	if f.Init.Kind == detect.OpenRC {
		return "OpenRC runlevels"
	}
	return "systemctl is-enabled ufw.service"
}

// loopbackOnly reports whether rules under fw let n through from loopback
// only, so that no remote session reaches it: its verdict is Restricted, and
// with the allow and limit rules that admit loopback alone (in on lo, from or
// to a loopback address) left out, ufw.Evaluate blocks or denies it.
func loopbackOnly(rules []ufw.Rule, fw ufw.Firewall, n ufw.Need) bool {
	rules = slices.DeleteFunc(slices.Clone(rules), func(r ufw.Rule) bool {
		return (r.Action == "allow" || r.Action == "limit") && (r.Iface == "lo" || loopback(r.Src) || loopback(r.Dst))
	})
	v := ufw.Evaluate(rules, fw, []ufw.Need{n})[0].Verdict
	return v == ufw.Blocked || v == ufw.Denied
}

// loopback: the address or prefix s starts with is within 127.0.0.0/8 or
// is ::1.
func loopback(s string) bool {
	a, _, _ := strings.Cut(s, " ")
	if p, err := netip.ParsePrefix(a); err == nil {
		return p.Addr().Unmap().IsLoopback() && (p.Addr().Unmap().Is4() && p.Bits() >= 8 || p.Bits() == 128)
	}
	addr, err := netip.ParseAddr(a)
	return err == nil && addr.Unmap().IsLoopback()
}

// allOpen finds a rule that accepts new TCP connections to every port
// from anywhere ("ufw allow from any", "1:65535/tcp"): the default policy
// then refuses no TCP connection that reaches it.
func allOpen(rules []ufw.Rule) (line string, ok bool) {
	for _, r := range rules {
		if (r.Action == "allow" || r.Action == "limit") && r.Dir == "in" && (r.Proto == "tcp" || r.Proto == "any") &&
			r.Src == "" && r.Dst == "" && r.Iface == "" && everyPort(r.DPort) {
			return strings.Join(strings.Fields(r.Line), " "), true
		}
	}
	return "", false
}

// everyPort: the port list spec ("" any, "1:65535", "1:1024,1025:65535")
// includes every port from 1 to 65535.
func everyPort(spec string) bool {
	if spec == "" {
		return true
	}
	var rs [][2]uint64
	for _, item := range strings.Split(spec, ",") {
		lo, hi, isRange := strings.Cut(item, ":")
		a, err := strconv.ParseUint(lo, 10, 16)
		b := a
		if err == nil && isRange {
			b, err = strconv.ParseUint(hi, 10, 16)
		}
		if err != nil {
			return false
		}
		rs = append(rs, [2]uint64{a, b})
	}
	slices.SortFunc(rs, func(x, y [2]uint64) int { return cmp.Compare(x[0], y[0]) })
	next := uint64(1)
	for _, r := range rs {
		if r[0] > next {
			return false
		}
		next = max(next, r[1]+1)
	}
	return next > 65535
}

// savedUnread is CodeSSHUnchecked's finding for ufw's saved state not read
// whole while ufw is enabled and stopped: what it would refuse at start
// was not checked.
func savedUnread(st ufw.State) engine.Finding {
	if len(st.Bad) > 0 {
		return engine.Finding{Code: CodeSSHUnchecked, Detail: "saved rules unreadable", Source: "ufw"}
	}
	return engine.Finding{Code: CodeSSHUnchecked, Detail: "default policy unreadable", Source: ufw.DefaultsFile}
}

// savedEvidence lists the saved rule lines that were not read.
func savedEvidence(st ufw.State) []string {
	if len(st.Bad) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("ufw: saved rule lines not read: %q", st.Bad)}
}

// ufwEvidence is what ufw showed: its status line, while active its
// defaults line, as ufw status verbose prints them, and ENABLED.
func ufwEvidence(st ufw.State) []string {
	s := st.Status
	ev := []string{"ufw status verbose: Status: inactive"}
	switch {
	case !s.Known:
		ev = []string{"ufw status verbose: not read"}
	case s.Active:
		ev = []string{"ufw status verbose: Status: active",
			fmt.Sprintf("ufw status verbose: Default: %s (incoming), %s (outgoing), %s (routed)", s.Incoming, s.Outgoing, s.Routed)}
	}
	if len(s.Bad) > 0 {
		ev = append(ev, fmt.Sprintf("ufw status verbose: lines not read: %q", s.Bad))
	}
	enabled := "no"
	if st.Enabled {
		enabled = "yes"
	}
	return append(ev, "ufw: ENABLED="+enabled)
}

// coverage is each SSH listener's verdict and what decided it, for the
// evidence.
func coverage(cs []ufw.Coverage) []string {
	ev := make([]string, len(cs))
	for i, c := range cs {
		ev[i] = fmt.Sprintf("ufw: %s (%s): %s by %s", c.Need.Why, c.Need.Family, c.Verdict, strings.Join(strings.Fields(c.By), " "))
	}
	return ev
}

func ufwFindings(fs []ufw.Finding) []engine.Finding {
	var out []engine.Finding
	for _, f := range fs {
		out = append(out, steps.UFWFinding(f))
	}
	return out
}
