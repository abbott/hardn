package sshd

import (
	"context"
	"fmt"
	"io/fs"
	"net/netip"
	"path"
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

// Listeners is the configured listener set: where sshd's files and, on a
// socket-activated host, ssh.socket's unit say connections are accepted.
// Both sources are configuration, not bound sockets: sshd -T reads the
// files on disk, and systemctl show -p Listen prints the unit's configured
// ports whether or not a descriptor is open. So between a Port change and
// the reload or restart that applies it, they name ports nothing listens
// on yet, and a port the running daemon still holds may be missing.
type Listeners struct {
	SSHD []Listener // sshd -T's listenaddress lines
	// Socket is ssh.socket's Listen entries when SocketRead: on a
	// socket-activated host systemd holds the listening sockets, not sshd.
	Socket []Listener
	// SocketRead: ssh.socket's listeners were read (SocketMayListen).
	SocketRead bool
	// SocketComplete: SocketRead, and every line systemctl show -p Listen
	// printed was read (none in a form other than "Listen=<address>
	// (Stream)"), so Socket is the whole of what it printed. It does not
	// say that Socket is non-empty (p2-sshd-migrate).
	SocketComplete bool
	// All is every listener the sources read name, without repeats, sshd's
	// first. It is complete only when Known: with Known false a source may
	// be missing or unreadable (a Finding says which), so All is then not a
	// set a firewall may be enabled or narrowed to.
	All []Listener
	// Known: the configured listener set is established. Every source was
	// read; on a socket-activated host sshd's and the socket's agree; the
	// files agree with sshd -T on Port; and detect shows the unit that
	// listens up (ssh.socket or ssh.service active, OpenRC's sshd started).
	// It does not show that the sockets are bound (see Listeners).
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
	// SameValue: Other's arguments, normalised as Setting values are (case,
	// without-password, port numbers), equal Hardn's; for an accumulating
	// keyword, as sets. Always false for FindingShadowed.
	SameValue bool
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

func (o Owner) file() string {
	if o.File == "" {
		return HardnDropIn
	}
	return o.File
}

func (o Owner) owns(file string, line int) bool {
	return file == o.file() && (o.First == 0 && o.Last == 0 || line >= o.First && line <= o.Last)
}

func (o Owner) String() string {
	if o.First == 0 && o.Last == 0 {
		return o.file()
	}
	return fmt.Sprintf("%s lines %d-%d", o.file(), o.First, o.Last)
}

// check refuses an Owner that cannot name a line: a relative File, or a
// range that is not 1 <= First <= Last.
func (o Owner) check() error {
	switch {
	case !path.IsAbs(o.file()):
		return fmt.Errorf("sshd: hardn's lines in %q: not an absolute path: %w", o.File, fs.ErrInvalid)
	case o.First == 0 && o.Last == 0:
		return nil
	case o.First < 1 || o.Last < o.First:
		return fmt.Errorf("sshd: hardn's lines %s: not a range of lines: %w", o, fs.ErrInvalid)
	}
	return nil
}

// File is a configuration file.
type File struct {
	Path string
	Data []byte
}

// Effective is the configuration sshd reads from the installed files: what
// sshd runs with once it rereads them (a reload or a restart). A running
// daemon that has not reread them still runs with what it read before.
type Effective struct {
	// Keywords is every line of sshd -T's output: the keyword in lower case
	// (OpenSSH 10.4 and 10.5 print mixed case; an alias reads as its
	// keyword), its values in printed order.
	Keywords map[string][]string

	PermitRootLogin              Setting // prohibit-password, forced-commands-only, yes or no
	PasswordAuthentication       Setting // yes or no
	KbdInteractiveAuthentication Setting // yes or no
	PubkeyAuthentication         Setting // yes or no

	// UsePAM is whether sshd uses PAM, yes or no: on Linux PAM is
	// keyboard-interactive's only device (auth2-chall.c). A build without
	// PAM prints no usepam line and ignores the files' UsePAM (servconf.c:
	// dump_config prints it under USE_PAM alone; the keyword is
	// unsupported there), so no line reads as Known "no", with
	// BuiltWithoutPAM. It is not checked against the files, which do not
	// decide it in such a build. Both fields describe the binary sshd -T
	// ran (EffectiveRequest.Binary), not the daemon running: on Alpine
	// that may be another build (a container's entrypoint; sshd.pam
	// started before UsePAM yes was removed, which only a restart
	// replaces, reload.md §2.6).
	UsePAM Setting
	// BuiltWithoutPAM: sshd -T printed no usepam line, and printed the
	// settings dump_config prints after it whatever the build, so this
	// sshd is built without PAM (Alpine's /usr/sbin/sshd).
	BuiltWithoutPAM bool

	// StrictModes is sshd's StrictModes, yes or no: with yes, sshd ignores
	// a user's authorized_keys where the home or ~/.ssh is writable by
	// others. sshd_config(5) does not allow it in a Match block. No line, or
	// a value hardn does not know, is Unknown with no finding: the outputs
	// recorded before slice 5 do not print it.
	StrictModes Setting
	// AuthorizedKeysFile is sshd -T's authorizedkeysfile fields as printed
	// (one line, servconf.c dump_cfg_strarray_oneline): %h, %u and %% not
	// expanded, a path without a leading / relative to the home, "none"
	// kept. Debian 13 and Ubuntu 26.04 print ".ssh/authorized_keys
	// .ssh/authorized_keys2", Alpine 3.23 ".ssh/authorized_keys". Nil: no
	// line. Match blocks are not read: a block setting it for a user is not
	// seen here.
	AuthorizedKeysFile []string

	// Port is sshd's Port set. Where it listens is Listeners: a
	// ListenAddress with a port of its own listens on a port not in Port.
	Port                                                      Set
	AllowUsers, AllowGroups, DenyUsers, DenyGroups, AcceptEnv Set
	Listeners                                                 Listeners

	// Conflicts lists the other files' lines for keywords hardn's lines
	// set, in the order sshd reads them.
	Conflicts []Conflict
	// ConflictsKnown: the conflict report is established, so an empty one
	// means nothing else sets hardn's keywords. False without the files,
	// and, with a FindingUnknown, when Owner is not a range of lines or
	// hardn's lines set no keyword in the files read (00-hardn.conf absent,
	// say).
	ConflictsKnown bool
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
		"usepam":                       {"yes", "no"}, // read by readUsePAM
		"strictmodes":                  {"yes", "no"}, // read in Evaluate, with no finding
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
	// SSH is how sshd is started (detect.Detect's SSH): its Mode and unit
	// states decide whether the listeners are Known. When
	// SocketMayListen(SSH), Socket is the output of systemctl show -p
	// Listen ssh.socket.
	SSH    detect.SSH
	Socket []byte
	// Main and DropIns are the configuration sshd read: sshd_config, and the
	// drop-ins its Include reads. Evaluate puts the drop-ins in sshd's
	// order (C byte order) and leaves out any the Include does not read
	// (outside DropInDir, not *.conf, a leading "."). With Main nil there
	// is no conflict report and no check of sshd -T against the files.
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
	if err != nil { // nothing of sshd's is read; the socket's listeners are kept
		if l := &e.Listeners; SocketMayListen(in.SSH) {
			var bad []string
			l.SocketRead = true
			l.Socket, bad = socketListeners(in.Socket)
			l.SocketComplete = len(bad) == 0
			l.All = union(l.Socket)
		}
		return e, []Finding{{Kind: FindingUnknown, Detail: err.Error()}}
	}
	e.Keywords = kw
	for _, k := range settingKeys {
		var why string
		if *e.setting(k), why = readSetting(kw, k); why != "" {
			found = append(found, Finding{Kind: FindingUnknown, Detail: why})
		}
	}
	var why string
	if e.UsePAM, e.BuiltWithoutPAM, why = readUsePAM(kw); why != "" {
		found = append(found, Finding{Kind: FindingUnknown, Detail: why})
	}
	for _, k := range setKeys {
		*e.set(k) = readSet(kw, k)
	}
	e.StrictModes, _ = readSetting(kw, "strictmodes")
	for _, v := range kw["authorizedkeysfile"] {
		e.AuthorizedKeysFile = append(e.AuthorizedKeysFile, strings.Fields(v)...)
	}
	if len(e.Port.Values) == 0 || slices.ContainsFunc(e.Port.Values, func(v string) bool { return portOf(v) == 0 }) {
		e.Port = Set{}
		found = append(found, Finding{Kind: FindingUnknown, Detail: fmt.Sprintf("sshd -T printed ports %q, not one or more numbers from 1 to 65535", kw["port"])})
	}
	var f []Finding
	e.Listeners, f = listeners(kw["listenaddress"], in.SSH, in.Socket)
	found = append(found, f...)
	if in.Main != nil {
		ls := flatten(in.Main, sshdOrder(in.DropIns), in.Owner)
		switch err := in.Owner.check(); {
		case err != nil:
			found = append(found, Finding{Kind: FindingUnknown, Detail: "the conflict report is not established: " + err.Error()})
		case !slices.ContainsFunc(ls, func(l setLine) bool { return l.hardn }):
			found = append(found, Finding{Kind: FindingUnknown, Detail: fmt.Sprintf(
				"the conflict report is not established: hardn's lines (%s) set no keyword in the files read", in.Owner)})
		default:
			e.Conflicts, f = conflicts(ls)
			e.ConflictsKnown = true
			found = append(found, f...)
		}
		found = append(found, crossCheck(ls, &e)...)
	}
	e.Overrides, f = overrides(&e, in.Probes)
	return e, append(found, f...)
}

// sshdOrder is drops as sshd's IncludeLine reads them: the files of
// DropInDir that "*.conf" matches (included), in C byte order.
func sshdOrder(drops []File) []File {
	var out []File
	for _, d := range drops {
		if path.Dir(d.Path) == DropInDir && included(path.Base(d.Path)) {
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b File) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// SocketMayListen reports whether ssh.socket may hold sshd's listening
// sockets by detect's facts: the SSH mode is SSHSocket, or it is Unknown
// and detect asked systemd about ssh.socket without establishing that the
// socket is neither enabled nor active (it and ssh.service both enabled,
// say). ReadEffective then reads the socket's listeners, and Evaluate
// expects them in EffectiveInputs.Socket.
func SocketMayListen(s detect.SSH) bool {
	if s.Mode != detect.SSHUnknown {
		return s.Mode == detect.SSHSocket
	}
	off := s.Socket.IsEnabled() == detect.No && s.Socket.IsActive() == detect.No
	return s.Socket != (detect.UnitState{}) && !off
}

// EffectiveRequest says how ReadEffective reads the host.
type EffectiveRequest struct {
	SSH    detect.SSH // detect.Detect's SSH (EffectiveInputs.SSH)
	Binary string     // the sshd to run; "" is DefaultBinary
	Conn   Conn       // where Match probes start; the zero value is DefaultConn
	Owner  Owner      // hardn's lines; the zero value is all of HardnDropIn
}

// ReadEffective reads the installed configuration, runs sshd -T -f
// MainConfig and one sshd -T -C per Match block against it, reads
// ssh.socket's listeners when SocketMayListen(req.SSH) (systemctl show -p
// Listen ssh.socket), and Evaluates them. It writes nothing. It needs root
// as sshd -T does (host keys; /run/sshd on Debian, Ubuntu and Proxmox VE).
// The findings are Validate's about the installed files (clobberable,
// include-not-first, match-untargeted), then Evaluate's. Errors: those of
// Validate's runs (*ConfigError, *EnvError, *UnexpectedError, the host's),
// ErrUnsupportedInclude, ErrNotRegular, fs.ErrInvalid (a NUL byte, a bad
// Conn or Owner), and systemctl's (*host.ExitError).
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
	if err := req.Owner.check(); err != nil {
		return Effective{}, nil, err
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
	in := EffectiveInputs{Effective: res.Stdout, SSH: req.SSH, Main: c.main, Owner: req.Owner}
	for _, d := range c.drops {
		in.DropIns = append(in.DropIns, File{d.path, d.data})
	}
	probes, found, err := c.probe(req.Conn, res.Stdout, runT)
	if err != nil {
		return Effective{}, nil, err
	}
	in.Probes = probes
	if SocketMayListen(req.SSH) {
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

// readUsePAM is sshd -T's usepam, whether this sshd is built without PAM,
// and why UsePAM is Unknown. dump_config prints usepam only in a build with
// PAM (#ifdef USE_PAM), before the four other settings Effective types,
// which it prints whatever the build. So no usepam line where one of those
// was printed is a build without PAM, in which UsePAM is an unsupported
// keyword and PAM is not used: Known "no". With none of them printed
// either, the output may not be sshd's whole answer: Unknown.
func readUsePAM(kw map[string][]string) (s Setting, noPAM bool, why string) {
	printed := slices.ContainsFunc(settingKeys, func(k string) bool { return len(kw[k]) > 0 })
	if len(kw["usepam"]) == 0 && printed {
		return Setting{Value: "no", Known: true}, true, ""
	}
	s, why = readSetting(kw, "usepam")
	return s, false, why
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
// when SocketMayListen(ssh), ssh.socket's Listen entries. Every entry it
// can read is in All, whatever else fails.
func listeners(addrs []string, ssh detect.SSH, socket []byte) (l Listeners, found []Finding) {
	unknown := func(kind FindingKind, format string, a ...any) {
		found = append(found, Finding{Kind: kind, Detail: fmt.Sprintf(format, a...)})
	}
	var bad []string
	for _, a := range addrs {
		ap, rest, _ := strings.Cut(a, " ")
		rd, ok := strings.CutPrefix(rest, "rdomain ")
		p, err := netip.ParseAddrPort(ap)
		if err != nil || p.Port() == 0 || rest != "" && (!ok || rd == "") {
			bad = append(bad, a)
			continue
		}
		l.SSHD = append(l.SSHD, Listener{p.Addr(), p.Port(), rd})
	}
	switch {
	case len(bad) > 0:
		unknown(FindingUnknown, "sshd -T printed listenaddress %q, not an address and a port", bad)
	case len(l.SSHD) == 0:
		unknown(FindingUnknown, "sshd -T printed no listenaddress")
	}
	if l.SocketRead = SocketMayListen(ssh); l.SocketRead {
		var sbad []string
		l.Socket, sbad = socketListeners(socket)
		l.SocketComplete = len(sbad) == 0
		switch {
		case len(sbad) > 0:
			unknown(FindingUnknown, "systemctl show -p Listen %s printed %q, not an address, a port and (Stream)", SocketUnit, sbad)
		case len(l.Socket) == 0:
			unknown(FindingUnknown, "%s lists no listener", SocketUnit)
		case ssh.Mode == detect.SSHSocket && len(found) == 0 && !sameSet(keys(l.SSHD), keys(l.Socket)):
			unknown(FindingDisagree, "sshd -T listens on %v, %s on %v: on a socket-activated host a Port or ListenAddress change reaches the socket's configuration at systemctl daemon-reload, and its sockets at a restart of %s", l.SSHD, SocketUnit, l.Socket, SocketUnit)
		}
	}
	l.All = union(l.SSHD, l.Socket)
	if why := notListening(ssh); why != "" {
		unknown(FindingUnknown, "%s", why)
	}
	l.Known = len(found) == 0
	return l, found
}

// socketListeners reads systemctl show -p Listen's "Listen=<address>
// (<type>)" lines; bad lists those that are not an IP address, a port and
// (Stream). An empty property ("Listen=") is no entry.
func socketListeners(out []byte) (ls []Listener, bad []string) {
	for _, line := range lines(string(out)) {
		v, ok := strings.CutPrefix(line, "Listen=")
		addr, typ, _ := strings.Cut(v, " ")
		p, err := netip.ParseAddrPort(addr)
		switch {
		case line == "" || v == "" && ok:
		case !ok || typ != "(Stream)" || err != nil || p.Port() == 0:
			bad = append(bad, line)
		default:
			ls = append(ls, Listener{Addr: p.Addr(), Port: p.Port()})
		}
	}
	return ls, bad
}

// ParseSocketListen reads systemctl show -p Listen's output as ReadEffective
// does; bad lists the lines that are not an IP address, a port and
// (Stream). p2-reload checks the socket's configuration with it after
// daemon-reload (docs/safety/reload.md).
func ParseSocketListen(out []byte) (ls []Listener, bad []string) { return socketListeners(out) }

// notListening says why detect's facts do not show the unit that listens
// up, or "" when they do: a failed ssh.socket or ssh.service, or an sshd
// OpenRC has not started, accepts nothing on the configured listeners.
func notListening(s detect.SSH) string {
	state := func(v string) string {
		if v == "" {
			return "not established"
		}
		return v
	}
	switch s.Mode {
	case detect.SSHSocket:
		if s.Socket.IsActive() != detect.Yes {
			return fmt.Sprintf("%s is not active (%s), so nothing may accept connections on its listeners", SocketUnit, state(s.Socket.Active))
		}
	case detect.SSHService:
		if s.Service.IsActive() != detect.Yes {
			return fmt.Sprintf("%s is not active (%s), so sshd may not be listening", s.ServiceUnit(), state(s.Service.Active))
		}
	case detect.SSHOpenRC:
		if s.OpenRC.Started != detect.Yes {
			return fmt.Sprintf("OpenRC has not started sshd (started: %s), so sshd may not be listening", s.OpenRC.Started)
		}
	default:
		return "how sshd is started is not established, so it is not known whether sshd or " + SocketUnit + " listens"
	}
	return ""
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
