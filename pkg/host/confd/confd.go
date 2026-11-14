// Package confd reads and rewrites one assignment of an OpenRC conf.d file
// (Alpine's /etc/conf.d/syslog, /etc/conf.d/ntpd): KEY="words" on one line,
// and a comment line, hardn's mark, naming words of hardn's
// (docs/engine/gallery-timesync.md §5). The file is read exactly or
// refused: hardn does not write a line back that it cannot read. What the
// mark's words mean is each caller's; so are the file's name and the error
// of a file that is refused, which Parse leaves for the caller to make
// (Fault).
package confd

import (
	"regexp"
	"slices"
	"strings"
)

// Fault is why Parse refuses a file: the line, counted from 1, and what is
// wrong there, in a few words that name the key. It is no error: the
// caller makes one, with the file's name and its own sentinel
// (logfwd.ErrMalformed).
type Fault struct {
	Line int
	What string
}

// value are the characters a value hardn reads may hold: options, paths,
// hosts and ports. Anything the shell would expand or split on other than a
// space ("$", a backquote, a backslash, a quote, ";") is refused.
var value = regexp.MustCompile(`^[A-Za-z0-9 ._:/@=,+\[\]-]*$`)

// Line is a file as Parse read it: where its key is set, the value's words,
// and hardn's mark.
type Line struct {
	// N is the assignment's line, counted from 1.
	N int
	// Words are the value's words.
	Words []string
	// Marked are the words the mark names; nil without a mark.
	Marked []string

	key, markText string
	lines         []string
	at, mark      int // indexes in lines; mark is -1 for none
}

// Parse reads a conf.d file's bytes: the one line that sets key, its value
// in double quotes, single quotes or bare, and the one line that opens with
// mark. key set on no line, on two, in another form, or to a value holding
// a character hardn does not write back, and two marks, are a *Fault; nil
// for a file that is read.
func Parse(data []byte, key, mark string) (Line, *Fault) {
	assign := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `=(?:"([^"]*)"|'([^']*)'|(\S*))\s*$`)
	l := Line{key: key, markText: mark, lines: strings.Split(string(data), "\n"), at: -1, mark: -1}
	bad := func(line int, what string) (Line, *Fault) { return Line{}, &Fault{Line: line, What: what} }
	for i, text := range l.lines {
		if rest, ok := strings.CutPrefix(text, mark); ok {
			if l.mark >= 0 {
				return bad(i+1, "hardn's mark, twice")
			}
			l.mark, l.Marked = i, strings.Fields(rest)
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(text), key+"=") {
			continue
		}
		m := assign.FindStringSubmatch(text)
		switch {
		case m == nil:
			return bad(i+1, key+" in a form hardn does not read")
		case l.at >= 0:
			return bad(i+1, key+" set twice")
		}
		v := m[1] + m[2] + m[3]
		if !value.MatchString(v) {
			return bad(i+1, key+" holds a character hardn does not write back")
		}
		l.at, l.Words = i, strings.Fields(v)
	}
	if l.at < 0 {
		return bad(len(l.lines), key+" is not set")
	}
	l.N = l.at + 1
	return l, nil
}

// HasMark reports whether the file has hardn's mark, whatever it names.
func (l Line) HasMark() bool { return l.mark >= 0 }

// Bytes is the file with words as the key's value, in double quotes, and
// the mark naming marked on the line above it; without the mark's line
// where marked is empty. Every other line is as it was read.
func (l Line) Bytes(words, marked []string) []byte {
	lines := slices.Clone(l.lines)
	lines[l.at] = l.key + `="` + strings.Join(words, " ") + `"`
	at := l.at
	if l.mark >= 0 {
		lines = slices.Delete(lines, l.mark, l.mark+1)
		if l.mark < at {
			at--
		}
	}
	if len(marked) > 0 {
		lines = slices.Insert(lines, at, l.markText+strings.Join(marked, " "))
	}
	return []byte(strings.Join(lines, "\n"))
}
