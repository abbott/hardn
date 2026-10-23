package dns

import (
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
)

// lines splits data into lines without their newlines.
func lines(data []byte) []string {
	s := strings.TrimSuffix(string(data), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func join(ls []string) []byte {
	if len(ls) == 0 {
		return nil
	}
	return []byte(strings.Join(ls, "\n") + "\n")
}

// keyword reports whether line is a resolv.conf keyword line as glibc's
// res_init matches one: the keyword at the start, then a space or a tab.
func keyword(line, kw string) bool {
	return strings.HasPrefix(line, kw) && len(line) > len(kw) && (line[len(kw)] == ' ' || line[len(kw)] == '\t')
}

// ParseResolvConf returns the nameservers and the search list a resolver
// reads from a resolv.conf: every nameserver line's address, and the last
// search or domain line ("domain" holds one name).
func ParseResolvConf(data []byte) (nameservers, search []string) {
	for _, l := range lines(data) {
		f := strings.Fields(l)
		switch {
		case keyword(l, "nameserver") && len(f) > 1:
			nameservers = append(nameservers, f[1])
		case keyword(l, "search") && len(f) > 1:
			search = f[1:]
		case keyword(l, "domain") && len(f) > 1:
			search = f[1:2]
		}
	}
	return nameservers, search
}

// renderResolvConf is old with its nameserver lines replaced by c's, where
// the first one was (else at the end), and, with c.ReplaceSearch, its
// search and domain lines replaced by one search line (none for an empty
// list). Every other line, comments and options included, is kept.
func renderResolvConf(old []byte, c Config) []byte {
	var ns, out []string
	for _, a := range c.Nameservers {
		ns = append(ns, "nameserver "+a)
	}
	search := ""
	if c.ReplaceSearch && len(c.Search) > 0 {
		search = searchLine(c.Search)
	}
	nsDone, searchDone := false, !c.ReplaceSearch
	for _, l := range lines(old) {
		switch {
		case keyword(l, "nameserver"):
			if !nsDone {
				out, nsDone = append(out, ns...), true
			}
		case !c.ReplaceSearch || !keyword(l, "search") && !keyword(l, "domain"):
			out = append(out, l)
		case !searchDone:
			if search != "" {
				out = append(out, search)
			}
			searchDone = true
		}
	}
	if !searchDone && search != "" {
		out = append(out, search)
	}
	if !nsDone {
		out = append(out, ns...)
	}
	return join(out)
}

// renderDropIn is hardn's resolved.conf.d drop-in for c. List settings
// collect across files, and an empty assignment resets the list, so each
// list starts with one: the result is exactly c's, whatever earlier files
// set. Without c.ReplaceSearch, the Domains= assignments of the drop-in
// hardn wrote before (old) are kept.
func renderDropIn(old Entry, c Config) []byte {
	out := []string{
		"# Written by hardn: the system DNS servers for systemd-resolved.",
		"# An empty assignment resets the list that earlier files built.",
		"[Resolve]",
		"DNS=",
		"DNS=" + strings.Join(c.Nameservers, " "),
	}
	switch {
	case c.ReplaceSearch:
		out = append(out, "Domains=")
		if len(c.Search) > 0 {
			out = append(out, "Domains="+strings.Join(c.Search, " "))
		}
	case old.Kind == EntryFile:
		for _, a := range confAssignments(old.Data) {
			if a.key == "Domains" {
				out = append(out, "Domains="+a.value)
			}
		}
	}
	return join(out)
}

// confSpace is systemd's WHITESPACE.
const confSpace = " \t\n\r"

// confAssignment is one key=value of a systemd configuration file.
type confAssignment struct{ key, value string }

// confAssignments reads data as systemd's conf-parser does (v257
// src/shared/conf-parser.c, config_parse and parse_line): a line whose first
// non-blank character is # or ; is a comment, even within a continuation; a
// line ending in an odd number of backslashes continues on the next, that
// backslash read as a space; a section header is skipped; an assignment is
// split at its first "=", and the key and the value are stripped of white
// space. A line with no "=" or no key is skipped, as systemd skips it with
// a warning. Keys are case-sensitive (resolved-gperf.gperf has no
// %ignore-case). Assignments in any section are returned, so a caller that
// refuses on one fails closed.
func confAssignments(data []byte) []confAssignment {
	var out []confAssignment
	parse := func(l string) {
		l = strings.Trim(l, confSpace)
		if l == "" || l[0] == '[' {
			return
		}
		k, v, ok := strings.Cut(l, "=")
		if k = strings.Trim(k, confSpace); ok && k != "" {
			out = append(out, confAssignment{k, strings.Trim(v, confSpace)})
		}
	}
	cont := ""
	for _, l := range lines(data) {
		if t := strings.TrimLeft(l, confSpace); t != "" && (t[0] == '#' || t[0] == ';') {
			continue
		}
		p := cont + l
		if n := len(p) - len(strings.TrimRight(p, "\\")); n%2 == 1 {
			cont = p[:len(p)-1] + " "
			continue
		}
		cont = ""
		parse(p)
	}
	if cont != "" {
		parse(cont)
	}
	return out
}

// resolvedSets reports whether a resolved.conf drop-in assigns DNS= and
// whether it assigns Domains=, which would add to or reset hardn's lists
// after it.
func resolvedSets(data []byte) (dnsSet, domains bool) {
	for _, a := range confAssignments(data) {
		switch a.key {
		case "DNS":
			dnsSet = true
		case "Domains":
			domains = true
		}
	}
	return dnsSet, domains
}

// assignment is a line busybox sh reads as one plain assignment: a name,
// then an unquoted word with nothing the shell expands or acts on, or a
// single- or double-quoted string without $, ` or \.
var assignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=([^\s'"$` + "`" + `\\;&|<>()]*|"[^"$` + "`" + `\\]*"|'[^']*')$`)

// udhcpc reads udhcpc.conf as default.script sources it, returning the
// value RESOLV_CONF ends with (set false when no line sets it) and NO_DNS.
// A line that is not blank, a comment or a plain assignment makes the
// result unknown: the shell could do anything there.
func udhcpc(data []byte) (resolv string, set bool, noDNS string, err error) {
	for i, l := range lines(data) {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		m := assignment.FindStringSubmatch(t)
		if m == nil {
			return "", false, "", fmt.Errorf("%s:%d: not a plain assignment: %w", UDHCPCConf, i+1, detect.ErrUnrecognised)
		}
		v := m[2]
		if v != "" && (v[0] == '"' || v[0] == '\'') {
			v = v[1 : len(v)-1] // the pattern holds one quoted string
		}
		switch m[1] {
		case "RESOLV_CONF":
			resolv, set = v, true
		case "NO_DNS":
			noDNS = v
		}
	}
	return resolv, set, noDNS, nil
}

// udhcpcRewrites is whether udhcpc's script rewrites /etc/resolv.conf for
// a RESOLV_CONF it ends with: it skips the file for "no", "NO" or an empty
// value, and writes another file for another absolute path.
func udhcpcRewrites(resolv string, set bool) detect.Tri {
	switch {
	case !set || path.Clean(resolv) == ResolvConf:
		return detect.Yes
	case resolv == "no" || resolv == "NO" || resolv == "":
		return detect.No
	case path.IsAbs(resolv):
		return detect.No
	}
	return detect.Unknown
}

// renderUDHCPC is old with RESOLV_CONF="no": its first RESOLV_CONF line
// replaced, later ones dropped, or the line appended.
func renderUDHCPC(old []byte) []byte {
	const set = `RESOLV_CONF="no"`
	var out []string
	done := false
	for _, l := range lines(old) {
		if m := assignment.FindStringSubmatch(strings.TrimSpace(l)); m != nil && m[1] == "RESOLV_CONF" {
			if !done {
				out, done = append(out, set), true
			}
			continue
		}
		out = append(out, l)
	}
	if !done {
		out = append(out, "# hardn: keep udhcpc from rewriting /etc/resolv.conf", set)
	}
	return join(out)
}

// Link is one interface's DNS servers and search domains as
// systemd-resolved reports them, or a delegate's (systemd 259 and later;
// Index 0, Name its id).
type Link struct {
	Index   int
	Name    string
	Servers []string
	// Domains are its search domains, from resolvectl domain ("~" for
	// route-only); read for the status and the screen, never written.
	Domains []string `json:",omitempty"`
}

func (l Link) String() string {
	if l.Index == 0 {
		return "delegate " + l.Name
	}
	return fmt.Sprintf("link %d (%s)", l.Index, l.Name)
}

// parseResolvectl reads `resolvectl --no-pager dns`: a "Global:" line, then
// a "Delegate <id>:" line per delegate (systemd 259), then a "Link <index>
// (<name>):" line per link, each followed by its servers; a line starting
// with a space continues the one before (resolvectl wraps at the terminal
// width).
func parseResolvectl(out []byte) (global []string, links []Link, err error) {
	var cur *[]string
	seen := false
	for _, l := range lines(out) {
		switch {
		case strings.TrimSpace(l) == "":
		case strings.HasPrefix(l, "Global:"):
			global, seen = strings.Fields(l[len("Global:"):]), true
			cur = &global
		case strings.HasPrefix(l, "Delegate "):
			id, rest, ok := strings.Cut(l[len("Delegate "):], ":")
			if !ok || id == "" || strings.ContainsAny(id, " \t") {
				return nil, nil, fmt.Errorf("resolvectl: %q: %w", l, detect.ErrUnrecognised)
			}
			links = append(links, Link{Name: id, Servers: strings.Fields(rest)})
			cur = &links[len(links)-1].Servers
		case strings.HasPrefix(l, "Link "):
			head, rest, ok := strings.Cut(l[len("Link "):], "):")
			idx, name, ok2 := strings.Cut(head, " (")
			n, nerr := strconv.Atoi(idx)
			if !ok || !ok2 || nerr != nil {
				return nil, nil, fmt.Errorf("resolvectl: %q: %w", l, detect.ErrUnrecognised)
			}
			links = append(links, Link{Index: n, Name: name, Servers: strings.Fields(rest)})
			cur = &links[len(links)-1].Servers
		case (l[0] == ' ' || l[0] == '\t') && cur != nil:
			*cur = append(*cur, strings.Fields(l)...)
		default:
			return nil, nil, fmt.Errorf("resolvectl: %q: %w", l, detect.ErrUnrecognised)
		}
	}
	if !seen {
		return nil, nil, fmt.Errorf("resolvectl: no Global line: %w", detect.ErrUnrecognised)
	}
	return global, links, nil
}
