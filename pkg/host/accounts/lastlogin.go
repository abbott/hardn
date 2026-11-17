package accounts

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

// The last-login reader that System Details' Last Login section and the User
// Management screen share (ReadLastLogins): each account's newest login with
// its terminal and remote host, from one lslogins call. Moved here from
// pkg/engine/details (decision 3 (a) of System Details) so the two screens
// share it; Account Review's ReadLogins (logins.go) is another reader, of the
// time alone.

// WtmpFile is the login history lslogins reads first, unless told another
// (--wtmp-file). lslogins' sources: the order it asks is wtmp, lastlog2's
// database where it is built with it, then lastlog (lslogins.c v2.41:827,
// :601). Both VMs' builds link lastlog2 (vm-runs/s1/08-lslogins.log), and
// neither has the database (vm-runs/s1/01-tools.log:24, :91).
const WtmpFile = "/var/log/wtmp"

// lastLoginArgs is decision 3 (a)'s one call, but for its names and, with
// LastLoginOptions.NoWtmp, --wtmp-file=/dev/null before -l.
var lastLoginArgs = []string{"--time-format=iso", "--noheadings", "--raw", "--output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME"}

// noWtmpArg leaves wtmp out of lslogins' sources: lastlog still holds each
// account's time and host on Debian 13 and Ubuntu 26.04, and the call takes
// about 5 ms where reading a 36 MB wtmp takes about 1 s
// (vm-runs/s1/08-lslogins.log, "wtmp empty: does lastlog answer?").
const noWtmpArg = "--wtmp-file=/dev/null"

// LoginsState is what became of the one lslogins call.
type LoginsState uint8

const (
	// LoginsUnread: it failed, it hit its bound, or a line did not parse
	// (LastLogins.Err says which). Never no login. The zero value.
	LoginsUnread LoginsState = iota
	// LoginsNoTool: lslogins is not installed, as on Alpine 3.23
	// (vm-runs/s1/01-tools.log).
	LoginsNoTool
	// LoginsRead: it answered, and List holds a LastLogin for each account
	// asked.
	LoginsRead
)

// LoginState is what the answer says of one account.
type LoginState uint8

const (
	// LoginUnknown: nothing is known of the account: lslogins had no line for
	// it, or a blank where a source refuses or none holds records. The zero
	// value.
	LoginUnknown LoginState = iota
	// LoginNone: a blank, where no source refuses and one holds records.
	LoginNone
	// LoginAt: its newest login with a terminal.
	LoginAt
)

// LastLogin is one account's answer. When is as lslogins printed it, and At
// the same parsed; TTY and From are its other fields, From "" for a login with
// no remote host, as lslogins' raw form prints them (a control character or a
// blank in one as \x1b, \x20: libsmartcols, fputs_nonblank).
type LastLogin struct {
	User      string
	State     LoginState
	When, TTY string
	From      string
	At        time.Time
}

// LastLogins is what ReadLastLogins read: a LastLogin per name asked, in
// their order, where State is LoginsRead; else Err says why not.
type LastLogins struct {
	State LoginsState
	List  []LastLogin // nil unless LoginsRead
	Err   error
}

// Of is name's answer, LoginUnknown where it has none.
func (l LastLogins) Of(name string) LastLogin {
	for _, x := range l.List {
		if x.User == name {
			return x
		}
	}
	return LastLogin{User: name}
}

// LastLoginOptions is how ReadLastLogins asks.
type LastLoginOptions struct {
	// NoWtmp leaves wtmp out of lslogins' sources (--wtmp-file=/dev/null),
	// and out of the blank guard's: the User Management screen's call.
	NoWtmp bool
	// Run, where set, bounds the lslogins call (host.RunBounded, a second's
	// grace); zero runs it through sys as it is, which may bound it itself.
	Run time.Duration
	// Guard bounds the blank guard's reads of the sources.
	Guard time.Duration
}

// runGrace is how long past LastLoginOptions.Run a call is waited for.
const runGrace = time.Second

// ReadLastLogins is decision 3 (a): one call for every name, its lines keyed
// by name (vm-runs/s1/02-login.log, 08-lslogins.log), changing nothing. A name
// lslogins would split or print escaped cannot be asked (askable), so it reads
// as a name with no line does. With no name to ask nothing runs, and each name
// is LoginUnknown.
func ReadLastLogins(ctx context.Context, sys host.System, names []string, o LastLoginOptions) LastLogins {
	var asked []string
	for _, n := range names {
		if askable(n) {
			asked = append(asked, n)
		}
	}
	s := LastLogins{State: LoginsRead, List: make([]LastLogin, len(names))}
	for i, n := range names {
		s.List[i] = LastLogin{User: n}
	}
	if len(asked) == 0 {
		return s
	}
	args := slices.Clone(lastLoginArgs)
	if o.NoWtmp {
		args = append(args, noWtmpArg)
	}
	args = append(args, "-l", strings.Join(asked, ","))
	var res host.Result
	var err error
	if o.Run > 0 {
		res, err = host.RunBounded(ctx, sys, o.Run, runGrace, "lslogins", args...)
	} else {
		res, err = sys.Run(ctx, "lslogins", args...)
	}
	switch {
	case errors.Is(err, host.ErrNotFound):
		return LastLogins{State: LoginsNoTool, Err: err}
	case err != nil:
		return LastLogins{Err: err}
	}
	read, err := parseLastLogins(asked, string(res.Stdout))
	if err != nil {
		return LastLogins{Err: err}
	}
	blank := false
	for i, n := range names {
		if l, ok := read[n]; ok {
			s.List[i] = l
			blank = blank || l.State == LoginNone
		}
	}
	if !blank {
		return s
	}
	// lslogins answers blank, exit 0, when its sources are missing, empty or
	// unreadable, wtmp among them (vm-runs/s1/09-lslogins-blank.log), so a login
	// held in one it cannot read answers blank too. A blank is no login only
	// where no source refuses and one holds records.
	ctx, cancel := context.WithTimeout(ctx, o.Guard)
	defer cancel()
	sources := []string{WtmpFile, Lastlog2File, LastlogFile}
	if o.NoWtmp {
		sources = sources[1:]
	}
	holds, refused := false, false
	for _, p := range sources {
		src := ReadLoginSource(ctx, sys, p)
		holds = holds || src.State == SourceRecords
		refused = refused || src.State == SourceUnread
	}
	if !holds || refused {
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

// parseLastLogins reads lslogins' lines, by name: any other line, or a second
// for a name, does not parse, so the whole answer is not trusted.
func parseLastLogins(names []string, out string) (map[string]LastLogin, error) {
	read := map[string]LastLogin{}
	if out == "" {
		return read, nil
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		l, err := parseLastLogin(line)
		if err != nil {
			return nil, err
		}
		if _, again := read[l.User]; again || !slices.Contains(names, l.User) {
			return nil, fmt.Errorf("accounts: lslogins: %q: %w", line, detect.ErrUnrecognised)
		}
		read[l.User] = l
	}
	return read, nil
}

// parseLastLogin reads one line: "alice 2026-10-20T11:42:05+00:00 pts/0
// 192.0.2.10" or, for no login, the name and three spaces
// (vm-runs/s1/02-login.log). Four fields at single spaces, so a blank one is
// empty. It says LoginNone of a blank; the guard takes that back where it must.
func parseLastLogin(line string) (LastLogin, error) {
	f := strings.Split(line, " ")
	if len(f) != 4 || f[0] == "" {
		return LastLogin{}, fmt.Errorf("accounts: lslogins: %q: %w", line, detect.ErrUnrecognised)
	}
	l := LastLogin{User: f[0], When: f[1], TTY: f[2], From: f[3]}
	if l.When+l.TTY+l.From == "" {
		l.State = LoginNone
		return l, nil
	}
	at, err := time.Parse(time.RFC3339, l.When)
	if err != nil {
		return LastLogin{}, fmt.Errorf("accounts: lslogins: %q: %w", line, detect.ErrUnrecognised)
	}
	l.State, l.At = LoginAt, at
	return l, nil
}

// SourceState is what the blank guard found of one of lslogins' sources.
type SourceState uint8

const (
	// SourceUnread: it is there, and could not be read (LoginSource.Err).
	// The zero value.
	SourceUnread SourceState = iota
	// SourceMissing: it is not there.
	SourceMissing
	// SourceNoRecords: it was read and holds nothing: no bytes, or only NUL
	// bytes as far as looked (recordsLook).
	SourceNoRecords
	// SourceRecords: it was read and holds records.
	SourceRecords
)

// LoginSource is one of lslogins' sources, as the blank guard read it.
type LoginSource struct {
	Path  string
	State SourceState
	Err   error
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

// ReadLoginSource is the blank guard's read of p, one of lslogins' sources. It
// truly reads p, as a mode says nothing of who may read it: ScanLines opens it,
// refuses a symlink, a FIFO and a device without blocking, and looks no further
// than the first line with a byte in it. Each of its forms is binary, so a line
// is only a piece of it. A lastlog2 database holds a table once a login has
// written one (lastlog2.c v2.41:239, CREATE TABLE in write_entry): any bytes
// count as records [I, read; no VM has the file].
func ReadLoginSource(ctx context.Context, sys host.System, p string) LoginSource {
	s := LoginSource{Path: p}
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
