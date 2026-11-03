package accounts_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/hostfake"
)

// Synthetic accounts only: alice, bob, george.
const (
	passwd = "root:x:0:0:root:/root:/bin/bash\n" +
		"# a comment\n\n" +
		"alice:x:1000:1000:Alice,,,:/home/alice:/bin/bash\n" +
		"bob:x:1001:27::/home/bob:\n"
	group = "root:x:0:\nsudo:x:27:alice,george,\nalice:x:1000:\nwheel:x:10:\n"
)

func TestParse(t *testing.T) {
	us, err := accounts.ParsePasswd([]byte(passwd))
	want := []accounts.User{
		{Name: "root", UID: 0, GID: 0, Home: "/root", Shell: "/bin/bash", Line: 1, PasswdLock: accounts.NotLocked, PasswdPassword: accounts.PasswordNone},
		{Name: "alice", UID: 1000, GID: 1000, Home: "/home/alice", Shell: "/bin/bash", Line: 4, PasswdLock: accounts.NotLocked, PasswdPassword: accounts.PasswordNone},
		{Name: "bob", UID: 1001, GID: 27, Home: "/home/bob", Shell: "", Line: 5, PasswdLock: accounts.NotLocked, PasswdPassword: accounts.PasswordNone},
	}
	if err != nil || !reflect.DeepEqual(us, want) {
		t.Errorf("ParsePasswd: %+v, %v", us, err)
	}
	gs, err := accounts.ParseGroup([]byte(group))
	if err != nil || len(gs) != 4 || !reflect.DeepEqual(gs[1], accounts.Group{Name: "sudo", GID: 27, Members: []string{"alice", "george"}, Line: 2}) || gs[3].Members != nil {
		t.Errorf("ParseGroup: %+v, %v", gs, err)
	}
}

// TestMalformed: a line in neither form is an error naming it, never
// skipped.
func TestMalformed(t *testing.T) {
	for _, l := range []string{
		"alice:x:1000:1000::/home/alice",          // six fields
		"alice:x:1000:1000::/home/alice:/bin/sh:", // eight
		":x:1000:1000::/home/alice:/bin/sh",       // no name
		"alice:x:-1:1000::/home/alice:/bin/sh",    // a negative uid
		"alice:x:4294967295:1000::/:/bin/sh",      // (uid_t)-1
		"alice:x:1000:g::/home/alice:/bin/sh",     // a gid not a number
		"+::::::",                                 // NIS compat
	} {
		if _, err := accounts.ParsePasswd([]byte("root:x:0:0::/root:/bin/sh\n" + l + "\n")); !errors.Is(err, accounts.ErrMalformed) || err.Error() != "accounts: /etc/passwd:2: "+accounts.ErrMalformed.Error() {
			t.Errorf("%q: %v", l, err)
		}
	}
	for _, l := range []string{"sudo:x:27", "sudo:x:x:alice", ":x:27:", "sudo:x:27:alice, george"} {
		if _, err := accounts.ParseGroup([]byte(l)); !errors.Is(err, accounts.ErrMalformed) {
			t.Errorf("%q: %v", l, err)
		}
	}
}

func TestMember(t *testing.T) {
	db := accounts.DB{Groups: []accounts.Group{{Name: "sudo", GID: 27, Members: []string{"george"}}, {Name: "sudo", GID: 28, Members: []string{"alice"}}, {Name: "admins", GID: 27, Members: []string{"bob"}}}}
	for _, c := range []struct {
		u    accounts.User
		want bool
	}{
		{accounts.User{Name: "george", GID: 100}, true}, // listed
		{accounts.User{Name: "alice", GID: 27}, true},   // primary
		{accounts.User{Name: "alice", GID: 100}, false}, // in a second sudo, which getgrnam does not find
		{accounts.User{Name: "bob", GID: 100}, true},    // listed in another group of the same gid
	} {
		if got := db.Member(c.u, "sudo"); got != c.want {
			t.Errorf("%s: %v", c.u.Name, got)
		}
	}
	if db.Member(accounts.User{Name: "george"}, "wheel") {
		t.Error("a group that does not exist")
	}
}

// TestRead: two reads through host.System, and a failed read wrapped.
func TestRead(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddFile(accounts.PasswdFile, []byte(passwd), 0o644, 0, 0)
	h.AddFile(accounts.GroupFile, []byte(group), 0o644, 0, 0)
	db, err := accounts.Read(context.Background(), h)
	if err != nil || len(db.Users) != 3 || len(db.Groups) != 4 {
		t.Fatalf("Read: %+v, %v", db, err)
	}
	calls := h.Calls()
	if len(calls) != 2 {
		t.Errorf("%d calls, want 2: %v", len(calls), calls)
	}
	for _, c := range calls {
		if c.Op != "read" {
			t.Errorf("a call that is not a read: %v", c)
		}
	}
	h = hostfake.New(host.Options{})
	h.AddFile(accounts.PasswdFile, []byte(passwd), 0o644, 0, 0)
	if _, err := accounts.Read(context.Background(), h); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no group file: %v", err)
	}
	h.AddFile(accounts.GroupFile, []byte("sudo:x:27\n"), 0o644, 0, 0)
	if _, err := accounts.Read(context.Background(), h); !errors.Is(err, accounts.ErrMalformed) {
		t.Errorf("a malformed group: %v", err)
	}
}

// Synthetic shadow lines (next/p4-locked). The hashes are made up, and
// each holds runs of characters that appear nowhere else, so that a test
// can show that none of them leaves the reader.
const (
	hashAlice = "$6$Qz7wVx3k$Jq9ZtYp4Wm2Rv8Ln5Hs1Kd6Fg0Xc"
	hashBob   = "$y$j9T$Mn4bVq7$Zr8Wt2Yu5Io1Pa3Sd6Fh9Jk"
	shadow    = "root:!:20000:0:99999:7:::\n" +
		"# a comment\n\n" +
		"alice:" + hashAlice + ":20000:0:99999:7:::\n" +
		"bob:!" + hashBob + ":20000:0:99999:7:::\n" +
		"daemon:*:20000:0:99999:7:::\n" +
		"sshd:!!:20000::::::\n" +
		"nobody::20000:0:99999:7:::\n" +
		"alice:!:20000:0:99999:7:::\n"
)

// TestShadow: each name's first line, its lock from the first character
// alone ("!", "!!" and "!" before a hash locked; "*" and a hash not; an
// empty field not, as platform_locked_account() reads it), and LockOf's
// fallback to /etc/passwd's field without a line (george has none).
func TestShadow(t *testing.T) {
	s, err := accounts.ParseShadow([]byte(shadow))
	want := accounts.Shadow{
		"root":   {Lock: accounts.Locked, Password: accounts.PasswordNone, Line: 1},
		"alice":  {Lock: accounts.NotLocked, Password: accounts.PasswordSet, Line: 4}, // the first line; the second, "!", is not getspnam's
		"bob":    {Lock: accounts.Locked, Password: accounts.PasswordNone, Line: 5},
		"daemon": {Lock: accounts.NotLocked, Password: accounts.PasswordNone, Line: 6},
		"sshd":   {Lock: accounts.Locked, Password: accounts.PasswordNone, Line: 7},
		"nobody": {Lock: accounts.Empty, Password: accounts.PasswordEmpty, Line: 8},
	}
	if err != nil || !reflect.DeepEqual(s, want) {
		t.Fatalf("ParseShadow: %#v, %v", s, err)
	}
	for _, c := range []struct {
		u        accounts.User
		lock     accounts.Lock
		at, text string
	}{
		{accounts.User{Name: "bob", Line: 3, PasswdLock: accounts.NotLocked, PasswdPassword: accounts.PasswordNone}, accounts.Locked, "/etc/shadow:5", "locked (!)"},
		{accounts.User{Name: "george", Line: 9, PasswdLock: accounts.NotLocked, PasswdPassword: accounts.PasswordNone}, accounts.NotLocked, "/etc/passwd:9", "not locked"},
		{accounts.User{Name: "george", Line: 9, PasswdLock: accounts.Locked}, accounts.Locked, "/etc/passwd:9", "locked (!)"},
		{accounts.User{Name: "nobody", Line: 4}, accounts.Empty, "/etc/shadow:8", "empty, not locked"},
	} {
		if l, at := s.LockOf(c.u); l != c.lock || at != c.at || l.String() != c.text {
			t.Errorf("%s: %v %q %q", c.u.Name, l, at, l)
		}
	}
	if accounts.LockNotRead.String() != "not read" {
		t.Error(accounts.LockNotRead.String())
	}
	us, err := accounts.ParsePasswd([]byte("root:!:0:0::/root:/bin/sh\nalice::1000:1000::/home/alice:/bin/sh\n"))
	if err != nil || us[0].PasswdLock != accounts.Locked || us[1].PasswdLock != accounts.Empty {
		t.Errorf("passwd's field: %+v, %v", us, err)
	}
	// Lines musl's getspnam passes over, or finds no entry for
	// (getspnam_r.c): sshd would read /etc/passwd's field instead.
	for _, l := range []string{
		"alice:!:20000:0:99999:7::\n",                                  // eight fields
		"alice:!:20000:0:99999:7::::\n",                                // ten
		":!:20000:0:99999:7:::\n",                                      // no name
		"alice\n",                                                      // one
		"alice:!:never:0:99999:7:::\n",                                 // a word in a number
		"alice:!:20000:-1:99999:7:::\n",                                // a sign
		"alice:!:20000:0:99999:7:::\r\n",                               // CRLF
		"alice:!:20000:0:99999:7:::",                                   // no final newline
		"alice:!" + strings.Repeat("x", 229) + ":20000:0:99999:7:::\n", // 255 bytes before the newline
	} {
		if _, err := accounts.ParseShadow([]byte("root:*:1::::::\n" + l)); !errors.Is(err, accounts.ErrMalformed) || err.Error() != "accounts: /etc/shadow:2: "+accounts.ErrMalformed.Error() {
			t.Errorf("%q: %v", l, err)
		}
	}
	for _, data := range []string{
		"",                    // no line
		"root:!:1::::::\n\n",  // a blank last line
		"root:!:1::::::\n# x", // a comment with no newline
		"alice:!" + strings.Repeat("x", 228) + ":20000:0:99999:7:::\n", // 254 bytes
	} {
		if _, err := accounts.ParseShadow([]byte(data)); err != nil {
			t.Errorf("%q: %v", data, err)
		}
	}
}

// TestPassword: the password state beside the lock (slice 5, unit (b)),
// in the forms the VMs' /etc/shadow has (s5/00-inspect-*.log, 2026-11-03,
// first character and length only): the login user's "!" of length 1 on
// Debian 13 and Ubuntu 26.04, "*" on Alpine 3.23, a yescrypt hash ("$",
// 73 long) for the second user, "!" of length 2 for root on Alpine and
// system accounts; and an empty field, a "!" before a hash, a field no
// hash is. With no line in /etc/shadow, /etc/passwd's field, where "x"
// is none.
func TestPassword(t *testing.T) {
	yescrypt := "$y$j9T$" + strings.Repeat("Ab3", 22) // 73 characters, synthetic
	for _, c := range []struct {
		field string
		want  accounts.Password
		text  string
	}{
		{"!", accounts.PasswordNone, "none"},
		{"*", accounts.PasswordNone, "none"},
		{"!!", accounts.PasswordNone, "none"},
		{"!" + yescrypt, accounts.PasswordNone, "none"},
		{yescrypt, accounts.PasswordSet, "set"},
		{hashAlice, accounts.PasswordSet, "set"},
		{"", accounts.PasswordEmpty, "empty"},
		{"$6$a b", accounts.PasswordNone, "none"},
		{"$6$a;b", accounts.PasswordNone, "none"},
	} {
		s, err := accounts.ParseShadow([]byte("george:" + c.field + ":20745:0:99999:7:::\n"))
		if err != nil {
			t.Fatalf("%q: %v", c.field, err)
		}
		if p := s.PasswordOf(accounts.User{Name: "george"}); p != c.want || p.String() != c.text {
			t.Errorf("%q: %v, want %v", c.field, p, c.want)
		}
		if l := s["george"].Lock; l != accounts.Locked && c.field != "" && c.field[0] == '!' {
			t.Errorf("%q: lock %v, unchanged by the password", c.field, l)
		}
	}
	us, err := accounts.ParsePasswd([]byte("george:x:1000:1000::/home/george:/bin/bash\nalice:" + hashAlice + ":1001:1001::/home/alice:/bin/bash\n"))
	if err != nil {
		t.Fatal(err)
	}
	empty := accounts.Shadow{}
	if p, q := empty.PasswordOf(us[0]), empty.PasswordOf(us[1]); p != accounts.PasswordNone || q != accounts.PasswordSet {
		t.Errorf("passwd's fields: %v %v", p, q)
	}
	if accounts.PasswordNotRead.String() != "not read" {
		t.Error(accounts.PasswordNotRead.String())
	}
}

// TestShadowPrivacy: no run of four characters of a password hash (the
// field after its "!") leaves the reader, in what it returns, in its
// errors, or in the evidence a lock gives.
func TestShadowPrivacy(t *testing.T) {
	leaks := func(where, out string) {
		t.Helper()
		for _, h := range []string{hashAlice, hashBob} {
			for i := 0; i+4 <= len(h); i++ {
				if strings.Contains(out, h[i:i+4]) {
					t.Errorf("%s holds %q of a hash: %s", where, h[i:i+4], out)
				}
			}
		}
	}
	s, err := accounts.ParseShadow([]byte(shadow))
	leaks("ParseShadow", fmt.Sprintf("%#v %+v %v", s, s, err))
	for _, u := range []string{"alice", "bob"} {
		l, at := s.LockOf(accounts.User{Name: u})
		leaks("LockOf", fmt.Sprintf("%#v %v %s", l, l, at))
		p := s.PasswordOf(accounts.User{Name: u})
		leaks("PasswordOf", fmt.Sprintf("%#v %v", p, p))
	}
	h := hostfake.New(host.Options{})
	h.AddFile(accounts.ShadowFile, []byte(shadow), 0o640, 0, 42)
	s, err = accounts.ReadShadow(context.Background(), h)
	leaks("ReadShadow", fmt.Sprintf("%#v %v", s, err))
	_, err = accounts.ParseShadow([]byte("alice:" + hashAlice + ":1\n"))
	leaks("an error", fmt.Sprint(err))
	us, err := accounts.ParsePasswd([]byte("alice:" + hashAlice + ":1000:1000::/home/alice:/bin/sh\n"))
	leaks("ParsePasswd", fmt.Sprintf("%#v %v", us, err))
	_, err = accounts.ParsePasswd([]byte("alice:" + hashBob + ":x:1000::/home/alice:/bin/sh\n"))
	leaks("a passwd error", fmt.Sprint(err))
}

// TestReadShadow: one read through host.System, and a failed read
// wrapped, never taken for an empty file.
func TestReadShadow(t *testing.T) {
	h := hostfake.New(host.Options{})
	h.AddFile(accounts.ShadowFile, []byte(shadow), 0o640, 0, 42)
	if s, err := accounts.ReadShadow(context.Background(), h); err != nil || len(s) != 6 {
		t.Fatalf("ReadShadow: %v, %v", s, err)
	}
	if calls := h.Calls(); len(calls) != 2 || calls[0].Op != "stat" || calls[0].Path != accounts.TCBDir || calls[1].Op != "read" || calls[1].Path != accounts.ShadowFile {
		t.Errorf("calls %v", calls)
	}
	// musl's getspnam reads /etc/tcb/<name>/shadow first: not established,
	// nor where whether /etc/tcb exists is not.
	if s, err := accounts.ReadShadow(context.Background(), tcbRefused{h}); !errors.Is(err, fs.ErrPermission) || s != nil {
		t.Errorf("tcb refused: %v, %v", s, err)
	}
	h.AddDir(accounts.TCBDir, 0o755, 0, 0)
	if s, err := accounts.ReadShadow(context.Background(), h); !errors.Is(err, accounts.ErrTCB) || s != nil {
		t.Errorf("tcb: %v, %v", s, err)
	}
	h = hostfake.New(host.Options{})
	if s, err := accounts.ReadShadow(context.Background(), h); !errors.Is(err, fs.ErrNotExist) || s != nil {
		t.Errorf("no shadow: %v, %v", s, err)
	}
	h.AddFile(accounts.PasswdFile, []byte(passwd), 0o644, 0, 0)
	if us, err := accounts.ReadPasswd(context.Background(), h); err != nil || len(us) != 3 {
		t.Errorf("ReadPasswd: %v, %v", us, err)
	}
}

// tcbRefused is a host whose Stat of /etc/tcb is refused.
type tcbRefused struct{ *hostfake.System }

func (h tcbRefused) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if name == accounts.TCBDir {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrPermission}
	}
	return h.System.Stat(ctx, name)
}
