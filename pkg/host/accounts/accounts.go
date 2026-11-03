// Package accounts reads the local account databases, /etc/passwd and
// /etc/group, through host.System (refactor plan Phase 4,
// next/p4-check-users): every account and every group, in file order, for
// a check that needs them all rather than one lookup (host.System's
// LookupUser and LookupGroup find one name). It reads only. Of
// /etc/shadow (next/p4-locked) and of the password field of /etc/passwd it
// keeps only what the field says of the account's lock (Lock) and of its
// password (Password), never the field (shadow.go).
//
// A line in neither file's form is an error (ErrMalformed), never skipped:
// a line hardn cannot read may be the account or the group a check is
// about. Blank lines and lines starting with '#' are skipped, as glibc's
// files backend skips them [I]. Accounts only NSS knows (LDAP, sssd) are
// not here.
//
// docs/engine/check-users.md describes the reader with the check that uses
// it.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/abbott/hardn/pkg/host"
)

// The files read.
const (
	PasswdFile = "/etc/passwd"
	GroupFile  = "/etc/group"
)

// ErrMalformed: a line of PasswdFile or GroupFile is not in its form.
var ErrMalformed = errors.New("not a line of the account database")

// User is one line of PasswdFile.
type User struct {
	Name     string
	UID, GID int // GID is the primary group's
	Home     string
	Shell    string // as written: "" means /bin/sh (passwd(5))
	Line     int    // its line in PasswdFile, from 1
	// PasswdLock is the lock state of its password field here, which
	// sshd reads when ShadowFile has no line for it (Shadow.LockOf).
	PasswdLock Lock
	// PasswdPassword is the password state of that field, read where
	// ShadowFile has no line for it (Shadow.PasswordOf).
	PasswdPassword Password
}

// Group is one line of GroupFile.
type Group struct {
	Name    string
	GID     int
	Members []string // the users it lists, not those whose primary group it is
	Line    int
}

// DB is the two files as read.
type DB struct {
	Users  []User
	Groups []Group
}

// Read reads and parses PasswdFile and GroupFile. It changes nothing.
func Read(ctx context.Context, sys host.System) (DB, error) {
	var db DB
	p, err := sys.ReadFile(ctx, PasswdFile)
	if err != nil {
		return db, fmt.Errorf("accounts: %w", err)
	}
	g, err := sys.ReadFile(ctx, GroupFile)
	if err != nil {
		return db, fmt.Errorf("accounts: %w", err)
	}
	if db.Users, err = ParsePasswd(p); err != nil {
		return db, err
	}
	db.Groups, err = ParseGroup(g)
	return db, err
}

// ReadPasswd reads and parses PasswdFile alone. It changes nothing.
func ReadPasswd(ctx context.Context, sys host.System) ([]User, error) {
	p, err := sys.ReadFile(ctx, PasswdFile)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	return ParsePasswd(p)
}

// ParsePasswd parses PasswdFile's content: name:password:uid:gid:gecos:home:shell.
func ParsePasswd(data []byte) ([]User, error) {
	var us []User
	err := each(data, PasswdFile, 7, func(n int, f []string) error {
		uid, err1 := id(f[2])
		gid, err2 := id(f[3])
		if f[0] == "" || err1 != nil || err2 != nil {
			return ErrMalformed
		}
		us = append(us, User{Name: f[0], UID: uid, GID: gid, Home: f[5], Shell: f[6], Line: n, PasswdLock: lockOf(f[1]), PasswdPassword: passwdPassword(f[1])})
		return nil
	})
	return us, err
}

// passwdPassword is the password state of PasswdFile's field: "x" says
// the password is in ShadowFile (passwd(5)), so with no line there, none.
func passwdPassword(field string) Password {
	if field == "x" {
		return PasswordNone
	}
	return passwordOf(field)
}

// ParseGroup parses GroupFile's content: name:password:gid:member,member,
// with no blank in a member.
func ParseGroup(data []byte) ([]Group, error) {
	var gs []Group
	err := each(data, GroupFile, 4, func(n int, f []string) error {
		gid, err := id(f[2])
		if f[0] == "" || err != nil {
			return ErrMalformed
		}
		g := Group{Name: f[0], GID: gid, Line: n}
		for _, m := range strings.Split(f[3], ",") {
			switch {
			case strings.ContainsFunc(m, unicode.IsSpace):
				// glibc skips a member's leading blanks [I]; others may
				// not: which user it names is not established.
				return ErrMalformed
			case m != "":
				g.Members = append(g.Members, m)
			}
		}
		gs = append(gs, g)
		return nil
	})
	return gs, err
}

// each calls fn with each line's number and its fields, which must be
// fields many; blank and comment lines are skipped.
func each(data []byte, file string, fields int, fn func(int, []string) error) error {
	for i, l := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(l); t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Split(l, ":")
		err := ErrMalformed
		if len(f) == fields {
			err = fn(i+1, f)
		}
		if err != nil {
			return fmt.Errorf("accounts: %s:%d: %w", file, i+1, err)
		}
	}
	return nil
}

// id is a uid or gid: decimal, 0 to 2^32-2.
func id(s string) (int, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n == 1<<32-1 {
		return 0, ErrMalformed
	}
	return int(n), nil
}

// Member reports whether u is in the group named group: the first group of
// that name, as getgrnam finds it [I], whose GID is u's primary group or
// which lists u.
func (db DB) Member(u User, group string) bool {
	i := slices.IndexFunc(db.Groups, func(g Group) bool { return g.Name == group })
	if i < 0 {
		return false
	}
	gid := db.Groups[i].GID
	if u.GID == gid {
		return true
	}
	for _, g := range db.Groups {
		if g.GID == gid && slices.Contains(g.Members, u.Name) {
			return true
		}
	}
	return false
}
