package logfwd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// busybox syslogd's settings on Alpine (gallery-logforward.md §3.4): the
// service runs "syslogd ${SYSLOGD_OPTS} -n", and ConfD sets SYSLOGD_OPTS.
// There is no drop-in directory, so hardn's forwarding is words in that one
// line, and a comment above it naming them (Mark), by which Read tells them
// hardn's and ClearOpts takes exactly them back out.
const (
	ConfD = "/etc/conf.d/syslog"
	// Mark opens the comment naming the words hardn added.
	Mark = "# Added by hardn (Log Forwarding): "
	// BusyboxService is the OpenRC service that runs syslogd.
	BusyboxService = "syslog"

	optsKey = "SYSLOGD_OPTS"
)

var (
	// optsLine is the assignment, its value in double quotes, single quotes
	// or bare.
	optsLine = regexp.MustCompile(`^\s*` + optsKey + `=(?:"([^"]*)"|'([^']*)'|(\S*))\s*$`)
	// optsValue are the characters a value hardn reads may hold: options,
	// paths, hosts and ports. Anything the shell would expand or split on
	// other than a space ("$", a backquote, a backslash, a quote, ";") is
	// ErrMalformed: hardn does not write a line back that it cannot read
	// exactly.
	optsValue = regexp.MustCompile(`^[A-Za-z0-9 ._:/@=,+\[\]-]*$`)
)

// opts is ConfD as read: its lines, where SYSLOGD_OPTS is set, its words,
// and hardn's mark.
type opts struct {
	lines []string
	at    int      // the assignment's index in lines
	words []string // the value's words
	mark  int      // the mark's index in lines; -1 for none
	added []string // the words the mark names
}

// parseOpts reads ConfD's bytes. SYSLOGD_OPTS set on no line, on two, or
// to a value outside optsValue, and two marks, are ErrMalformed.
func parseOpts(data []byte) (opts, error) {
	o := opts{lines: strings.Split(string(data), "\n"), at: -1, mark: -1}
	bad := func(line int, what string) (opts, error) {
		return opts{}, fmt.Errorf("logfwd: %s:%d: %s: %w", ConfD, line, what, ErrMalformed)
	}
	for i, l := range o.lines {
		if rest, ok := strings.CutPrefix(l, Mark); ok {
			if o.mark >= 0 {
				return bad(i+1, "hardn's mark, twice")
			}
			o.mark, o.added = i, strings.Fields(rest)
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(l), optsKey+"=") {
			continue
		}
		m := optsLine.FindStringSubmatch(l)
		switch {
		case m == nil:
			return bad(i+1, optsKey+" in a form hardn does not read")
		case o.at >= 0:
			return bad(i+1, optsKey+" set twice")
		}
		v := m[1] + m[2] + m[3]
		if !optsValue.MatchString(v) {
			return bad(i+1, optsKey+" holds a character hardn does not write back")
		}
		o.at, o.words = i, strings.Fields(v)
	}
	if o.at < 0 {
		return bad(len(o.lines), optsKey+" is not set")
	}
	return o, nil
}

// remotes are the words' -R arguments, each "-R HOST[:PORT]" or
// "-RHOST[:PORT]", with the index of its first word and how many words it
// takes.
func (o opts) remotes() (args []string, at, n []int) {
	for i := 0; i < len(o.words); i++ {
		switch w := o.words[i]; {
		case w == "-R" && i+1 < len(o.words):
			args, at, n = append(args, o.words[i+1]), append(at, i), append(n, 2)
			i++
		case strings.HasPrefix(w, "-R") && len(w) > 2:
			args, at, n = append(args, w[2:]), append(at, i), append(n, 1)
		}
	}
	return args, at, n
}

// own are the indexes, in words, of the words the mark names, in its order.
// "-R" and its argument are matched as a pair, two words side by side, so
// that another "-R" on the line is never taken for hardn's; any other word
// is the first such word after the one before. ok is false where one is not
// there, the line edited since.
func (o opts) own() (idx []int, ok bool) {
	from := 0
	for k := 0; k < len(o.added); k++ {
		w := o.added[k]
		if w != "-R" {
			i := slices.Index(o.words[from:], w)
			if i < 0 {
				return nil, false
			}
			idx = append(idx, from+i)
			from += i + 1
			continue
		}
		if k+1 == len(o.added) {
			return nil, false // a mark hardn does not write
		}
		k++
		i := o.pair(from, o.added[k])
		if i < 0 {
			return nil, false
		}
		idx = append(idx, i, i+1)
		from = i + 2
	}
	return idx, len(idx) > 0
}

// pair is the index, from from on, of the "-R" whose argument, the next
// word, is arg; -1 for none. The words are walked as syslogd reads them
// (remotes), so the argument of one "-R" is never taken for another's flag.
func (o opts) pair(from int, arg string) int {
	args, at, n := o.remotes()
	for i, a := range args {
		if at[i] >= from && n[i] == 2 && a == arg {
			return at[i]
		}
	}
	return -1
}

// bytes is the file with words as SYSLOGD_OPTS's value, in double quotes.
func (o opts) bytes(words []string) []byte {
	lines := slices.Clone(o.lines)
	lines[o.at] = optsKey + `="` + strings.Join(words, " ") + `"`
	return []byte(strings.Join(lines, "\n"))
}

// readBusybox reads syslogd's forwarding from ConfD: each -R of
// SYSLOGD_OPTS, over UDP, port 514 where it names none. A file that is not
// there, or cannot be made out (parseOpts), is the error.
func readBusybox(ctx context.Context, sys host.System) ([]Forward, error) {
	data, ok, err := readFile(ctx, sys, ConfD)
	switch {
	case err != nil:
		return nil, err
	case !ok:
		return nil, fmt.Errorf("logfwd: %s: %w", ConfD, fs.ErrNotExist)
	}
	o, err := parseOpts(data)
	if err != nil {
		return nil, err
	}
	mine, _ := o.own()
	args, at, _ := o.remotes()
	var out []Forward
	for i, a := range args {
		s := legacyHostPort(a)
		if s.Address == "" {
			return nil, fmt.Errorf("logfwd: %s:%d: -R with no host: %w", ConfD, o.at+1, ErrMalformed)
		}
		out = append(out, Forward{Server: s, File: ConfD, Line: o.at + 1, ByHardn: slices.Contains(mine, at[i])})
	}
	return out, nil
}

// legacyHostPort reads "HOST[:PORT]" over UDP, port 514 where none.
func legacyHostPort(arg string) Server {
	s, _ := legacyServer("@"+arg, false)
	return s
}

// SetOpts is ConfD's bytes with s as syslogd's server: "-R address:port",
// and "-L", which keeps the local log, where the options have none, added
// to SYSLOGD_OPTS, and Mark above it naming what was added. Words hardn
// added before are taken out first (ClearOpts), so a second server replaces
// the first. s is checked (Server.Check) and must be UDP, all syslogd
// sends; a -R that is not hardn's is ErrForeign.
func SetOpts(data []byte, s Server) ([]byte, error) {
	if err := s.Check(); err != nil {
		return nil, err
	}
	if s.Proto != UDP {
		return nil, fmt.Errorf("logfwd: busybox syslogd sends UDP alone: %w", ErrServer)
	}
	o, err := parseOpts(data)
	if err != nil {
		return nil, err
	}
	if o.mark >= 0 {
		cleared, err := ClearOpts(data)
		if err != nil {
			return nil, err
		}
		if o, err = parseOpts(cleared); err != nil {
			return nil, err
		}
	}
	if args, _, _ := o.remotes(); len(args) > 0 {
		return nil, fmt.Errorf("logfwd: %s: -R %s: %w", ConfD, args[0], ErrForeign)
	}
	added := []string{"-R", s.Address + ":" + s.Port}
	if !slices.Contains(o.words, "-L") {
		added = append(added, "-L")
	}
	out := opts{lines: slices.Insert(slices.Clone(o.lines), o.at, Mark+strings.Join(added, " ")), at: o.at + 1}
	return out.bytes(append(slices.Clone(o.words), added...)), nil
}

// ErrForeign: syslogd already forwards by words hardn did not add.
var ErrForeign = errors.New("forwarding set outside hardn")

// ErrNotOwn: nothing in ConfD is hardn's to take out: no mark, or its words
// are not all in SYSLOGD_OPTS any more.
var ErrNotOwn = errors.New("no forwarding of hardn's own")

// ClearOpts is ConfD's bytes without what SetOpts added: the mark's line,
// and exactly the words it names. No mark, or a word of it gone from the
// options, is ErrNotOwn: hardn takes out nothing it cannot identify.
func ClearOpts(data []byte) ([]byte, error) {
	o, err := parseOpts(data)
	if err != nil {
		return nil, err
	}
	mine, ok := o.own()
	if o.mark < 0 || !ok {
		return nil, fmt.Errorf("logfwd: %s: %w", ConfD, ErrNotOwn)
	}
	var words []string
	for i, w := range o.words {
		if !slices.Contains(mine, i) {
			words = append(words, w)
		}
	}
	out := o.bytes(words)
	lines := strings.Split(string(out), "\n")
	return []byte(strings.Join(slices.Delete(lines, o.mark, o.mark+1), "\n")), nil
}
