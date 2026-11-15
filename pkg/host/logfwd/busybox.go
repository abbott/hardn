package logfwd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/confd"
)

// busybox syslogd's settings on Alpine (gallery-logforward.md §3.4): the
// service runs "syslogd ${SYSLOGD_OPTS} -n", and ConfD sets SYSLOGD_OPTS.
// There is no drop-in directory, so hardn's forwarding is words in that one
// line, and a comment above it naming them (Mark), by which Read tells them
// hardn's and ClearOpts takes exactly them back out.
const (
	ConfD = "/etc/conf.d/syslog"
	// Mark opens the comment naming the words hardn added.
	Mark = "# Added by hardn (Syslog Settings): "
	// BusyboxService is the OpenRC service that runs syslogd.
	BusyboxService = "syslog"

	optsKey = "SYSLOGD_OPTS"
)

// opts is ConfD as read (confd.Parse): where SYSLOGD_OPTS is set, its
// words, and the words hardn's mark names.
type opts struct{ confd.Line }

// parseOpts reads ConfD's bytes. SYSLOGD_OPTS set on no line, on two, or
// to a value hardn does not write back, and two marks, are ErrMalformed,
// naming the file and the line.
func parseOpts(data []byte) (opts, error) {
	l, bad := confd.Parse(data, optsKey, Mark)
	if bad != nil {
		return opts{}, fmt.Errorf("logfwd: %s:%d: %s: %w", ConfD, bad.Line, bad.What, ErrMalformed)
	}
	return opts{l}, nil
}

// remotes are the words' -R arguments, each "-R HOST[:PORT]" or
// "-RHOST[:PORT]", with the index of its first word and how many words it
// takes.
func (o opts) remotes() (args []string, at, n []int) {
	for i := 0; i < len(o.Words); i++ {
		switch w := o.Words[i]; {
		case w == "-R" && i+1 < len(o.Words):
			args, at, n = append(args, o.Words[i+1]), append(at, i), append(n, 2)
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
	for k := 0; k < len(o.Marked); k++ {
		w := o.Marked[k]
		if w != "-R" {
			i := slices.Index(o.Words[from:], w)
			if i < 0 {
				return nil, false
			}
			idx = append(idx, from+i)
			from += i + 1
			continue
		}
		if k+1 == len(o.Marked) {
			return nil, false // a mark hardn does not write
		}
		k++
		i := o.pair(from, o.Marked[k])
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
			return nil, fmt.Errorf("logfwd: %s:%d: -R with no host: %w", ConfD, o.N, ErrMalformed)
		}
		out = append(out, Forward{Server: s, File: ConfD, Line: o.N, ByHardn: slices.Contains(mine, at[i])})
	}
	return out, nil
}

// legacyHostPort reads "HOST[:PORT]" over UDP, port 514 where none.
func legacyHostPort(arg string) Server {
	s, _ := legacyServer("@"+arg, false)
	return s
}

// SetOpts is ConfD's bytes with servers as syslogd's servers: one
// "-R address:port" for each, in their order, and "-L", which keeps the
// local log, where the options have none, added to SYSLOGD_OPTS, and Mark
// above it naming what was added (design-syslog-servers.md §2.3). Words
// hardn added before are taken out first (ClearOpts), so a second list
// replaces the first. Each server is checked (Server.Check) and must be
// UDP, all syslogd sends: one that is not refuses the whole list. No
// server is ErrServer; a -R that is not hardn's is ErrForeign.
func SetOpts(data []byte, servers []Server) ([]byte, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("logfwd: no server: %w", ErrServer)
	}
	var added []string
	for _, s := range servers {
		if err := s.Check(); err != nil {
			return nil, err
		}
		if s.Proto != UDP {
			return nil, fmt.Errorf("logfwd: busybox syslogd sends UDP alone: %w", ErrServer)
		}
		added = append(added, "-R", s.Address+":"+s.Port)
	}
	o, err := parseOpts(data)
	if err != nil {
		return nil, err
	}
	if o.HasMark() {
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
	if !slices.Contains(o.Words, "-L") {
		added = append(added, "-L")
	}
	return o.Bytes(append(slices.Clone(o.Words), added...), added), nil
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
	if !ok { // no mark, or one whose words are not all there
		return nil, fmt.Errorf("logfwd: %s: %w", ConfD, ErrNotOwn)
	}
	var words []string
	for i, w := range o.Words {
		if !slices.Contains(mine, i) {
			words = append(words, w)
		}
	}
	return o.Bytes(words, nil), nil
}
