package details

import (
	"context"
	"time"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
)

// lslogins' sources, as the blank guard reads them (accounts.ReadLastLogins).
const (
	WtmpFile     = accounts.WtmpFile
	LastlogFile  = accounts.LastlogFile
	Lastlog2File = accounts.Lastlog2File
)

// LoginsState is what became of the one lslogins call (accounts.LoginsState).
type LoginsState = accounts.LoginsState

const (
	// LoginsUnread: it failed, it hit its bound, or a line did not parse (the
	// section's Unread says which). Never no login. The zero value.
	LoginsUnread = accounts.LoginsUnread
	// LoginsNoTool: lslogins is not installed, as on Alpine 3.23
	// (vm-runs/s1/01-tools.log).
	LoginsNoTool = accounts.LoginsNoTool
	// LoginsRead: it answered, and List holds a Login for each account asked.
	LoginsRead = accounts.LoginsRead
)

// LoginState is what the answer says of one account (accounts.LoginState).
type LoginState = accounts.LoginState

const (
	// LoginUnknown: nothing is known of the account. The zero value.
	LoginUnknown = accounts.LoginUnknown
	// LoginNone: a blank, where no source refuses and one holds records.
	LoginNone = accounts.LoginNone
	// LoginAt: its newest login with a terminal.
	LoginAt = accounts.LoginAt
)

// Logins is the Last Login section: root, then each listed user.
type Logins struct {
	State  LoginsState
	List   []Login // nil unless LoginsRead
	Unread []Unread
}

// Login is one account's answer (accounts.LastLogin).
type Login = accounts.LastLogin

// readLogins is decision 3 (a): one call for root and each listed user, wtmp
// among its sources, through the reader the User Management screen shares
// (accounts.ReadLastLogins); its blank guard under limit.
func readLogins(ctx context.Context, sys host.System, listed []User, limit time.Duration) Logins {
	names := []string{"root"}
	for _, u := range listed {
		names = append(names, u.Name)
	}
	l := accounts.ReadLastLogins(ctx, sys, names, accounts.LastLoginOptions{Guard: limit})
	if l.Err != nil {
		return Logins{State: l.State, Unread: []Unread{{SourceLslogins, l.Err}}}
	}
	return Logins{State: l.State, List: l.List}
}
