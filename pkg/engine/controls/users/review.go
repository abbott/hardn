package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
)

// Account Review's reading (account hygiene, adopted 2026-11-13:
// docs/engine/gallery-accounts.md §4; docs/engine/screen-accounts.md): the
// accounts with an empty password, those whose password never expires and
// those with no login for StaleDays, as facts, no words. It reads only:
// hardn changes none of them.

// StaleDays is how long without a login makes an account stale (decision
// 3: 90 days, fixed).
const StaleDays = 90

// Stale is an account with no login for StaleDays: Last its last login,
// the zero time where the record has none of it.
type Stale struct {
	Name string
	Last time.Time
}

// Review is what Account Review lists. With Err nothing is established.
type Review struct {
	// Err is why the accounts are not read: PasswdFile or ShadowFile
	// (accounts.ErrTCB, accounts.ErrMalformed, a failed read).
	Err error
	// EmptyPassword are the accounts, any uid, whose password field is
	// empty, as CodeEmptyPassword's; NeverExpires those with a password
	// set and no date for it to expire on (accounts.ShadowEntry); each
	// the first line of its name, in PasswdFile's order.
	EmptyPassword []string
	NeverExpires  []string
	// Stale are the accounts Check takes as able to log in (uid MinUID or
	// more, a login shell or one whose stat failed), not yet expired,
	// whose last login is more than StaleDays before the reading, or of
	// which the record has none; empty with NoRecord, NoRecent or LoginErr.
	Stale []Stale
	// NoRecord: the host keeps no login record: neither file is there
	// (accounts.ErrNoLoginRecord).
	NoRecord bool
	// NoRecent: the record is there and read, and has no login of any
	// account in the last StaleDays, so nothing shows that it is still
	// written: no account is called stale by it.
	NoRecent bool
	// LoginErr is why a record that is there is not read: lslogins
	// missing (host.ErrNotFound) or failing, its output not in its form
	// (accounts.ErrLogins), or an account of PasswdFile it did not list
	// (accounts.ErrLogins too).
	LoginErr error
}

// ReadReview reads PasswdFile, ShadowFile and the login record
// (accounts.ReadLogins) as they are at now, changing nothing.
func ReadReview(ctx context.Context, sys host.System, now time.Time) Review {
	us, err := accounts.ReadPasswd(ctx, sys)
	if err != nil {
		return Review{Err: err}
	}
	sh, err := accounts.ReadShadow(ctx, sys)
	if err != nil {
		return Review{Err: err}
	}
	var r Review
	var cs []string
	today := int(now.Unix() / 86400)
	seen := map[string]bool{}
	for _, u := range us {
		if seen[u.Name] {
			continue
		}
		seen[u.Name] = true
		if sh.PasswordOf(u) == accounts.PasswordEmpty {
			r.EmptyPassword = append(r.EmptyPassword, u.Name)
		}
		e, ok := sh[u.Name]
		if ok && e.Password == accounts.PasswordSet && e.NeverExpires {
			r.NeverExpires = append(r.NeverExpires, u.Name)
		}
		if u.UID < MinUID || (ok && e.Expire > 0 && e.Expire < today) { // an expired account: usermod -e 1 has run
			continue
		}
		if login, _ := loginShell(ctx, sys, u.Shell); login != detect.No {
			cs = append(cs, u.Name)
		}
	}
	ls, err := accounts.ReadLogins(ctx, sys)
	switch {
	case errors.Is(err, accounts.ErrNoLoginRecord):
		r.NoRecord = true
		return r
	case err != nil:
		r.LoginErr = err
		return r
	}
	limit := now.Add(-StaleDays * 24 * time.Hour)
	live := false
	for _, t := range ls {
		live = live || t.After(limit)
	}
	if !live {
		r.NoRecent = true
		return r
	}
	for _, name := range cs {
		t, ok := ls[name]
		if !ok {
			r.Stale, r.LoginErr = nil, fmt.Errorf("users: an account lslogins did not list: %w", accounts.ErrLogins)
			return r
		}
		if !t.After(limit) {
			r.Stale = append(r.Stale, Stale{Name: name, Last: t})
		}
	}
	return r
}
