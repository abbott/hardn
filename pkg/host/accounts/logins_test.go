package accounts_test

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// TestAgeing: whether a password never expires, as chage -l says "never"
// (no last change, no maximum age, or one of 10000 or more; a last change
// of 0 must be changed), and the day the account expires, 0 for none.
func TestAgeing(t *testing.T) {
	for _, c := range []struct {
		line   string
		never  bool
		expire int
	}{
		{"alice:$y$x:20000:0:99999:7:::", true, 0},
		{"alice:$y$x:20000:0:10000:7:::", true, 0},
		{"alice:$y$x:20000:0:9999:7:::", false, 0},
		{"alice:$y$x:20000:0:365:7:::", false, 0},
		{"alice:$y$x:20000:0::7:::", true, 0},
		{"alice:$y$x:20000::::::", true, 0},
		{"alice:$y$x::0:365:7:::", true, 0},     // no last change: ageing off
		{"alice:$y$x:0:0:99999:7:::", false, 0}, // must be changed at the next login
		{"alice:$y$x:0:0::7:::", false, 0},      // the same with no maximum age
		{"alice:$y$x:0::::::", false, 0},
		{"alice:$y$x:20000:0:99999999999999999999:7:::", true, 0},
		{"alice:$y$x:20000:0:365:7::1:", false, 1},
		{"alice:$y$x:20000:0:365:7::20500:", false, 20500},
		{"alice:$y$x:20000:0:365:7::0:", false, 0},
		{"alice:$y$x:20000:0:365:7::99999999999999999999:", false, 0},
	} {
		s, err := accounts.ParseShadow([]byte(c.line + "\n"))
		if e := s["alice"]; err != nil || e.NeverExpires != c.never || e.Expire != c.expire {
			t.Errorf("%q: %+v, %v", c.line, e, err)
		}
	}
}

// loginsArgv is what ReadLogins runs.
var loginsArgv = []string{"lslogins", "--export", "--time-format=iso", "--output=USER,LAST-LOGIN", "--wtmp-file=/dev/null"}

// loginsHost is a host with the record files named and lslogins answering r.
func loginsHost(r hostfake.Response, files ...string) *hostfake.System {
	h := hostfake.New(host.Options{})
	for _, f := range files {
		h.AddFile(f, nil, 0o644, 0, 0)
	}
	h.Respond(r, loginsArgv[0], loginsArgv[1:]...)
	return h
}

const loginsOut = `USER="root" LAST-LOGIN=""
USER="alice" LAST-LOGIN="2026-03-02T10:00:00+01:00"
USER="deploy" LAST-LOGIN=""
USER="alice" LAST-LOGIN="2020-01-01T00:00:00+00:00"
`

// TestReadLogins: each record file alone and both; the first line of a
// name; an account with no login the zero time.
func TestReadLogins(t *testing.T) {
	for _, files := range [][]string{{accounts.LastlogFile}, {accounts.Lastlog2File}, {accounts.LastlogFile, accounts.Lastlog2File}} {
		h := loginsHost(hostfake.Response{Stdout: loginsOut}, files...)
		ls, err := accounts.ReadLogins(context.Background(), h)
		want := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
		if err != nil || len(ls) != 3 || !ls["alice"].Equal(want) || !ls["deploy"].IsZero() || !ls["root"].IsZero() {
			t.Errorf("%v: %v, %v", files, ls, err)
		}
		if _, ok := ls["george"]; ok {
			t.Error("an account lslogins did not list")
		}
	}
}

// TestNoLoginRecord: without either file nothing runs.
func TestNoLoginRecord(t *testing.T) {
	h := loginsHost(hostfake.Response{Stdout: loginsOut})
	if _, err := accounts.ReadLogins(context.Background(), h); !errors.Is(err, accounts.ErrNoLoginRecord) {
		t.Errorf("no file: %v", err)
	}
	if len(h.Runs()) != 0 {
		t.Errorf("ran %v", h.Runs())
	}
}

// TestLoginsNotRead: lslogins missing or failing, and a record file whose
// Stat fails otherwise than for its absence.
func TestLoginsNotRead(t *testing.T) {
	h := loginsHost(hostfake.Response{Err: host.ErrNotFound}, accounts.LastlogFile)
	if _, err := accounts.ReadLogins(context.Background(), h); !errors.Is(err, host.ErrNotFound) {
		t.Errorf("lslogins missing: %v", err)
	}
	h = loginsHost(hostfake.Response{ExitCode: 1, Stderr: "lslogins: unrecognized option '--wtmp-file=/dev/null'\n"}, accounts.LastlogFile)
	var ee *host.ExitError
	if _, err := accounts.ReadLogins(context.Background(), h); !errors.As(err, &ee) {
		t.Errorf("lslogins failing: %v", err)
	}
	// A record under a path that is a file: Stat fails, not for absence.
	h = loginsHost(hostfake.Response{Stdout: loginsOut})
	h.AddFile("/var/lib/lastlog", nil, 0o644, 0, 0)
	if _, err := accounts.ReadLogins(context.Background(), h); err == nil || errors.Is(err, accounts.ErrNoLoginRecord) || errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a stat that fails: %v", err)
	}
}

// TestParseLogins: the forms lslogins writes, and each form that is not
// one, which names its line and never its content.
func TestParseLogins(t *testing.T) {
	ls, err := accounts.ParseLogins([]byte(`USER="alice" UID="1000" LAST_LOGIN="2026-03-02T10:00:00-05:00" GECOS="Alice \"A\" \x41 x"` + "\n\n" + `USER="bob" LAST-LOGIN=""` + "\n"))
	if err != nil || len(ls) != 2 || !ls["alice"].Equal(time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)) || !ls["bob"].IsZero() {
		t.Errorf("ParseLogins: %v, %v", ls, err)
	}
	for _, out := range []string{
		"",
		"\n\n",
		"USER   LAST-LOGIN\nalice  2026-03-02\n", // the table
		`USER="alice"` + "\n",                    // no last login column
		`LAST-LOGIN="2026-03-02T10:00:00+00:00"` + "\n",             // no name
		`USER="" LAST-LOGIN=""` + "\n",                              // an empty name
		`USER="alice" LAST-LOGIN="Mar02/10:00"` + "\n",              // the short form
		`USER="alice" LAST-LOGIN="2026-03-02"` + "\n",               // a date alone
		`USER="alice" LAST-LOGIN="2026-03-02T10:00:00+00:00` + "\n", // no closing quote
		`USER="alice"LAST-LOGIN=""` + "\n",                          // no blank between
		`USER="alice" LAST-LOGIN="\xzz"` + "\n",                     // an escape that is not one
		`USER="secret-name" LAST-LOGIN="secret-value"` + "\n",
	} {
		_, err := accounts.ParseLogins([]byte(out))
		if !errors.Is(err, accounts.ErrLogins) || strings.Contains(err.Error(), "secret") {
			t.Errorf("%q: %v", out, err)
		}
	}
}
