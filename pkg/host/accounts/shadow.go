package accounts

import (
	"context"
	"fmt"
	"strconv"

	"github.com/abbott/hardn/pkg/host"
)

// ShadowFile is the shadow password file (refactor plan Phase 4,
// next/p4-locked). Of each line it keeps the name and what the first
// character of the password field says of the lock sshd checks without PAM
// (Lock), never the field: a password hash is read through host.System,
// as the file is, and nothing of it is kept, returned or logged.
const ShadowFile = "/etc/shadow"

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

// ShadowEntry is one name's first line of ShadowFile: its lock state, and
// the line, from 1.
type ShadowEntry struct {
	Lock Lock
	Line int
}

// Shadow is ShadowFile as read: each name's first line, as getspnam finds
// it [I].
type Shadow map[string]ShadowEntry

// ReadShadow reads and parses ShadowFile. It changes nothing.
func ReadShadow(ctx context.Context, sys host.System) (Shadow, error) {
	data, err := sys.ReadFile(ctx, ShadowFile)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	return ParseShadow(data)
}

// ParseShadow parses ShadowFile's content: name:password:lastchg:min:max:
// warn:inactive:expire:reserved, nine fields (shadow(5)). Of each line it
// keeps the name and its lock state; a line in another form is
// ErrMalformed, whose message names the line, never its content.
func ParseShadow(data []byte) (Shadow, error) {
	s := Shadow{}
	err := each(data, ShadowFile, 9, func(n int, f []string) error {
		if f[0] == "" {
			return ErrMalformed
		}
		if _, ok := s[f[0]]; !ok {
			s[f[0]] = ShadowEntry{Lock: lockOf(f[1]), Line: n}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s, nil
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
