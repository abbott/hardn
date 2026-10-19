package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host/sshd/migrate"
)

// dropInHeader opens a 00-hardn.conf p2drive writes from nothing.
const dropInHeader = "# hardn's SSH settings (brief §2: hardn owns this file alone).\n# Written by p2drive, the dev-only driver of refactor plan P2; delete it to undo them.\n"

// setMarker labels the lines setDirectives writes, as migrate labels each
// run of lines it moves ("# From <file>:", "# Set by hardn:").
const setMarker = "# Set by p2drive:"

// setDirectives returns old, hardn's 00-hardn.conf (nil when absent), with
// set in force: every line of a keyword set names is dropped, and set's
// lines come first, under setMarker, so they are global whatever follows.
// They go after old's header: its leading comments up to a blank line or
// to the first comment that labels the lines after it (one ending in ":",
// as migrate writes them), which stays with those lines. Every other line
// stays as it was, so the lines a migration moved there are kept
// (docs/safety/sshd-migrate.md §6: edit the file line by line, never
// regenerate it); an old setMarker is dropped, as it is written again. A
// Match or Include line in old is refused, as is an argument that would
// need quoting: either would make this simple reading differ from sshd's
// (fs.ErrInvalid).
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
			return nil, fmt.Errorf("%s line %q: p2drive does not edit a file with Match or Include lines: %w", "00-hardn.conf", text, fs.ErrInvalid)
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
