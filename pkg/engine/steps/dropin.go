package steps

import (
	"bytes"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
	"github.com/abbott/hardn/pkg/host/sshd"
	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// dropInHeader opens a 00-hardn.conf hardn writes from nothing, as
// migrate's opens one (brief §2: hardn owns this file alone; deleting it
// undoes hardn's SSH settings).
const dropInHeader = "# Managed by hardn.\n"

// setMarker labels the lines setDirectives writes: migrate's label for
// hardn's own settings, so a migrated file and an edited one read alike.
const setMarker = "# Set by hardn:"

// setDirectives returns old, hardn's 00-hardn.conf (nil when absent), with
// set in force, edited line by line, never regenerated
// (docs/safety/sshd-migrate.md §6): every line of a keyword set names is
// dropped, and set's lines come first, under setMarker, so they are global
// whatever follows. They go after old's header: its leading comments up to
// a blank line or to the first comment that labels the lines after it
// (one ending in ":", as migrate writes them), which stays with those
// lines. Every other line stays as it was, so the lines a migration moved
// there are kept (the AuthorizedKeysFile union that keeps ~/.ssh_<user>
// keys working, R8); an old setMarker is dropped, as it is written again.
// A Match or Include line in old is refused, as is an argument that would
// need quoting: either would make this simple reading differ from sshd's
// (fs.ErrInvalid). It is p2drive's edit (vmtest.md §2.4) with hardn's
// header and label.
func setDirectives(old []byte, set []migrate.Directive) ([]byte, error) {
	setting := map[string]bool{}
	var head []string
	for _, d := range set {
		line, err := render(d)
		if err != nil {
			return nil, err
		}
		setting[strings.ToLower(d.Keyword)] = true
		head = append(head, line)
	}
	lines := strings.SplitAfter(string(old), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var top, rest []string
	inTop := true
	for _, l := range lines {
		kw, text := keyword(l), strings.TrimSpace(l)
		switch {
		case kw == "match" || kw == "include":
			return nil, fmt.Errorf("%s line %q: hardn does not edit a file with Match or Include lines: %w", sshd.HardnDropIn, text, fs.ErrInvalid)
		case text == setMarker:
		case kw == "" && inTop && !strings.HasSuffix(text, ":"):
			top = append(top, withNewline(l))
			inTop = text != ""
		case setting[kw]:
			inTop = false
		default:
			inTop = false
			rest = append(rest, withNewline(l))
		}
	}
	if len(old) == 0 {
		top = []string{dropInHeader}
	}
	var b bytes.Buffer
	for _, l := range top {
		b.WriteString(l)
	}
	b.WriteString(setMarker + "\n")
	for _, l := range head {
		b.WriteString(l + "\n")
	}
	for _, l := range rest {
		b.WriteString(l)
	}
	return b.Bytes(), nil
}

// saysAlready reports whether old, 00-hardn.conf, has exactly set's lines
// for every keyword set names, in another layout than setDirectives
// writes: the same arguments in the same order, wherever they stand (a
// migrated file keeps them under its own label). Every line of the file is
// global (setDirectives refuses a Match), so sshd reads the same values
// from old as from the file setDirectives would write; writing it would be
// a lockout-risk change under a revert timer that changes nothing sshd
// reads.
func saysAlready(old []byte, set []migrate.Directive) bool {
	have := map[string][][]string{}
	for _, l := range sshd.ReadOrder([]byte(sshd.IncludeLine+"\n"), []sshd.File{{Path: sshd.HardnDropIn, Data: old}}) {
		if l.Match != nil {
			return false
		}
		have[l.Keyword] = append(have[l.Keyword], l.Args)
	}
	want := map[string][][]string{}
	for _, d := range set {
		k := sshd.CanonicalKeyword(d.Keyword, d.Args)
		want[k] = append(want[k], d.Args)
	}
	for k, w := range want {
		if !slices.EqualFunc(have[k], w, slices.Equal) {
			return false
		}
	}
	return true
}

// setFirstBy names the lines outside 00-hardn.conf that sshd reads first
// for a keyword of set whose first value wins (sshd_config(5)): ", set
// first by <file>:<line> "<text>"", or "" when there is none.
func setFirstBy(found migrate.Found, set []migrate.Directive) string {
	var at []string
	for _, d := range set {
		k := sshd.CanonicalKeyword(d.Keyword, d.Args)
		if sshd.Accumulates(k) {
			continue
		}
		for _, l := range sshd.ReadOrder(found.Main, found.DropIns) {
			if l.Keyword != k || l.Match != nil {
				continue
			}
			if l.File != sshd.HardnDropIn {
				at = append(at, fmt.Sprintf("%s:%d %q", l.File, l.Line, l.Text))
			}
			break
		}
	}
	if len(at) == 0 {
		return ""
	}
	return ", set first by " + strings.Join(slices.Compact(at), ", ")
}

// keyword is a line's keyword, lower case, as sshd takes the first word up
// to whitespace or "="; "" for a blank line or a comment.
func keyword(line string) string {
	t := strings.TrimLeft(line, " \t\r")
	if t == "" || t[0] == '#' || t[0] == '\n' {
		return ""
	}
	if i := strings.IndexAny(t, " \t=\r\n"); i >= 0 {
		t = t[:i]
	}
	return strings.ToLower(t)
}

func withNewline(l string) string {
	if strings.HasSuffix(l, "\n") {
		return l
	}
	return l + "\n"
}

// render is one directive's line. Its keyword must be a plain word and
// each argument free of whitespace, quotes, "#" and "=": arguments hardn's
// settings take (ports, yes and no, user names), which sshd reads back
// unquoted.
func render(d migrate.Directive) (string, error) {
	bad := func(s string) bool { return s == "" || strings.ContainsAny(s, " \t\r\n\"'#=\\\x00") }
	if bad(d.Keyword) || len(d.Args) == 0 || slices.ContainsFunc(d.Args, bad) {
		return "", fmt.Errorf("directive %q %q: %w", d.Keyword, d.Args, fs.ErrInvalid)
	}
	return d.Keyword + " " + strings.Join(d.Args, " "), nil
}

// keywordsOf are set's keywords, each once, as written: reload.Change's.
func keywordsOf(set []migrate.Directive) []string {
	var out []string
	for _, d := range set {
		if !slices.ContainsFunc(out, func(k string) bool { return strings.EqualFold(k, d.Keyword) }) {
			out = append(out, d.Keyword)
		}
	}
	return out
}

// pveSet is pve.PlanSSH over the whole of the 00-hardn.conf the change
// leaves, not only over set: the lines setDirectives keeps (a migrated
// AllowUsers, say) are global lines PlanSSH's rules read too (pve.md §6:
// every write goes through it). It returns set as PlanSSH has it, plus the
// lines of every kept keyword PlanSSH changed (root added to AllowUsers, a
// PermitRootLogin made prohibit-password) and the lines it added, so that
// setDirectives writes those in place of the old ones; every other kept
// line stays as it is. A refusal of PlanSSH's refuses the change. It is
// p2drive's (vmtest.md §2.4).
func pveSet(px detect.Proxmox, old []byte, set []migrate.Directive, groups pve.Groups) ([]migrate.Directive, []pve.Finding, error) {
	setting := map[string]bool{}
	for _, d := range set {
		setting[strings.ToLower(d.Keyword)] = true
	}
	var kept []migrate.Directive
	for _, l := range sshd.ReadOrder([]byte(sshd.IncludeLine+"\n"), []sshd.File{{Path: sshd.HardnDropIn, Data: old}}) {
		if kw := firstWord(l.Text); !setting[strings.ToLower(kw)] {
			kept = append(kept, migrate.Directive{Keyword: kw, Args: l.Args})
		}
	}
	full := append(slices.Clone(set), kept...)
	planned, findings, err := pve.PlanSSH(px, full, groups)
	if err != nil {
		return nil, nil, err
	}
	// PlanSSH edits its input in place and appends (its doc); anything else
	// cannot be mapped back to the file's lines, so it is refused.
	for i, d := range full {
		if i >= len(planned) || !strings.EqualFold(planned[i].Keyword, d.Keyword) {
			return nil, nil, fmt.Errorf("pve.PlanSSH did not return the directives in their order: %w", fs.ErrInvalid)
		}
	}
	out := slices.Clone(planned[:len(set)])
	changed := map[string]bool{}
	for i, k := range kept {
		if !slices.Equal(planned[len(set)+i].Args, k.Args) {
			changed[strings.ToLower(k.Keyword)] = true
		}
	}
	for i, k := range kept {
		if changed[strings.ToLower(k.Keyword)] {
			out = append(out, planned[len(set)+i])
		}
	}
	return append(out, planned[len(full):]...), findings, nil
}

// firstWord is a directive line's keyword as written, before whitespace
// or "=".
func firstWord(text string) string {
	t := strings.TrimSpace(text)
	if i := strings.IndexAny(t, " \t="); i >= 0 {
		return t[:i]
	}
	return t
}

// pinnedAt names the installed ListenAddress lines with a port of their
// own, which sshd listens on whatever the Port lines say: ", pinned by
// <file>:<line> "<text>"", or "" when there is none.
func pinnedAt(found migrate.Found) string {
	var at []string
	for _, l := range sshd.ReadOrder(found.Main, found.DropIns) {
		if l.Keyword == "listenaddress" && len(l.Args) > 0 && hasPort(l.Args[0]) {
			at = append(at, fmt.Sprintf("%s:%d %q", l.File, l.Line, l.Text))
		}
	}
	if len(at) == 0 {
		return ""
	}
	return ", pinned by " + strings.Join(at, ", ")
}

// hasPort reports whether a ListenAddress argument names its own port
// (sshd_config(5): host:port, IPv4:port, [host]:port); a bare IPv6
// address has colons but no port.
func hasPort(a string) bool {
	if strings.HasPrefix(a, "[") {
		return strings.Contains(a, "]:")
	}
	return strings.Count(a, ":") == 1
}
