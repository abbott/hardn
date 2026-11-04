// Package aptconf says which file sets each apt configuration value (refactor
// plan slice 7, unit (b); gallery-updates.md §7 item 6). apt-config never
// says: it has no per-file readout, and neither `-o Dir::Etc::Parts=` nor
// APT_CONFIG keeps apt.conf.d out of its read (01-inspect-b-*.log, Debian 13
// and Ubuntu 26.04, 2026-11-03). So hardn reads the files itself, in apt's
// order, by apt.conf(5)'s syntax, and checks its effective values against
// apt-config dump's for the roots asked: any disagreement is
// detect.ErrUnrecognised, never a guess.
//
// Everything goes through host.System (depguard aptconf-through-host).
// docs/safety/aptconf.md gives the rules and their sources.
package aptconf

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// The files apt reads with APT_CONFIG unset: Dir::Etc::parts' files, then
// Dir::Etc::main where present, both under Dir "/" and Dir::Etc "etc/apt"
// (apt-config dump on the VMs). Read checks the host's Dir values are these.
const (
	PartsDir = "/etc/apt/apt.conf.d"
	MainFile = "/etc/apt/apt.conf"
)

// Item is one list item and the file that adds it.
type Item struct {
	Value, File string
}

// Config is the files' effective configuration under the roots Read was
// given. The zero value holds nothing.
type Config struct {
	roots []string // lower case
	files []parsed
	tree  *node
}

type parsed struct {
	name  string
	stmts []stmt
}

// stmt is one assignment (`A::B "v";`) or, item set, one list item of path.
type stmt struct {
	path  []string
	value string
	item  bool
}

// node is one configuration item, as apt-pkg's Configuration::Item: a tag
// (empty for a list item), a value, children in the order made.
type node struct {
	tag      string
	value    string
	file     string // the last file to set value; "" if none did
	children []*node
}

// Read reads the files apt reads and checks the values under roots (at least
// one, such as "APT::Periodic") against `apt-config dump <roots>`. A root
// apt itself gives a default under (most of "APT", "Dir") disagrees, and is
// refused.
func Read(ctx context.Context, sys host.System, roots ...string) (Config, error) {
	if len(roots) == 0 {
		return Config{}, fmt.Errorf("aptconf: no root: %w", fs.ErrInvalid)
	}
	c := Config{}
	for _, r := range roots {
		if _, err := split(r); err != nil {
			return Config{}, fmt.Errorf("aptconf: root %q: %w", r, fs.ErrInvalid)
		}
		c.roots = append(c.roots, strings.ToLower(r))
	}
	ignore, err := checkDirs(ctx, sys)
	if err != nil {
		return Config{}, err
	}
	names, err := partsFiles(ctx, sys, ignore)
	if err != nil {
		return Config{}, err
	}
	switch fi, err := sys.Stat(ctx, MainFile); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return Config{}, err
	case !fi.Mode().IsRegular():
		return Config{}, fmt.Errorf("%s: not a regular file: %w", MainFile, detect.ErrUnrecognised)
	default:
		names = append(names, MainFile)
	}
	for _, name := range names {
		data, err := sys.ReadFile(ctx, name)
		if err != nil {
			return Config{}, err
		}
		stmts, err := parse(name, string(data))
		if err != nil {
			return Config{}, err
		}
		c.files = append(c.files, parsed{name, stmts})
	}
	c.tree = build(c.files)
	if err := c.crossCheck(ctx, sys, roots); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Value is name's value and the file that sets it last; ok is false where
// no file sets it (an item only made as the parent of another is not set),
// and for a name under none of Read's roots, which is not checked. Names
// compare without case (apt.conf(5)).
func (c Config) Value(name string) (value, file string, ok bool) {
	n := c.lookup(name)
	if n == nil || n.file == "" {
		return "", "", false
	}
	return n.value, n.file, true
}

// Items is name's list items in apt's order, each with the file that adds
// it; nil under none of Read's roots.
func (c Config) Items(name string) []Item {
	n := c.lookup(name)
	if n == nil {
		return nil
	}
	var items []Item
	for _, ch := range n.children {
		if ch.tag == "" {
			items = append(items, Item{ch.value, ch.file})
		}
	}
	return items
}

// Files is the files read, in apt's order.
func (c Config) Files() []string {
	var names []string
	for _, f := range c.files {
		names = append(names, f.name)
	}
	return names
}

// Without is the effective configuration with file left out, the others
// read as they are: what the values would be were file removed. It is
// worked out, not checked against apt; a file not read changes nothing.
func (c Config) Without(file string) Config {
	files := slices.DeleteFunc(slices.Clone(c.files), func(f parsed) bool { return f.name == file })
	return Config{roots: c.roots, files: files, tree: build(files)}
}

func (c Config) lookup(name string) *node {
	segs, err := split(name)
	if err != nil || c.tree == nil {
		return nil
	}
	lower := strings.ToLower(name)
	if !slices.ContainsFunc(c.roots, func(r string) bool { return lower == r || strings.HasPrefix(lower, r+"::") }) {
		return nil
	}
	n := c.tree
	for _, s := range segs {
		if n = n.find(s); n == nil {
			return nil
		}
	}
	return n
}

func (n *node) find(tag string) *node {
	for _, ch := range n.children {
		if ch.tag != "" && strings.EqualFold(ch.tag, tag) {
			return ch
		}
	}
	return nil
}

// build applies the statements in order, as apt-pkg's Configuration::Set
// does: a name's items are found without case or made at the end of their
// parent's children, spelt as first written; a later value replaces an
// earlier; a list item is always a new child with an empty tag.
func build(files []parsed) *node {
	root := &node{}
	for _, f := range files {
		for _, st := range f.stmts {
			n := root
			for _, s := range st.path {
				ch := n.find(s)
				if ch == nil {
					ch = &node{tag: s}
					n.children = append(n.children, ch)
				}
				n = ch
			}
			if st.item {
				n.children = append(n.children, &node{value: st.value, file: f.name})
				continue
			}
			n.value, n.file = st.value, f.name
		}
	}
	return root
}

// dump renders the subtree at root as apt-config dump prints it by default
// (`%F "%v";`, empty values included): the item, then its children depth
// first, a list item named after its parent with "::" (02-inspect-c-*.log).
func (c Config) dump(root string) string {
	segs, _ := split(root)
	n, full := c.tree, ""
	for _, s := range segs {
		if n = n.find(s); n == nil {
			return ""
		}
		if full != "" {
			full += "::"
		}
		full += n.tag
	}
	var b strings.Builder
	var walk func(n *node, full string)
	walk = func(n *node, full string) {
		fmt.Fprintf(&b, "%s \"%s\";\n", full, n.value)
		for _, ch := range n.children {
			walk(ch, full+"::"+ch.tag)
		}
	}
	walk(n, full)
	return b.String()
}

// crossCheck compares the values under roots with apt-config dump's,
// byte for byte, names' spelling and order included.
func (c Config) crossCheck(ctx context.Context, sys host.System, roots []string) error {
	res, err := sys.Run(ctx, "apt-config", append([]string{"dump"}, roots...)...)
	if err != nil {
		return err
	}
	var want strings.Builder
	for _, r := range roots {
		want.WriteString(c.dump(r))
	}
	got, mine := strings.Split(string(res.Stdout), "\n"), strings.Split(want.String(), "\n")
	for i := 0; i < max(len(got), len(mine)); i++ {
		g, m := at(got, i), at(mine, i)
		if g != m {
			return fmt.Errorf("apt-config dump %s: line %d is %q, the files give %q: hardn can't tell which file sets what: %w",
				strings.Join(roots, " "), i+1, g, m, detect.ErrUnrecognised)
		}
	}
	return nil
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// checkDirs reads `apt-config dump Dir`: Dir, Dir::Etc, Dir::Etc::parts and
// Dir::Etc::main must name PartsDir and MainFile (the VMs' values, a
// trailing slash allowed on Dir::Etc), and Dir::Ignore-Files-Silently's
// items are the names apt skips without a word (02-inspect-c-*.log).
func checkDirs(ctx context.Context, sys host.System) ([]*regexp.Regexp, error) {
	res, err := sys.Run(ctx, "apt-config", "dump", "Dir")
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	var ignore []*regexp.Regexp
	for _, line := range strings.Split(strings.TrimSuffix(string(res.Stdout), "\n"), "\n") {
		name, rest, ok := strings.Cut(line, ` "`)
		value, closed := strings.CutSuffix(rest, `";`)
		if !ok || !closed || strings.ContainsAny(name, " \t\"") || strings.Contains(value, `"`) {
			return nil, fmt.Errorf("apt-config dump Dir: line %q: %w", line, detect.ErrUnrecognised)
		}
		name = strings.ToLower(name)
		if name == "dir::ignore-files-silently::" {
			re, err := regexp.Compile(value)
			if err != nil {
				return nil, fmt.Errorf("apt-config dump Dir: Dir::Ignore-Files-Silently %q: %w", value, detect.ErrUnrecognised)
			}
			ignore = append(ignore, re)
			continue
		}
		values[name] = value
	}
	for name, want := range map[string][]string{
		"dir": {"/"}, "dir::etc": {"etc/apt", "etc/apt/"},
		"dir::etc::parts": {"apt.conf.d"}, "dir::etc::main": {"apt.conf"},
	} {
		if v, ok := values[name]; !ok || !slices.Contains(want, v) {
			return nil, fmt.Errorf("apt-config dump Dir: %s is %q, not %q: %w", name, v, want[0], detect.ErrUnrecognised)
		}
	}
	return ignore, nil
}

// partsFiles is PartsDir's files in apt's order, byte order of their names.
// A name Dir::Ignore-Files-Silently matches is skipped. Any other is read
// where it is letters, digits, "_" and "-", with no extension or ".conf"
// (apt's run-parts-like rule; a dot elsewhere before ".conf" allowed); a name
// outside that, or an entry that is not a regular file through its
// symlinks, is refused: what apt does with it is [unverified].
func partsFiles(ctx context.Context, sys host.System, ignore []*regexp.Regexp) ([]string, error) {
	entries, err := sys.ReadDir(ctx, PartsDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	var files []string
	for _, name := range names {
		if slices.ContainsFunc(ignore, func(re *regexp.Regexp) bool { return re.MatchString(name) }) {
			continue
		}
		p := path.Join(PartsDir, name)
		if !validName(name) {
			return nil, fmt.Errorf("%s: a name apt's rules here do not cover: %w", p, detect.ErrUnrecognised)
		}
		fi, err := sys.Stat(ctx, p)
		if err != nil {
			return nil, err
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s: not a regular file: %w", p, detect.ErrUnrecognised)
		}
		files = append(files, p)
	}
	return files, nil
}

func validName(name string) bool {
	base, ext, dotted := cutLast(name, ".")
	if dotted && ext != "conf" {
		return false
	}
	return base != "" && strings.Trim(base, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.") == "" && base[0] != '.'
}

func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

// split is a name's tags: "::"-separated, none empty, none holding space,
// a quote, a brace, ";" or "#".
func split(name string) ([]string, error) {
	segs := strings.Split(name, "::")
	for _, s := range segs {
		if s == "" || strings.ContainsAny(s, " \t\r\n\"{};#") {
			return nil, fs.ErrInvalid
		}
	}
	return segs, nil
}
