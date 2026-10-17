package migrate

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sshd"
)

// ErrIncludeOther: sshd_config has an Include, but its first directive is
// not the stock one, so 00-hardn.conf would not be read first and hardn has
// no other place for its settings (docs/safety/sshd-validate.md §6, open
// for the maintainer).
var ErrIncludeOther = errors.New("sshd_config's first directive is not the stock Include, so hardn has no place for its settings")

// ErrNotEstablished: a value the plan or the login-path comparison needs is
// not in sshd -T's output or the files, or not in a form hardn reads.
var ErrNotEstablished = errors.New("the configuration's login paths are not established")

// Directive is one setting hardn wants in 00-hardn.conf.
type Directive struct {
	Keyword string
	Args    []string
}

// Input is what PlanFor plans from.
type Input struct {
	Found Found
	// Desired are hardn's settings for 00-hardn.conf, written first, in
	// order. They must not narrow the login paths of the configuration
	// installed now: Apply refuses a plan that does. A change that narrows
	// them (PasswordAuthentication no, say) is the engine's, confirmed from
	// a new session after the migration (brief 2.3 step 6). They are used
	// only when there is something to migrate.
	Desired []Directive
	// SSH is detect.Detect's SSH: whether listen.conf takes effect, and
	// whether sshd-socket-generator derives ssh.socket's listeners from
	// the files.
	SSH detect.SSH
	// Platform is detect.Detect's Platform: the Alpine restore.
	Platform detect.Platform
	// Before is sshd.ReadEffective of the installed configuration, with
	// SSH: the login paths no state of the migration may narrow (Apply
	// compares with it), sshd's default key files when no line names any,
	// the values the Alpine restore keeps, and, on a socket host whose
	// generator derives ssh.socket's listeners from the files, the
	// socket's listeners now (Listeners.Socket, read whole and not empty),
	// which the migrated files keep configured.
	Before sshd.Effective
}

// OpKind is what an Op does.
type OpKind uint8

const (
	OpWriteDropIn        OpKind = iota + 1 // write 00-hardn.conf
	OpRemove                               // remove a legacy drop-in
	OpRestoreMain                          // write 00-hardn.conf, then sshd_config in the structure of Alpine's stock file
	OpRemoveSocketDropIn                   // remove listen.conf
)

// String is the step's name in the plan view: write, remove, restore or
// remove-socket-drop-in.
func (k OpKind) String() string {
	switch k {
	case OpWriteDropIn:
		return "write"
	case OpRemove:
		return "remove"
	case OpRestoreMain:
		return "restore"
	case OpRemoveSocketDropIn:
		return "remove-socket-drop-in"
	}
	return "unknown"
}

// Op is one step of a Plan.
type Op struct {
	Kind OpKind
	Path string // the file written or removed; OpRestoreMain: sshd_config
	Data []byte // OpWriteDropIn, OpRestoreMain: Path's new content
	// DropIn is OpRestoreMain's 00-hardn.conf, written before sshd_config
	// (sshd reads it only once sshd_config has the Include).
	DropIn []byte
	// Artifact and Gen name the legacy file the step migrates (zero for
	// OpWriteDropIn).
	Artifact Kind
	Gen      Generation
	// Note is the printed note (§3.17): what the step does to which file.
	// The caller adds where the backup is (backup.Run.Dir).
	Note string
}

// Carried is a legacy line, and what became of it.
type Carried struct {
	sshd.Origin
	Why string // a dropped line: why it is not in 00-hardn.conf
}

// Plan is the migration's steps, in order, and what they do to the lines.
type Plan struct {
	Ops []Op
	// DropIn is the new 00-hardn.conf; nil when it is not written.
	DropIn []byte
	// Carried are the legacy lines 00-hardn.conf holds; Dropped the ones it
	// does not, with why (a line sshd reads first elsewhere had the effect).
	Carried, Dropped []Carried
	Left             []Left
	// Keywords are the keywords whose lines the steps change, in lower
	// case, for reload.Change.Keywords: the lines written, moved or
	// removed, "port" when listen.conf's port moves, and on the Alpine
	// restore the lines of every drop-in the new Include starts reading.
	Keywords []string
	// Hold is socket listeners the migrated files must configure, an
	// unspecified IPv6 address as both families (a dual-stack socket,
	// systemd's default): listen.conf's port when it moves into
	// 00-hardn.conf ([::]:port, or 0.0.0.0:port where ssh.socket lists no
	// IPv6 address), and, on a socket host whose generator derives
	// ssh.socket's listeners from the files, the socket's listeners now.
	// Apply requires every state to keep them.
	Hold []sshd.Listener
	// Listening is the listener set configured to accept connections now,
	// and the Before of the reload that follows (Reload). On a socket host whose
	// generator derives ssh.socket's listeners from the files, when the
	// plan writes them: ssh.socket's (All is Before.Listeners.Socket, also
	// in Hold), Known when the socket's listeners were read whole, sshd
	// -T's listenaddress lines were all read and detect shows ssh.socket
	// active. sshd's own set is configuration only there, so Apply keeps
	// Hold, not it, and a port sshd's files name but the socket does not
	// listen on is not kept. Otherwise: Before.Listeners, sshd's set,
	// which Apply keeps.
	Listening sshd.Listeners
	// Before is Input.Before: the configuration the plan was made from.
	Before sshd.Effective
	// SSH and Platform are Input's: Apply validates for Platform
	// (sshd.Request) and keeps the listeners SSH says accept connections.
	SSH      detect.SSH
	Platform detect.Platform
	// Sources are the files the steps write or remove, as PlanFor read
	// them, in the order Apply backs them up: Apply goes no further when a
	// backup shows one changed since.
	Sources []Source
}

// Source is a file a step writes or removes, as the plan read it.
type Source struct {
	Path   string
	Data   []byte
	Absent bool // the file was not there
}

// Empty reports whether the plan has no step.
func (p Plan) Empty() bool { return len(p.Ops) == 0 }

// PlanFor plans the migration of in.Found's artifacts. It reads and runs
// nothing. Errors: fs.ErrInvalid for a bad Desired or an unknown Include
// mode, ErrIncludeOther for legacy drop-ins on a host whose first directive
// is not the stock Include, and ErrNotEstablished when the listeners to
// keep cannot be (ssh.socket's not read whole, or none, on a socket host
// whose generator follows the files; Port and ListenAddress lines hardn
// does not read) or a value the Alpine restore keeps is not in sshd -T's
// output.
func PlanFor(in Input) (Plan, error) {
	f := in.Found
	p := Plan{Left: slices.Clone(f.Left), Before: in.Before, Listening: in.Before.Listeners, SSH: in.SSH, Platform: in.Platform}
	if err := checkDesired(in.Desired); err != nil {
		return Plan{}, err
	}
	if len(f.Artifacts) == 0 {
		return p, nil
	}
	mode := f.Include.Mode
	if mode == sshd.IncludeUnknown {
		return Plan{}, fmt.Errorf("migrate: how sshd_config includes %s is not established: %w", sshd.DropInDir, fs.ErrInvalid)
	}
	left := func(a Artifact, why string) {
		p.Left = append(p.Left, Left{a.Path, "written by " + a.Gen.String() + ", but " + why})
	}
	var removes []Artifact
	var listen, gen *Artifact
	for i, a := range f.Artifacts {
		switch {
		case a.Kind == KindListenConf:
			listen = &f.Artifacts[i]
		case a.Kind == KindGeneratedMain:
			gen = &f.Artifacts[i]
		case hasMatch(fileLines(a.Path, a.Data)):
			left(a, "it has a Match block, which 00-hardn.conf does not hold")
		default:
			removes = append(removes, a)
		}
	}
	if mode == sshd.IncludeOther && len(removes) > 0 {
		return Plan{}, fmt.Errorf("migrate: %s line %d: %w", sshd.MainConfig, f.Include.Line, ErrIncludeOther)
	}
	restore := false
	if gen != nil {
		switch {
		case mode != sshd.IncludeNone:
			left(*gen, "it has an Include now, so it is left as it is")
		case in.Platform.Distro != detect.DistroAlpine:
			left(*gen, "restoring the distribution's sshd_config is automated on Alpine only")
		case hasMatch(fileLines(gen.Path, gen.Data)):
			left(*gen, "it has a Match block, which 00-hardn.conf does not hold")
		default:
			restore = true
		}
	}
	var ports []uint16
	removeListen := false
	if listen != nil {
		var why string
		if why, removeListen = listenRule(in.SSH, mode); !removeListen {
			left(*listen, why)
		} else if why == "" {
			ports = listen.Ports
		}
	}

	lines := sshd.ReadOrder(f.Main, f.DropIns)
	sources := map[string]Generation{}
	for _, a := range removes {
		sources[a.Path] = a.Gen
	}
	oldDropIn := slices.IndexFunc(f.DropIns, func(d sshd.File) bool { return d.Path == sshd.HardnDropIn })
	if oldDropIn >= 0 && mode == sshd.IncludeFirst {
		sources[sshd.HardnDropIn] = 0 // replaced: its lines are carried as the legacy ones are
	}
	if restore {
		sources[sshd.MainConfig] = gen.Gen
	}
	var c composed
	writes := restore || mode == sshd.IncludeFirst && (len(removes) > 0 || len(ports) > 0)
	// At the next daemon-reload the generator gives ssh.socket the files'
	// listeners: the ones it has now stay among them, and they are what
	// accepts connections (sshd's own set is configuration only).
	socketHeld := writes && socketFollowsFiles(in.SSH)
	var need []needed
	if socketHeld {
		b := in.Before.Listeners
		switch {
		case !b.SocketRead || !b.SocketComplete:
			return Plan{}, fmt.Errorf("migrate: %s's listeners were not read whole (Input.Before): %w", sshd.SocketUnit, ErrNotEstablished)
		case len(b.Socket) == 0:
			return Plan{}, fmt.Errorf("migrate: %s lists no listener (Input.Before), so what it listens on now is not established: %w", sshd.SocketUnit, ErrNotEstablished)
		}
		p.Listening = socketListening(in)
	}
	for _, port := range ports {
		need = append(need, needed{sshd.Listener{Addr: portOnlyAddr(in.Before.Listeners.Socket), Port: port}, ListenConf})
	}
	if socketHeld {
		for _, l := range in.Before.Listeners.Socket {
			need = append(need, needed{l, sshd.SocketUnit})
		}
	}
	for _, n := range need {
		if !slices.Contains(p.Hold, n.Listener) {
			p.Hold = append(p.Hold, n.Listener)
		}
	}
	if writes {
		if slices.ContainsFunc(lines, func(l sshd.Line) bool { _, ok := sources[l.File]; return ok && l.Match != nil }) {
			return Plan{}, fmt.Errorf("migrate: %s has a Match block, which is not hardn's form: %w", sshd.HardnDropIn, fs.ErrInvalid)
		}
		c = compose(in.Desired, lines, sources, in.Before)
		if restore {
			if err := c.keepStock(in.Before); err != nil {
				return Plan{}, err
			}
		}
		rest := restLines(lines, sources)
		if err := c.addListeners(need, rest); err != nil {
			return Plan{}, err
		}
		c.dedupe(rest)
		p.DropIn, p.Carried, p.Dropped = c.render(), c.carried, c.dropped
	}
	dropInSource := Source{Path: sshd.HardnDropIn, Absent: true}
	if oldDropIn >= 0 {
		dropInSource = Source{Path: sshd.HardnDropIn, Data: f.DropIns[oldDropIn].Data}
	}

	if writes && !restore {
		note := sshd.HardnDropIn + " written with hardn's SSH settings, read before the other drop-ins"
		if oldDropIn >= 0 {
			note = sshd.HardnDropIn + " rewritten with hardn's SSH settings, read before the other drop-ins (a backup of the one there is kept)"
		}
		if n := len(c.carried); n > 0 {
			note += fmt.Sprintf("; %d line(s) moved from the legacy files", n)
		}
		if ls := c.added(sshd.SocketUnit); len(ls) > 0 {
			note += "; " + strings.Join(ls, ", ") + " added so that the files configure what " + sshd.SocketUnit + " listens on now"
		}
		p.Ops = append(p.Ops, Op{Kind: OpWriteDropIn, Path: sshd.HardnDropIn, Data: p.DropIn, Note: note})
		p.Sources = append(p.Sources, dropInSource)
	}
	for _, a := range removes {
		note := a.Path + " (written by " + a.Gen.String() + ") removed: the lines of it sshd uses are in " + sshd.HardnDropIn + "; a backup is kept"
		if mode == sshd.IncludeNone {
			note = a.Path + " (written by " + a.Gen.String() + ") removed: sshd did not read it, as sshd_config has no Include; a backup is kept"
		}
		p.Ops = append(p.Ops, Op{Kind: OpRemove, Path: a.Path, Artifact: a.Kind, Gen: a.Gen, Note: note})
		p.Sources = append(p.Sources, Source{Path: a.Path, Data: a.Data})
	}
	var others []sshd.File
	if restore {
		others = otherDropIns(f.DropIns, removes)
		note := sshd.MainConfig + " (written whole by " + gen.Gen.String() + ") restored to the structure of Alpine's stock file: the Include of " +
			sshd.DropInDir + " first, then " + stockSummary + "; hardn's lines are in " + sshd.HardnDropIn + "; a backup is kept"
		if kept := c.added(keptFrom); len(kept) > 0 {
			note += "; " + strings.Join(kept, ", ") + " kept in " + sshd.HardnDropIn + ", which is read first, because the configuration installed now has it where the stock line differs (changing it is a later, confirmed change)"
		}
		if len(others) > 0 {
			var names []string
			for _, d := range others {
				names = append(names, d.Path)
			}
			note += "; with the Include, sshd also reads " + strings.Join(names, ", ")
		}
		p.Ops = append(p.Ops, Op{Kind: OpRestoreMain, Path: sshd.MainConfig, Data: restoredMain(c), DropIn: p.DropIn,
			Artifact: KindGeneratedMain, Gen: gen.Gen, Note: note})
		p.Sources = append(p.Sources, dropInSource, Source{Path: sshd.MainConfig, Data: gen.Data})
	}
	if removeListen {
		note := ListenConf + " (written by hardn v0.3.x) removed: it had no effect, as ssh.socket does not listen on this host; a backup is kept"
		if len(ports) > 0 {
			held := sshd.Listener{Addr: portOnlyAddr(in.Before.Listeners.Socket), Port: ports[0]}
			family := "for both families, as " + held.String() + " (dual-stack, as systemd binds ListenStream=" + strconv.Itoa(int(ports[0])) + ")"
			if held.Addr.Is4() {
				family = "as " + held.String() + " (" + sshd.SocketUnit + " lists no IPv6 address now, as where systemd finds IPv6 unsupported)"
			}
			how := "as they already did"
			if added := c.added(ListenConf); len(added) > 0 {
				how = "with " + strings.Join(added, ", ") + " added to " + sshd.HardnDropIn
			}
			note = fmt.Sprintf("%s (written by hardn v0.3.x) removed: the sshd files keep its port %s, %s, and sshd-socket-generator gives ssh.socket their listeners at the next systemctl daemon-reload; a backup is kept",
				ListenConf, family, how)
		}
		p.Ops = append(p.Ops, Op{Kind: OpRemoveSocketDropIn, Path: ListenConf, Artifact: KindListenConf, Gen: GenV03, Note: note})
		p.Sources = append(p.Sources, Source{Path: ListenConf, Data: listen.Data})
	}
	var read []sshd.Line
	for _, d := range others {
		read = append(read, fileLines(d.Path, d.Data)...)
	}
	p.Keywords = keywords(lines, sources, c, restore, len(ports) > 0, read)
	return p, nil
}

// socketFollowsFiles reports whether ssh.socket listens and its generator
// derives its listeners from sshd's files (detect: SSHSocket, Generator
// Yes).
func socketFollowsFiles(s detect.SSH) bool {
	return s.Mode == detect.SSHSocket && s.Generator == detect.Yes
}

// socketListening is the listener set that accepts connections on a socket
// host whose generator follows the files: ssh.socket's, read whole and not
// empty (PlanFor checked), Known when sshd -T's listeners were read too
// and detect shows ssh.socket active. sshd's and the socket's sets may
// disagree (listen.conf in effect, or written without a daemon-reload,
// D9): the plan holds the socket's, so their agreement is not needed.
func socketListening(in Input) sshd.Listeners {
	l := in.Before.Listeners
	l.All = nil
	for _, s := range l.Socket {
		if !slices.Contains(l.All, s) {
			l.All = append(l.All, s)
		}
	}
	n := len(in.Before.Keywords["listenaddress"])
	l.Known = in.SSH.Socket.IsActive() == detect.Yes && n > 0 && n == len(l.SSHD)
	return l
}

// portOnlyAddr is the address systemd gives a ListenStream= of a port
// alone: [::], a dual-stack socket, where IPv6 is supported, and 0.0.0.0
// where it is not (systemd v257 socket_address_parse). Whether it is, is
// read off the socket's listeners now: none of them IPv6 is taken as not,
// so that no IPv6 listener the host may not bind is added.
func portOnlyAddr(socket []sshd.Listener) netip.Addr {
	if slices.ContainsFunc(socket, func(l sshd.Listener) bool { return l.Addr.Is6() && !l.Addr.Is4In6() }) {
		return netip.IPv6Unspecified()
	}
	return netip.IPv4Unspecified()
}

// restLines are the global lines of the files the migration leaves, as
// 00-hardn.conf's lines are kept (compose).
func restLines(lines []sshd.Line, sources map[string]Generation) []outLine {
	var rest []outLine
	for _, l := range lines {
		if _, ok := sources[l.File]; !ok && l.Match == nil {
			rest = append(rest, outLine{keyword: l.Keyword, args: l.Args})
		}
	}
	return rest
}

// added lists the lines compose added for from (ListenConf, or
// sshd.SocketUnit), as written.
func (c composed) added(from string) []string {
	var out []string
	for _, l := range c.out {
		if l.from == from {
			out = append(out, l.text)
		}
	}
	return out
}

// listenRule says what becomes of listen.conf, and why: on a socket host
// whose generator derives ssh.socket's listeners from the sshd files, its
// port moves into 00-hardn.conf (why ""); where ssh.socket does not listen
// it had no effect and goes; otherwise (no generator, whether it runs not
// established, or the SSH mode Unknown) it stays.
func listenRule(s detect.SSH, mode sshd.IncludeMode) (why string, remove bool) {
	switch {
	case s.Mode == detect.SSHSocket && s.Generator == detect.Yes && mode == sshd.IncludeFirst:
		return "", true
	case s.Mode == detect.SSHSocket && s.Generator == detect.Yes:
		return "sshd_config has no stock first-line Include, so its port cannot move into " + sshd.HardnDropIn, false
	case s.Mode == detect.SSHService || s.Mode == detect.SSHOpenRC:
		return "it had no effect", true
	case s.Mode == detect.SSHSocket && s.Generator == detect.No:
		return "ssh.socket takes its port from ssh.socket.d on this host (sshd-socket-generator is not in use)", false
	case s.Mode == detect.SSHSocket:
		return "whether sshd-socket-generator derives ssh.socket's listeners from the files is not established", false
	}
	return "how sshd is started is not established", false
}

// The structure of Alpine's stock sshd_config: the directives it does not
// comment out, in OpenSSH's order (docs/safety/sshd-migrate.md §3: the
// Include first, by aports' include-config-dir.patch, from 3.19;
// AllowTcpForwarding, GatewayPorts and X11Forwarding no; the internal
// sftp server; and upstream's AuthorizedKeysFile).
var alpineStock = []string{
	"AuthorizedKeysFile\t.ssh/authorized_keys",
	"AllowTcpForwarding no",
	"GatewayPorts no",
	"X11Forwarding no",
	"Subsystem\tsftp\tinternal-sftp",
}

const stockSummary = "AuthorizedKeysFile .ssh/authorized_keys, AllowTcpForwarding no, GatewayPorts no, X11Forwarding no and Subsystem sftp internal-sftp"

// restoredMain is sshd_config in the structure of Alpine's stock file. A
// stock Subsystem whose name 00-hardn.conf defines is left out: sshd
// refuses a second definition of a name [I], and 00-hardn.conf's is read
// first anyway.
func restoredMain(c composed) []byte {
	var b strings.Builder
	b.WriteString("# Restored by hardn to the structure of Alpine's stock sshd_config.\n# hardn's settings are in " +
		sshd.HardnDropIn + ", which the Include reads first.\n\n" + sshd.IncludeLine + "\n\n")
	for _, s := range alpineStock {
		f := strings.Fields(s)
		if k := sshd.CanonicalKeyword(f[0], f[1:]); strings.HasPrefix(k, "subsystem ") && c.sets(k) {
			continue
		}
		b.WriteString(s + "\n")
	}
	return []byte(b.String())
}

// keepStock adds to 00-hardn.conf, for each first-value directive of
// alpineStock whose keyword it does not set, the installed configuration's
// value (sshd -T's, in before) when it differs from the stock one: the
// Include reads 00-hardn.conf first, so the stock line then has no effect
// and the effective value stays (docs/safety/sshd-migrate.md §2.12; the
// generated files set no forwarding keyword, so sshd's default "yes" for
// AllowTcpForwarding stays rather than becoming Alpine's "no"). A value
// sshd -T does not give is ErrNotEstablished.
func (c *composed) keepStock(before sshd.Effective) error {
	for _, s := range alpineStock {
		f := strings.Fields(s)
		k := sshd.CanonicalKeyword(f[0], f[1:])
		if strings.HasPrefix(k, "subsystem ") || c.sets(k) {
			continue
		}
		vs := before.Keywords[k]
		if len(vs) != 1 {
			return fmt.Errorf("migrate: sshd -T gives %d values for %s, so the restore cannot keep the installed one: %w", len(vs), k, ErrNotEstablished)
		}
		v := strings.Fields(vs[0])
		if sameValue(k, v, f[1:]) {
			continue
		}
		c.out = append(c.out, outLine{text: render(f[0], v), keyword: k, args: v, from: keptFrom})
	}
	return nil
}

// sameValue reports whether a and b are one value of k: key file paths
// exactly, the other keywords' words in any case (sshd -T prints them in
// lower case).
func sameValue(k string, a, b []string) bool {
	if k == "authorizedkeysfile" {
		return slices.Equal(a, b)
	}
	return slices.EqualFunc(a, b, strings.EqualFold)
}

// otherDropIns are the drop-ins of drops not removed and not hardn's own.
func otherDropIns(drops []sshd.File, removes []Artifact) []sshd.File {
	var out []sshd.File
	for _, d := range drops {
		if d.Path != sshd.HardnDropIn && !slices.ContainsFunc(removes, func(a Artifact) bool { return a.Path == d.Path }) {
			out = append(out, d)
		}
	}
	return out
}

// keywords lists, once each, the first word of the keyword of every line
// the plan removes, moves or writes, and of every line of read: the
// drop-ins the restored Include starts reading, whose lines take effect
// as the plan's do (a UsePAM there changes the binary OpenRC runs, which
// the reload refuses, reload.ErrFlavour).
func keywords(lines []sshd.Line, sources map[string]Generation, c composed, restore, ports bool, read []sshd.Line) []string {
	var ks []string
	add := func(k string) {
		if k, _, _ = strings.Cut(k, " "); !slices.Contains(ks, k) {
			ks = append(ks, k)
		}
	}
	for _, l := range lines {
		if _, ok := sources[l.File]; ok {
			add(l.Keyword)
		}
	}
	for _, l := range c.out {
		add(l.keyword)
	}
	if restore {
		for _, s := range alpineStock {
			add(strings.ToLower(strings.Fields(s)[0]))
		}
	}
	if ports {
		add("port")
	}
	for _, l := range read {
		add(l.Keyword)
	}
	slices.Sort(ks)
	return ks
}

// fileLines is one file's directives as sshd reads them.
func fileLines(name string, data []byte) []sshd.Line {
	if name == sshd.MainConfig {
		return sshd.ReadOrder(data, nil)
	}
	return sshd.ReadOrder([]byte(sshd.IncludeLine), []sshd.File{{Path: name, Data: data}})
}

func hasMatch(ls []sshd.Line) bool {
	return slices.ContainsFunc(ls, func(l sshd.Line) bool { return l.Match != nil })
}

var keywordRE = regexp.MustCompile(`\A[A-Za-z][A-Za-z0-9]*\z`)

// checkDesired refuses what 00-hardn.conf cannot hold as one line per
// setting: a keyword that is not a plain word, a Match or Include, no
// argument, or an argument with a line break or a NUL.
func checkDesired(ds []Directive) error {
	for _, d := range ds {
		k := strings.ToLower(d.Keyword)
		switch {
		case !keywordRE.MatchString(d.Keyword) || k == "match" || k == "include":
			return fmt.Errorf("migrate: desired keyword %q: %w", d.Keyword, fs.ErrInvalid)
		case len(d.Args) == 0:
			return fmt.Errorf("migrate: desired %s: no argument: %w", d.Keyword, fs.ErrInvalid)
		case slices.ContainsFunc(d.Args, func(a string) bool { return strings.ContainsAny(a, "\n\r\x00") }):
			return fmt.Errorf("migrate: desired %s: a line break or NUL in an argument: %w", d.Keyword, fs.ErrInvalid)
		}
	}
	return nil
}

// outLine is one line of the new 00-hardn.conf.
type outLine struct {
	text    string
	keyword string // canonical
	args    []string
	from    string      // the file it moved from; "" for Desired; keysFrom, keptFrom; ListenConf or sshd.SocketUnit for an added listener
	origin  sshd.Origin // a moved line's
}

// The from of a line compose or keepStock wrote rather than moved.
const (
	keysFrom = "AuthorizedKeysFile"   // the AuthorizedKeysFile union
	keptFrom = "the installed values" // keepStock
)

// composed is the new 00-hardn.conf, and what became of the legacy lines.
type composed struct {
	out              []outLine
	carried, dropped []Carried
}

// compose builds 00-hardn.conf: desired first, then each line of the
// source files that sshd uses today, in read order (lines is the installed
// configuration in sshd's order):
//   - an accumulating keyword's line moves, unless 00-hardn.conf has it;
//   - a first-value keyword's line moves when it is the first line sshd
//     reads for the keyword, and desired does not set it (otherwise sshd
//     uses another line, or will use hardn's);
//   - AuthorizedKeysFile, when the line sshd reads first is in a source
//     file or no line names one, becomes one line naming every key file
//     desired, the source lines and the line sshd reads first name (sshd's
//     default, from before's sshd -T, when no line names one), and, for an
//     unreleased build's bare path (V5's ".ssh_%u"), that path's
//     authorized_keys and .ssh/authorized_keys (R8; side-effects.md D7).
//     When the line sshd reads first is in a file the migration leaves,
//     the source lines had no effect and are dropped: 00-hardn.conf has
//     no AuthorizedKeysFile then, so that line stays first, unless desired
//     sets one, which then also names that line's files.
//
// The listeners to keep are added after (addListeners, dedupe).
func compose(desired []Directive, lines []sshd.Line, sources map[string]Generation, before sshd.Effective) composed {
	var c composed
	set := map[string]bool{}
	akf := -1
	for _, d := range desired {
		k := sshd.CanonicalKeyword(d.Keyword, d.Args)
		set[k] = true
		if k == "authorizedkeysfile" && akf < 0 {
			akf = len(c.out)
		}
		c.out = append(c.out, outLine{text: render(d.Keyword, d.Args), keyword: k, args: d.Args})
	}
	first := map[string]int{}
	for i, l := range lines {
		if _, ok := first[l.Keyword]; !ok && l.Match == nil {
			first[l.Keyword] = i
		}
	}
	var keys []string
	if akf >= 0 {
		keys = slices.Clone(c.out[akf].args)
	}
	keysTouched := akf >= 0
	// keysFromSources: the source files' AuthorizedKeysFile lines decide
	// which key files sshd reads today, so their union keeps every one.
	fk, hasKeys := first["authorizedkeysfile"]
	keysFromSources := true
	if hasKeys {
		_, keysFromSources = sources[lines[fk].File]
	}
	for i, l := range lines {
		g, ok := sources[l.File]
		if !ok || l.Match != nil {
			continue
		}
		move := func() {
			c.out = append(c.out, outLine{text: strings.TrimLeft(l.Text, " \t\r"), keyword: l.Keyword, args: l.Args, from: l.File, origin: l.Origin})
			c.carried = append(c.carried, Carried{Origin: l.Origin})
		}
		switch k := l.Keyword; {
		case k == "authorizedkeysfile" && !keysFromSources:
			o := lines[fk]
			c.dropped = append(c.dropped, Carried{l.Origin, fmt.Sprintf("no effect: sshd reads %s line %d first", o.File, o.Line)})
		case k == "authorizedkeysfile":
			keysTouched = true
			keys = append(keys, l.Args...)
			for _, a := range l.Args {
				if g == GenMain && !strings.HasSuffix(a, "authorized_keys") && a != "none" {
					keys = append(keys, strings.TrimSuffix(a, "/")+"/authorized_keys", ".ssh/authorized_keys")
				}
			}
			c.carried = append(c.carried, Carried{Origin: l.Origin, Why: "its key files are in 00-hardn.conf's AuthorizedKeysFile"})
		case sshd.Accumulates(k) && c.has(k, l.Args):
			c.dropped = append(c.dropped, Carried{l.Origin, "00-hardn.conf has the same line"})
		case sshd.Accumulates(k):
			move()
		case set[k]:
			c.dropped = append(c.dropped, Carried{l.Origin, "hardn's settings set " + k})
		case first[k] != i:
			o := lines[first[k]]
			c.dropped = append(c.dropped, Carried{l.Origin, fmt.Sprintf("no effect: sshd reads %s line %d first", o.File, o.Line)})
		default:
			move()
		}
	}
	if keysTouched {
		if hasKeys {
			keys = append(keys, lines[fk].Args...)
		} else if v := before.Keywords["authorizedkeysfile"]; len(v) == 1 {
			keys = append(keys, strings.Fields(v[0])...) // sshd's default, which no line overrides today
		}
		var uniq []string
		for _, k := range keys {
			if !slices.Contains(uniq, k) {
				uniq = append(uniq, k)
			}
		}
		l := outLine{text: render("AuthorizedKeysFile", uniq), keyword: "authorizedkeysfile", args: uniq, from: keysFrom}
		if akf >= 0 {
			c.out[akf] = l
		} else {
			c.out = append(c.out, l)
		}
	}
	return c
}

// sameArg reports whether a line's arguments are arg: for Port, the same
// number.
func sameArg(keyword string, args []string, arg string) bool {
	if len(args) != 1 {
		return false
	}
	if keyword == "port" {
		a, errA := strconv.Atoi(args[0])
		b, errB := strconv.Atoi(arg)
		return errA == nil && errB == nil && a == b
	}
	return args[0] == arg
}

// has reports whether 00-hardn.conf has a line setting k to args.
func (c composed) has(k string, args []string) bool {
	return slices.ContainsFunc(c.out, func(l outLine) bool {
		return l.keyword == k && (len(args) == 1 && sameArg(k, l.args, args[0]) || slices.Equal(l.args, args))
	})
}

// sets reports whether 00-hardn.conf sets the canonical keyword k.
func (c composed) sets(k string) bool {
	return slices.ContainsFunc(c.out, func(l outLine) bool { return l.keyword == k })
}

// render is 00-hardn.conf: a comment before each run of lines that came
// from one place.
func (c composed) render() []byte {
	var b strings.Builder
	b.WriteString("# Managed by hardn.\n")
	from := "\x00" // before any line
	for _, l := range c.out {
		if l.from != from {
			switch from = l.from; from {
			case "":
				b.WriteString("# Set by hardn:\n")
			case keysFrom:
				b.WriteString("# Every key file the files hardn replaced named, so that no key stops working:\n")
			case keptFrom:
				b.WriteString("# The installed configuration's values, where " + sshd.MainConfig + "'s stock lines differ:\n")
			case sshd.SocketUnit:
				b.WriteString("# What " + sshd.SocketUnit + " listens on now, so that sshd-socket-generator keeps it:\n")
			default:
				b.WriteString("# From " + from + ":\n")
			}
		}
		b.WriteString(l.text + "\n")
	}
	return []byte(b.String())
}

// render is a directive as one line: an argument with a space, a quote, a
// backslash or a '#' in it, or none at all, in double quotes, with a
// backslash before a double quote or a backslash (sshd's argv_split).
func render(keyword string, args []string) string {
	var b strings.Builder
	b.WriteString(keyword)
	for _, a := range args {
		b.WriteByte(' ')
		if a != "" && !strings.ContainsAny(a, " \t\"'\\#") {
			b.WriteString(a)
			continue
		}
		b.WriteString(`"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a) + `"`)
	}
	return b.String()
}
