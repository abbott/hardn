// Package sysctl reads the kernel's network and kernel settings against
// hardn's baseline, and builds the one file that sets the ones below it
// (docs/engine/gallery-sysctl.md, adopted 2026-11-14,
// built in its §9).
//
// Read reads each key of the baseline from /proc/sys, every interface's own
// value among them; the files the host reads after hardn's, for a key one
// of them sets back; whether the host routes; the sysctl tool; and, under
// OpenRC, whether the sysctl service is in a runlevel. It runs no command
// and writes nothing. A key the kernel does not have is left out; a key
// that cannot be read is never a pass.
//
// Reading.Next is hardn's file as Apply settings would write it,
// Reading.Changes the keys it sets now, and Own.Restore the values Remove
// settings puts back. Neither names a
// forwarding key, accept_ra or the "all" reverse-path filter (Writable),
// and neither lowers a value.
package sysctl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Where things are.
const (
	// File is hardn's own file: read after the vendors' 50-* and 55-*, and
	// before 99-sysctl.conf, so the administrator's own file wins.
	File = "/etc/sysctl.d/90-hardn.conf"
	// Header opens File: hardn's mark.
	Header = "# Written by hardn (Kernel Settings).\n"
	// Proc is where the kernel shows its settings.
	Proc = "/proc/sys"
	// Command is the tool that sets them.
	Command = "sysctl"
	// Service is the OpenRC service that reads the files at boot.
	Service = "sysctl"
	// Conf is the file Alpine reads after /etc/sysctl.d.
	Conf = "/etc/sysctl.conf"

	beforeMark = "# before: "
	base       = "90-hardn.conf"
)

// Errors, testable with errors.Is.
var (
	// ErrMalformed: a file at File opens with hardn's mark and holds a line
	// hardn does not write.
	ErrMalformed = errors.New("not as hardn wrote it")
	// ErrValue: a key's value is not a number.
	ErrValue = errors.New("not a number")
)

// Tool is the sysctl tool the host has.
type Tool uint8

// The tools.
const (
	// NoTool: no sysctl on the search path.
	NoTool Tool = iota
	// Procps is procps's sysctl, which reads a key with "*" in it as every
	// key it matches.
	Procps
	// Busybox is busybox's, Alpine's: no "*", and exit status 0 whether or
	// not a key was set.
	Busybox
)

// Kind is how a setting's keys are found and judged.
type Kind uint8

// The kinds.
const (
	// Plain: the keys the spec names, each judged alone.
	Plain Kind = iota
	// PerInterface: "all", "default" and every interface's own key.
	PerInterface
	// RPFilter: the reverse-path filter. "default" and every interface,
	// each interface judged by the higher of its own value and "all"'s,
	// which is never written.
	RPFilter
	// Forwarding: shown, never judged, never written.
	Forwarding
)

// Section is the screen's section a setting is drawn in.
type Section uint8

// The sections.
const (
	Network Section = iota
	Kernel
	Files
)

// Spec is one setting of the baseline: a row of the screen.
type Spec struct {
	// Name is the setting in plain words.
	Name    string
	Section Section
	Kind    Kind
	// Keys are a Plain setting's keys.
	Keys []string
	// Family and Leaf name the others': "ipv4" and "accept_redirects" are
	// net.ipv4.conf.<interface>.accept_redirects.
	Family, Leaf string
	// Want is the value hardn writes. AtLeast: a higher value passes too.
	// From, where set, is the least value that passes, below Want.
	Want    int
	AtLeast bool
	From    int
	// Under are the keys a value below Want passes for too.
	Under []string
	// Words say what a value means.
	Words map[int]string
	// Routed: left alone on a host that routes.
	Routed bool
}

// Passes reports whether key, reading v, is at the baseline.
func (s Spec) Passes(key string, v int) bool {
	return v == s.Want || (s.AtLeast && v > s.Want) || (s.From > 0 && v >= s.From && v <= s.Want) ||
		(v < s.Want && slices.Contains(s.Under, key))
}

// PtraceKey is Process tracing's key: with it set, gdb -p and strace -p
// need root.
const PtraceKey = "kernel.yama.ptrace_scope"

const sourceRoute6 = "net.ipv6.conf.all.accept_source_route"

var onOff = map[int]string{0: "off", 1: "on"}

// Specs are the settings, in the screen's and the file's order
// (gallery-sysctl.md §3.1).
var Specs = []Spec{
	{Name: "ICMP redirects", Kind: PerInterface, Family: "ipv4", Leaf: "accept_redirects", Words: map[int]string{0: "ignored", 1: "accepted"}},
	{Name: "IPv6 redirects", Kind: PerInterface, Family: "ipv6", Leaf: "accept_redirects", Words: map[int]string{0: "ignored", 1: "accepted"}},
	// IPv6 reads a value below 0 as no routing header accepted at all
	// (the kernel's ip-sysctl): stricter than 0, and left.
	{Name: "Source routing", Keys: []string{"net.ipv4.conf.all.accept_source_route", sourceRoute6}, Under: []string{sourceRoute6},
		Words: map[int]string{-1: "refused", 0: "refused", 1: "accepted"}},
	{Name: "SYN cookies", Keys: []string{"net.ipv4.tcp_syncookies"}, Want: 1, AtLeast: true, Words: map[int]string{0: "off", 1: "on", 2: "on"}},
	{Name: "Broadcast pings", Keys: []string{"net.ipv4.icmp_echo_ignore_broadcasts"}, Want: 1, Words: map[int]string{0: "answered", 1: "ignored"}},
	{Name: "Bogus ICMP errors", Keys: []string{"net.ipv4.icmp_ignore_bogus_error_responses"}, Want: 1, Words: map[int]string{0: "logged", 1: "ignored"}},
	{Name: "Sending redirects", Kind: PerInterface, Family: "ipv4", Leaf: "send_redirects", Words: onOff, Routed: true},
	{Name: "Reverse-path filter", Kind: RPFilter, Family: "ipv4", Leaf: "rp_filter", Want: 2, From: 1,
		Words: map[int]string{0: "off", 1: "strict", 2: "loose"}, Routed: true},
	{Name: "Forwarding", Kind: Forwarding, Words: onOff},

	{Name: "Kernel addresses", Section: Kernel, Keys: []string{"kernel.kptr_restrict"}, Want: 1, AtLeast: true,
		Words: map[int]string{0: "shown to users", 1: "hidden from users", 2: "hidden"}},
	{Name: "Kernel log", Section: Kernel, Keys: []string{"kernel.dmesg_restrict"}, Want: 1, Words: map[int]string{0: "any user", 1: "root only"}},
	{Name: "Address randomizing", Section: Kernel, Keys: []string{"kernel.randomize_va_space"}, Want: 2,
		Words: map[int]string{0: "off", 1: "partial", 2: "full"}},
	{Name: "Process tracing", Section: Kernel, Keys: []string{PtraceKey}, Want: 1, AtLeast: true,
		Words: map[int]string{0: "unrestricted", 1: "children only", 2: "root only", 3: "off"}},
	// 1 passes and is never written: it cannot be undone before a reboot.
	{Name: "Unprivileged BPF", Section: Kernel, Keys: []string{"kernel.unprivileged_bpf_disabled"}, Want: 2, From: 1,
		Words: map[int]string{0: "allowed", 1: "off", 2: "off"}},
	{Name: "Performance events", Section: Kernel, Keys: []string{"kernel.perf_event_paranoid"}, Want: 2, AtLeast: true,
		Words: map[int]string{-1: "open", 0: "open", 1: "open", 2: "limited", 3: "limited", 4: "limited"}},

	{Name: "Hard links", Section: Files, Keys: []string{"fs.protected_hardlinks"}, Want: 1, Words: map[int]string{0: "not protected", 1: "protected"}},
	{Name: "Symbolic links", Section: Files, Keys: []string{"fs.protected_symlinks"}, Want: 1, Words: map[int]string{0: "not protected", 1: "protected"}},
	{Name: "FIFOs in /tmp", Section: Files, Keys: []string{"fs.protected_fifos"}, Want: 1, AtLeast: true,
		Words: map[int]string{0: "not protected", 1: "protected", 2: "protected"}},
	{Name: "Files in /tmp", Section: Files, Keys: []string{"fs.protected_regular"}, Want: 1, AtLeast: true,
		Words: map[int]string{0: "not protected", 1: "protected", 2: "protected"}},
}

// The names every net.<family>.conf directory has beside its interfaces.
const (
	confAll     = "all"
	confDefault = "default"
	glob        = "*"
)

// The forwarding keys, never written: a host routes when one reads 1.
const (
	ipForward   = "net.ipv4.ip_forward"
	forwardLeaf = "forwarding"
)

// confKey is an interface's own key: "net.ipv4.conf.eth0.accept_redirects".
// A "." in the interface's name is written "/", as sysctl reads it.
func confKey(family, name, leaf string) string {
	return "net." + family + ".conf." + strings.ReplaceAll(name, ".", "/") + "." + leaf
}

// splitConf is confKey's inverse; ok is false for any other key.
func splitConf(key string) (family, name, leaf string, ok bool) {
	parts := strings.Split(key, ".")
	if len(parts) != 5 || parts[0] != "net" || parts[2] != "conf" || parts[3] == "" {
		return "", "", "", false
	}
	return parts[1], parts[3], parts[4], true
}

// ProcPath is the file under Proc that holds key: sysctl's "." is the
// path's "/", and its "/" a "." in a name.
func ProcPath(key string) string {
	swap := strings.NewReplacer(".", "/", "/", ".")
	return Proc + "/" + swap.Replace(key)
}

// Writable reports whether hardn may write key, a key or one with "*" for
// the interface: a key of the baseline, and never a forwarding key,
// accept_ra, or the reverse-path filter of "all" (or of "*", which holds
// it).
func Writable(key string) bool {
	_, ok := specOf(key)
	return ok
}

// specOf is the setting key belongs to, as a key hardn writes.
func specOf(key string) (Spec, bool) {
	family, name, leaf, conf := splitConf(key)
	for _, s := range Specs {
		switch s.Kind {
		case Plain:
			if slices.Contains(s.Keys, key) {
				return s, true
			}
		case PerInterface:
			if conf && family == s.Family && leaf == s.Leaf {
				return s, true
			}
		case RPFilter:
			if conf && family == s.Family && leaf == s.Leaf && name != confAll && name != glob {
				return s, true
			}
		}
	}
	return Spec{}, false
}

// Value is one key as read.
type Value struct {
	Key string
	N   int
	// Err is why it is not read; nil where N is its value.
	Err error
}

// ReadValue reads key from Proc. A key the kernel does not have is an Err
// that wraps fs.ErrNotExist; one that is no number, ErrValue.
func ReadValue(ctx context.Context, sys host.System, key string) Value {
	data, err := sys.ReadFile(ctx, ProcPath(key))
	if err != nil {
		return Value{Key: key, Err: err}
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return Value{Key: key, Err: fmt.Errorf("sysctl: %s: %w", key, ErrValue)}
	}
	return Value{Key: key, N: n}
}

// State is a row's verdict.
type State uint8

// The states.
const (
	// Pass: every key is at the baseline.
	Pass State = iota
	// Below: a key is below it, and Apply settings would set it.
	Below
	// Held: a key is below it, and a file read after hardn's sets it so
	// (Row.By). hardn leaves it.
	Held
	// LeftAlone: the host routes, or is a Proxmox VE node (Reading.Routes,
	// Reading.Proxmox).
	LeftAlone
	// NoVerdict: shown only: Forwarding, and every row in a container.
	NoVerdict
	// Absent: the kernel has none of the row's keys (IPv6 off).
	Absent
	// Unread: a key could not be read. Never a pass.
	Unread
)

// Row is one setting as read.
type Row struct {
	Spec Spec
	// Values are the row's keys the kernel has, as read.
	Values []Value
	// Shown is the value the row shows: the weakest of its keys.
	Shown int
	State State
	// Low are the keys below the baseline, which Apply settings would set.
	Low []Value
	// By is the file that holds a Held row's key, by its path.
	By string
	// Err is why an Unread row's key was not read.
	Err error
}

// Reading is the host as read.
type Reading struct {
	Rows []Row
	// Container: the host is a container. Its rows have no verdict.
	Container bool
	// Routes: a forwarding key reads 1. Proxmox: a Proxmox VE node, which
	// may route for its guests at any time.
	Routes, Proxmox bool
	// Tool is the sysctl tool.
	Tool Tool
	// OpenRC: the files are read at boot by OpenRC's sysctl service, in
	// its order, where it is in a runlevel.
	OpenRC bool
	// Boot is whether the files of /etc/sysctl.d are applied at boot: Yes
	// under systemd; under OpenRC, whether Service is in a runlevel.
	Boot detect.Tri
	// Own is hardn's file; nil with none. Foreign: a file is at File
	// without hardn's mark.
	Own     *Own
	Foreign bool
	// Err is why the host's files, or hardn's own, were not read.
	Err error

	conf map[string][]string // family: all, default and each interface
}

// LeftAlone reports whether the rows that depend on routing are left alone.
func (r Reading) LeftAlone() bool { return r.Routes || r.Proxmox }

// Todo are the rows Apply settings would set.
func (r Reading) Todo() []Row {
	var out []Row
	for _, row := range r.Rows {
		if row.State == Below {
			out = append(out, row)
		}
	}
	return out
}

// Count is how many rows are in state st.
func (r Reading) Count(st State) int {
	n := 0
	for _, row := range r.Rows {
		if row.State == st {
			n++
		}
	}
	return n
}

// Unread is the first row that could not be read, if any.
func (r Reading) Unread() (Row, bool) {
	i := slices.IndexFunc(r.Rows, func(row Row) bool { return row.State == Unread })
	if i < 0 {
		return Row{}, false
	}
	return r.Rows[i], true
}

// Read reads the host, writing nothing and running nothing.
func Read(ctx context.Context, sys host.System, f detect.Facts) Reading {
	r := Reading{Container: f.Container.Detected == detect.Yes, Proxmox: f.Platform.Distro == detect.DistroProxmox,
		OpenRC: f.Init.Kind == detect.OpenRC, conf: map[string][]string{}}
	var errs []error
	for _, family := range []string{"ipv4", "ipv6"} {
		names, err := confNames(ctx, sys, family)
		if err != nil {
			errs = append(errs, err)
		}
		r.conf[family] = names
	}
	r.Tool = readTool(ctx, sys)
	switch {
	case f.Init.Kind == detect.Systemd:
		r.Boot = detect.Yes
	case r.OpenRC:
		var err error
		if _, r.Boot, err = detect.ServiceRunlevels(ctx, sys, Service); err != nil && r.Boot == detect.Unknown {
			errs = append(errs, err)
		}
	}
	own, foreign, err := readOwn(ctx, sys)
	if err != nil {
		errs = append(errs, err)
	}
	r.Own, r.Foreign = own, foreign
	after, err := later(ctx, sys, r.OpenRC)
	if err != nil {
		errs = append(errs, err)
	}

	forward := r.forwarding(ctx, sys)
	for _, v := range forward.Values {
		r.Routes = r.Routes || v.N == 1
	}
	for _, s := range Specs {
		if s.Kind == Forwarding {
			r.Rows = append(r.Rows, forward)
			continue
		}
		r.Rows = append(r.Rows, r.row(ctx, sys, s, after))
	}
	if !slices.ContainsFunc(r.Rows, func(row Row) bool { return len(row.Values) > 0 }) {
		// Not one key: Proc is not there, and nothing is a pass.
		errs = append(errs, fmt.Errorf("sysctl: %s: %w", Proc, fs.ErrNotExist))
	}
	r.Err = errors.Join(errs...)
	return r
}

// confNames are the names under net.<family>.conf: all, default, then each
// interface in name order. None, and no error, where the kernel has no such
// family.
func confNames(ctx context.Context, sys host.System, family string) ([]string, error) {
	entries, err := sys.ReadDir(ctx, Proc+"/net/"+family+"/conf")
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}
	var names, fixed []string
	for _, e := range entries {
		switch n := e.Name(); {
		case n == confAll || n == confDefault:
			fixed = append(fixed, n)
		case e.IsDir():
			names = append(names, n)
		}
	}
	slices.Sort(fixed)
	slices.Sort(names)
	return append(fixed, names...), nil
}

// readTool tells the sysctl tool: the first on the search path, busybox's
// where it is a link to busybox.
func readTool(ctx context.Context, sys host.System) Tool {
	for _, dir := range strings.Split(host.SearchPath, ":") {
		name := dir + "/" + Command
		if _, err := sys.Stat(ctx, name); err != nil {
			continue
		}
		if target, err := sys.Readlink(ctx, name); err == nil && path.Base(target) == "busybox" {
			return Busybox
		}
		return Procps
	}
	return NoTool
}

// read reads keys, leaving out the ones the kernel does not have; err is
// the first other failure.
func read(ctx context.Context, sys host.System, keys []string) (vs []Value, err error) {
	for _, k := range keys {
		v := ReadValue(ctx, sys, k)
		switch {
		case errors.Is(v.Err, fs.ErrNotExist):
			continue
		case v.Err != nil && err == nil:
			err = v.Err
		}
		vs = append(vs, v)
	}
	return vs, err
}

// keys are a setting's keys on this host, before any is read.
func (r Reading) keys(s Spec) []string {
	switch s.Kind {
	case Plain:
		return s.Keys
	case PerInterface:
		var out []string
		for _, n := range r.conf[s.Family] {
			out = append(out, confKey(s.Family, n, s.Leaf))
		}
		return out
	case RPFilter:
		var out []string
		for _, n := range r.conf[s.Family] {
			if n != confAll {
				out = append(out, confKey(s.Family, n, s.Leaf))
			}
		}
		return out
	}
	return nil
}

// forwarding is the Forwarding row: every forwarding key, and 1 where any
// reads 1.
func (r Reading) forwarding(ctx context.Context, sys host.System) Row {
	keys := []string{ipForward}
	for _, family := range []string{"ipv4", "ipv6"} {
		for _, n := range r.conf[family] {
			keys = append(keys, confKey(family, n, forwardLeaf))
		}
	}
	row := Row{Spec: Specs[slices.IndexFunc(Specs, func(s Spec) bool { return s.Kind == Forwarding })], State: NoVerdict}
	row.Values, row.Err = read(ctx, sys, keys)
	for _, v := range row.Values {
		if v.Err == nil && v.N == 1 {
			row.Shown = 1
		}
	}
	switch {
	case row.Err != nil:
		row.State = Unread
	case len(row.Values) == 0:
		row.State = Absent
	}
	return row
}

// row reads and judges one setting.
func (r Reading) row(ctx context.Context, sys host.System, s Spec, after []assignment) Row {
	row := Row{Spec: s}
	row.Values, row.Err = read(ctx, sys, r.keys(s))
	switch {
	case row.Err != nil:
		row.State = Unread
		return row
	case len(row.Values) == 0:
		row.State = Absent
		return row
	}
	low := func(v Value) bool { return !s.Passes(v.Key, v.N) }
	if s.Kind == RPFilter {
		// The kernel uses the higher of "all" and the interface's own.
		all := ReadValue(ctx, sys, confKey(s.Family, confAll, s.Leaf))
		if all.Err != nil && !errors.Is(all.Err, fs.ErrNotExist) {
			row.State, row.Err = Unread, all.Err
			return row
		}
		low = func(v Value) bool {
			_, name, _, _ := splitConf(v.Key)
			return !s.Passes(v.Key, v.N) && (name == confDefault || !s.Passes(all.Key, all.N))
		}
	}
	row.Shown = row.Values[0].N
	for _, v := range row.Values {
		if low(v) {
			row.Low = append(row.Low, v)
		}
	}
	if len(row.Low) > 0 {
		row.Shown = row.Low[0].N
	}
	switch {
	case r.Container:
		row.State, row.Low = NoVerdict, nil
	case s.Routed && r.LeftAlone():
		row.State, row.Low = LeftAlone, nil
	case len(row.Low) == 0:
	default:
		row.State = Below
		for _, v := range row.Low {
			if by := heldBy(after, v.Key, s, !r.OpenRC); by != "" {
				row.State, row.By = Held, by
				break
			}
		}
	}
	return row
}

// assignment is one "key = value" line of a file the host reads.
type assignment struct {
	file, key string
	n         int
}

// heldBy is the file whose line is the last, among the files read after
// hardn's, to set key, where it sets a value below the baseline; "" for
// none. globs: a "*" in a file's key stands for any name, as systemd and
// procps read it.
func heldBy(after []assignment, key string, s Spec, globs bool) string {
	by := ""
	for _, a := range after {
		same := a.key == key
		if !same && globs && strings.Contains(a.key, glob) {
			same, _ = path.Match(ProcPath(a.key), ProcPath(key))
		}
		if !same {
			continue
		}
		by = ""
		if !s.Passes(key, a.n) {
			by = a.file
		}
	}
	return by
}

// assignments reads a sysctl file's lines: "key = value", a "-" before the
// key allowed, comments and anything else left out. A key written with "/"
// is read as the same key with ".".
func assignments(name string, data []byte) []assignment {
	var out []assignment
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(line, "-"), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		k = strings.TrimSpace(k)
		if slash, dot := strings.IndexByte(k, '/'), strings.IndexByte(k, '.'); slash >= 0 && (dot < 0 || slash < dot) {
			k = strings.NewReplacer(".", "/", "/", ".").Replace(k)
		}
		out = append(out, assignment{file: name, key: k, n: n})
	}
	return out
}

// The directories of sysctl files, in systemd's order of precedence: a
// file hides one of the same name in a later directory (sysctl.d(5)).
var confDirs = []string{"/etc/sysctl.d", "/run/sysctl.d", "/usr/local/lib/sysctl.d", "/usr/lib/sysctl.d", "/lib/sysctl.d"}

// confFiles are the *.conf files of dir, by name; none where it is not
// there.
func confFiles(ctx context.Context, sys host.System, dir string) ([]string, error) {
	entries, err := sys.ReadDir(ctx, dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".conf") && !e.IsDir() {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names, nil
}

// later are the lines of the files the host reads after hardn's, in the
// order it reads them. Under systemd: every directory's files together, by
// name, the first directory's of a name alone. Under OpenRC (Alpine's
// sysctl service): /etc/sysctl.d's by name, then Conf, then
// /run/sysctl.d's.
func later(ctx context.Context, sys host.System, openRC bool) ([]assignment, error) {
	var files []string
	var errs []error
	list := func(dir string) []string {
		names, err := confFiles(ctx, sys, dir)
		if err != nil {
			errs = append(errs, err)
		}
		return names
	}
	if openRC {
		run := list("/run/sysctl.d")
		for _, n := range list("/etc/sysctl.d") {
			if n > base && !slices.Contains(run, n) {
				files = append(files, "/etc/sysctl.d/"+n)
			}
		}
		files = append(files, Conf)
		for _, n := range run {
			files = append(files, "/run/sysctl.d/"+n)
		}
	} else {
		from := map[string]string{}
		var names []string
		for _, dir := range confDirs {
			for _, n := range list(dir) {
				if _, seen := from[n]; !seen && n > base {
					from[n] = dir + "/" + n
					names = append(names, n)
				}
			}
		}
		slices.Sort(names)
		for _, n := range names {
			files = append(files, from[n])
		}
	}
	var out []assignment
	for _, name := range files {
		data, ok, err := host.ReadFileIfThere(ctx, sys, name)
		switch {
		case err != nil:
			errs = append(errs, err)
		case ok:
			out = append(out, assignments(name, data)...)
		}
	}
	return out, errors.Join(errs...)
}
