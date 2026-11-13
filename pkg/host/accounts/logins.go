package accounts

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// The last-login records (account hygiene, docs/engine/screen-accounts.md):
// the two files that hold each account's last login for as long as the
// account lives, which wtmp, rotated, does not.
const (
	// LastlogFile is the record pam_lastlog and sshd write, indexed by uid
	// (glibc's _PATH_LASTLOG [I]).
	LastlogFile = "/var/log/lastlog"
	// Lastlog2File is lastlog2's database (util-linux's liblastlog2,
	// LL2_DEFAULT_DATABASE, v2.41).
	Lastlog2File = "/var/lib/lastlog/lastlog2.db"
)

// loginsCommand is what ReadLogins runs: util-linux's lslogins, each
// account's name and last login, as KEY="value" pairs with the time in ISO
// 8601 (login-utils/lslogins.c, v2.37 and v2.41). Its last login is the
// account's newest entry in wtmp, else lastlog2's (from v2.41, where built
// with it), else LastlogFile's; --wtmp-file=/dev/null leaves wtmp out, an
// entry of which says nothing of an account it no longer holds.
var loginsCommand = []string{"lslogins", "--export", "--time-format=iso", "--output=USER,LAST-LOGIN", "--wtmp-file=/dev/null"}

// ErrNoLoginRecord: the host has neither LastlogFile nor Lastlog2File.
var ErrNoLoginRecord = errors.New("no login record: neither " + LastlogFile + " nor " + Lastlog2File + " is there")

// ErrLogins: lslogins' output is not in the form ReadLogins asked for.
var ErrLogins = errors.New("lslogins' output is not in its form")

// Logins are the accounts lslogins listed, each with its last login; the
// zero time for an account the record has no login of.
type Logins map[string]time.Time

// ReadLogins reads each account's last login, changing nothing. Without
// LastlogFile and Lastlog2File it is ErrNoLoginRecord, and nothing runs;
// with either, it runs lslogins (loginsCommand). An error where a file's
// Stat fails otherwise than for its absence, lslogins is not installed
// (host.ErrNotFound) or fails, or its output is not in its form
// (ErrLogins), which names the line, never its content.
func ReadLogins(ctx context.Context, sys host.System) (Logins, error) {
	found := false
	for _, f := range []string{Lastlog2File, LastlogFile} {
		switch _, err := sys.Stat(ctx, f); {
		case err == nil:
			found = true
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("accounts: %w", err)
		}
	}
	if !found {
		return nil, fmt.Errorf("accounts: %w", ErrNoLoginRecord)
	}
	res, err := sys.Run(ctx, loginsCommand[0], loginsCommand[1:]...)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	return ParseLogins(res.Stdout)
}

// ParseLogins parses lslogins' --export output: a line per account, each
// column KEY="value" (libsmartcols). The last login's key is LAST-LOGIN,
// or LAST_LOGIN where the columns are named as shell variables; its value
// is empty or a time as lslogins' iso format writes it
// (2026-03-02T10:00:00+00:00). A line without a name or without that key,
// a time in another form, and an output with no line are ErrLogins. Of an
// account listed twice the first line is kept.
func ParseLogins(out []byte) (Logins, error) {
	ls := Logins{}
	for i, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		bad := fmt.Errorf("accounts: lslogins: line %d: %w", i+1, ErrLogins)
		cols, ok := exportColumns(line)
		name := cols["USER"]
		last, has := cols["LAST-LOGIN"]
		if !has {
			last, has = cols["LAST_LOGIN"]
		}
		if !ok || name == "" || !has {
			return nil, bad
		}
		var t time.Time
		if last != "" {
			var err error
			if t, err = time.Parse(time.RFC3339, last); err != nil || t.IsZero() {
				return nil, bad
			}
		}
		if _, seen := ls[name]; !seen {
			ls[name] = t
		}
	}
	if len(ls) == 0 {
		return nil, fmt.Errorf("accounts: lslogins: no account listed: %w", ErrLogins)
	}
	return ls, nil
}

// exportColumns are a line's KEY="value" pairs, blank-separated, a value's
// backslash escapes undone (\xHH a byte, any other the character itself);
// false for a line in another form.
func exportColumns(line string) (map[string]string, bool) {
	cols := map[string]string{}
	for rest := strings.TrimSpace(line); rest != ""; rest = strings.TrimLeft(rest, " \t") {
		key, after, ok := strings.Cut(rest, `="`)
		if !ok || key == "" || strings.ContainsAny(key, " \t\"") {
			return nil, false
		}
		var val strings.Builder
		i, closed := 0, false
		for i < len(after) && !closed {
			switch c := after[i]; {
			case c == '"':
				closed = true
			case c == '\\' && i+3 < len(after) && after[i+1] == 'x':
				b, err := strconv.ParseUint(after[i+2:i+4], 16, 8)
				if err != nil {
					return nil, false
				}
				val.WriteByte(byte(b))
				i += 3
			case c == '\\' && i+1 < len(after):
				val.WriteByte(after[i+1])
				i++
			default:
				val.WriteByte(c)
			}
			i++
		}
		if !closed || (i < len(after) && after[i] != ' ' && after[i] != '\t') {
			return nil, false
		}
		if _, dup := cols[key]; !dup {
			cols[key] = val.String()
		}
		rest = after[i:]
	}
	return cols, len(cols) > 0
}
