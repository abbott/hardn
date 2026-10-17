package sshd

import (
	"context"
	"fmt"
	"io/fs"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// SocketUnit is the unit that holds sshd's listening sockets on a
// socket-activated host (detect.SSHSocket: Ubuntu 24.04 and 26.04).
const SocketUnit = "ssh.socket"

// Setting is the effective value of a keyword whose first value wins.
type Setting struct {
	// Value is sshd -T's value in lower case. PermitRootLogin's
	// without-password, which OpenSSH before 10.1 prints, reads
	// prohibit-password. "" when Unknown.
	Value string
	// Known is false when the value is Unknown: sshd -T printed none hardn
	// knows, or the sources disagree (a Finding says which).
	Known bool
}

// Set is the effective value of a keyword whose values accumulate across
// the files: sshd -T's values, in its order, without repeats.
type Set struct {
	Values []string // nil when Unknown
	Known  bool
}

// Has reports whether v is one of s's values.
func (s Set) Has(v string) bool { return slices.Contains(s.Values, v) }

// Listener is an address and TCP port connections are accepted on.
type Listener struct {
	// Addr is as printed; an unspecified address (0.0.0.0, ::) means every
	// address of its family, and a dual-stack socket on :: both families.
	Addr    netip.Addr
	Port    uint16
	RDomain string // sshd's routing domain; "" for none
}

func (l Listener) String() string {
	s := netip.AddrPortFrom(l.Addr, l.Port).String()
	if l.RDomain != "" {
		s += " rdomain " + l.RDomain
	}
	return s
}

// key compares listeners across sources: every unspecified address is one,
// since systemd's dual-stack [::]:22 and sshd's 0.0.0.0:22 plus [::]:22
// accept the same connections.
func (l Listener) key() string {
	a := l.Addr.Unmap()
	s := a.String()
	if a.IsUnspecified() {
		s = "*"
	}
	return s + "|" + strconv.Itoa(int(l.Port)) + "|" + l.RDomain
}

// Listeners is where SSH connections are accepted.
type Listeners struct {
	SSHD []Listener // sshd -T's listenaddress lines
	// Socket is ssh.socket's Listen entries when sshd is socket-activated:
	// systemd then holds the listening sockets, not sshd. nil otherwise.
	Socket []Listener
	// All is every listener either source names, without repeats, sshd's
	// first: what a firewall must allow (p2-ufw). It is kept when Known is
	// false, since allowing more is the safe direction there.
	All []Listener
	// Known: the listeners are established. On a socket-activated host
	// that needs sshd's and the socket's to agree; with the SSH mode
	// Unknown, they are not established.
	Known bool
}

// Ports lists the ports of All, ascending, without repeats.
func (l Listeners) Ports() []uint16 {
	var ps []uint16
	for _, x := range l.All {
		ps = append(ps, x.Port)
	}
	slices.Sort(ps)
	return slices.Compact(ps)
}

// Origin is one line of a configuration file.
type Origin struct {
	File string
	Line int
	Text string
}

// Conflict is a line of another file that sets a keyword hardn's lines set.
type Conflict struct {
	// Kind is FindingOverridden, FindingShadowed, FindingAccumulates or
	// FindingMatchOverride.
	Kind FindingKind
	// Keyword is the canonical lower-case name: an alias reads as its
	// keyword (ChallengeResponseAuthentication is
	// kbdinteractiveauthentication), and a Subsystem as "subsystem <name>".
	Keyword string
	Hardn   Origin // hardn's first line for the keyword
	Other   Origin
	Match   Block // FindingMatchOverride: the Match line Other is under
}

// Override is a value a Match probe printed differently from sshd -T: a
// connection like Conn gets Value for Keyword.
type Override struct {
	Block   Block
	Conn    Conn
	Keyword string
	Value   string // a Setting's value, or a Set's values joined by spaces
}

// Owner names hardn's lines: a file, and optionally a range of its lines
// (hardn's block at the top of sshd_config, IncludeNone).
type Owner struct {
	File        string // "" is HardnDropIn
	First, Last int    // 1-based, inclusive; both 0: the whole file
}

func (o Owner) owns(file string, line int) bool {
	f := o.File
	if f == "" {
		f = HardnDropIn
	}
	return file == f && (o.First == 0 && o.Last == 0 || line >= o.First && line <= o.Last)
}

// File is a configuration file.
type File struct {
	Path string
	Data []byte
}

// Effective is the configuration sshd runs with.
type Effective struct {
	// Keywords is every line of sshd -T's output: the keyword in lower case
	// (OpenSSH 10.4 and 10.5 print mixed case; an alias reads as its
	// keyword), its values in printed order.
	Keywords map[string][]string

	PermitRootLogin              Setting // prohibit-password, forced-commands-only, yes or no
	PasswordAuthentication       Setting // yes or no
	KbdInteractiveAuthentication Setting // yes or no
	PubkeyAuthentication         Setting // yes or no

	// Port is sshd's Port set. Where it listens is Listeners: a
	// ListenAddress with a port of its own listens on a port not in Port.
	Port                                                      Set
	AllowUsers, AllowGroups, DenyUsers, DenyGroups, AcceptEnv Set
	Listeners                                                 Listeners

	// Conflicts lists the other files' lines for keywords hardn's lines
	// set, in the order sshd reads them (none without the files).
	Conflicts []Conflict
	// Overrides lists the values Match probes printed differently.
	Overrides []Override
}

// The keywords Effective has a field for, and the values a Setting takes.
var (
	settingValues = map[string][]string{
		"permitrootlogin":              {"prohibit-password", "forced-commands-only", "yes", "no"},
		"passwordauthentication":       {"yes", "no"},
		"kbdinteractiveauthentication": {"yes", "no"},
		"pubkeyauthentication":         {"yes", "no"},
	}
	settingKeys = []string{"permitrootlogin", "passwordauthentication", "kbdinteractiveauthentication", "pubkeyauthentication"}
	setKeys     = []string{"port", "allowusers", "allowgroups", "denyusers", "denygroups", "acceptenv"}
)

func (e *Effective) setting(k string) *Setting {
	return map[string]*Setting{"permitrootlogin": &e.PermitRootLogin, "passwordauthentication": &e.PasswordAuthentication,
		"kbdinteractiveauthentication": &e.KbdInteractiveAuthentication, "pubkeyauthentication": &e.PubkeyAuthentication}[k]
}

func (e *Effective) set(k string) *Set {
	return map[string]*Set{"port": &e.Port, "allowusers": &e.AllowUsers, "allowgroups": &e.AllowGroups,
		"denyusers": &e.DenyUsers, "denygroups": &e.DenyGroups, "acceptenv": &e.AcceptEnv}[k]
}

// EffectiveInputs are the sources Evaluate reads.
type EffectiveInputs struct {
	Effective []byte  // sshd -T's output: ReadEffective's, or Report.Effective
	Probes    []Probe // one sshd -T -C per Match block: ReadEffective's, or Report.Probes
	// Mode is how sshd is started (detect.SSH.Mode); with SSHSocket, Socket
	// is the output of systemctl show -p Listen ssh.socket.
	Mode   detect.SSHMode
	Socket []byte
	// Main and DropIns are the configuration sshd read: sshd_config, and the
	// drop-ins its Include reads, in sshd's order. With Main nil there is
	// no conflict report and no check of sshd -T against the files.
	// Includes other than IncludeLine in sshd_config are not followed
	// (Validate and ReadEffective refuse them).
	Main    []byte
	DropIns []File
	Owner   Owner
}

// Evaluate computes the effective configuration from in. It changes
// nothing and runs nothing. Every value it cannot establish is Unknown,
// with a Finding saying why; the other findings are the conflict report.
func Evaluate(in EffectiveInputs) (Effective, []Finding) {
	var e Effective
	var found []Finding
	kw, err := parseT(in.Effective)
	if err != nil {
		return e, []Finding{{Kind: FindingUnknown, Detail: err.Error()}}
	}
	e.Keywords = kw
	for _, k := range settingKeys {
		var why string
		if *e.setting(k), why = readSetting(kw, k); why != "" {
			found = append(found, Finding{Kind: FindingUnknown, Detail: why})
		}
	}
	for _, k := range setKeys {
		*e.set(k) = readSet(kw, k)
	}
	if len(e.Port.Values) == 0 || slices.ContainsFunc(e.Port.Values, func(v string) bool { return portOf(v) == 0 }) {
		e.Port = Set{}
		found = append(found, Finding{Kind: FindingUnknown, Detail: fmt.Sprintf("sshd -T printed ports %q, not one or more numbers from 1 to 65535", kw["port"])})
	}
	var f []Finding
	e.Listeners, f = listeners(kw["listenaddress"], in.Mode, in.Socket)
	found = append(found, f...)
	if in.Main != nil {
		ls := flatten(in.Main, in.DropIns, in.Owner)
		e.Conflicts, f = conflicts(ls)
		found = append(found, f...)
		found = append(found, crossCheck(ls, &e)...)
	}
	e.Overrides, f = overrides(&e, in.Probes)
	return e, append(found, f...)
}

// EffectiveRequest says how ReadEffective reads the host.
type EffectiveRequest struct {
	SSH    detect.SSH // detect.Detect's SSH: SSHSocket adds ssh.socket's listeners
	Binary string     // the sshd to run; "" is DefaultBinary
	Conn   Conn       // where Match probes start; the zero value is DefaultConn
	Owner  Owner      // hardn's lines; the zero value is all of HardnDropIn
}

// ReadEffective reads the installed configuration, runs sshd -T -f
// MainConfig and one sshd -T -C per Match block against it, reads
// ssh.socket's listeners on a socket-activated host (systemctl show -p
// Listen ssh.socket), and Evaluates them. It writes nothing. It needs root
// as sshd -T does (host keys; /run/sshd on Debian, Ubuntu and Proxmox VE).
// The findings are Validate's about the installed files (clobberable,
// include-not-first, match-untargeted), then Evaluate's. Errors: those of
// Validate's runs (*ConfigError, *EnvError, *UnexpectedError, the host's),
// ErrUnsupportedInclude, ErrNotRegular, fs.ErrInvalid (a NUL byte, a bad
// Conn), and systemctl's (*host.ExitError).
func ReadEffective(ctx context.Context, sys host.System, req EffectiveRequest) (Effective, []Finding, error) {
	if err := ctx.Err(); err != nil {
		return Effective{}, nil, fmt.Errorf("sshd: effective: %w", err)
	}
	if req.Binary == "" {
		req.Binary = DefaultBinary
	}
	if req.Conn == (Conn{}) {
		req.Conn = DefaultConn()
	}
	if err := req.Conn.check(); err != nil {
		return Effective{}, nil, fmt.Errorf("%w: %w", err, fs.ErrInvalid)
	}
	var rep Report
	c, err := assemble(ctx, sys, nil, &rep)
	if err != nil {
		return Effective{}, nil, err
	}
	hostKeys := c.setsHostKey()
	runT := func(extra ...string) (host.Result, error) {
		res, err := sys.Run(ctx, req.Binary, append([]string{"-T", "-f", MainConfig}, extra...)...)
		return res, classify(res, err, strings.NewReplacer(), hostKeys)
	}
	res, err := runT()
	if err != nil {
		return Effective{}, nil, err
	}
	in := EffectiveInputs{Effective: res.Stdout, Mode: req.SSH.Mode, Main: c.main, Owner: req.Owner}
	for _, d := range c.drops {
		in.DropIns = append(in.DropIns, File{d.path, d.data})
	}
	probes, found, err := c.probe(req.Conn, res.Stdout, runT)
	if err != nil {
		return Effective{}, nil, err
	}
	in.Probes = probes
	if req.SSH.Mode == detect.SSHSocket {
		res, err := sys.Run(ctx, "systemctl", "show", "-p", "Listen", SocketUnit)
		if err != nil {
			return Effective{}, nil, fmt.Errorf("sshd: %s listeners: %w", SocketUnit, err)
		}
		in.Socket = res.Stdout
	}
	e, f := Evaluate(in)
	return e, append(append(rep.Findings, found...), f...), nil
}

// parseT reads sshd -T's output: one "keyword value" per line
// (servconf.c, dump_config). sshd prints the first name its keywords table
// gives an option (kbdinteractiveauthentication, before its aliases, in 9.2
// and 10.3); an alias is read as its keyword all the same, in case a build
// prints one.
func parseT(out []byte) (map[string][]string, error) {
	kw := map[string][]string{}
	for _, l := range lines(string(out)) {
		if l == "" {
			continue
		}
		k, v, ok := strings.Cut(l, " ")
		if !ok || k == "" {
			return nil, fmt.Errorf("sshd -T printed %q, not a keyword and a value", l)
		}
		k = strings.ToLower(k)
		if a, ok := aliases[k]; ok {
			k = a
		}
		kw[k] = append(kw[k], v)
	}
	if len(kw) == 0 {
		return nil, fmt.Errorf("sshd -T printed nothing")
	}
	return kw, nil
}

// normalize is v as hardn compares it: a Setting's value in lower case
// (sshd reads these case-insensitively), without-password as
// prohibit-password (one value, servconf.c multistate_permitrootlogin), a
// port in decimal.
func normalize(k, v string) string {
	switch {
	case k == "port":
		if p := portOf(v); p != 0 {
			return strconv.Itoa(p)
		}
	case settingValues[k] != nil:
		if v = strings.ToLower(v); v == "without-password" {
			return "prohibit-password"
		}
	}
	return v
}

// portOf is v as a port number, or 0.
func portOf(v string) int {
	p, err := strconv.Atoi(v)
	if err != nil || p < 1 || p > 65535 {
		return 0
	}
	return p
}

// readSetting is keyword k of sshd -T's output, and why it is Unknown.
func readSetting(kw map[string][]string, k string) (Setting, string) {
	vs := kw[k]
	if len(vs) == 0 {
		return Setting{}, "sshd -T printed no " + k
	}
	v := normalize(k, vs[0])
	for _, o := range vs[1:] {
		if normalize(k, o) != v {
			return Setting{}, fmt.Sprintf("sshd -T printed %s twice: %q and %q", k, vs[0], o)
		}
	}
	if !slices.Contains(settingValues[k], v) {
		return Setting{}, fmt.Sprintf("sshd -T printed %s %q, a value hardn does not know", k, vs[0])
	}
	return Setting{Value: v, Known: true}, ""
}

// readSet is accumulating keyword k of sshd -T's output (one line per
// value, servconf.c dump_cfg_strarray). No line is the empty set.
func readSet(kw map[string][]string, k string) Set {
	vs := make([]string, len(kw[k]))
	for i, v := range kw[k] {
		vs[i] = normalize(k, v)
	}
	return Set{Values: uniq(vs), Known: true}
}

// uniq is vs in order, each value once.
func uniq(vs []string) []string {
	var out []string
	for _, v := range vs {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// sameSet reports whether a and b hold the same values, ignoring order and
// repeats.
func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(slices.Compact(a), slices.Compact(b))
}

// listeners reads sshd -T's listenaddress lines ("addr:port", "[addr]:port",
// either followed by " rdomain <name>", servconf.c format_listen_addrs) and,
// on a socket-activated host, ssh.socket's Listen entries.
func listeners(addrs []string, mode detect.SSHMode, socket []byte) (l Listeners, found []Finding) {
	unknown := func(kind FindingKind, format string, a ...any) (Listeners, []Finding) {
		l.All, l.Known = union(l.SSHD, l.Socket), false
		return l, append(found, Finding{Kind: kind, Detail: fmt.Sprintf(format, a...)})
	}
	for _, a := range addrs {
		ap, rest, _ := strings.Cut(a, " ")
		rd, ok := strings.CutPrefix(rest, "rdomain ")
		p, err := netip.ParseAddrPort(ap)
		if err != nil || p.Port() == 0 || rest != "" && (!ok || rd == "") {
			return unknown(FindingUnknown, "sshd -T printed listenaddress %q, not an address and a port", a)
		}
		l.SSHD = append(l.SSHD, Listener{p.Addr(), p.Port(), rd})
	}
	l.All = union(l.SSHD)
	switch {
	case len(l.SSHD) == 0:
		return unknown(FindingUnknown, "sshd -T printed no listenaddress")
	case mode == detect.SSHService || mode == detect.SSHOpenRC:
		l.Known = true
		return l, nil
	case mode != detect.SSHSocket:
		return unknown(FindingUnknown, "how sshd is started is not established, so it is not known whether sshd or %s listens", SocketUnit)
	}
	var bad []string
	for _, line := range lines(string(socket)) {
		v, ok := strings.CutPrefix(line, "Listen=")
		addr, typ, _ := strings.Cut(v, " ")
		p, err := netip.ParseAddrPort(addr)
		switch {
		case line == "" || v == "" && ok:
			continue
		case !ok || typ != "(Stream)" || err != nil || p.Port() == 0:
			bad = append(bad, line)
			continue
		}
		l.Socket = append(l.Socket, Listener{Addr: p.Addr(), Port: p.Port()})
	}
	l.All = union(l.SSHD, l.Socket)
	switch {
	case len(bad) > 0:
		return unknown(FindingUnknown, "systemctl show -p Listen %s printed %q, not an address, a port and (Stream)", SocketUnit, bad)
	case len(l.Socket) == 0:
		return unknown(FindingUnknown, "%s lists no listener", SocketUnit)
	case !sameSet(keys(l.SSHD), keys(l.Socket)):
		return unknown(FindingDisagree, "sshd -T listens on %v, %s on %v: a Port or ListenAddress change takes effect on a socket-activated host after systemctl daemon-reload and a restart of %s", l.SSHD, SocketUnit, l.Socket, SocketUnit)
	}
	l.Known = true
	return l, nil
}

func keys(ls []Listener) []string {
	ks := make([]string, len(ls))
	for i, l := range ls {
		ks[i] = l.key()
	}
	return ks
}

// union is the listeners of lists in order, each once. Unlike key, it keeps
// 0.0.0.0 and :: apart, so All shows both families where a source has them.
func union(lists ...[]Listener) []Listener {
	var all []Listener
	for _, l := range slices.Concat(lists...) {
		if !slices.Contains(all, l) {
			all = append(all, l)
		}
	}
	return all
}

// overrides compares each probe's output with e's for the keywords e has a
// field for.
func overrides(e *Effective, probes []Probe) (ovs []Override, found []Finding) {
	for _, p := range probes {
		kw, err := parseT(p.Output)
		if err != nil {
			found = append(found, Finding{FindingUnknown, p.Block.File, p.Block.Line, "the probe of " + p.Block.Text + ": " + err.Error()})
			continue
		}
		for _, k := range settingKeys {
			if v, why := readSetting(kw, k); why == "" && e.setting(k).Known && v.Value != e.setting(k).Value {
				ovs = append(ovs, Override{p.Block, p.Conn, k, v.Value})
			}
		}
		for _, k := range setKeys {
			if v := readSet(kw, k); k != "port" && e.set(k).Known && !sameSet(v.Values, e.set(k).Values) { // Port is not allowed in a Match block
				ovs = append(ovs, Override{p.Block, p.Conn, k, strings.Join(v.Values, " ")})
			}
		}
	}
	return ovs, found
}
