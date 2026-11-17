package main

import (
	"context"
	"fmt"
	"time"

	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/screen"
)

// The Account Review mocks (account hygiene;
// docs/engine/gallery-accounts.md, adopted 2026-11-13): read only, a listing
// alone. Each mock is a reading of the User control (usersctl.Review),
// written here by hand, drawn through hardn's own mapping
// (usrscreen.AccountsView), as hardn's User Management screen draws it
// (docs/engine/screen-accounts.md). No host is read here: "play users" runs
// the screen itself on the simulated hosts, item 3 with it.

// accountsPrefix names the listing and its goldens.
const accountsPrefix = "accounts"

// accountsLabel is the source label every mock's note opens with.
const accountsLabel = "Proposed: no Figma frame."

// accountsDay is a last login on the day y-m-d.
func accountsDay(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 10, 0, 0, 0, time.UTC)
}

// accountsFound are the accounts of the host with something of each kind.
func accountsFound() screen.Accounts {
	return usrscreen.AccountsView(usersctl.Review{
		EmptyPassword: []string{"carol"},
		NeverExpires:  []string{"george", "alice"},
		Stale: []usersctl.Stale{{Name: "alice", Last: accountsDay(2026, 3, 2)}, {Name: "deploy"},
			{Name: "backup-operator", Last: accountsDay(2025, 11, 20)}},
	})
}

// accountsUsers is the User Management screen over a, with two users, as
// hardn's screen draws them on Debian 13.
func accountsUsers(a screen.Accounts) screen.Users {
	return screen.Users{
		Status: component.StatusList{Rows: []component.StatusRow{{Label: "User(s)", State: component.StatusConfigured, Detail: "non-root, sudo"}}},
		Tool:   "sudo",
		Users: []screen.UsersSection{
			{Name: "george", Rows: []component.KeyValue{{Key: "Sudo", Value: "Enabled", State: component.ValueOK}, {Key: screen.UsersByKey, Value: "group sudo"},
				screen.UsersKeysRow(2), {Key: screen.UsersPasswordKey, Value: "Set"}, {Key: "UID:GID", Value: "1000:1000"}}, Login: "Oct 20 08:14 from 192.0.2.10"},
			{Name: "alice", Rows: []component.KeyValue{{Key: "Sudo", Value: "Disabled"}, screen.UsersKeysRow(1), {Key: screen.UsersPasswordKey, Value: "Set"}, {Key: "UID:GID", Value: "1001:1001"}},
				Login: "Oct 16 17:42 on tty1"},
		},
		Review: &a,
	}
}

// accountsMock is one mock: its screen and what it shows.
type accountsMock struct {
	name, note string
	vm         func() screen.SubMenu
}

// accountsMocks are the mocks, in the listing's order.
func accountsMocks() []accountsMock {
	clean := usrscreen.AccountsView(usersctl.Review{})
	noRecord := usrscreen.AccountsView(usersctl.Review{EmptyPassword: []string{"carol"}, NoRecord: true})
	noRecent := usrscreen.AccountsView(usersctl.Review{EmptyPassword: []string{"carol"}, NoRecent: true})
	noLogins := usrscreen.AccountsView(usersctl.Review{EmptyPassword: []string{"carol"}, LoginErr: host.ErrNotFound})
	notRead := usrscreen.AccountsView(usersctl.Review{Err: fmt.Errorf("accounts: %w", accounts.ErrTCB)})
	return []accountsMock{
		{"menu", "User Management with the new item 3, which says how many accounts there are to review. Items 1 and 2 are unchanged.",
			func() screen.SubMenu { return accountsUsers(accountsFound()).SubMenu() }},
		{"found", "Item 3's screen: carol has an empty password, george and alice have passwords that never expire, alice, deploy and backup-operator have no login in 90 days. Each row is the command to run; hardn changes nothing.",
			func() screen.SubMenu { return accountsFound().SubMenu() }},
		{"no-login-record", "A host with no login record, as Alpine: the last section says so. The other two are read from shadow as anywhere.",
			noRecord.SubMenu},
		{"no-recent-login", "A host whose login record has no login of anyone in 90 days, so it may no longer be written: the last section says that and lists no one.",
			noRecent.SubMenu},
		{"login-not-read", "A host with a login record hardn can't read, here lslogins not installed: the last section says so and lists no one.",
			noLogins.SubMenu},
		{"not-read", "A host whose accounts hardn can't read, here /etc/tcb: the screen says why and lists nothing.",
			notRead.SubMenu},
		{"not-read-menu", "User Management on that host: item 3 says the accounts can't be read, never \"Nothing to review\".",
			func() screen.SubMenu { return accountsUsers(notRead).SubMenu() }},
		{"clean", "A clean host: nothing in any section.", clean.SubMenu},
		{"clean-menu", "User Management on the clean host: item 3 says there is nothing to review.",
			func() screen.SubMenu { return accountsUsers(clean).SubMenu() }},
	}
}

// accountsListMocks are the listing's mocks: each mock's screen.
func accountsListMocks() []listMock {
	var out []listMock
	for _, m := range accountsMocks() {
		out = append(out, listMock{name: m.name, note: m.note, draw: func(_ context.Context, c component.Ctx) (string, error) {
			return m.vm().Render(c), nil
		}})
	}
	return out
}

// accountsUsage is the listing's usage line.
const accountsUsage = "usage: go run ./internal/tools/uigallery accounts [flags] [mock | all]..."

// accountsListing is the Account Review listing.
var accountsListing = listing{prefix: accountsPrefix, usage: accountsUsage, label: accountsLabel, mocks: accountsListMocks}
