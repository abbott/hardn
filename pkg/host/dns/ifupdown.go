package dns

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Which DHCP client ifupdown-ng runs on Alpine (next/s2-udhcpc; the
// maintainer's decision of 2026-10-23: udhcpc is reported and asked about
// only where it is the client in use). docs/safety/dns.md §3 cites
// ifupdown-ng 0.12.1 (Alpine 3.23) and 0.13.0 (Alpine 3.24), Alpine's
// networking service and OpenRC for each rule below.

// The DHCP clients DHCPExecutorRule tries, in its order, by path.
var dhcpOrder = []string{"/sbin/dhcpcd", "/usr/sbin/dhclient", "/sbin/udhcpc"}

// ifupdownConf is ifupdown-ng's own configuration, absent as Alpine
// installs it; it can turn off auto_executor_selection, which makes a
// dhcp-* property an interface's use of DHCP.
const ifupdownConf = "/etc/network/ifupdown-ng.conf"

// confD is where OpenRC reads a service's configuration before it starts
// it: for Alpine's networking service "networking", its runlevel's
// "networking.<runlevel>", or a "networking.<iface>" service's own. One
// may set cfgfile, the interfaces file the service brings up in place of
// Interfaces; Alpine's openrc package installs none.
const confD = "/etc/conf.d"

// runDir is where ifupdown-ng's DHCP executor has udhcpc write its pid
// file, udhcpc.<interface>.pid.
const runDir = "/var/run"

// maxIfaceLine is the longest line ifupdown-ng reads whole
// (lif_fgetline's buffer of 4096 bytes, less its newline and NUL); it
// reads a longer one as two.
const maxIfaceLine = 4094

// iface is what Interfaces says of one interface's DHCP.
type iface struct {
	name string
	// auto: named on an "auto" line; Alpine's networking service brings
	// up those alone (ifquery --list -a).
	auto bool
	// dhcp: a "dhcp" word on its iface line, or "use dhcp".
	dhcp bool
	// implied: a dhcp-* property, which uses DHCP while ifupdown-ng's
	// auto_executor_selection is on (its default).
	implied bool
	// programs are its dhcp-program values, blanks trimmed.
	programs []string
	// script: a dhcp-script, which the DHCP executor gives udhcpc as -s.
	script bool
	// opts are its udhcpc-opts and dhcp-opts values, which the executor
	// gives udhcpc after eval (dhcp-opts since 0.13.0).
	opts []string
}

// inUse is what udhcpcInUse establishes: InUse, why it is Unknown, and why
// an interface running udhcpc may run it with another script than its
// default, whose RESOLV_CONF UDHCPC.Rewrites reads (nil: none does).
type inUse struct {
	tri         detect.Tri
	err, script error
}

// inUseError is why InUse is Unknown, with the file it is about, which
// the finding names.
type inUseError struct {
	path string
	err  error
}

func (e *inUseError) Error() string { return e.err.Error() }
func (e *inUseError) Unwrap() error { return e.err }

// unknownAt is Unknown for err, about the file path.
func unknownAt(path string, err error) inUse {
	return inUse{tri: detect.Unknown, err: &inUseError{path: path, err: err}}
}

// udhcpcInUse is whether udhcpc is the DHCP client ifupdown-ng runs for an
// interface of Interfaces that Alpine's networking service brings up: Yes
// when an auto interface would run it; No when it is established that
// none does (no interface on DHCP, each running another client) and no
// udhcpc pid file is in runDir; else Unknown with why: DHCPExecutor absent
// or not holding DHCPExecutorRule (the choice is then not ifupdown-ng's as
// read), the networking service's file in confD (it may name another
// interfaces file), Interfaces not a file read, a line not followed, a
// dhcp-program not evaluated, an interface that is not auto running it
// (brought up by hand, or as another's dependency), or a udhcpc pid file
// (udhcpc may run though Interfaces no longer starts it).
func udhcpcInUse(ctx context.Context, sys host.System) inUse {
	if err := readExecutor(ctx, sys); err != nil {
		return inUse{tri: detect.Unknown, err: err}
	}
	if err := networkingConf(ctx, sys); err != nil {
		return inUse{tri: detect.Unknown, err: err}
	}
	switch fi, err := sys.Stat(ctx, Interfaces); { // links followed, as ifupdown-ng's fopen
	case err != nil:
		return unknownAt(Interfaces, fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err))
	case !fi.Mode().IsRegular():
		return unknownAt(Interfaces, fmt.Errorf("dns: %s: not a regular file: %w", Interfaces, ErrUnknown))
	}
	data, err := sys.ReadFile(ctx, Interfaces)
	if err != nil {
		return unknownAt(Interfaces, fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err))
	}
	ifaces, err := parseInterfaces(data)
	if err != nil {
		return unknownAt(Interfaces, err)
	}
	var (
		errs      []error
		yes       bool
		scriptErr error
		// Whether ifupdownConf is there, read only when an interface
		// needs it; and DHCPExecutorRule's [ -x ] tests, made once.
		conf  = detect.Unknown
		order = sync.OnceValues(func() (string, error) { return firstClient(ctx, sys) })
	)
	for _, i := range ifaces {
		if !i.dhcp && i.implied && conf == detect.Unknown {
			switch _, err := sys.Lstat(ctx, ifupdownConf); {
			case errors.Is(err, fs.ErrNotExist):
				conf = detect.No
			case err != nil:
				return unknownAt(ifupdownConf, fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err))
			default:
				conf = detect.Yes
			}
		}
		client, err := clientOf(i, conf, order)
		switch {
		case err != nil:
			if !slices.Contains(errs, err) { // order's, once for all
				errs = append(errs, err)
			}
			continue
		case client != "udhcpc":
			continue
		case i.auto:
			yes = true
		default:
			errs = append(errs, &inUseError{path: Interfaces, err: fmt.Errorf(
				"dns: %s: iface %s would run udhcpc, and is not auto: whether it is brought up is not established: %w", Interfaces, i.name, ErrUnknown)})
		}
		if scriptErr == nil {
			scriptErr = i.otherScript()
		}
	}
	switch {
	case yes:
		return inUse{tri: detect.Yes, script: scriptErr}
	case len(errs) > 0:
		return inUse{tri: detect.Unknown, err: errors.Join(errs...), script: scriptErr}
	}
	if err := udhcpcPidFile(ctx, sys); err != nil {
		return inUse{tri: detect.Unknown, err: err}
	}
	return inUse{tri: detect.No}
}

// clientOf is the client ifupdown-ng runs for i: "" when it uses no DHCP
// or none is found. conf is whether ifupdownConf is there, read only for
// an interface whose DHCP rests on a dhcp-* property.
func clientOf(i *iface, conf detect.Tri, order func() (string, error)) (string, error) {
	switch {
	case !i.dhcp && !i.implied:
		return "", nil
	case !i.dhcp && conf == detect.Yes:
		return "", &inUseError{path: ifupdownConf, err: fmt.Errorf(
			"dns: %s: iface %s uses DHCP only through a dhcp-* property, and %s, which can turn that off, is not read: %w",
			Interfaces, i.name, ifupdownConf, ErrUnknown)}
	case len(i.programs) == 0:
		return order()
	}
	p := i.programs[0]
	for _, q := range i.programs[1:] {
		if q != p {
			return "", &inUseError{path: Interfaces, err: fmt.Errorf("dns: %s: iface %s: dhcp-program given as %q and %q: %w", Interfaces, i.name, p, q, ErrUnknown)}
		}
	}
	switch p {
	case "udhcpc", "dhcpcd", "dhclient":
		return p, nil
	}
	return "", &inUseError{path: Interfaces, err: fmt.Errorf("dns: %s: iface %s: dhcp-program %q: %w", Interfaces, i.name, p, detect.ErrUnrecognised)}
}

// readExecutor checks that DHCPExecutor is the executable regular file
// whose rule udhcpcInUse follows.
func readExecutor(ctx context.Context, sys host.System) error {
	at := func(err error) error { return &inUseError{path: DHCPExecutor, err: err} }
	fi, err := sys.Stat(ctx, DHCPExecutor)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return at(fmt.Errorf("dns: which DHCP client runs: %s is absent: %w", DHCPExecutor, ErrUnknown))
	case err != nil:
		return at(fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err))
	case !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0:
		return at(fmt.Errorf("dns: which DHCP client runs: %s is not an executable file: %w", DHCPExecutor, ErrUnknown))
	}
	data, err := sys.ReadFile(ctx, DHCPExecutor)
	if err != nil {
		return at(fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err))
	}
	if !bytes.Contains(data, []byte(DHCPExecutorRule)) {
		return at(fmt.Errorf("dns: which DHCP client runs: %s chooses the client in another way: %w", DHCPExecutor, detect.ErrUnrecognised))
	}
	return nil
}

// networkingConf: the networking service's file in confD, which may set
// another interfaces file (cfgfile).
func networkingConf(ctx context.Context, sys host.System) error {
	des, err := sys.ReadDir(ctx, confD)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return &inUseError{path: confD, err: fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)}
	}
	for _, de := range des {
		if n := de.Name(); n == "networking" || strings.HasPrefix(n, "networking.") {
			p := confD + "/" + n
			return &inUseError{path: p, err: fmt.Errorf("dns: which DHCP client runs: %s may set the networking service's interfaces file (cfgfile): %w", p, ErrUnknown)}
		}
	}
	return nil
}

// udhcpcPidFile: a pid file in runDir of the name udhcpc writes as the
// DHCP executor starts it, so that udhcpc may run though Interfaces does
// not start it (changed since, or brought up from another file).
func udhcpcPidFile(ctx context.Context, sys host.System) error {
	des, err := sys.ReadDir(ctx, runDir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return &inUseError{path: runDir, err: fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)}
	}
	for _, de := range des {
		if n := de.Name(); strings.HasPrefix(n, "udhcpc.") && strings.HasSuffix(n, ".pid") {
			p := runDir + "/" + n
			return &inUseError{path: p, err: fmt.Errorf("dns: which DHCP client runs: %s, udhcpc's pid file: it may run: %w", p, ErrUnknown)}
		}
	}
	return nil
}

// firstClient is the first of dhcpOrder that `[ -x ]` accepts, as root
// (busybox ash's test: stat, following links, then any execute bit): its
// name, or "" for none. A path that is there but not a regular file is
// Unknown: [ -x ] may accept it, and running it would fail.
func firstClient(ctx context.Context, sys host.System) (string, error) {
	for _, p := range dhcpOrder {
		fi, err := sys.Stat(ctx, p)
		switch {
		case errors.Is(err, fs.ErrNotExist): // absent, or a dangling link
		case err != nil:
			return "", &inUseError{path: p, err: fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)}
		case !fi.Mode().IsRegular():
			return "", &inUseError{path: p, err: fmt.Errorf("dns: %s: not a regular file: %w", p, ErrUnknown)}
		case fi.Mode().Perm()&0o111 != 0:
			return p[strings.LastIndexByte(p, '/')+1:], nil
		}
	}
	return "", nil
}

// otherScript is why i, which runs udhcpc, may run it with another script
// than its default: a dhcp-script, or options that may name one (-s or
// --script, or what eval expands); nil when it runs the default.
func (i *iface) otherScript() error {
	if i.script {
		return fmt.Errorf("dns: %s: iface %s runs udhcpc with its dhcp-script: %w", Interfaces, i.name, ErrUnknown)
	}
	for _, o := range i.opts {
		if !plainOpts(o) {
			return fmt.Errorf("dns: %s: iface %s: udhcpc's options %q may name another script: %w", Interfaces, i.name, o, ErrUnknown)
		}
	}
	return nil
}

// plainOpts: options v, which the DHCP executor gives udhcpc through
// eval, are words eval leaves as they are (no quote, expansion, glob or
// operator), none of which may set udhcpc's script: -s, alone or bundled,
// or --script or a prefix of it, as getopt_long takes one.
func plainOpts(v string) bool {
	if strings.ContainsAny(v, "$`\\'\"*?[]~;&|<>(){}") {
		return false
	}
	for _, w := range strings.FieldsFunc(v, isIfaceSpace) {
		name, _, _ := strings.Cut(w, "=")
		switch {
		case strings.HasPrefix(w, "--"):
			if len(name) > 2 && strings.HasPrefix("--script", name) {
				return false
			}
		case strings.HasPrefix(w, "-") && strings.ContainsRune(w[1:], 's'):
			return false
		}
	}
	return true
}

// ifaceSpace is C's isspace in the C locale, which ifupdown-ng splits
// words with.
const ifaceSpace = " \t\n\v\f\r"

func isIfaceSpace(r rune) bool { return strings.ContainsRune(ifaceSpace, r) }

// legacyKeys are the legacy ifupdown properties ifupdown-ng reads as dhcp-*
// ones (interface-file.c, its remap table).
var legacyKeys = map[string]string{"client": "dhcp-client-id", "leasetime": "dhcp-leasetime", "script": "dhcp-script", "vendor": "dhcp-vendor"}

// envName is the name ifupdown-ng gives a property in its executors'
// environment, less "IF_" (lifecycle.c, build_environment): ASCII letters
// in upper case, "-" as "_". dhcp_program and DHCP-PROGRAM are
// dhcp-program to the executor.
func envName(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case 'a' <= r && r <= 'z':
			return r - 'a' + 'A'
		case r == '-':
			return '_'
		}
		return r
	}, key)
}

// parseInterfaces reads Interfaces as ifupdown-ng 0.12.1 and 0.13.0 do
// (libifupdown/interface-file.c, fgetline.c), for what decides each
// interface's DHCP client: "#" starts a comment anywhere on a line; a line
// whose first word does not start with a letter is skipped; "iface",
// "interface" and "auto" make the interface they name the current one,
// stanzas of one name merging, and "auto" makes it one the networking
// service brings up; a "dhcp" word on an iface line and "use dhcp" make it
// use DHCP, and so does a dhcp-* property (legacy "client", "leasetime",
// "script" and "vendor" included) while auto executor selection is on;
// dhcp-program names its client, dhcp-script and udhcpc-opts or dhcp-opts
// its script, each by its name in the executor's environment (envName),
// and their values may name udhcpc (but for options eval would run).
// What it does not follow is an error wrapping ErrUnknown: a backslash,
// carriage return or NUL (escapes and continued lines), a line longer than
// ifupdown-ng reads whole, "source" and "source-directory", templates and
// inheritance ("template", "inherit", "inherits", 0.13.0's "defaults",
// and the interface of that name, whose properties every later one
// inherits), a property name with "=" (which the environment would take
// as its value), and any other line naming udhcpc, which may run it.
func parseInterfaces(data []byte) ([]*iface, error) {
	if i := bytes.IndexAny(data, "\\\r\x00"); i >= 0 {
		return nil, fmt.Errorf("dns: %s:%d: a backslash, carriage return or NUL, not followed: %w",
			Interfaces, bytes.Count(data[:i], []byte("\n"))+1, ErrUnknown)
	}
	var out []*iface
	byName := map[string]*iface{}
	var cur *iface
	for n, line := range strings.Split(string(data), "\n") {
		no := func(what string) error {
			return fmt.Errorf("dns: %s:%d: %s, not followed: %w", Interfaces, n+1, what, ErrUnknown)
		}
		if len(line) > maxIfaceLine {
			return nil, no("a line longer than ifupdown-ng reads whole")
		}
		line, _, _ = strings.Cut(line, "#")
		f := strings.FieldsFunc(line, isIfaceSpace)
		if c := firstByte(f); !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			continue // no word, or not one starting with a letter: ifupdown-ng skips the line
		}
		key, ok := legacyKeys[f[0]]
		if !ok {
			key = f[0]
		}
		switch key {
		case "iface", "interface", "auto":
			switch {
			case len(f) < 2 && key == "auto":
				return nil, no(`"auto" without an interface`)
			case len(f) < 2:
				continue // ifupdown-ng reports it and goes on
			case f[1] == "defaults":
				return nil, no(`the interface "defaults"`)
			}
			if byName[f[1]] == nil {
				byName[f[1]] = &iface{name: f[1]}
				out = append(out, byName[f[1]])
			}
			cur = byName[f[1]]
			if key == "auto" {
				cur.auto = true // the first name alone, as ifupdown-ng reads it
				break
			}
			for _, w := range f[2:] {
				switch w {
				case "dhcp":
					cur.dhcp = true
				case "inherits":
					return nil, no(`"inherits"`)
				}
			}
		case "use":
			if cur != nil && len(f) > 1 && f[1] == "dhcp" {
				cur.dhcp = true
			}
		case "source", "source-directory", "template", "inherit", "defaults":
			return nil, no(fmt.Sprintf("%q", f[0]))
		case "address", "gateway", "hostname", "dhcp-hostname": // their own handlers, none choosing DHCP
		default:
			if cur == nil {
				continue
			}
			if strings.Contains(key, "=") {
				return nil, no(`a property name with "="`)
			}
			if strings.HasPrefix(key, "dhcp-") {
				cur.implied = true
			}
			v := value(line, f[0])
			switch envName(key) {
			case "DHCP_PROGRAM":
				cur.programs = append(cur.programs, v)
				continue // the client, which clientOf reads
			case "DHCP_SCRIPT":
				cur.script = true
				continue // a script the client runs
			case "UDHCPC_OPTS", "DHCP_OPTS":
				cur.opts = append(cur.opts, v)
				if plainOpts(v) || !strings.Contains(v, "udhcpc") {
					continue // options; what eval would run is followed only where it names udhcpc
				}
			}
		}
		if strings.Contains(line, "udhcpc") {
			return nil, no("a line naming udhcpc")
		}
	}
	return out, nil
}

// value is a property's value: line after its first word, word, with the
// blanks around it trimmed.
func value(line, word string) string {
	_, v, _ := strings.Cut(strings.TrimLeft(line, ifaceSpace), word)
	return strings.Trim(v, ifaceSpace)
}

// firstByte is the first byte of the first word, or 0 for none.
func firstByte(words []string) byte {
	if len(words) == 0 {
		return 0
	}
	return words[0][0]
}
