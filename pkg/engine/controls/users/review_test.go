package users_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Account Review's reading (review.go), on hosts written here.

// reviewNow is the reading's clock; day 20405 since 1970.
var reviewNow = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

// reviewArgv is what accounts.ReadLogins runs.
var reviewArgv = []string{"lslogins", "--export", "--time-format=iso", "--output=USER,LAST-LOGIN", "--wtmp-file=/dev/null"}

const (
	reviewPasswd = `root:x:0:0:root:/root:/bin/bash
daemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin
ftp:x:101:101::/srv/ftp:/bin/bash
george:x:1000:1000::/home/george:/bin/bash
alice:x:1001:1001::/home/alice:/bin/bash
carol:x:1002:1002::/home/carol:/bin/bash
deploy:x:1003:1003::/home/deploy:/bin/sh
bob:x:1004:1004::/home/bob:/bin/bash
svc:x:1005:1005::/home/svc:/usr/sbin/nologin
gone:x:1006:1006::/home/gone:/bin/bash
alice:x:1099:1099::/home/alice2:/bin/bash
`
	// root: a hash, no maximum age. daemon "*", ftp empty (a system
	// account), george a hash and 99999, alice a hash and no maximum,
	// carol empty, deploy locked before a hash, bob a hash and 365 days,
	// svc "!", gone a hash and expired on day 1. alice's second line is
	// not getspnam's.
	reviewShadow = `root:$y$j9T$r:20000:0::7:::
daemon:*:20000:0:99999:7:::
ftp::20000:0:99999:7:::
george:$y$j9T$g:20000:0:99999:7:::
alice:$y$j9T$a:20000:0::7:::
carol::20000:0:99999:7:::
deploy:!$y$j9T$d:20000:0:99999:7:::
bob:$y$j9T$b:20000:0:365:7:::
svc:!:20000:0:99999:7:::
gone:$y$j9T$x:20000:0:365:7::1:
alice::20000:0:99999:7:::
`
	// george logged in last week, alice in March, bob 90 days ago to the
	// second and deploy never.
	reviewLogins = `USER="root" LAST-LOGIN=""
USER="daemon" LAST-LOGIN=""
USER="ftp" LAST-LOGIN=""
USER="george" LAST-LOGIN="2026-11-06T09:00:00+00:00"
USER="alice" LAST-LOGIN="2026-03-02T10:00:00+01:00"
USER="carol" LAST-LOGIN="2026-11-12T10:00:00+00:00"
USER="deploy" LAST-LOGIN=""
USER="bob" LAST-LOGIN="2026-08-15T12:00:00+00:00"
USER="svc" LAST-LOGIN=""
USER="gone" LAST-LOGIN=""
`
)

// reviewHost is a host with passwd and shadow, the record files named, and
// lslogins answering logins; "" for a file leaves it out.
func reviewHost(passwd, shadow string, logins hostfake.Response, records ...string) *hostfake.System {
	h := hostfake.New(host.Options{})
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/usr/sbin/nologin"} {
		h.AddFile(sh, nil, 0o755, 0, 0)
	}
	if passwd != "" {
		h.AddFile(accounts.PasswdFile, []byte(passwd), 0o644, 0, 0)
	}
	if shadow != "" {
		h.AddFile(accounts.ShadowFile, []byte(shadow), 0o640, 0, 42)
	}
	for _, f := range records {
		h.AddFile(f, nil, 0o644, 0, 0)
	}
	h.Respond(logins, reviewArgv[0], reviewArgv[1:]...)
	return h
}

// reviewReadOnly fails for a call that is not a read or lslogins.
func reviewReadOnly(t *testing.T, h *hostfake.System) {
	t.Helper()
	for _, c := range h.Calls() {
		switch {
		case c.Op == "read" || c.Op == "stat" || c.Op == "lstat":
		case c.Op == "run" && reflect.DeepEqual(c.Argv, reviewArgv):
		default:
			t.Errorf("not a read: %v", c)
		}
	}
}

// TestReview: each list on a host with something of every kind. An empty
// field is an empty password at any uid; "!", "*" and "!" before a hash are
// not. Never expires asks a password set. Stale asks a uid of MinUID or
// more, a login shell and an account not expired; never logged in counts,
// 90 days to the second counts, root and the system accounts do not.
func TestReview(t *testing.T) {
	for _, records := range [][]string{{accounts.LastlogFile}, {accounts.Lastlog2File}} {
		h := reviewHost(reviewPasswd, reviewShadow, hostfake.Response{Stdout: reviewLogins}, records...)
		r := users.ReadReview(context.Background(), h, reviewNow)
		want := users.Review{
			EmptyPassword: []string{"ftp", "carol"},
			NeverExpires:  []string{"root", "george", "alice"},
			Stale: []users.Stale{{Name: "alice", Last: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)}, {Name: "deploy"},
				{Name: "bob", Last: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}},
		}
		if len(r.Stale) == 3 {
			for i := range r.Stale {
				r.Stale[i].Last = r.Stale[i].Last.UTC()
			}
		}
		if !reflect.DeepEqual(r, want) {
			t.Errorf("%v:\n got %+v\nwant %+v", records, r, want)
		}
		reviewReadOnly(t, h)
	}
}

// TestReviewClean: nothing to list is three empty lists, no error.
func TestReviewClean(t *testing.T) {
	h := reviewHost("root:x:0:0::/root:/bin/bash\ngeorge:x:1000:1000::/home/george:/bin/bash\n",
		"root:!:20000:0:99999:7:::\ngeorge:$y$j9T$g:20000:0:365:7:::\n",
		hostfake.Response{Stdout: `USER="root" LAST-LOGIN=""` + "\n" + `USER="george" LAST-LOGIN="2026-11-13T08:00:00+00:00"` + "\n"}, accounts.LastlogFile)
	if r := users.ReadReview(context.Background(), h, reviewNow); !reflect.DeepEqual(r, users.Review{}) {
		t.Errorf("%+v", r)
	}
}

// TestReviewNoRecord: without a record file nothing runs and no account is
// stale; shadow's two lists are read as anywhere. A record that is read and
// has no login of any account in the last StaleDays lists no one either,
// and is not called no record.
func TestReviewNoRecord(t *testing.T) {
	h := reviewHost(reviewPasswd, reviewShadow, hostfake.Response{Stdout: reviewLogins})
	r := users.ReadReview(context.Background(), h, reviewNow)
	if !r.NoRecord || r.NoRecent || r.LoginErr != nil || r.Err != nil || r.Stale != nil || len(r.EmptyPassword) != 2 || len(r.NeverExpires) != 3 {
		t.Errorf("no file: %+v", r)
	}
	if len(h.Runs()) != 0 {
		t.Errorf("ran %v", h.Runs())
	}

	old := strings.NewReplacer("2026-11-06", "2026-08-01", "2026-11-12", "2026-08-02").Replace(reviewLogins)
	h = reviewHost(reviewPasswd, reviewShadow, hostfake.Response{Stdout: old}, accounts.LastlogFile)
	r = users.ReadReview(context.Background(), h, reviewNow)
	if !r.NoRecent || r.NoRecord || r.LoginErr != nil || r.Stale != nil || len(r.EmptyPassword) != 2 {
		t.Errorf("a record with no recent login: %+v", r)
	}
}

// TestReviewLoginsNotRead: a record that is there and not read lists no
// stale account and says why; shadow's lists stand.
func TestReviewLoginsNotRead(t *testing.T) {
	short := strings.Replace(reviewLogins, `USER="deploy" LAST-LOGIN=""`+"\n", "", 1)
	for name, c := range map[string]struct {
		resp hostfake.Response
		is   error
	}{
		"lslogins missing":      {hostfake.Response{Err: host.ErrNotFound}, host.ErrNotFound},
		"lslogins fails":        {hostfake.Response{ExitCode: 1, Stderr: "lslogins: unrecognized option\n"}, nil},
		"the table":             {hostfake.Response{Stdout: "USER LAST-LOGIN\nroot\n"}, accounts.ErrLogins},
		"no output":             {hostfake.Response{}, accounts.ErrLogins},
		"another time":          {hostfake.Response{Stdout: `USER="george" LAST-LOGIN="Nov06/09:00"` + "\n"}, accounts.ErrLogins},
		"an account not listed": {hostfake.Response{Stdout: short}, accounts.ErrLogins},
	} {
		h := reviewHost(reviewPasswd, reviewShadow, c.resp, accounts.LastlogFile)
		r := users.ReadReview(context.Background(), h, reviewNow)
		if r.LoginErr == nil || (c.is != nil && !errors.Is(r.LoginErr, c.is)) || r.NoRecord || r.Err != nil || r.Stale != nil {
			t.Errorf("%s: %+v", name, r)
		}
		if len(r.EmptyPassword) != 2 || len(r.NeverExpires) != 3 {
			t.Errorf("%s: shadow's lists: %+v", name, r)
		}
		reviewReadOnly(t, h)
	}
}

// TestReviewNotRead: passwd or shadow missing, unreadable or malformed, and
// /etc/tcb, establish nothing: Err, no list, and lslogins does not run.
func TestReviewNotRead(t *testing.T) {
	ok := hostfake.Response{Stdout: reviewLogins}
	denied := func(path string) host.System {
		return faulty{System: reviewHost(reviewPasswd, reviewShadow, ok, accounts.LastlogFile), fail: map[string]error{"read " + path: fs.ErrPermission}}
	}
	tcb := reviewHost(reviewPasswd, reviewShadow, ok, accounts.LastlogFile)
	tcb.AddDir(accounts.TCBDir, 0o755, 0, 0)
	for name, c := range map[string]struct {
		sys host.System
		is  error
	}{
		"no passwd":         {reviewHost("", reviewShadow, ok, accounts.LastlogFile), fs.ErrNotExist},
		"no shadow":         {reviewHost(reviewPasswd, "", ok, accounts.LastlogFile), fs.ErrNotExist},
		"passwd unreadable": {denied(accounts.PasswdFile), fs.ErrPermission},
		"shadow unreadable": {denied(accounts.ShadowFile), fs.ErrPermission},
		"passwd malformed":  {reviewHost("root:x:0\n", reviewShadow, ok, accounts.LastlogFile), accounts.ErrMalformed},
		"shadow malformed":  {reviewHost(reviewPasswd, "root:secret-hash:x\n", ok, accounts.LastlogFile), accounts.ErrMalformed},
		"tcb":               {tcb, accounts.ErrTCB},
	} {
		r := users.ReadReview(context.Background(), c.sys, reviewNow)
		if !errors.Is(r.Err, c.is) || r.EmptyPassword != nil || r.NeverExpires != nil || r.Stale != nil || r.NoRecord || r.LoginErr != nil {
			t.Errorf("%s: %+v", name, r)
		}
		if r.Err != nil && strings.Contains(r.Err.Error(), "secret") {
			t.Errorf("%s: the error holds the field: %v", name, r.Err)
		}
	}
}

// TestEmptyPasswordFinding: the User row's finding names each account with
// an empty field once, and none for "!", "*" or a hash.
func TestEmptyPasswordFinding(t *testing.T) {
	p := debian13()
	s := site{plat: p, users: []string{aliceSh, "carol:x:1001:1001::/home/carol:/bin/bash"}, members: sudoGroup,
		locks: map[string]string{"alice": aliceHash, "carol": "", "root": "!", "daemon": "*"}, sshd: sshdPAM}
	sys, _ := s.build(t)
	st, err := (users.Admin{}).Check(context.Background(), sys, p.facts)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range st.Findings {
		if f.Code == users.CodeEmptyPassword {
			got = append(got, f.Detail+" @"+f.Source)
		}
	}
	if len(got) != 1 || !strings.HasPrefix(got[0], "carol has an empty password @"+accounts.ShadowFile+":") {
		t.Errorf("findings: %q", got)
	}
}
