package drift

import (
	"context"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	dnsctl "github.com/abbott/hardn/pkg/engine/controls/dns"
	"github.com/abbott/hardn/pkg/engine/controls/firewall"
	"github.com/abbott/hardn/pkg/engine/controls/logforward"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/controls/sshchange"
	"github.com/abbott/hardn/pkg/engine/controls/updates"
	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/detect"
	hostdns "github.com/abbott/hardn/pkg/host/dns"
	"github.com/abbott/hardn/pkg/host/firewalld"
	"github.com/abbott/hardn/pkg/host/logfwd"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/ufw"
)

// Words a fact's value takes.
const (
	On           = "on"
	Off          = "off"
	Yes          = "yes"
	No           = "no"
	NotInstalled = "not installed"
	// NotApplicable: hardn does not manage it on this host (automatic
	// updates on Proxmox VE).
	NotApplicable = "not applicable"
	// unsure marks a set's item whose own state is not established, such
	// as an account a rule hardn does not read may make an admin.
	unsure = " (not established)"
)

// NoSS is why the listening ports are not compared on a host with no ss;
// NoSSBaseline why on a host that has ss, against a baseline saved when it
// had none.
const (
	NoSS         = "ss is not installed"
	NoSSBaseline = "the baseline was saved without ss"
)

// Read reads every fact of Specs from sys, a host with facts f, through
// the readers the screens and the status checks use: it runs only reads,
// writes nothing and plans nothing. A fact that cannot be established is
// returned as such, with why; Read itself fails only with ctx's error. It
// needs root, as sshd -T, /etc/shadow and the elevation tools' rules do:
// as another user most facts are not established.
func Read(ctx context.Context, sys host.System, f detect.Facts) (Facts, error) {
	var out Facts
	eff, effErr := ssh.ReadEffective(ctx, sys, f)
	out = append(out, readUsers(ctx, sys, f, eff, effErr)...)
	out = append(out, readSSH(eff, effErr)...)
	out = append(out, readFirewall(ctx, sys, f)...)
	out = append(out, readDNS(ctx, sys, f), readUpdates(ctx, sys, f), readSyslog(ctx, sys, f))
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("drift: %w", err)
	}
	return out, nil
}

// readUsers reads the accounts host-wide (decision 6), as the User
// Management check does (users.ReadElevation's rules, accounts.Read,
// accounts.ReadShadow) and the SSH keys screen does (authkeys.ReadAccounts
// over sshd -T's AuthorizedKeysFile): each account, the first line of its
// name, that a tool lets run every command as root; each of uid 0; the
// keys of both kinds, by type and fingerprint; and each with an empty
// password field.
func readUsers(ctx context.Context, sys host.System, f detect.Facts, eff effective, effErr error) []Fact {
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		return []Fact{unknown(Admins, err), unknown(UID0, err), unknown(Keys, err), unknown(EmptyPasswords, err)}
	}
	var us []accounts.User
	seen := map[string]bool{}
	for _, u := range db.Users {
		if !seen[u.Name] {
			us = append(us, u)
		}
		seen[u.Name] = true
	}

	elev := users.ReadElevation(ctx, sys, f)
	admins := unknown(Admins, "")
	holds := map[string]bool{} // the accounts whose keys are kept
	if nr := elev.NotRead(); len(nr) > 0 {
		admins.Why = nr[0].Tool + "'s rules are not read"
		if nr[0].Err != nil {
			admins.Why += ": " + nr[0].Err.Error()
		}
	} else {
		var names []string
		for _, u := range us {
			if u.UID == 0 {
				continue
			}
			switch admin(elev.GrantsOf(db, u)) {
			case detect.Yes:
				names = append(names, u.Name)
			case detect.Unknown:
				names = append(names, u.Name+unsure)
			default:
				continue
			}
			holds[u.Name] = true
		}
		admins = set(Admins, names)
	}

	var zero []string
	for _, u := range us {
		if u.UID == 0 {
			zero = append(zero, u.Name)
			holds[u.Name] = true
		}
	}

	keys := unknown(Keys, "the admins are not established")
	switch {
	case !admins.Known:
	case effErr != nil:
		keys = unknown(Keys, effErr)
	case len(eff.AuthorizedKeysFile) == 0:
		keys = unknown(Keys, "sshd -T printed no authorizedkeysfile")
	default:
		if accts, err := authkeys.ReadAccounts(ctx, sys, eff.AuthorizedKeysFile); err != nil {
			keys = unknown(Keys, err)
		} else {
			keys = set(Keys, keyItems(accts, holds))
		}
	}

	var empty Fact
	if sh, err := accounts.ReadShadow(ctx, sys); err != nil {
		empty = unknown(EmptyPasswords, err)
	} else {
		var names []string
		for _, u := range us {
			if sh.PasswordOf(u) == accounts.PasswordEmpty {
				names = append(names, u.Name)
			}
		}
		empty = set(EmptyPasswords, names)
	}
	return []Fact{admins, set(UID0, zero), keys, empty}
}

// admin is what the tools' grants make of an account: Yes where one lets
// it run every command as root, Unknown where none does and one is not
// established, else No (details.User.Admin's rule).
func admin(grants []users.Grant) detect.Tri {
	t := detect.No
	for _, g := range grants {
		switch g.Grants {
		case detect.Yes:
			return detect.Yes
		case detect.Unknown:
			t = detect.Unknown
		}
	}
	return t
}

// keyItems are the keys of the accounts holds names, one item for each
// account and key: the name, the key's type and its fingerprint
// (authkeys.Review: every key of the file, the weak ones sshd refuses
// too). A file that is there and not read is an item of its own, so that
// what it holds is never taken as nothing.
func keyItems(accts []authkeys.Account, holds map[string]bool) []string {
	var items []string
	for _, a := range accts {
		if !holds[a.User.Name] {
			continue
		}
		for _, file := range a.Files {
			if file.Err != nil {
				items = append(items, a.User.Name+" "+file.Path+unsure)
				continue
			}
			ks, _ := authkeys.Review(file)
			for _, k := range ks {
				items = append(items, a.User.Name+" "+k.Type+" "+k.Fingerprint)
			}
		}
	}
	return items
}

// effective is sshd -T's answer.
type effective = sshd.Effective

// readSSH is sshd's configuration as sshd -T prints it (ssh.ReadEffective,
// the SSH checks' reading): root login, whether a password logs in (the
// SSH Login screen's rule, sshchange.Reading.PasswordsOn), the ports, the
// listeners and AllowUsers. Match blocks are not read.
func readSSH(eff effective, err error) []Fact {
	ids := []string{RootLogin, PasswordLogin, SSHPorts, SSHListeners, AllowUsers}
	if err != nil {
		var out []Fact
		for _, id := range ids {
			out = append(out, unknown(id, err))
		}
		return out
	}
	r := sshchange.Reading{Effective: eff}
	root := unknown(RootLogin, "sshd -T printed no value hardn knows, or its sources disagree")
	if eff.PermitRootLogin.Known {
		root = value(RootLogin, r.RootValue())
	}
	pw := unknown(PasswordLogin, "sshd -T printed no value hardn knows, or its sources disagree")
	if eff.PasswordAuthentication.Known && eff.KbdInteractiveAuthentication.Known && eff.UsePAM.Known {
		pw = value(PasswordLogin, yesNo(r.PasswordsOn()))
	}
	list := func(id string, s []string, known bool) Fact {
		if !known {
			return unknown(id, "not established from sshd -T")
		}
		return value(id, strings.Join(sorted(s), ", "))
	}
	var ls []string
	for _, l := range eff.Listeners.All {
		ls = append(ls, l.String())
	}
	return []Fact{root, pw, list(SSHPorts, eff.Port.Values, eff.Port.Known),
		list(SSHListeners, ls, eff.Listeners.Known), list(AllowUsers, eff.AllowUsers.Values, eff.AllowUsers.Known)}
}

func yesNo(b bool) string {
	if b {
		return Yes
	}
	return No
}

func triFact(id string, t detect.Tri, why string) Fact {
	switch t {
	case detect.Yes:
		return value(id, Yes)
	case detect.No:
		return value(id, No)
	}
	return unknown(id, why)
}

// readFirewall is the firewall hardn reads on this host, as the Firewall
// screen reads it but for its plan, which is never made: ufw (ufw.Read,
// ufw.ReadBootUnit); on Rocky Linux, AlmaLinux and RHEL firewalld
// (firewalld.Read, and firewalld.ReadRuntime while it runs); on Proxmox VE
// pve-firewall's state alone (pve.ReadFirewall), with no ufw call. Then
// the listening ports against those rules (firewall.ReadPorts).
func readFirewall(ctx context.Context, sys host.System, f detect.Facts) []Fact {
	all := func(why any) []Fact {
		return []Fact{unknown(FirewallState, why), unknown(Incoming, why), unknown(FirewallBoot, why), unknown(Rules, why), unknown(Ports, why)}
	}
	onPVE, err := pve.Applies(f.Proxmox)
	switch {
	case err != nil:
		return all(err)
	case onPVE:
		const notRead = "not read on Proxmox VE"
		fw, err := pve.ReadFirewall(ctx, sys, f.Proxmox)
		if err != nil {
			return all(err)
		}
		why := "pve-firewall's state is not established"
		if fw.Err != nil {
			why = fw.Err.Error()
		}
		state := unknown(FirewallState, why)
		switch fw.Enabled {
		case detect.Yes:
			state = value(FirewallState, On)
		case detect.No:
			state = value(FirewallState, Off)
		}
		in := unknown(Incoming, why)
		if fw.PolicyIn != "" {
			in = value(Incoming, strings.ToLower(fw.PolicyIn))
		}
		return []Fact{state, in, unknown(FirewallBoot, notRead), unknown(Rules, notRead), unknown(Ports, notRead)}
	}
	r := firewall.Reading{EL: f.Platform.Distro.EL()}
	var facts []Fact
	if r.EL {
		r.Firewalld, r.FirewalldErr = firewalld.Read(ctx, sys)
		facts = firewalldFacts(ctx, sys, r.Firewalld, r.FirewalldErr)
	} else {
		r.State, r.StateErr = ufw.Read(ctx, sys, f.Proxmox)
		boot := detect.Unknown
		if r.StateErr == nil && r.State.Installed {
			boot, _ = ufw.ReadBootUnit(ctx, sys, f)
		}
		facts = ufwFacts(r.State, r.StateErr, boot)
	}
	return append(facts, portsFact(firewall.ReadPorts(ctx, sys, r)))
}

// ufwFacts are ufw's: on while its chains are loaded, the saved default
// incoming policy, whether its service starts at boot, and every saved
// rule, the ones it loads when it starts, in ufw's order: the first rule
// that matches is the one applied.
func ufwFacts(st ufw.State, err error, boot detect.Tri) []Fact {
	switch {
	case err != nil:
		return []Fact{unknown(FirewallState, err), unknown(Incoming, err), unknown(FirewallBoot, err), unknown(Rules, err)}
	case !st.Installed:
		return []Fact{value(FirewallState, NotInstalled), value(Incoming, ""), value(FirewallBoot, No), set(Rules, nil)}
	}
	const unread = "ufw printed lines hardn does not read"
	state := unknown(FirewallState, unread)
	if st.Status.Known {
		state = value(FirewallState, Off)
		if st.Status.Active {
			state = value(FirewallState, On)
		}
	}
	in := unknown(Incoming, "ufw's default incoming policy is not set")
	if st.Incoming != "" {
		in = value(Incoming, string(st.Incoming))
	}
	rules := unknown(Rules, unread)
	if len(st.Bad) == 0 {
		var items []string
		for _, r := range st.Rules {
			items = append(items, ruleWords(r))
		}
		rules = ordered(Rules, items)
	}
	return []Fact{state, in, triFact(FirewallBoot, boot, "whether ufw starts at boot is not established"), rules}
}

// ruleWords is one saved ufw rule as an item: its action and direction,
// then what it matches, "allow in 8080/tcp from 192.0.2.7 on eth0 (IPv6)".
// A rule's comment is no part of it.
func ruleWords(r ufw.Rule) string {
	w := []string{r.Action, r.Dir}
	switch {
	case r.App != "":
		w = append(w, "app "+r.App)
	case r.DPort != "" && r.Proto != "any":
		w = append(w, r.DPort+"/"+r.Proto)
	case r.DPort != "":
		w = append(w, r.DPort)
	case r.Proto != "any":
		w = append(w, "proto "+r.Proto)
	default:
		w = append(w, "anything")
	}
	if r.Dst != "" {
		w = append(w, "to "+r.Dst)
	}
	if r.Src != "" {
		w = append(w, "from "+r.Src)
	}
	if r.Iface != "" {
		w = append(w, "on "+r.Iface)
	}
	if r.Family == ufw.IPv6 {
		w = append(w, "(IPv6)")
	}
	return strings.Join(w, " ")
}

// firewalldFacts are firewalld's: on while it runs, its default zone,
// whether it starts at boot, and, while it runs, what each zone a
// connection can arrive in lets in. A rich rule's own text is not read:
// its zone has one item saying it has some.
func firewalldFacts(ctx context.Context, sys host.System, st firewalld.State, err error) []Fact {
	if err != nil {
		return []Fact{unknown(FirewallState, err), unknown(Incoming, err), unknown(FirewallBoot, err), unknown(Rules, err)}
	}
	boot := triFact(FirewallBoot, st.Enabled, "whether firewalld starts at boot is not established")
	const down = "firewalld is not running"
	switch {
	case st.Installed == detect.No:
		return []Fact{value(FirewallState, NotInstalled), value(Incoming, ""), value(FirewallBoot, No), set(Rules, nil)}
	case st.Running == detect.No:
		return []Fact{value(FirewallState, Off), unknown(Incoming, down), boot, unknown(Rules, down)}
	case st.Running != detect.Yes:
		const why = "whether firewalld runs is not established"
		return []Fact{unknown(FirewallState, why), unknown(Incoming, why), boot, unknown(Rules, why)}
	}
	var rules Fact
	if rt, err := firewalld.ReadRuntime(ctx, sys, st); err != nil {
		rules = unknown(Rules, err)
	} else {
		var items []string
		for _, z := range rt.Zones {
			add := func(kind string, vs ...string) {
				for _, v := range vs {
					items = append(items, "zone "+z.Name+": "+kind+" "+v)
				}
			}
			add("target", z.Target)
			add("interface", z.Interfaces...)
			add("source", z.Sources...)
			add("service", z.Services...)
			add("port", z.Ports...)
			add("forward", z.Forwards...)
			if z.Rich {
				add("rich rules,", "not read")
			}
		}
		rules = set(Rules, items)
	}
	return []Fact{value(FirewallState, On), value(Incoming, "default zone "+st.Default), boot, rules}
}

// portsFact is decision 5: the listening ports the firewall lets in from
// outside, each "443/tcp": open, let in from some sources, or not filtered
// at all. One whose reach is not established is an item that says so. A
// port on loopback alone, a blocked one and a rule with nothing listening
// are never items. Without ss the fact is not compared; where ss failed,
// or printed lines hardn does not read, it is not established.
func portsFact(p firewall.Ports) Fact {
	switch {
	case p.NoSS:
		f := unknown(Ports, NoSS)
		f.NotCompared = true
		return f
	case p.Failed:
		return unknown(Ports, "ss failed")
	case p.Partial:
		return unknown(Ports, "ss printed lines hardn does not read")
	}
	var items []string
	for _, row := range p.List {
		item := row.Spec + "/" + row.Proto
		switch row.Reach {
		case firewall.ReachOpen, firewall.ReachSome, firewall.ReachUnfiltered:
			items = append(items, item)
		case firewall.ReachUnknown:
			items = append(items, item+unsure)
		}
	}
	return set(Ports, items)
}

// readDNS is the nameservers in use, in their order, as the DNS check
// reads them: /etc/resolv.conf's, or systemd-resolved's global ones.
func readDNS(ctx context.Context, sys host.System, f detect.Facts) Fact {
	s, err := hostdns.Read(ctx, sys, f)
	switch {
	case err != nil:
		return unknown(Nameservers, err)
	case s.Err != nil:
		return unknown(Nameservers, s.Err)
	case s.Manager == hostdns.ManagerUnknown:
		return unknown(Nameservers, hostdns.ErrUnknown)
	}
	return value(Nameservers, strings.Join(dnsctl.InUse(s), ", "))
}

// readUpdates is whether the host updates itself, as the Auto Updates
// check reads it: on for Configured and Partial, as its screen's On.
func readUpdates(ctx context.Context, sys host.System, f detect.Facts) Fact {
	c := updates.Auto{}
	if ok, _ := c.Applies(f); !ok {
		return value(AutoUpdates, NotApplicable)
	}
	st, err := c.Check(ctx, sys, f)
	switch {
	case err != nil:
		return unknown(AutoUpdates, err)
	case st.State == engine.Configured, st.State == engine.Partial:
		return value(AutoUpdates, On)
	case st.State == engine.NotConfigured:
		return value(AutoUpdates, Off)
	case st.State == engine.NotApplicable:
		return value(AutoUpdates, NotApplicable)
	}
	return unknown(AutoUpdates, st.Detail)
}

// readSyslog is the set of syslog servers the host forwards to, as Syslog
// Settings reads it (logfwd.Read), hardn's or not, each "192.0.2.51:514
// TCP" (docs/engine/design-syslog-servers.md §2.7). A host with journald
// alone forwards to none. Connections are not compared: they come and go.
// A logger hardn does not read, or settings it cannot make out, is not
// established.
func readSyslog(ctx context.Context, sys host.System, f detect.Facts) Fact {
	r := logfwd.Read(ctx, sys, f)
	if r.Err != nil {
		return unknown(SyslogServers, r.Err)
	}
	var items []string
	for _, fw := range r.Forwards {
		items = append(items, logforward.ServerWords(fw.Server))
	}
	return set(SyslogServers, items)
}
