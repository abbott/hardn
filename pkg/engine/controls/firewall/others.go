package firewall

import (
	"cmp"
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/reload"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// The other listeners Enable blocks (the maintainer, 2026-11-11: option (a)
// of the firewall-listeners design). Enable lists in its review the
// listening ports, other than SSH's, that the firewall it starts would
// block, with the program ss names, and how to keep one open. hardn adds no
// rule for them (gallery-firewall.md §5 decision 3; ufw.md §6), and the
// finding never refuses.

// othersSource is the findings' Source: the reads they come from.
var othersSource = host.FormatArgv(reload.ListenTCPArgv) + ", " + host.FormatArgv(reload.ListenUDPArgv)

// ufwBeforeUDP are the UDP ports ufw's stock before.rules and before6.rules
// accept ahead of any user rule, by IP version: the DHCP client's (68 from
// 67; 546 from 547, link-local), and mDNS's 5353 and UPnP's 1900 to their
// multicast groups. ufw 0.36.2's conf/before.rules and before6.rules, which
// Alpine's package installs unpatched (aports community/ufw 0.36.2-r3), and
// Debian 13's and Ubuntu 26.04's, read 2026-11-11.
var ufwBeforeUDP = map[ufw.Family][]uint16{ufw.IPv4: {68, 5353, 1900}, ufw.IPv6: {546, 5353, 1900}}

// dockerProxy is the program Docker's userland proxy runs as on a published
// port. Docker sends outside traffic to a published port through its own
// NAT and FORWARD rules, before ufw's input rules see it, so ufw doesn't
// block it and ufw allow wouldn't change that (Docker's documentation,
// "Packet filtering and firewalls"; not run on a VM).
const dockerProxy = "docker-proxy"

// dhcpClient is the DHCPv4 client's port. Its server's answers come in as
// replies to its own requests, or on a packet socket before it has an
// address, so firewalld's zones need no rule for it (not run on a VM).
const dhcpClient = 68

// blockedPort is one port, of one protocol, the firewall would block, and
// the first program ss names on it ("" for none).
type blockedPort struct {
	port    uint16
	proto   string
	process string
}

func (b blockedPort) spec() string { return fmt.Sprintf("%d/%s", b.port, b.proto) }

func (b blockedPort) String() string {
	if b.process == "" {
		return b.spec()
	}
	return b.spec() + " (" + b.process + ")"
}

// addPort puts s's port in bs once, keeping the first program named on it.
func addPort(bs []blockedPort, s reload.Socket) []blockedPort {
	i := slices.IndexFunc(bs, func(b blockedPort) bool { return b.port == s.Port && b.proto == s.Proto })
	switch {
	case i < 0:
		return append(bs, blockedPort{port: s.Port, proto: s.Proto, process: s.Process})
	case bs[i].process == "":
		bs[i].process = s.Process
	}
	return bs
}

// blockedFinding is CodePortsBlocked's finding of bs, by fw ("ufw",
// "firewalld"), with keep, the command that keeps the first one open;
// none for no port. TCP comes first, each protocol's ports ascending.
func blockedFinding(fw string, bs []blockedPort, keep func(blockedPort) string) []engine.Finding {
	if len(bs) == 0 {
		return nil
	}
	slices.SortFunc(bs, func(a, b blockedPort) int {
		return cmp.Or(cmp.Compare(a.proto, b.proto), cmp.Compare(a.port, b.port))
	})
	names := make([]string, len(bs))
	for i, b := range bs {
		names[i] = b.String()
	}
	one := "it"
	if len(bs) > 1 {
		one = "one"
	}
	return []engine.Finding{{Code: CodePortsBlocked, Source: othersSource, Volatile: true,
		Detail: fmt.Sprintf("%s will block %s. To keep %s open, run %s first.", fw, strings.Join(names, ", "), one, keep(bs[0]))}}
}

// unreadFinding is CodePortsUnread's finding: the sockets were not all
// read, so the other ports fw would block are not established.
func unreadFinding(fw string) engine.Finding {
	return engine.Finding{Code: CodePortsUnread, Source: othersSource, Volatile: true, Detail: "hardn can't tell which other ports " + fw + " will block."}
}

// others are the sockets Enable may block: every listening socket ss shows,
// but loopback's (a loopback address, or bound to lo), which no other host
// reaches, and TCP's on ssh, the SSH ports Enable lets through. unread: a
// read failed or left lines unread.
func others(ctx context.Context, sys host.System, ssh func(port uint16) bool) (socks []reload.Socket, unread bool) {
	all, bad, err := reload.ReadListening(ctx, sys)
	if err != nil {
		return nil, true
	}
	for _, s := range all {
		if s.Addr.Unmap().IsLoopback() || s.Device == "lo" || s.Proto == "tcp" && ssh(s.Port) {
			continue
		}
		socks = append(socks, s)
	}
	return socks, len(bad) > 0
}

// ufwOthers are the findings of the ports ufw, once changes ran, would block
// that it lets through now: under what ufw shows while active, else
// inactive (every port open), against what verdicts evaluates after (the
// status's rules while active, else the saved rules; deny where changes set
// it). A port is listed where one of its sockets is let through now and
// blocked or denied after, and none is let through after, from anywhere or
// some sources: a port a rule of the user's still lets in (Restricted,
// Excepted), or one family still reaches (IPv6 off in ufw, IPv4 allowed),
// is not, as SSH's IPv4-only case is not refused (ipv4Only). A port changes
// let through, sshd's ports (Sockets.Ports) and the UDP ports before.rules
// accepts are not listed either.
func ufwOthers(ctx context.Context, sys host.System, st ufw.State, sk Sockets, changes []engine.Change) []engine.Finding {
	socks, unread := others(ctx, sys, func(p uint16) bool { return slices.Contains(sk.Ports(), p) || letThrough(changes, p) })
	now := ufw.Firewall{Active: st.Status.Active, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Status.Incoming}
	after := ufw.Firewall{Active: true, IPv6: st.IPv6, NoKernelIPv6: st.NoKernelIPv6, Incoming: st.Incoming}
	rules := st.Rules
	if st.Status.Active {
		after.Incoming, rules = st.Status.Incoming, st.Status.Rules
	}
	if does(changes).SetsDeny {
		after.Incoming = ufw.PolicyDeny
	}
	var bs, reached []blockedPort // blocked after, of a socket let through now; let through after
	for _, s := range socks {
		if s.Process == dockerProxy {
			continue
		}
		for _, n := range socketNeeds(s) {
			if n.Proto == "udp" && slices.Contains(ufwBeforeUDP[n.Family], n.Port) {
				continue
			}
			was := ufw.Evaluate(st.Status.Rules, now, []ufw.Need{n})[0].Verdict
			switch will := ufw.Evaluate(rules, after, []ufw.Need{n})[0].Verdict; {
			case will != ufw.Blocked && will != ufw.Denied:
				reached = addPort(reached, s)
			case was.OK():
				bs = addPort(bs, s)
			}
		}
	}
	bs = slices.DeleteFunc(bs, func(b blockedPort) bool {
		return slices.ContainsFunc(reached, func(r blockedPort) bool { return r.port == b.port && r.proto == b.proto })
	})
	out := blockedFinding("ufw", bs, func(b blockedPort) string { return "ufw allow " + b.spec() })
	if unread {
		out = append(out, unreadFinding("ufw"))
	}
	return out
}

// socketNeeds are what ufw must let through for s to be reached: its
// address in its IP version, and IPv4 too for a dual-stack socket (ss's
// "*").
func socketNeeds(s reload.Socket) []ufw.Need {
	a, fam := s.Addr.Unmap(), ufw.IPv4
	if a.Is6() {
		fam = ufw.IPv6
	}
	ns := []ufw.Need{{Port: s.Port, Family: fam, Addr: a, Proto: s.Proto, Why: "listener " + s.String()}}
	if s.Dual {
		ns = append(ns, ufw.Need{Port: s.Port, Family: ufw.IPv4, Addr: netip.IPv4Unspecified(), Proto: s.Proto, Why: "listener " + s.String() + " (dual-stack)"})
	}
	return ns
}

// firewalldOthers are the findings of the ports firewalld, started as p
// plans, would block: firewalld is stopped, so every port is open now, and
// a port is listed where no zone of p lets it through (EnablePlan.Lets).
// ssh are the SSH ports Enable lets through.
func firewalldOthers(ctx context.Context, sys host.System, p firewalld.EnablePlan, ssh []uint16) []engine.Finding {
	socks, unread := others(ctx, sys, func(port uint16) bool { return slices.Contains(ssh, port) })
	var bs []blockedPort
	for _, s := range socks {
		if s.Proto == "udp" && s.Port == dhcpClient {
			continue
		}
		ok, err := p.Lets(ctx, sys, s.Port, s.Proto)
		if err != nil {
			return []engine.Finding{unreadFinding("firewalld")}
		}
		if !ok {
			bs = addPort(bs, s)
		}
	}
	out := blockedFinding("firewalld", bs, func(b blockedPort) string { return "firewall-offline-cmd --add-port=" + b.spec() })
	if unread {
		out = append(out, unreadFinding("firewalld"))
	}
	return out
}

// withFindings is changes with fs's findings added to the change of id; fs
// is not called, and nothing read, where there is no such change (a plan
// that only adds ufw's service to the boot services blocks nothing new).
func withFindings(changes []engine.Change, id string, fs func() []engine.Finding) []engine.Change {
	if i := slices.IndexFunc(changes, func(c engine.Change) bool { return c.ID == id }); i >= 0 {
		changes[i].Findings = append(changes[i].Findings, fs()...)
	}
	return changes
}
