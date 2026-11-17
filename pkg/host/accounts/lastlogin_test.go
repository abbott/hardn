package accounts_test

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// The shared last-login reader (accounts.ReadLastLogins) with the User
// Management screen's flags: wtmp left out of lslogins' sources and of the
// blank guard's, the call bounded. System Details' own call, wtmp in, is
// pkg/engine/details' tests'.

// lastArgs is what the screen's call runs before the names.
var lastArgs = []string{"lslogins", "--time-format=iso", "--noheadings", "--raw", "--output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME", "--wtmp-file=/dev/null", "-l"}

// screenOpts are the screen's options.
var screenOpts = accounts.LastLoginOptions{NoWtmp: true, Run: time.Second, Guard: time.Second}

// refusing is a host whose ScanLines of a path in refuse fails, as a source
// root may not read does.
type refusing struct {
	*hostfake.System
	refuse []string
}

func (r refusing) ScanLines(ctx context.Context, p string, fn func(host.Line) bool) (fs.FileInfo, error) {
	if slices.Contains(r.refuse, p) {
		return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrPermission}
	}
	return r.System.ScanLines(ctx, p, fn)
}

// stuck is a host whose lslogins never answers until its ctx is done, as
// one waiting for the password lock does.
type stuck struct{ *hostfake.System }

func (s stuck) Run(ctx context.Context, name string, args ...string) (host.Result, error) {
	<-ctx.Done()
	return host.Result{}, ctx.Err()
}

// lastHost is a host with lslogins answering out to the screen's call for
// names, and the files given, each holding a record.
func lastHost(names, out string, files ...string) *hostfake.System {
	h := hostfake.New(host.Options{})
	for _, f := range files {
		h.AddFile(f, []byte("\x07\x00alice"), 0o644, 0, 0)
	}
	h.Respond(hostfake.Response{Stdout: out}, lastArgs[0], append(slices.Clone(lastArgs[1:]), names)...)
	return h
}

// TestReadLastLoginsScreen: one call for every name, wtmp left out; a login
// with a host, one with none, and a blank where lastlog holds records, no
// login; a name lslogins has no line for, not known.
func TestReadLastLoginsScreen(t *testing.T) {
	h := lastHost("george,alice,carol,deploy", "george 2026-11-16T22:31:05+00:00 pts/0 192.0.2.10\nalice 2026-11-16T20:34:00+00:00 tty1 \ncarol   \n", accounts.LastlogFile)
	l := accounts.ReadLastLogins(context.Background(), h, []string{"george", "alice", "carol", "deploy"}, screenOpts)
	at := time.Date(2026, 11, 16, 22, 31, 5, 0, time.UTC)
	want := []accounts.LastLogin{
		{User: "george", State: accounts.LoginAt, When: "2026-11-16T22:31:05+00:00", TTY: "pts/0", From: "192.0.2.10", At: at},
		{User: "alice", State: accounts.LoginAt, When: "2026-11-16T20:34:00+00:00", TTY: "tty1", At: time.Date(2026, 11, 16, 20, 34, 0, 0, time.UTC)},
		{User: "carol", State: accounts.LoginNone},
		{User: "deploy"},
	}
	if l.State != accounts.LoginsRead || l.Err != nil || len(l.List) != len(want) {
		t.Fatalf("%+v", l)
	}
	for i, w := range want {
		if g := l.List[i]; g.User != w.User || g.State != w.State || g.When != w.When || g.TTY != w.TTY || g.From != w.From || !g.At.Equal(w.At) {
			t.Errorf("%d: %+v, want %+v", i, g, w)
		}
	}
	if o := l.Of("carol"); o.State != accounts.LoginNone {
		t.Errorf("Of(carol) = %+v", o)
	}
	if o := l.Of("nobody"); o.State != accounts.LoginUnknown || o.User != "nobody" {
		t.Errorf("Of(nobody) = %+v", o)
	}
	if runs := h.Runs(); len(runs) != 1 || !slices.Equal(runs[0], append(slices.Clone(lastArgs), "george,alice,carol,deploy")) {
		t.Errorf("ran %q", runs)
	}
}

// TestLastLoginsGuardNoWtmp: with wtmp left out, the blank guard reads
// lastlog2 and lastlog alone: wtmp's records do not make a blank no login,
// nor does wtmp refusing make it not known.
func TestLastLoginsGuardNoWtmp(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		files  []string
		refuse []string
		want   accounts.LoginState
	}{
		{"lastlog holds records", []string{accounts.LastlogFile}, nil, accounts.LoginNone},
		{"lastlog2 holds records", []string{accounts.Lastlog2File}, nil, accounts.LoginNone},
		{"wtmp alone holds records", []string{accounts.WtmpFile}, nil, accounts.LoginUnknown},
		{"wtmp refuses", []string{accounts.LastlogFile, accounts.WtmpFile}, []string{accounts.WtmpFile}, accounts.LoginNone},
		{"lastlog refuses", []string{accounts.Lastlog2File, accounts.LastlogFile}, []string{accounts.LastlogFile}, accounts.LoginUnknown},
		{"no record", nil, nil, accounts.LoginUnknown},
	} {
		h := refusing{System: lastHost("alice", "alice   \n", tc.files...), refuse: tc.refuse}
		l := accounts.ReadLastLogins(ctx, h, []string{"alice"}, screenOpts)
		if l.State != accounts.LoginsRead || l.List[0].State != tc.want {
			t.Errorf("%s: %+v, want %v", tc.name, l, tc.want)
		}
		for _, c := range h.Calls() {
			if c.Path == accounts.WtmpFile {
				t.Errorf("%s: the guard read wtmp", tc.name)
			}
		}
	}
	// System Details' call, wtmp in: wtmp's records count.
	h := hostfake.New(host.Options{})
	h.AddFile(accounts.WtmpFile, []byte("\x07\x00alice"), 0o644, 0, 0)
	h.Respond(hostfake.Response{Stdout: "alice   \n"}, "lslogins", "--time-format=iso", "--noheadings", "--raw", "--output=USER,LAST-LOGIN,LAST-TTY,LAST-HOSTNAME", "-l", "alice")
	if l := accounts.ReadLastLogins(ctx, h, []string{"alice"}, accounts.LastLoginOptions{Guard: time.Second}); l.State != accounts.LoginsRead || l.List[0].State != accounts.LoginNone {
		t.Errorf("wtmp in: %+v", l)
	}
}

// TestLastLoginsNotRead: no lslogins is LoginsNoTool; one that fails, an
// answer not in its form, or a call past its bound is LoginsUnread, its
// error kept; with no name that lslogins can take, nothing runs.
func TestLastLoginsNotRead(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New(host.Options{})
	h.Respond(hostfake.Response{Err: host.ErrNotFound}, lastArgs[0], append(slices.Clone(lastArgs[1:]), "george")...)
	if l := accounts.ReadLastLogins(ctx, h, []string{"george"}, screenOpts); l.State != accounts.LoginsNoTool || !errors.Is(l.Err, host.ErrNotFound) || l.List != nil {
		t.Errorf("no lslogins: %+v", l)
	}
	for name, out := range map[string]string{"three fields": "george 2026-11-16T22:31:05+00:00 pts/0\n", "a name not asked": "alice   \n", "a time in another form": "george Nov16 pts/0 192.0.2.10\n"} {
		l := accounts.ReadLastLogins(ctx, lastHost("george", out), []string{"george"}, screenOpts)
		if l.State != accounts.LoginsUnread || !errors.Is(l.Err, detect.ErrUnrecognised) {
			t.Errorf("%s: %+v", name, l)
		}
	}
	h = hostfake.New(host.Options{})
	h.Respond(hostfake.Response{ExitCode: 1, Stderr: "lslogins: cannot open /var/log/lastlog\n"}, lastArgs[0], append(slices.Clone(lastArgs[1:]), "george")...)
	if l := accounts.ReadLastLogins(ctx, h, []string{"george"}, screenOpts); l.State != accounts.LoginsUnread || l.Err == nil {
		t.Errorf("exit 1: %+v", l)
	}
	o := screenOpts
	o.Run = 20 * time.Millisecond
	if l := accounts.ReadLastLogins(ctx, stuck{hostfake.New(host.Options{})}, []string{"george"}, o); l.State != accounts.LoginsUnread || !errors.Is(l.Err, context.DeadlineExceeded) {
		t.Errorf("stuck: %+v", l)
	}
	h = hostfake.New(host.Options{})
	l := accounts.ReadLastLogins(ctx, h, []string{"ali ce", "a,b"}, screenOpts)
	if len(h.Runs()) != 0 || l.State != accounts.LoginsRead || len(l.List) != 2 || l.List[0].State != accounts.LoginUnknown || l.List[1].User != "a,b" {
		t.Errorf("no name to ask: ran %q, %+v", h.Runs(), l)
	}
}
