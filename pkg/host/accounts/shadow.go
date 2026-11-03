package accounts

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/host"
)

// ShadowFile is the shadow password file (refactor plan Phase 4,
// next/p4-locked). Of each line it keeps the name and what the first
// character of the password field says of the lock sshd checks without PAM
// (Lock), never the field: a password hash is read through host.System,
// as the file is, and nothing of it is kept, returned or logged.
const ShadowFile = "/etc/shadow"

// TCBDir is the directory of Openwall's tcb shadow files, which musl's
// getspnam reads before ShadowFile (/etc/tcb/<name>/shadow, src/passwd/
// getspnam_r.c), as glibc's does with nss_tcb [I].
const TCBDir = "/etc/tcb"

// ErrTCB: TCBDir exists, so the password field sshd reads may be in a
// file hardn does not read.
var ErrTCB = errors.New(TCBDir + " exists: getspnam may read it before " + ShadowFile)

// lineMax is the longest line, without its newline, that musl's getspnam
// reads whole: it reads into 256 bytes (getspnam.c's LINE_LIM), the
// newline and a NUL included, and finds no entry for an account whose
// line is longer (ERANGE).
const lineMax = 254

// Lock is what an account's password field says of the lock sshd checks
// when it does not use PAM: platform.c's platform_locked_account()
// compares the field with LOCKED_PASSWD_PREFIX, which configure.ac
// defines as "!" on Linux, and an empty field is never locked. It is read
// from the field's first character alone.
type Lock uint8

// The lock states. The zero value is not read.
const (
	LockNotRead Lock = iota
	NotLocked        // a first character other than "!": "*", "x", a hash's "$"
	Locked           // a first character "!": "!", "!!", "!" before a hash (passwd -l, usermod -L)
	Empty            // an empty field: not locked
)

// String is the lock as the evidence says it.
func (l Lock) String() string {
	switch l {
	case NotLocked:
		return "not locked"
	case Locked:
		return "locked (!)"
	case Empty:
		return "empty, not locked"
	}
	return "not read"
}

// lockOf is the lock state of a password field, from its first character.
func lockOf(field string) Lock {
	switch {
	case field == "":
		return Empty
	case field[0] == '!':
		return Locked
	}
	return NotLocked
}

// Password is what an account's password field says of a password a tool
// that asks one (sudo, doas, through PAM or the field itself) could check:
// one set, none, or an empty field. It is read from the field alone, never
// kept (slice 5, unit (b); gallery-users.md §7 item 3): Lock folds "*" and
// a hash into NotLocked, which sshd's lock reads alike, and a grant that
// asks a password does not.
type Password uint8

// The password states. The zero value is not read.
const (
	PasswordNotRead Password = iota
	PasswordSet              // a usable hash: not empty, not starting "!" or "*", no character crypt(5) never writes
	PasswordNone             // "*", "!", "!" before a hash, or any other field no password matches
	PasswordEmpty            // "": no password at all, which login and su may take as none asked [I]
)

// String is the password as the evidence says it.
func (p Password) String() string {
	switch p {
	case PasswordSet:
		return "set"
	case PasswordNone:
		return "none"
	case PasswordEmpty:
		return "empty"
	}
	return "not read"
}

// passwordOf is the password state of a field. A field that is not empty
// and starts with neither "!" nor "*" is a hash, unless it holds a
// character crypt(5) says no hash holds (white space, ';', '!', '*', a
// backslash; ':' ends the field) [I: libxcrypt's crypt(5), not read
// here]: no password hashes to such a field.
func passwordOf(field string) Password {
	switch {
	case field == "":
		return PasswordEmpty
	case field[0] == '!' || field[0] == '*', strings.ContainsAny(field, " \t;\\!*"):
		return PasswordNone
	}
	return PasswordSet
}

// ShadowEntry is one name's first line of ShadowFile: its lock state, its
// password state, and the line, from 1.
type ShadowEntry struct {
	Lock     Lock
	Password Password
	Line     int
}

// Shadow is ShadowFile as read: each name's first line, as getspnam finds
// it (musl's getspnam_r; glibc's [I]).
type Shadow map[string]ShadowEntry

// ReadShadow reads and parses ShadowFile. Where TCBDir exists (ErrTCB),
// or its Stat fails otherwise than for its absence, getspnam may read an
// account's tcb file instead: an error. It changes nothing.
func ReadShadow(ctx context.Context, sys host.System) (Shadow, error) {
	switch _, err := sys.Stat(ctx, TCBDir); {
	case err == nil:
		return nil, fmt.Errorf("accounts: %w", ErrTCB)
	case !errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("accounts: %w", err)
	}
	data, err := sys.ReadFile(ctx, ShadowFile)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	return ParseShadow(data)
}

// ParseShadow parses ShadowFile's content: name:password:lastchg:min:max:
// warn:inactive:expire:reserved, nine fields (shadow(5)). Of each line it
// keeps the name, its lock state and its password state. A line that musl's getspnam would
// pass over, or for which it would find no entry, is ErrMalformed, whose
// message names the line, never its content (src/passwd/getspnam_r.c):
// another number of fields, a numeric field neither empty nor decimal
// digits (a CRLF line's last field included), a line longer than
// lineMax, or a last line with no newline. sshd would then read the
// account's PasswdFile field, or a later line of the same name.
func ParseShadow(data []byte) (Shadow, error) {
	s := Shadow{}
	last := strings.Count(string(data), "\n") + 1 // reached only when it holds a line: no final newline
	err := each(data, ShadowFile, 9, func(n int, f []string) error {
		if f[0] == "" || n == last || len(strings.Join(f, ":")) > lineMax || slices.ContainsFunc(f[2:], notNumber) {
			return ErrMalformed
		}
		if _, ok := s[f[0]]; !ok {
			s[f[0]] = ShadowEntry{Lock: lockOf(f[1]), Password: passwordOf(f[1]), Line: n}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// notNumber reports whether a numeric field is neither empty nor decimal
// digits alone, which musl's __parsespent refuses (xatol).
func notNumber(field string) bool {
	return strings.ContainsFunc(field, func(r rune) bool { return r < '0' || r > '9' })
}

// LockOf is u's lock state as sshd reads it, and where it was read
// ("file:line"): u's line in ShadowFile, else, with none, its PasswdFile
// field (platform_locked_account() reads sp_pwdp when getspnam finds the
// account, else pw_passwd).
func (s Shadow) LockOf(u User) (Lock, string) {
	if e, ok := s[u.Name]; ok {
		return e.Lock, ShadowFile + ":" + strconv.Itoa(e.Line)
	}
	return u.PasswdLock, PasswdFile + ":" + strconv.Itoa(u.Line)
}

// PasswordOf is u's password state as a tool that asks one reads it: u's
// line in ShadowFile, else, with none, its PasswdFile field (pam_unix and
// doas's shadow read take the shadow entry where there is one [I]).
func (s Shadow) PasswordOf(u User) Password {
	if e, ok := s[u.Name]; ok {
		return e.Password
	}
	return u.PasswdPassword
}
