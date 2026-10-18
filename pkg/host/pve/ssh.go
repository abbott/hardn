package pve

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// RootLogin is PermitRootLogin on Proxmox VE: key-only, never no (brief
// 2.5), so cluster nodes keep logging in to each other as root.
const RootLogin = "prohibit-password"

// GroupFile is where RootGroups reads root's supplementary groups.
const GroupFile = "/etc/group"

// keyOnlyDetail is the detail brief 2.5 asks a status to show.
const keyOnlyDetail = "root: key-only (required by Proxmox clustering)"

// PlanSSH applies the Proxmox VE rules to desired, the settings hardn wants
// in 00-hardn.conf, and returns them as they are to be written. On another
// host it returns a copy of desired and nothing else.
//
//   - Every PermitRootLogin is prohibit-password, and one is added when
//     desired has none: Proxmox VE's own sshd_config says yes.
//   - A Port other than 22, or a ListenAddress with a port other than 22, is
//     refused with ErrPortChange (§3.18).
//   - root stays reachable by key: an AllowUsers that lets no root in from
//     every host gets root added; PubkeyAuthentication no, a DenyUsers or
//     DenyGroups pattern matching root, an AllowGroups matching none of
//     rootGroups, an AuthenticationMethods with no list that is publickey
//     alone, and an AuthorizedKeysFile that does not read root's
//     authorized_keys are refused with ErrRootExcluded.
//
// rootGroups are root's groups (RootGroups); nil when not established, and
// then only the group root counts as one of them. Each change is a Finding.
func PlanSSH(p detect.Proxmox, desired []migrate.Directive, rootGroups []string) ([]migrate.Directive, []Finding, error) {
	out := make([]migrate.Directive, len(desired))
	for i, d := range desired {
		out[i] = migrate.Directive{Keyword: d.Keyword, Args: slices.Clone(d.Args)}
	}
	if ok, err := Applies(p); !ok || err != nil {
		return out, nil, err
	}
	var found []Finding
	login, allow := false, -1
	allowsRoot := false
	for i, d := range out {
		k := sshd.CanonicalKeyword(d.Keyword, d.Args)
		refuse := func(why string) error {
			return fmt.Errorf("pve: %s %s: %s: %w", d.Keyword, strings.Join(d.Args, " "), why, ErrRootExcluded)
		}
		switch k {
		case "permitrootlogin":
			login = true
			if v := strings.ToLower(strings.Join(d.Args, " ")); v != RootLogin && v != "without-password" {
				found = append(found, Finding{Kind: FindingRootKeyOnly, Detail: fmt.Sprintf("PermitRootLogin %s becomes %s: %s", v, RootLogin, keyOnlyDetail)})
			}
			out[i].Args = []string{RootLogin}
		case "port":
			for _, a := range d.Args {
				if n, err := strconv.Atoi(a); err != nil || n != SSHPort {
					return nil, nil, fmt.Errorf("pve: Port %s: %w", a, ErrPortChange)
				}
			}
		case "listenaddress":
			if port, ok := listenPort(d.Args); ok && port != strconv.Itoa(SSHPort) {
				return nil, nil, fmt.Errorf("pve: ListenAddress %s: %w", strings.Join(d.Args, " "), ErrPortChange)
			}
		case "allowusers":
			if allow < 0 {
				allow = i
			}
			allowsRoot = allowsRoot || slices.ContainsFunc(d.Args, func(a string) bool { u, h, at := strings.Cut(a, "@"); return match("root", u) && !at && h == "" })
		case "denyusers":
			if slices.ContainsFunc(d.Args, func(a string) bool { u, _, _ := strings.Cut(a, "@"); return match("root", u) }) {
				return nil, nil, refuse("a pattern matches root")
			}
		case "denygroups":
			if anyMatch(d.Args, groupsOrRoot(rootGroups)) || rootGroups == nil {
				return nil, nil, refuse("a pattern matches one of root's groups, or root's groups are not established")
			}
		case "allowgroups":
			if !anyMatch(d.Args, groupsOrRoot(rootGroups)) {
				return nil, nil, refuse("no pattern matches one of root's groups")
			}
		case "pubkeyauthentication":
			if strings.EqualFold(strings.Join(d.Args, " "), "no") {
				return nil, nil, refuse("root logs in by key")
			}
		case "authenticationmethods":
			if !keyAlone(d.Args) {
				return nil, nil, refuse("no list is publickey alone, which cluster nodes use")
			}
		case "authorizedkeysfile":
			if !slices.ContainsFunc(d.Args, readsRootKeys) {
				return nil, nil, refuse("it does not read " + RootKeys)
			}
		}
	}
	if !login {
		out = append(out, migrate.Directive{Keyword: "PermitRootLogin", Args: []string{RootLogin}})
		found = append(found, Finding{Kind: FindingRootKeyOnly, Detail: "PermitRootLogin " + RootLogin + " added: " + keyOnlyDetail})
	}
	if allow >= 0 && !allowsRoot {
		out[allow].Args = append(out[allow].Args, "root")
		found = append(found, Finding{Kind: FindingRootAdded, Detail: "root added to AllowUsers: cluster nodes log in to each other as root, by key"})
	}
	return out, found, nil
}

// CheckPort says whether sshd may be set to listen on ports: on Proxmox VE
// only {22} (or no change, an empty list) is allowed, and anything else is
// ErrPortChange, with the reason (§3.18); ErrProxmoxUnknown when that is not
// established and ports is not {22}; nil on another host.
func CheckPort(p detect.Proxmox, ports []uint16) error {
	if len(ports) == 0 || !slices.ContainsFunc(ports, func(n uint16) bool { return n != SSHPort }) {
		return nil
	}
	ok, err := Applies(p)
	switch {
	case err != nil:
		return err
	case ok:
		return fmt.Errorf("pve: port %v: %w", ports, ErrPortChange)
	}
	return nil
}

// listenPort returns the port a ListenAddress argument names, as sshd
// splits "host:port" and "[v6]:port"; a bare IPv6 address has no port.
func listenPort(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	a := args[0]
	if strings.HasPrefix(a, "[") {
		_, port, ok := strings.Cut(a, "]:")
		return port, ok
	}
	if strings.Count(a, ":") == 1 {
		_, port, _ := strings.Cut(a, ":")
		return port, true
	}
	return "", false
}

// keyAlone: one of AuthenticationMethods' lists is "any" or publickey
// alone, which a cluster node's BatchMode root login satisfies.
func keyAlone(lists []string) bool {
	return slices.ContainsFunc(lists, func(l string) bool {
		return l == "any" || !slices.ContainsFunc(strings.Split(l, ","), func(m string) bool { return m != "publickey" })
	})
}

// readsRootKeys: an AuthorizedKeysFile entry that names root's keys file
// (sshd takes a relative one from the home directory, and expands %h), or
// the cluster-wide file it links to.
func readsRootKeys(f string) bool {
	switch f {
	case ".ssh/authorized_keys", "%h/.ssh/authorized_keys", RootKeys, ClusterKeys:
		return true
	}
	return false
}

func groupsOrRoot(gs []string) []string {
	if gs == nil {
		return []string{"root"}
	}
	return gs
}

func anyMatch(patterns, names []string) bool {
	return slices.ContainsFunc(patterns, func(p string) bool {
		return slices.ContainsFunc(names, func(n string) bool { return match(n, p) })
	})
}

// match is sshd's match_pattern: '*' matches any run of bytes, '?' one
// byte (ssh_config(5), PATTERNS).
func match(s, p string) bool {
	for p != "" {
		switch {
		case p[0] == '*':
			p = strings.TrimLeft(p, "*")
			for i := 0; i <= len(s); i++ {
				if match(s[i:], p) {
					return true
				}
			}
			return false
		case s == "" || p[0] != '?' && p[0] != s[0]:
			return false
		}
		s, p = s[1:], p[1:]
	}
	return s == ""
}

// RootGroups returns root's primary group and the groups /etc/group lists
// root in, as sshd's AllowGroups and DenyGroups see them.
func RootGroups(ctx context.Context, sys host.System) ([]string, error) {
	u, err := sys.LookupUser(ctx, "root")
	if err != nil {
		return nil, fmt.Errorf("pve: root's groups: %w", err)
	}
	data, err := sys.ReadFile(ctx, GroupFile)
	if err != nil {
		return nil, fmt.Errorf("pve: root's groups: %w", err)
	}
	var gs []string
	for _, l := range strings.Split(string(data), "\n") {
		f := strings.Split(l, ":")
		if len(f) != 4 {
			continue
		}
		if gid, err := strconv.Atoi(f[2]); err == nil && gid == u.GID || slices.Contains(strings.Split(f[3], ","), "root") {
			gs = append(gs, f[0])
		}
	}
	return gs, nil
}

// SSHCheck is what sshd's effective configuration says against the Proxmox
// VE rules.
type SSHCheck struct {
	// RootKeyOnly: PermitRootLogin is prohibit-password, for every
	// connection a Match probe looked at.
	RootKeyOnly detect.Tri
	// RootByKey: root can log in with a key: PermitRootLogin, Pubkey-
	// Authentication, the user and group lists, AuthenticationMethods and
	// AuthorizedKeysFile all let it. Unknown when a value is not
	// established, or a pattern restricts root to some hosts, or a Match
	// block changes one of them for some connections.
	RootByKey detect.Tri
	// Port22: sshd's Port set is {22} and every ListenAddress is on 22.
	Port22   detect.Tri
	Findings []Finding
}

// rootKeywords are the keywords whose Match override can change whether
// root logs in by key.
var rootKeywords = []string{"permitrootlogin", "pubkeyauthentication", "allowusers", "denyusers", "allowgroups", "denygroups"}

// CheckSSH checks e, sshd.ReadEffective's values on a Proxmox VE host,
// against the rules PlanSSH writes. rootGroups are root's groups
// (RootGroups); nil when not established. Proxmox VE's own "PermitRootLogin
// yes" in sshd_config, shadowed by hardn's line, is a
// FindingProxmoxRootLogin: expected, not drift.
func CheckSSH(e sshd.Effective, rootGroups []string) SSHCheck {
	c := SSHCheck{RootKeyOnly: detect.Unknown, RootByKey: detect.Yes, Port22: detect.Unknown}
	if e.PermitRootLogin.Known {
		c.RootKeyOnly = tri(e.PermitRootLogin.Value == RootLogin)
	}
	why := func(t detect.Tri, format string, args ...any) {
		if c.RootByKey == detect.Yes || t == detect.No {
			c.RootByKey = t
		}
		c.Findings = append(c.Findings, Finding{Kind: FindingRootUnreachable, Detail: fmt.Sprintf(format, args...)})
	}
	setting := func(s sshd.Setting, name string, bad ...string) {
		switch {
		case !s.Known:
			why(detect.Unknown, "%s is not established", name)
		case slices.Contains(bad, s.Value):
			why(detect.No, "%s %s", name, s.Value)
		}
	}
	setting(e.PermitRootLogin, "PermitRootLogin", "no", "forced-commands-only")
	setting(e.PubkeyAuthentication, "PubkeyAuthentication", "no")
	users(e, why)
	groups(e, rootGroups, why)
	methods(e, why)
	for _, o := range e.Overrides {
		if !slices.Contains(rootKeywords, o.Keyword) {
			continue
		}
		if o.Keyword == "permitrootlogin" && o.Value != RootLogin && c.RootKeyOnly == detect.Yes {
			c.RootKeyOnly = detect.No
		}
		why(detect.Unknown, "the Match block at %s:%d sets %s %s for connections like %s", o.Block.File, o.Block.Line, o.Keyword, o.Value, o.Conn.Spec())
	}
	c.Port22 = port22(e, &c.Findings)
	for _, cf := range e.Conflicts {
		if f := strings.Fields(cf.Other.Text); cf.Kind == sshd.FindingShadowed && cf.Keyword == "permitrootlogin" &&
			cf.Other.File == sshd.MainConfig && len(f) == 2 && strings.EqualFold(f[1], "yes") {
			c.Findings = append(c.Findings, Finding{Kind: FindingProxmoxRootLogin, File: cf.Other.File, Line: cf.Other.Line,
				Detail: "Proxmox VE's own PermitRootLogin yes, shadowed by " + cf.Hardn.File + ": expected, not drift"})
		}
	}
	return c
}

func users(e sshd.Effective, why func(detect.Tri, string, ...any)) {
	switch {
	case !e.DenyUsers.Known:
		why(detect.Unknown, "DenyUsers is not established")
	default:
		for _, v := range e.DenyUsers.Values {
			if u, h, _ := strings.Cut(v, "@"); match("root", u) && h == "" {
				why(detect.No, "DenyUsers %s matches root", v)
			} else if match("root", u) {
				why(detect.Unknown, "DenyUsers %s denies root from some hosts; the cluster nodes' addresses are not read", v)
			}
		}
	}
	switch {
	case !e.AllowUsers.Known:
		why(detect.Unknown, "AllowUsers is not established")
	case len(e.AllowUsers.Values) == 0:
	case slices.ContainsFunc(e.AllowUsers.Values, func(v string) bool { u, h, _ := strings.Cut(v, "@"); return match("root", u) && h == "" }):
	case slices.ContainsFunc(e.AllowUsers.Values, func(v string) bool { u, _, _ := strings.Cut(v, "@"); return match("root", u) }):
		why(detect.Unknown, "AllowUsers lets root in from some hosts only; the cluster nodes' addresses are not read")
	default:
		why(detect.No, "AllowUsers %s does not match root", strings.Join(e.AllowUsers.Values, " "))
	}
}

func groups(e sshd.Effective, rootGroups []string, why func(detect.Tri, string, ...any)) {
	gs := groupsOrRoot(rootGroups)
	switch {
	case !e.DenyGroups.Known:
		why(detect.Unknown, "DenyGroups is not established")
	case anyMatch(e.DenyGroups.Values, gs):
		why(detect.No, "DenyGroups %s matches one of root's groups", strings.Join(e.DenyGroups.Values, " "))
	case len(e.DenyGroups.Values) > 0 && rootGroups == nil:
		why(detect.Unknown, "DenyGroups is set and root's groups are not established")
	}
	switch {
	case !e.AllowGroups.Known:
		why(detect.Unknown, "AllowGroups is not established")
	case len(e.AllowGroups.Values) == 0 || anyMatch(e.AllowGroups.Values, gs):
	case rootGroups == nil:
		why(detect.Unknown, "AllowGroups is set and root's groups are not established")
	default:
		why(detect.No, "AllowGroups %s matches none of root's groups", strings.Join(e.AllowGroups.Values, " "))
	}
}

// methods checks AuthenticationMethods and the keys sources sshd -T prints:
// "authenticationmethods any" when none is set, "authorizedkeysfile" with
// sshd's default files, "authorizedkeyscommand none" when none is set
// (servconf.c dump_config, fill_default_server_options).
func methods(e sshd.Effective, why func(detect.Tri, string, ...any)) {
	one := func(k string) ([]string, bool) {
		vs := e.Keywords[k]
		if len(vs) != 1 {
			why(detect.Unknown, "%s is not established", k)
			return nil, false
		}
		return strings.Fields(vs[0]), true
	}
	if ms, ok := one("authenticationmethods"); ok && !keyAlone(ms) {
		why(detect.No, "AuthenticationMethods %s: no list is publickey alone", strings.Join(ms, " "))
	}
	files, ok := one("authorizedkeysfile")
	cmd, cmdOK := one("authorizedkeyscommand")
	if ok && cmdOK && !slices.ContainsFunc(files, readsRootKeys) {
		if len(cmd) == 1 && cmd[0] == "none" {
			why(detect.No, "AuthorizedKeysFile %s does not read %s", strings.Join(files, " "), RootKeys)
		} else {
			why(detect.Unknown, "AuthorizedKeysFile does not read %s; AuthorizedKeysCommand may supply root's keys", RootKeys)
		}
	}
}

func port22(e sshd.Effective, found *[]Finding) detect.Tri {
	if !e.Port.Known || len(e.Listeners.SSHD) == 0 {
		return detect.Unknown
	}
	var off []string
	for _, p := range e.Port.Values {
		if p != strconv.Itoa(SSHPort) {
			off = append(off, "Port "+p)
		}
	}
	for _, l := range e.Listeners.SSHD {
		if l.Port != SSHPort {
			off = append(off, "ListenAddress "+l.String())
		}
	}
	if !e.Port.Has(strconv.Itoa(SSHPort)) {
		off = append(off, "no Port 22")
	}
	if len(off) == 0 {
		return detect.Yes
	}
	*found = append(*found, Finding{Kind: FindingPortNot22, Detail: strings.Join(off, ", ") + ": cluster SSH and pve-firewall's management rule use port 22"})
	return detect.No
}

func tri(b bool) detect.Tri {
	if b {
		return detect.Yes
	}
	return detect.No
}
