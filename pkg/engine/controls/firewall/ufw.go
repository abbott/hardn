package firewall

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// checkUFW is the row off Proxmox VE, from ufw.Read: not installed and
// inactive are NotConfigured; active, the default incoming policy and the
// SSH listeners decide (ufw.Check over ufw.Needs). An SSH listener ufw
// refuses is Partial with CodeSSHBlocked, whatever else is not
// established; an allow policy, or ENABLED=no (ufw not started at the next
// boot), is Partial; deny or reject with every listener let through
// (rate-limited, or from some sources only, included: their findings say
// so) is Configured, and Unknown while the listeners are not established
// or while ufw refuses a socket on 22 that no configured listener names.
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
	if !active && !st.Enabled {
		out.State, out.Detail = engine.NotConfigured, "inactive"
		return out, nil
	}
	sk := readSockets(ctx, sys, f)
	out.Evidence = append(out.Evidence, sk.ev...)
	var refused, stray []netip.AddrPort // refused, a stray socket refused
	unread := false
	if ls, ok := sk.listeners(); ok {
		cs, fs := ufw.Check(st, ufw.Needs(ls, nil, nil))
		if active {
			out.Evidence = append(out.Evidence, coverage(cs)...)
		}
		out.Findings = ufwFindings(fs)
		for _, x := range fs {
			a := netip.AddrPortFrom(x.Coverage.Need.Addr, x.Coverage.Need.Port)
			switch {
			case x.Kind == ufw.FindingNotAllowed && sk.stray(a.Port()):
				stray = append(stray, a)
			case x.Kind == ufw.FindingNotAllowed && !slices.Contains(refused, a):
				refused = append(refused, a)
			case x.Kind == ufw.FindingUnknown:
				unread = true
			}
		}
	}
	out.Findings = append(out.Findings, sk.notInEffect()...)
	if len(refused) > 0 {
		out.Findings = append(out.Findings, blocked(refused, "ufw status verbose"))
	}
	if len(stray) > 0 {
		out.Findings = append(out.Findings, engine.Finding{Code: ssh.Code22Bound, Source: "ss -Htln",
			Detail: sk.on([]uint16{22}) + " bound, 22 not configured: sshd not reloaded, or another program"})
	}
	switch {
	case !active: // ENABLED=yes: ufw.Check said what its saved rules refuse at start
		out.State, out.Detail = engine.NotConfigured, "enabled, not running"
		if sk.established() && !st.Known() {
			out.Findings = append(out.Findings, engine.Finding{Code: CodeSSHUnchecked, Source: "ufw",
				Detail: fmt.Sprintf("saved rules unreadable: %q", st.Bad)})
		}
	case len(refused) > 0:
		out.State, out.Detail = engine.Partial, "ssh blocked"
		return out, nil
	case st.Status.Incoming == ufw.PolicyAllow:
		out.State, out.Detail = engine.Partial, "allow policy"
	case !st.Enabled:
		out.State, out.Detail = engine.Partial, "off after restart"
	case !sk.established():
		out.State, out.Detail = engine.Unknown, sk.why
		return out, sk.err
	case unread:
		out.State, out.Detail = engine.Unknown, "can't read"
		return out, nil
	case len(stray) > 0: // ufw refuses it; whether that refuses sshd is not established
		out.State, out.Detail = engine.Unknown, "can't tell if 22 is ssh"
		return out, nil
	default:
		out.State, out.Detail = engine.Configured, string(st.Status.Incoming)+" policy"
		return out, nil
	}
	if !sk.established() {
		out.Findings = append(out.Findings, unchecked(sk))
	}
	return out, nil
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
