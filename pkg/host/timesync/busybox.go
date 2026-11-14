package timesync

import (
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/confd"
)

// busybox ntpd's settings on Alpine (gallery-timesync.md §3.4): the service
// runs "ntpd ${NTPD_OPTS:--N -p pool.ntp.org} -n", and ConfD sets
// NTPD_OPTS. There is no drop-in directory, so hardn's servers are the -p
// words of that one line, and a comment above it names the words they took
// the place of (Mark), which ClearPeers puts back.
const (
	ConfD = "/etc/conf.d/ntpd"
	// Mark opens the comment naming the words hardn's took the place of.
	Mark = "# Set by hardn (Time Sync), in place of: "
	// BusyboxService is the OpenRC service that runs ntpd.
	BusyboxService = "ntpd"

	optsKey = "NTPD_OPTS"
)

// peerArg are the characters of a peer hardn writes back: a name or an
// address, never an option.
var peerArg = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]*$`)

// peers are the words' -p arguments, each "-p PEER" or "-pPEER", and the
// indexes of the words they take. ok is false where a -p has no argument
// or one hardn does not write back.
func peers(words []string) (args []string, at []int, ok bool) {
	for i := 0; i < len(words); i++ {
		arg := ""
		switch w := words[i]; {
		case w == "-p" && i+1 < len(words):
			arg, at = words[i+1], append(at, i, i+1)
			i++
		case w == "-p":
			return nil, nil, false
		case strings.HasPrefix(w, "-p"):
			arg, at = w[2:], append(at, i)
		default:
			continue
		}
		if !peerArg.MatchString(arg) {
			return nil, nil, false
		}
		args = append(args, arg)
	}
	return args, at, true
}

// parseOpts reads ConfD's bytes (confd.Parse) and its peers, which must be
// side by side: hardn's words go where they are, and the ones they replace
// go back there. NTPD_OPTS that cannot be read, a peer hardn does not write
// back, no peer at all (ntpd then reads /etc/ntp.conf, which hardn does
// not), or peers with other words between them are ErrMalformed.
func parseOpts(data []byte) (l confd.Line, args []string, first int, err error) {
	l, fault := confd.Parse(data, optsKey, Mark)
	if fault != nil {
		return l, nil, 0, bad(ConfD, fault.Line, fault.What)
	}
	args, at, ok := peers(l.Words)
	switch {
	case !ok:
		return l, nil, 0, bad(ConfD, l.N, "a -p hardn does not write back")
	case len(at) == 0:
		return l, nil, 0, bad(ConfD, l.N, "no -p: ntpd's servers are not in "+optsKey)
	case at[len(at)-1]-at[0] != len(at)-1:
		return l, nil, 0, bad(ConfD, l.N, "other options between the -p ones")
	}
	return l, args, at[0], nil
}

// readBusybox reads ntpd's servers from ConfD: each -p of NTPD_OPTS. With
// hardn's mark they are hardn's, and the mark's are the ones to put back.
// A file that is not there is the error: the service then runs with its
// built-in options, which hardn has no line to set in.
func (r *Reading) readBusybox(ctx context.Context, sys host.System) error {
	data, ok, err := readFile(ctx, sys, ConfD)
	switch {
	case err != nil:
		return err
	case !ok:
		return bad(ConfD, 0, fs.ErrNotExist.Error())
	}
	l, args, _, err := parseOpts(data)
	if err != nil {
		return err
	}
	from := FromFile
	if l.HasMark() {
		was, at, ok := peers(l.Marked)
		if !ok || len(at) != len(l.Marked) || len(was) == 0 {
			return bad(ConfD, l.N, "a mark hardn does not write")
		}
		from, r.File, r.Mine = FromHardn, ConfD, true
		for _, a := range was {
			r.Restores = append(r.Restores, Server{Name: a, From: FromFile, File: ConfD})
		}
	}
	for _, a := range args {
		r.Servers = append(r.Servers, Server{Name: a, From: from, File: ConfD})
	}
	return nil
}

// SetPeers is ConfD's bytes with servers as ntpd's peers: a "-p" each, in
// place of the ones there, and Mark above the line naming those. What
// SetPeers did before is undone first (ClearPeers), so a second set
// replaces the first. A file that is not written as hardn writes it back,
// one assignment in double quotes with single spaces, is ErrMalformed:
// ClearPeers could not restore it byte for byte. servers are checked
// (ErrServer).
func SetPeers(data []byte, servers []string) ([]byte, error) {
	if err := check(servers); err != nil {
		return nil, err
	}
	l0, _, _, err := parseOpts(data)
	if err != nil {
		return nil, err
	}
	if l0.HasMark() {
		if data, err = ClearPeers(data); err != nil {
			return nil, err
		}
	}
	l, _, first, err := parseOpts(data)
	if err != nil {
		return nil, err
	}
	if string(l.Bytes(l.Words, nil)) != string(data) {
		return nil, bad(ConfD, l.N, optsKey+" is not written as hardn writes it back")
	}
	_, at, _ := peers(l.Words)
	was := slices.Clone(l.Words[first : first+len(at)])
	var mine []string
	for _, s := range servers {
		mine = append(mine, "-p", s)
	}
	return l.Bytes(slices.Concat(l.Words[:first], mine, l.Words[first+len(at):]), was), nil
}

// ClearPeers is ConfD's bytes without what SetPeers did: the mark's line
// gone, and the words it names back where hardn's peers are. No mark is
// ErrNotOwn; a mark that names anything but peers is ErrMalformed: hardn
// puts back nothing it did not write there.
func ClearPeers(data []byte) ([]byte, error) {
	l, _, first, err := parseOpts(data)
	if err != nil {
		return nil, err
	}
	if !l.HasMark() {
		return nil, fmtNotOwn()
	}
	was, wasAt, ok := peers(l.Marked)
	if !ok || len(wasAt) != len(l.Marked) || len(was) == 0 {
		return nil, bad(ConfD, l.N, "a mark hardn does not write")
	}
	_, at, _ := peers(l.Words)
	return l.Bytes(slices.Concat(l.Words[:first], l.Marked, l.Words[first+len(at):]), nil), nil
}

// fmtNotOwn is ErrNotOwn naming ConfD.
func fmtNotOwn() error { return fmt.Errorf("timesync: %s: %w", ConfD, ErrNotOwn) }
