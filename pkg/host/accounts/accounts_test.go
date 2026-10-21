package accounts_test

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
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
		{Name: "root", UID: 0, GID: 0, Home: "/root", Shell: "/bin/bash", Line: 1},
		{Name: "alice", UID: 1000, GID: 1000, Home: "/home/alice", Shell: "/bin/bash", Line: 4},
		{Name: "bob", UID: 1001, GID: 27, Home: "/home/bob", Shell: "", Line: 5},
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
	for _, c := range h.Calls() {
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
