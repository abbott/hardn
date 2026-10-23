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
// ifupdown-ng 0.12.1 (Alpine 3.23) and 0.13.0 (Alpine 3.24) for each rule
// below.

// The DHCP clients DHCPExecutorRule tries, in its order, by path.
var dhcpOrder = []string{"/sbin/dhcpcd", "/usr/sbin/dhclient", "/sbin/udhcpc"}

// ifupdownConf is ifupdown-ng's own configuration, absent as Alpine
// installs it; it can turn off auto_executor_selection, which makes a
// dhcp-* property an interface's use of DHCP.
const ifupdownConf = "/etc/network/ifupdown-ng.conf"

// maxIfaceLine is the longest line ifupdown-ng reads whole
// (lif_fgetline's buffer of 4096 bytes, less its newline and NUL); it
// reads a longer one as two.
const maxIfaceLine = 4094

// iface is what Interfaces says of one interface's DHCP.
type iface struct {
	name string
	// dhcp: a "dhcp" word on its iface line, or "use dhcp".
	dhcp bool
	// implied: a dhcp-* property, which uses DHCP while ifupdown-ng's
	// auto_executor_selection is on (its default).
	implied bool
	// programs are its dhcp-program values, blanks trimmed.
	programs []string
}

// udhcpcInUse is whether udhcpc is the DHCP client ifupdown-ng runs for an
// interface of Interfaces: Yes when one would run it, No when it is
// established that none does (no interface uses DHCP, or each runs
// another client), else Unknown with why: DHCPExecutor absent or not
// holding DHCPExecutorRule (the choice is then not ifupdown-ng's as read),
// Interfaces not read, a line hardn does not follow, or a dhcp-program it
// cannot evaluate.
func udhcpcInUse(ctx context.Context, sys host.System) (detect.Tri, error) {
	if err := readExecutor(ctx, sys); err != nil {
		return detect.Unknown, err
	}
	data, err := sys.ReadFile(ctx, Interfaces)
	if err != nil {
		return detect.Unknown, fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)
	}
	ifaces, err := parseInterfaces(data)
	if err != nil {
		return detect.Unknown, err
	}
	var errs []error
	// DHCPExecutorRule's [ -x ] tests, made once; and whether
	// ifupdownConf is there, read only when an interface needs it.
	order := sync.OnceValues(func() (string, error) { return firstClient(ctx, sys) })
	conf := detect.Unknown
	for _, i := range ifaces {
		if !i.dhcp && i.implied && conf == detect.Unknown {
			switch _, err := sys.Lstat(ctx, ifupdownConf); {
			case errors.Is(err, fs.ErrNotExist):
				conf = detect.No
			case err != nil:
				return detect.Unknown, fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)
			default:
				conf = detect.Yes
			}
		}
		switch client, err := clientOf(i, conf, order); {
		case err != nil:
			if !slices.Contains(errs, err) { // order's, once for all
				errs = append(errs, err)
			}
		case client == "udhcpc":
			return detect.Yes, nil
		}
	}
	if len(errs) > 0 {
		return detect.Unknown, errors.Join(errs...)
	}
	return detect.No, nil
}

// clientOf is the client ifupdown-ng runs for i: "" when it uses no DHCP
// or none is found. conf is whether ifupdownConf is there, read only for
// an interface whose DHCP rests on a dhcp-* property.
func clientOf(i *iface, conf detect.Tri, order func() (string, error)) (string, error) {
	switch {
	case !i.dhcp && !i.implied:
		return "", nil
	case !i.dhcp && conf == detect.Yes:
		return "", fmt.Errorf("dns: %s: iface %s uses DHCP only through a dhcp-* property, and %s, which can turn that off, is not read: %w",
			Interfaces, i.name, ifupdownConf, ErrUnknown)
	case len(i.programs) == 0:
		return order()
	}
	p := i.programs[0]
	for _, q := range i.programs[1:] {
		if q != p {
			return "", fmt.Errorf("dns: %s: iface %s: dhcp-program given as %q and %q: %w", Interfaces, i.name, p, q, ErrUnknown)
		}
	}
	switch p {
	case "udhcpc", "dhcpcd", "dhclient":
		return p, nil
	}
	return "", fmt.Errorf("dns: %s: iface %s: dhcp-program %q: %w", Interfaces, i.name, p, detect.ErrUnrecognised)
}

// readExecutor checks that DHCPExecutor is the executable regular file
// whose rule udhcpcInUse follows.
func readExecutor(ctx context.Context, sys host.System) error {
	fi, err := sys.Stat(ctx, DHCPExecutor)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("dns: which DHCP client runs: %s is absent: %w", DHCPExecutor, ErrUnknown)
	case err != nil:
		return fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)
	case !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0:
		return fmt.Errorf("dns: which DHCP client runs: %s is not an executable file: %w", DHCPExecutor, ErrUnknown)
	}
	data, err := sys.ReadFile(ctx, DHCPExecutor)
	if err != nil {
		return fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)
	}
	if !bytes.Contains(data, []byte(DHCPExecutorRule)) {
		return fmt.Errorf("dns: which DHCP client runs: %s does not choose it as hardn reads: %w", DHCPExecutor, detect.ErrUnrecognised)
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
			return "", fmt.Errorf("dns: which DHCP client runs: %w: %w", ErrUnknown, err)
		case !fi.Mode().IsRegular():
			return "", fmt.Errorf("dns: %s: not a regular file: %w", p, ErrUnknown)
		case fi.Mode().Perm()&0o111 != 0:
			return p[strings.LastIndexByte(p, '/')+1:], nil
		}
	}
	return "", nil
}

// ifaceSpace is C's isspace in the C locale, which ifupdown-ng splits
// words with.
const ifaceSpace = " \t\n\v\f\r"

// legacyKeys are the legacy ifupdown properties ifupdown-ng reads as dhcp-*
// ones (interface-file.c, its remap table).
var legacyKeys = map[string]string{"client": "dhcp-client-id", "leasetime": "dhcp-leasetime", "script": "dhcp-script", "vendor": "dhcp-vendor"}

// parseInterfaces reads Interfaces as ifupdown-ng 0.12.1 and 0.13.0 do
// (libifupdown/interface-file.c, fgetline.c), for what decides each
// interface's DHCP client: "#" starts a comment anywhere on a line; a line
// whose first word does not start with a letter is skipped; "iface",
// "interface" and "auto" make the interface they name the current one,
// stanzas of one name merging; a "dhcp" word on an iface line and "use
// dhcp" make it use DHCP, and so does a dhcp-* property (legacy "client",
// "leasetime", "script" and "vendor" included) while auto executor
// selection is on; dhcp-program names its client. What it does not follow
// is an error wrapping ErrUnknown: a backslash, carriage return or NUL
// (escapes and continued lines), a line longer than ifupdown-ng reads
// whole, "source" and "source-directory", templates and inheritance
// ("template", "inherit", "inherits", 0.13.0's "defaults"), and any other
// line naming udhcpc, which may run it.
func parseInterfaces(data []byte) ([]*iface, error) {
	if i := bytes.IndexAny(data, "\\\r\x00"); i >= 0 {
		return nil, fmt.Errorf("dns: %s:%d: a backslash, carriage return or NUL, which hardn does not follow: %w",
			Interfaces, bytes.Count(data[:i], []byte("\n"))+1, ErrUnknown)
	}
	var out []*iface
	byName := map[string]*iface{}
	var cur *iface
	set := func(name string) {
		if byName[name] == nil {
			byName[name] = &iface{name: name}
			out = append(out, byName[name])
		}
		cur = byName[name]
	}
	for n, line := range strings.Split(string(data), "\n") {
		no := func(what string) error {
			return fmt.Errorf("dns: %s:%d: %s, which hardn does not follow: %w", Interfaces, n+1, what, ErrUnknown)
		}
		if len(line) > maxIfaceLine {
			return nil, no("a line longer than ifupdown-ng reads whole")
		}
		line, _, _ = strings.Cut(line, "#")
		f := strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune(ifaceSpace, r) })
		if c := firstByte(f); !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			continue // no word, or not one starting with a letter: ifupdown-ng skips the line
		}
		key, ok := legacyKeys[f[0]]
		if !ok {
			key = f[0]
		}
		switch key {
		case "iface", "interface":
			if len(f) < 2 {
				continue // ifupdown-ng reports it and goes on
			}
			if f[1] == "defaults" {
				return nil, no(`an iface named "defaults"`)
			}
			set(f[1])
			for _, w := range f[2:] {
				switch w {
				case "dhcp":
					cur.dhcp = true
				case "inherits":
					return nil, no(`"inherits"`)
				}
			}
		case "auto":
			if len(f) < 2 {
				return nil, no(`"auto" without an interface`)
			}
			set(f[1])
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
			if key == "dhcp-program" {
				_, v, _ := strings.Cut(strings.TrimLeft(line, ifaceSpace), f[0])
				cur.programs = append(cur.programs, strings.Trim(v, ifaceSpace))
			}
			if strings.HasPrefix(key, "dhcp-") {
				cur.implied = true
			}
		}
		if key != "dhcp-program" && key != "udhcpc-opts" && strings.Contains(line, "udhcpc") {
			return nil, no("a line naming udhcpc")
		}
	}
	return out, nil
}

// firstByte is the first byte of the first word, or 0 for none.
func firstByte(words []string) byte {
	if len(words) == 0 {
		return 0
	}
	return words[0][0]
}
