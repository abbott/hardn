package pve

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"net/netip"
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

// GroupFile is where RootGroups reads root's groups.
const GroupFile = "/etc/group"

// keyOnlyDetail is the detail brief 2.5 asks a status to show.
const keyOnlyDetail = "root: key-only (required by Proxmox clustering)"

// nodeAlgorithms are the signatures a node's root key makes: it is an
// ssh-rsa key (setup_rootsshconfig generates /root/.ssh/id_rsa), which
// OpenSSH signs with rsa-sha2-512 or rsa-sha2-256.
var nodeAlgorithms = []string{"rsa-sha2-512", "rsa-sha2-256"}

// Groups is root's groups, as sshd's AllowGroups and DenyGroups see them.
type Groups struct {
	Names []string
	// Known: Names is established (RootGroups found them). When false only
	// the group root counts as one of root's.
	Known bool
}

func (g Groups) names() []string {
	if !g.Known {
		return []string{"root"}
	}
	return g.Names
}

// PlanSSH applies the Proxmox VE rules to desired, the settings hardn wants
// in 00-hardn.conf, and returns them as they are to be written. On another
// host it returns a copy of desired and nothing else.
//
//   - Every PermitRootLogin is prohibit-password, and one is added when
//     desired has none: Proxmox VE's own sshd_config says yes.
//   - A Port other than 22, or a ListenAddress with a port other than 22, is
//     refused with ErrPortChange (§3.18).
//   - root stays reachable by key from the other nodes: an AllowUsers that
//     lets no root in from every host gets root added; every line excludes
//     says may keep root out is refused with ErrRootExcluded, and so are
//     ListenAddress lines that are all loopback. ListenAddress lines that
//     leave out every address of a family, and an AddressFamily other than
//     any, are a FindingRootUnreachable: the nodes' addresses are not read.
//
// root is RootGroups' answer. Each change is a Finding. A Match or Include
// line is refused with fs.ErrInvalid, as migrate.PlanFor refuses it: the
// rules read every line as global.
func PlanSSH(p detect.Proxmox, desired []migrate.Directive, root Groups) ([]migrate.Directive, []Finding, error) {
	out := make([]migrate.Directive, len(desired))
	for i, d := range desired {
		out[i] = migrate.Directive{Keyword: d.Keyword, Args: slices.Clone(d.Args)}
	}
	if ok, err := Applies(p); !ok || err != nil {
		return out, nil, err
	}
	var found []Finding
	login, allow, allowsRoot := false, -1, false
	var listen []listenAt
	for i, d := range out {
		k := sshd.CanonicalKeyword(d.Keyword, d.Args)
		line := strings.TrimSpace(d.Keyword + " " + strings.Join(d.Args, " "))
		switch k {
		case "match", "include":
			return nil, nil, fmt.Errorf("pve: desired %s: settings for 00-hardn.conf are global lines: %w", d.Keyword, fs.ErrInvalid)
		case "permitrootlogin":
			login = true
			if v := strings.ToLower(strings.Join(d.Args, " ")); v != RootLogin && v != "without-password" {
				found = append(found, Finding{Kind: FindingRootKeyOnly, Detail: fmt.Sprintf("PermitRootLogin %s becomes %s: %s", v, RootLogin, keyOnlyDetail)})
			}
			out[i].Args = []string{RootLogin}
			continue
		case "allowusers":
			if allow < 0 {
				allow = i
			}
			allowsRoot = allowsRoot || slices.ContainsFunc(d.Args, rootFromAnywhere)
			continue
		case "port":
			for _, a := range d.Args {
				if !is22(a) {
					return nil, nil, fmt.Errorf("pve: Port %s: %w", a, ErrPortChange)
				}
			}
		case "listenaddress":
			if port, ok := listenPort(d.Args); ok && !is22(port) {
				return nil, nil, fmt.Errorf("pve: %s: %w", line, ErrPortChange)
			}
			listen = append(listen, parseListen(d.Args))
		case "addressfamily":
			if v := strings.ToLower(strings.Join(d.Args, " ")); v != "any" {
				found = append(found, Finding{Kind: FindingRootUnreachable, Detail: line +
					": sshd accepts one address family only; the cluster nodes' addresses are not read, so whether they reach it is not established"})
			}
		}
		if why := excludes(k, d.Args, root); why != "" {
			return nil, nil, fmt.Errorf("pve: %s: %s: %w", line, why, ErrRootExcluded)
		}
	}
	if f, err := narrows(listen); err != nil {
		return nil, nil, err
	} else if f != nil {
		found = append(found, *f)
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

// excludes says how the directive k args may keep root from logging in by
// key from another node, or "" when it cannot. PlanSSH refuses such a line;
// CheckSSH reads one in a Match block as Unknown. A user pattern with a host
// part does not count as root's: the other nodes' addresses are not read.
func excludes(k string, args []string, root Groups) string {
	v := strings.Join(args, " ")
	switch k {
	case "permitrootlogin":
		if s := strings.ToLower(v); s == "no" || s == "forced-commands-only" {
			return "root may not log in with a key alone"
		}
	case "pubkeyauthentication":
		if strings.EqualFold(v, "no") {
			return "the nodes log in by key"
		}
	case "allowusers":
		if !slices.ContainsFunc(args, rootFromAnywhere) {
			return "no pattern lets root in from every host"
		}
	case "denyusers":
		if slices.ContainsFunc(args, func(a string) bool { u, _, _ := strings.Cut(a, "@"); return match("root", u) }) {
			return "a pattern matches root"
		}
	case "allowgroups":
		if !anyMatch(args, root.names()) {
			return "no pattern matches one of root's groups"
		}
	case "denygroups":
		if anyMatch(args, root.names()) || !root.Known {
			return "a pattern matches one of root's groups, or root's groups are not established"
		}
	case "authenticationmethods":
		if !keyAlone(args) {
			return "no list is any or publickey alone, which a node's login with its one key needs"
		}
	case "authorizedkeysfile":
		if !slices.ContainsFunc(args, readsRootKeys) {
			return "it does not read " + RootKeys
		}
	case "pubkeyacceptedalgorithms":
		if !acceptsNodeKeys(args) {
			return "it accepts neither rsa-sha2-512 nor rsa-sha2-256, the signatures of the nodes' ssh-rsa root keys"
		}
	}
	return ""
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

// is22: s is port 22 as a number (a service name is not read).
func is22(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n == SSHPort
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

// listenAt is a ListenAddress line: its text, its host (an address or a
// name, as written) and whether it names a routing domain.
type listenAt struct {
	text, host string
	rdomain    bool
}

func parseListen(args []string) listenAt {
	l := listenAt{text: strings.Join(args, " "), rdomain: len(args) > 1}
	if len(args) == 0 {
		return l
	}
	switch a := args[0]; {
	case strings.HasPrefix(a, "["):
		l.host, _, _ = strings.Cut(a[1:], "]")
	case strings.Count(a, ":") == 1:
		l.host, _, _ = strings.Cut(a, ":")
	default:
		l.host = a
	}
	return l
}

func (l listenAt) loopback() bool {
	a, err := netip.ParseAddr(l.host)
	return strings.EqualFold(l.host, "localhost") || err == nil && a.Unmap().IsLoopback()
}

// every: l is every address of the IPv4 (v6 false) or IPv6 family.
func (l listenAt) every(v6 bool) bool {
	a, err := netip.ParseAddr(l.host)
	return err == nil && !l.rdomain && a.IsUnspecified() && a.Is6() == v6
}

// narrows checks the ListenAddress lines hardn writes, which replace sshd's
// default of every address: all loopback is ErrRootExcluded, and lines
// that leave out every address of a family (0.0.0.0, ::) are a Finding.
func narrows(listen []listenAt) (*Finding, error) {
	if len(listen) == 0 {
		return nil, nil
	}
	var texts []string
	for _, l := range listen {
		texts = append(texts, l.text)
	}
	at := "ListenAddress " + strings.Join(texts, ", ")
	switch {
	case !slices.ContainsFunc(listen, func(l listenAt) bool { return !l.loopback() }):
		return nil, fmt.Errorf("pve: %s: sshd would listen on loopback only, which the other nodes cannot reach: %w", at, ErrRootExcluded)
	case !slices.ContainsFunc(listen, func(l listenAt) bool { return l.every(false) }) ||
		!slices.ContainsFunc(listen, func(l listenAt) bool { return l.every(true) }):
		return &Finding{Kind: FindingRootUnreachable, Detail: at +
			": sshd listens there only; the cluster nodes' addresses are not read, so whether they reach it is not established"}, nil
	}
	return nil, nil
}

// rootFromAnywhere: an AllowUsers pattern that lets root in from every host
// (no host part).
func rootFromAnywhere(a string) bool { return !strings.Contains(a, "@") && match("root", a) }

// keyAlone: one of AuthenticationMethods' lists is "any" or publickey
// alone. A node logs in with its one key, and a list naming publickey twice
// needs two different keys (sshd_config(5), AuthenticationMethods).
func keyAlone(lists []string) bool {
	return slices.ContainsFunc(lists, func(l string) bool { return l == "any" || l == "publickey" })
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

// acceptsNodeKeys: a PubkeyAcceptedAlgorithms value accepts one of
// nodeAlgorithms (sshd_config(5)): a list of patterns, a "!" one excluding
// what it matches; a list starting with "+" or "^" adds to the default set,
// which has both (OpenSSH 10.0p1's KEX_DEFAULT_PK_ALG); one starting with
// "-" removes what its patterns match from it. sshd -T prints the resulting
// list. A "!" in a "-" list is read as a plain pattern, which refuses more.
func acceptsNodeKeys(args []string) bool {
	if len(args) == 0 || args[0] == "" {
		return false
	}
	v := args[0]
	switch v[0] {
	case '+', '^':
		return true
	case '-':
		pats := strings.Split(v[1:], ",")
		return slices.ContainsFunc(nodeAlgorithms, func(a string) bool {
			return !slices.ContainsFunc(pats, func(p string) bool { return match(a, strings.TrimPrefix(p, "!")) })
		})
	}
	return slices.ContainsFunc(nodeAlgorithms, func(a string) bool { return inList(a, strings.Split(v, ",")) })
}

// inList is sshd's match_pattern_list: some pattern matches name and no
// negated ("!") one does.
func inList(name string, pats []string) bool {
	in := false
	for _, p := range pats {
		q, neg := strings.CutPrefix(p, "!")
		switch {
		case neg && match(name, q):
			return false
		case !neg && match(name, p):
			in = true
		}
	}
	return in
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
// root in, as sshd's AllowGroups and DenyGroups see them. A group file with
// no line for root's primary group id is fs.ErrNotExist: the name sshd would
// match is not established.
func RootGroups(ctx context.Context, sys host.System) (Groups, error) {
	u, err := sys.LookupUser(ctx, "root")
	if err != nil {
		return Groups{}, fmt.Errorf("pve: root's groups: %w", err)
	}
	data, err := sys.ReadFile(ctx, GroupFile)
	if err != nil {
		return Groups{}, fmt.Errorf("pve: root's groups: %w", err)
	}
	g, primary := Groups{Names: []string{}, Known: true}, false
	for _, l := range strings.Split(string(data), "\n") {
		f := strings.Split(l, ":")
		if len(f) != 4 {
			continue
		}
		gid, err := strconv.Atoi(f[2])
		isPrimary := err == nil && gid == u.GID
		primary = primary || isPrimary
		if isPrimary || slices.Contains(strings.Split(f[3], ","), "root") {
			g.Names = append(g.Names, f[0])
		}
	}
	if !primary {
		return Groups{}, fmt.Errorf("pve: root's groups: no line of %s has root's group id %d: %w", GroupFile, u.GID, fs.ErrNotExist)
	}
	return g, nil
}

// SSHInput is what CheckSSH reads, from one reading of the host.
type SSHInput struct {
	Effective sshd.Effective // sshd.ReadEffective's
	// Main and DropIns are the files sshd read (migrate.Find's Main and
	// DropIns), for their Match blocks: sshd -T shows the global values,
	// and its probes only connections they could make, never root's from
	// each node. Main nil: not read, so what a Match block sets is not
	// established.
	Main    []byte
	DropIns []sshd.File
	Root    Groups // RootGroups'
}

// SSHCheck is what sshd's configuration says against the Proxmox VE rules.
type SSHCheck struct {
	// RootKeyOnly: PermitRootLogin is prohibit-password, globally and in
	// every Match block. Unknown when the global value or the files are
	// not established.
	RootKeyOnly detect.Tri
	// RootByKey: root can log in with a key from the other nodes: the
	// global values of PermitRootLogin, PubkeyAuthentication, the user and
	// group lists, AuthenticationMethods, AuthorizedKeysFile (or
	// AuthorizedKeysCommand) and PubkeyAcceptedAlgorithms all let it, and
	// sshd listens on every address of both families. No when one keeps it
	// out. Unknown when a value is not established, a pattern or the
	// listeners restrict root to some hosts, or a Match block sets one of
	// those keywords so that it may keep root out (or, where the global
	// value keeps root out, may let it in) for the connections it matches.
	RootByKey detect.Tri
	// Port22: sshd's Port set is {22}, and every listener, sshd's and on a
	// socket-activated host ssh.socket's, is on 22. Unknown when the Port
	// set or the listeners are not established (Listeners.Known false:
	// ReadEffective's findings say why).
	Port22   detect.Tri
	Findings []Finding
}

// rootKeywords are the keywords whose value decides whether root logs in
// by key, as CheckSSH reads them (AddressFamily and ListenAddress cannot
// be set in a Match block, and their effect is in the listeners).
var rootKeywords = []string{"permitrootlogin", "pubkeyauthentication", "allowusers", "denyusers", "allowgroups", "denygroups",
	"authenticationmethods", "authorizedkeysfile", "pubkeyacceptedalgorithms"}

// CheckSSH checks in, sshd.ReadEffective's values and the files sshd read,
// against the rules PlanSSH writes. Off Proxmox VE it is ErrNotApplicable,
// and ErrProxmoxUnknown when that is not established. Proxmox VE's own
// "PermitRootLogin yes" in sshd_config, shadowed by hardn's line, is a
// FindingProxmoxRootLogin: expected, not drift.
func CheckSSH(p detect.Proxmox, in SSHInput) (SSHCheck, error) {
	switch ok, err := Applies(p); {
	case err != nil:
		return SSHCheck{}, err
	case !ok:
		return SSHCheck{}, fmt.Errorf("pve: sshd: %w", ErrNotApplicable)
	}
	e := in.Effective
	c := SSHCheck{RootKeyOnly: detect.Unknown}
	if e.PermitRootLogin.Known {
		if c.RootKeyOnly = tri(e.PermitRootLogin.Value == RootLogin); c.RootKeyOnly == detect.No {
			c.Findings = append(c.Findings, Finding{Kind: FindingNotKeyOnly, Detail: "PermitRootLogin " + e.PermitRootLogin.Value})
		}
	}
	b := byKey{c: &c, t: map[string]detect.Tri{}}
	b.setting(e.PermitRootLogin, "permitrootlogin", "PermitRootLogin", "no", "forced-commands-only")
	b.setting(e.PubkeyAuthentication, "pubkeyauthentication", "PubkeyAuthentication", "no")
	b.users(e)
	b.groups(e, in.Root)
	b.methods(e)
	b.listening(e.Listeners)
	b.matchBlocks(in)
	c.RootByKey = b.result()
	c.Port22 = port22(e, &c.Findings)
	for _, cf := range e.Conflicts {
		if f := strings.Fields(cf.Other.Text); cf.Kind == sshd.FindingShadowed && cf.Keyword == "permitrootlogin" &&
			cf.Other.File == sshd.MainConfig && len(f) == 2 && strings.EqualFold(f[1], "yes") {
			c.Findings = append(c.Findings, Finding{Kind: FindingProxmoxRootLogin, File: cf.Other.File, Line: cf.Other.Line,
				Detail: "Proxmox VE's own PermitRootLogin yes, shadowed by " + cf.Hardn.File + ": expected, not drift"})
		}
	}
	return c, nil
}

// byKey collects, per keyword, whether its values let root in by key: No
// when one keeps root out, Unknown when that is not established. A keyword
// not in t lets root in.
type byKey struct {
	c *SSHCheck
	t map[string]detect.Tri
}

// not records t (No or Unknown) for k with a FindingRootUnreachable; No
// stays No.
func (b byKey) not(k string, t detect.Tri, file string, line int, format string, args ...any) {
	if cur, ok := b.t[k]; !ok || cur == detect.Unknown {
		b.t[k] = t
	}
	b.c.Findings = append(b.c.Findings, Finding{Kind: FindingRootUnreachable, File: file, Line: line, Detail: fmt.Sprintf(format, args...)})
}

func (b byKey) result() detect.Tri {
	switch {
	case slices.Contains(slices.Collect(maps.Values(b.t)), detect.No):
		return detect.No
	case len(b.t) > 0:
		return detect.Unknown
	}
	return detect.Yes
}

func (b byKey) setting(s sshd.Setting, k, name string, bad ...string) {
	switch {
	case !s.Known:
		b.not(k, detect.Unknown, "", 0, "%s is not established", name)
	case slices.Contains(bad, s.Value):
		b.not(k, detect.No, "", 0, "%s %s", name, s.Value)
	}
}

func (b byKey) users(e sshd.Effective) {
	switch {
	case !e.DenyUsers.Known:
		b.not("denyusers", detect.Unknown, "", 0, "DenyUsers is not established")
	default:
		for _, v := range e.DenyUsers.Values {
			if u, h, _ := strings.Cut(v, "@"); match("root", u) && h == "" {
				b.not("denyusers", detect.No, "", 0, "DenyUsers %s matches root", v)
			} else if match("root", u) {
				b.not("denyusers", detect.Unknown, "", 0, "DenyUsers %s denies root from some hosts; the cluster nodes' addresses are not read", v)
			}
		}
	}
	switch {
	case !e.AllowUsers.Known:
		b.not("allowusers", detect.Unknown, "", 0, "AllowUsers is not established")
	case len(e.AllowUsers.Values) == 0, slices.ContainsFunc(e.AllowUsers.Values, rootFromAnywhere):
	case slices.ContainsFunc(e.AllowUsers.Values, func(v string) bool { u, _, _ := strings.Cut(v, "@"); return match("root", u) }):
		b.not("allowusers", detect.Unknown, "", 0, "AllowUsers lets root in from some hosts only; the cluster nodes' addresses are not read")
	default:
		b.not("allowusers", detect.No, "", 0, "AllowUsers %s does not match root", strings.Join(e.AllowUsers.Values, " "))
	}
}

func (b byKey) groups(e sshd.Effective, root Groups) {
	gs := root.names()
	switch {
	case !e.DenyGroups.Known:
		b.not("denygroups", detect.Unknown, "", 0, "DenyGroups is not established")
	case anyMatch(e.DenyGroups.Values, gs):
		b.not("denygroups", detect.No, "", 0, "DenyGroups %s matches one of root's groups", strings.Join(e.DenyGroups.Values, " "))
	case len(e.DenyGroups.Values) > 0 && !root.Known:
		b.not("denygroups", detect.Unknown, "", 0, "DenyGroups is set and root's groups are not established")
	}
	switch {
	case !e.AllowGroups.Known:
		b.not("allowgroups", detect.Unknown, "", 0, "AllowGroups is not established")
	case len(e.AllowGroups.Values) == 0 || anyMatch(e.AllowGroups.Values, gs):
	case !root.Known:
		b.not("allowgroups", detect.Unknown, "", 0, "AllowGroups is set and root's groups are not established")
	default:
		b.not("allowgroups", detect.No, "", 0, "AllowGroups %s matches none of root's groups", strings.Join(e.AllowGroups.Values, " "))
	}
}

// methods checks AuthenticationMethods, the keys sources and the accepted
// signatures, which sshd -T prints as "authenticationmethods any" when none
// is set, "authorizedkeysfile" with sshd's default files,
// "authorizedkeyscommand none" when none is set and
// "pubkeyacceptedalgorithms" as the list it resolved (servconf.c
// dump_config, fill_default_server_options, assemble_algorithms).
func (b byKey) methods(e sshd.Effective) {
	one := func(k string) ([]string, bool) {
		vs := e.Keywords[k]
		if len(vs) != 1 {
			b.not(k, detect.Unknown, "", 0, "%s is not established", k)
			return nil, false
		}
		return strings.Fields(vs[0]), true
	}
	if ms, ok := one("authenticationmethods"); ok && !keyAlone(ms) {
		b.not("authenticationmethods", detect.No, "", 0, "AuthenticationMethods %s: no list is any or publickey alone", strings.Join(ms, " "))
	}
	files, ok := one("authorizedkeysfile")
	cmd, cmdOK := one("authorizedkeyscommand")
	if ok && cmdOK && !slices.ContainsFunc(files, readsRootKeys) {
		if len(cmd) == 1 && cmd[0] == "none" {
			b.not("authorizedkeysfile", detect.No, "", 0, "AuthorizedKeysFile %s does not read %s", strings.Join(files, " "), RootKeys)
		} else {
			b.not("authorizedkeysfile", detect.Unknown, "", 0, "AuthorizedKeysFile does not read %s; AuthorizedKeysCommand may supply root's keys", RootKeys)
		}
	}
	if algs, ok := one("pubkeyacceptedalgorithms"); ok && !acceptsNodeKeys(algs) {
		b.not("pubkeyacceptedalgorithms", detect.No, "", 0, "PubkeyAcceptedAlgorithms %s: %s", strings.Join(algs, " "), excludes("pubkeyacceptedalgorithms", algs, Groups{}))
	}
}

// listening checks that the other nodes can reach sshd: every address of
// both families, as sshd -T prints the listeners (their family follows
// AddressFamily: servconf.c add_one_listen_addr), or on a socket-activated
// host as ssh.socket's, whose [::] is dual-stack (reload.md §3).
func (b byKey) listening(l sshd.Listeners) {
	if !l.Known || len(l.All) == 0 {
		b.not("listenaddress", detect.Unknown, "", 0, "where sshd listens is not established")
		return
	}
	ls := l.All
	if l.SocketRead {
		ls = l.Socket
	}
	var at []string
	v4, v6 := false, false
	for _, x := range ls {
		at = append(at, x.String())
		if a := x.Addr.Unmap(); x.RDomain == "" && a.IsUnspecified() {
			v4 = v4 || a.Is4() || l.SocketRead
			v6 = v6 || a.Is6()
		}
	}
	switch list := strings.Join(at, ", "); {
	case !slices.ContainsFunc(ls, func(x sshd.Listener) bool { return !x.Addr.Unmap().IsLoopback() }):
		b.not("listenaddress", detect.No, "", 0, "sshd listens on loopback only (%s): the other nodes cannot reach it", list)
	case !v4 || !v6:
		b.not("listenaddress", detect.Unknown, "", 0, "sshd listens on %s, not on every address of both families; the cluster nodes' addresses are not read, so whether they reach it is not established", list)
	}
}

// matchBlocks reads the Match blocks of in's files: a PermitRootLogin other
// than prohibit-password makes RootKeyOnly No; a line of rootKeywords that
// may keep root out makes its keyword Unknown, and so does one that may let
// root in where the global value keeps it out (an AuthorizedKeysCommand
// counts for AuthorizedKeysFile). Which connections a block matches, the
// nodes' among them, is not established.
func (b byKey) matchBlocks(in SSHInput) {
	if in.Main == nil {
		if b.c.RootKeyOnly == detect.Yes {
			b.c.RootKeyOnly = detect.Unknown
		}
		b.not("match", detect.Unknown, "", 0, "sshd's files were not read, so what their Match blocks set is not established")
		return
	}
	for _, l := range sshd.ReadOrder(in.Main, in.DropIns) {
		if l.Match == nil {
			continue
		}
		under := fmt.Sprintf("%q under %q (%s line %d)", l.Text, l.Match.Text, l.Match.File, l.Match.Line)
		k := l.Keyword
		if v := strings.ToLower(strings.Join(l.Args, " ")); k == "permitrootlogin" && v != RootLogin && v != "without-password" {
			b.c.RootKeyOnly = detect.No
			b.c.Findings = append(b.c.Findings, Finding{Kind: FindingNotKeyOnly, File: l.File, Line: l.Line,
				Detail: under + ": PermitRootLogin " + v + " for the connections that block matches"})
		}
		if k == "authorizedkeyscommand" {
			k = "authorizedkeysfile"
			if strings.EqualFold(strings.Join(l.Args, " "), "none") {
				continue
			}
		} else if !slices.Contains(rootKeywords, k) {
			continue
		}
		switch why := excludes(l.Keyword, l.Args, in.Root); {
		case why != "":
			b.not(k, detect.Unknown, l.File, l.Line, "%s: %s, for the connections that block matches", under, why)
		case b.t[k] == detect.No:
			b.t[k] = detect.Unknown
			b.c.Findings = append(b.c.Findings, Finding{Kind: FindingRootUnreachable, File: l.File, Line: l.Line,
				Detail: under + ": the connections that block matches may let root in where the global value does not"})
		}
	}
}

func port22(e sshd.Effective, found *[]Finding) detect.Tri {
	l := e.Listeners
	if !e.Port.Known || !l.Known || len(l.All) == 0 {
		return detect.Unknown
	}
	var off []string
	for _, p := range e.Port.Values {
		if p != strconv.Itoa(SSHPort) {
			off = append(off, "Port "+p)
		}
	}
	for _, x := range l.SSHD {
		if x.Port != SSHPort {
			off = append(off, "ListenAddress "+x.String())
		}
	}
	for _, x := range l.Socket {
		if x.Port != SSHPort {
			off = append(off, sshd.SocketUnit+" Listen "+x.String())
		}
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
