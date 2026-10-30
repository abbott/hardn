package details

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// lslogins' sources: the order it asks is wtmp, lastlog2's database where it is
// built with it, then lastlog (lslogins.c v2.41:827, :601). Both VMs' builds
// link lastlog2 (vm-runs/s1/08-lslogins.log), and neither has the database
// (vm-runs/s1/01-tools.log:24, :91).
const (
	WtmpFile     = "/var/log/wtmp"
	LastlogFile  = "/var/log/lastlog"
	Lastlog2File = "/var/lib/lastlog/lastlog2.db"
)

// lsloginsArgs is decision 3 (a)'s one call, but for its names.
var lsloginsArgs = []string{"--time-format=iso", "--noheadings", "--raw", "--output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME", "-l"}

// LoginsState is what became of the one lslogins call.
type LoginsState uint8

const (
	// LoginsUnread: it failed, it hit its bound, or a line did not parse (the
	// section's Unread says which). Never no login. The zero value.
	LoginsUnread LoginsState = iota
	// LoginsNoTool: lslogins is not installed, as on Alpine 3.23
	// (vm-runs/s1/01-tools.log).
	LoginsNoTool
	// LoginsRead: it answered, and List holds a Login for each account asked.
	LoginsRead
)

// LoginState is what the answer says of one account.
type LoginState uint8

const (
	// LoginUnknown: nothing is known of the account: lslogins had no line for
	// it, or a blank where no source shows it could have had one. The zero value.
	LoginUnknown LoginState = iota
	// LoginNone: a blank, where a source can be read and holds records.
	LoginNone
	// LoginAt: its newest login with a terminal.
	LoginAt
)

// Logins is the Last Login section: root, then each listed user.
type Logins struct {
	State LoginsState
	List  []Login // nil unless LoginsRead
	// Sources is what the blank guard found, in lslogins' order: nil where no
	// account answered blank, so nothing asked.
	Sources []Source
	Unread  []Unread
}

// Login is one account's answer. When is as lslogins printed it, and At the same
// parsed; TTY and From are its other fields, From "" for a login with no remote
// host, as lslogins' raw form prints them (a control character or a blank in one
// as \x1b, \x20: libsmartcols, fputs_nonblank).
type Login struct {
	User      string
	State     LoginState
	When, TTY string
	From      string
	At        time.Time
}

// SourceState is what the blank guard found of one of lslogins' sources.
type SourceState uint8

const (
	// SourceUnread: it is there, and could not be read (Source.Err). The zero
	// value.
	SourceUnread SourceState = iota
	// SourceMissing: it is not there.
	SourceMissing
	// SourceNoRecords: it was read and holds nothing: no bytes, or only NUL
	// bytes as far as looked (recordsLook).
	SourceNoRecords
	// SourceRecords: it was read and holds records.
	SourceRecords
)

// Source is one of lslogins' sources, as the blank guard read it.
type Source struct {
	Path  string
	State SourceState
	Err   error
}

// readLogins is decision 3 (a): one call for root and each listed user, its
// lines keyed by name (vm-runs/s1/02-login.log, 08-lslogins.log). A name
// lslogins would split or print escaped cannot be asked (askable), so it reads
// as a name with no line does.
func readLogins(ctx context.Context, sys host.System, listed []User, limit time.Duration) Logins {
	names := []string{"root"}
	for _, u := range listed {
		names = append(names, u.Name)
	}
	var asked []string
	for _, n := range names {
		if askable(n) {
			asked = append(asked, n)
		}
	}
	res, err := sys.Run(ctx, "lslogins", slices.Concat(lsloginsArgs, []string{strings.Join(asked, ",")})...)
	switch {
	case errors.Is(err, host.ErrNotFound):
		return Logins{State: LoginsNoTool, Unread: []Unread{{SourceLslogins, err}}}
	case err != nil:
		return Logins{Unread: []Unread{{SourceLslogins, err}}}
	}
	read, err := parseLogins(asked, string(res.Stdout))
	if err != nil {
		return Logins{Unread: []Unread{{SourceLslogins, err}}}
	}
	s := Logins{State: LoginsRead, List: make([]Login, len(names))}
	blank := false
	for i, n := range names {
		s.List[i] = Login{User: n}
		if l, ok := read[n]; ok {
			s.List[i] = l
			blank = blank || l.State == LoginNone
		}
	}
	if !blank {
		return s
	}
	// lslogins answers blank, exit 0, when its sources are missing, empty or
	// unreadable (vm-runs/s1/09-lslogins-blank.log), so a blank is no login only
	// where a source can be read and holds records.
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	holds := false
	for _, p := range []string{WtmpFile, Lastlog2File, LastlogFile} {
		src := readSource(ctx, sys, p)
		s.Sources = append(s.Sources, src)
		holds = holds || src.State == SourceRecords
	}
	if !holds {
		for i := range s.List {
			if s.List[i].State == LoginNone {
				s.List[i].State = LoginUnknown
			}
		}
	}
	return s
}

// askable reports whether lslogins' -l takes name whole and its raw form prints
// it as it is: -l splits at a comma, and the raw form escapes a blank, a
// backslash and what does not print (libsmartcols, fputs_nonblank).
func askable(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == ',' || c == '\\' || c <= ' ' || c >= 0x7f {
			return false
		}
	}
	return name != ""
}

// parseLogins reads lslogins' lines, by name: any other line, or a second for a
// name, does not parse, so the whole answer is not trusted.
func parseLogins(names []string, out string) (map[string]Login, error) {
	read := map[string]Login{}
	if out == "" {
		return read, nil
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		l, err := parseLogin(line)
		if err != nil {
			return nil, err
		}
		if _, again := read[l.User]; again || !slices.Contains(names, l.User) {
			return nil, fmt.Errorf("details: lslogins: %q: %w", line, detect.ErrUnrecognised)
		}
		read[l.User] = l
	}
	return read, nil
}

// parseLogin reads one line: "alice 2026-10-20T11:42:05+00:00 pts/0 192.0.2.10"
// or, for no login, the name and three spaces (vm-runs/s1/02-login.log). Four
// fields at single spaces, so a blank one is empty. It says LoginNone of a
// blank; the guard takes that back where it must.
func parseLogin(line string) (Login, error) {
	f := strings.Split(line, " ")
	if len(f) != 4 || f[0] == "" {
		return Login{}, fmt.Errorf("details: lslogins: %q: %w", line, detect.ErrUnrecognised)
	}
	l := Login{User: f[0], When: f[1], TTY: f[2], From: f[3]}
	if l.When+l.TTY+l.From == "" {
		l.State = LoginNone
		return l, nil
	}
	at, err := time.Parse(time.RFC3339, l.When)
	if err != nil {
		return Login{}, fmt.Errorf("details: lslogins: %q: %w", line, detect.ErrUnrecognised)
	}
	l.State, l.At = LoginAt, at
	return l, nil
}

// recordsLook bounds how far a file with no bytes but NUL in its first MaxLine
// is read on: lastlog is sparse, a slot for each uid at uid times the slot's
// size (lslogins.c v2.41:605), 292 bytes on these builds [I: glibc's struct], so
// the VMs' 292292 bytes are 1001 slots, uid 1000's the last
// (vm-runs/s1/01-tools.log:29), and root's blank leaves its first MaxLine all
// NUL. So 4 MiB holds uids to about 14,000. A file longer than that with nothing
// in its first MaxLine reads as no records: the safe side, as it leaves a blank
// Unknown.
const recordsLook = 4 << 20

// readSource truly reads p, as a mode says nothing of who may read it: ScanLines
// opens it, refuses a symlink, a FIFO and a device without blocking, and looks
// no further than the first line with a byte in it. Each of its forms is binary,
// so a line is only a piece of it. A lastlog2 database holds a table once a login
// has written one (lastlog2.c v2.41:239, CREATE TABLE in write_entry): any bytes
// count as records [I, read; no VM has the file].
func readSource(ctx context.Context, sys host.System, p string) Source {
	s := Source{Path: p}
	holds, cut := false, false
	fi, err := sys.ScanLines(ctx, p, func(l host.Line) bool {
		holds = holds || hasData(l.Text)
		cut = l.Cut
		return !holds && !cut
	})
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.State = SourceMissing
		return s
	case err != nil:
		s.Err = err
		return s
	case holds:
		s.State = SourceRecords
		return s
	case !cut || fi.Size() > recordsLook:
		s.State = SourceNoRecords
		return s
	}
	data, _, err := sys.ReadFileNoFollow(ctx, p)
	if err != nil {
		s.Err = err
		return s
	}
	s.State = SourceNoRecords
	if hasData(data) {
		s.State = SourceRecords
	}
	return s
}

// hasData reports whether b holds a byte other than NUL.
func hasData(b []byte) bool { return len(bytes.Trim(b, "\x00")) > 0 }
