package screen

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/ui/component"
)

// The Account Review screen (account hygiene, adopted 2026-11-13:
// docs/engine/gallery-accounts.md; docs/engine/screen-accounts.md): a
// read-only sub-screen of User Management that lists accounts with an empty
// password, accounts whose password never expires and accounts with no
// login for AccountsStaleDays, each row with the command that fixes it.
// hardn changes nothing here. There is no Figma frame. pkg/ui cannot reach
// the host: pkg/screens/users fills Accounts from the User control's
// reading.

// The screen's words, adopted (gallery-accounts.md, "Words"); the two that
// say a reading failed are the real unit's.
const (
	accountsTitle        = "Account Review"
	accountsLabel        = "Account Review"
	accountsEmptySection = "Empty password"
	accountsNeverSection = "Password never expires"
	accountsStaleSection = "No login in %d days"
	accountsNone         = "None"
	accountsNoRecord     = "This host keeps no login record"
	accountsNoRecent     = "The login record has no login in %d days"
	accountsHeading      = "Accounts"
	accountsCantRead     = "Can't read the accounts"
	accountsNoLogins     = "Can't read the login record"
	accountsNothing      = "Nothing to review"
	accountsOne          = "1 account to review"
	accountsMany         = "%d accounts to review"
	accountsNeverLogin   = "Never,"
	accountsLastLogin    = "%s,"

	// The fixes, each a command the user runs.
	accountsLockFix   = "Run passwd -l %s"
	accountsExpireFix = "Run chage -M %d %s"
	accountsStaleFix  = "run usermod -e 1 %s"
)

// AccountsStaleDays is how long without a login makes an account stale;
// AccountsMaxDays the password age the fix for one that never expires sets.
const (
	AccountsStaleDays = 90
	AccountsMaxDays   = 365
)

// UsersReviewItem is Account Review's key on the users' screen.
const UsersReviewItem = "3"

// AccountsStale is an account with no login for AccountsStaleDays.
type AccountsStale struct {
	// Name is the account's; Last the day of its last login, as
	// "2026-03-02", "" for an account that never logged in.
	Name, Last string
}

// Accounts is the Account Review screen's reading of the host, which its
// caller fills.
type Accounts struct {
	// EmptyPassword are the accounts whose password field is empty.
	EmptyPassword []string
	// NeverExpires are the accounts with a password and no maximum age.
	NeverExpires []string
	// Stale are the accounts with no login for AccountsStaleDays.
	Stale []AccountsStale
	// NoLoginRecord: the host keeps no last-login record, so Stale is not
	// read; the section says so.
	NoLoginRecord bool
	// NoRecentLogin: the record was read and has no login of any account
	// in AccountsStaleDays, so it may no longer be written; the section
	// says that and lists no one, whatever Stale holds.
	NoRecentLogin bool
	// LoginNotRead: the host has a login record that could not be read;
	// the section says so, whatever Stale holds.
	LoginNotRead bool
	// NotRead is why the accounts are not read, in the control's words;
	// the screen then says it and lists nothing.
	NotRead string
}

// Count is how many accounts the screen lists, one counted once: none of
// Stale where the section says the record is not there, not read or has no
// recent login, and
// none at all with the accounts not read.
func (a Accounts) Count() int {
	seen := map[string]bool{}
	if a.NotRead != "" {
		return 0
	}
	if a.NoLoginRecord || a.NoRecentLogin || a.LoginNotRead {
		a.Stale = nil
	}
	for _, n := range a.EmptyPassword {
		seen[n] = true
	}
	for _, n := range a.NeverExpires {
		seen[n] = true
	}
	for _, s := range a.Stale {
		seen[s.Name] = true
	}
	return len(seen)
}

// item is Account Review's item on the users' screen: how many accounts
// there are to review; what could not be read where that leaves none
// listed, never "Nothing to review".
func (a Accounts) item() component.MenuItem {
	it := component.MenuItem{Key: UsersReviewItem, Label: accountsLabel, Description: accountsNothing}
	if a.NotRead != "" {
		it.Description = accountsCantRead
		return it
	}
	switch n := a.Count(); {
	case n == 0 && a.LoginNotRead:
		it.Description = accountsNoLogins
	case n == 1:
		it.Description = accountsOne
	case n > 1:
		it.Description = fmt.Sprintf(accountsMany, n)
	}
	return it
}

// accountsSection is a section of rows, the muted line none where it has
// no row.
func accountsSection(title, none string, rows []component.KeyValue) Section {
	if len(rows) == 0 {
		rows = []component.KeyValue{{Key: none, State: component.ValueNone}}
	}
	return usersRows(title, rows)
}

// accountsNotRead is the one section of a screen whose accounts were not
// read: why, muted.
func accountsNotRead(why string) Section {
	return usersRows(accountsHeading, []component.KeyValue{usersNone(why, "")})
}

// accountsArg is name as a command's argument: as it is where it is
// letters, digits, ".", "_" and "-" alone, not starting with "-"; else in
// single quotes, so that the shell takes it as one word (a name starting
// with "-", which useradd refuses, is still an option to the command).
func accountsArg(name string) string {
	plain := name != "" && name[0] != '-'
	for _, r := range name {
		plain = plain && (r == '.' || r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
	}
	if plain {
		return name
	}
	return "'" + strings.ReplaceAll(name, "'", `'\''`) + "'"
}

// sections are the three lists, each row an account and its fix: an empty
// password bad, a stale account a warning, a password that never expires
// plain.
func (a Accounts) sections() []Section {
	if a.NotRead != "" {
		return []Section{accountsNotRead(a.NotRead)}
	}
	stale := fmt.Sprintf(accountsStaleSection, AccountsStaleDays)
	var empty, never, old []component.KeyValue
	for _, n := range a.EmptyPassword {
		empty = append(empty, component.KeyValue{Key: n, Value: fmt.Sprintf(accountsLockFix, accountsArg(n)), State: component.ValueBad})
	}
	for _, n := range a.NeverExpires {
		never = append(never, component.KeyValue{Key: n, Value: fmt.Sprintf(accountsExpireFix, AccountsMaxDays, accountsArg(n))})
	}
	for _, s := range a.Stale {
		last := accountsNeverLogin
		if s.Last != "" {
			last = fmt.Sprintf(accountsLastLogin, s.Last)
		}
		old = append(old, component.KeyValue{Key: s.Name, Value: last, Note: fmt.Sprintf(accountsStaleFix, accountsArg(s.Name)), State: component.ValueWarn, KeepNote: true})
	}
	none := accountsNone
	switch {
	case a.LoginNotRead:
		none, old = accountsNoLogins, nil
	case a.NoLoginRecord:
		none, old = accountsNoRecord, nil
	case a.NoRecentLogin:
		none, old = fmt.Sprintf(accountsNoRecent, AccountsStaleDays), nil
	}
	return []Section{
		accountsSection(accountsEmptySection, accountsNone, empty),
		accountsSection(accountsNeverSection, accountsNone, never),
		accountsSection(stale, none, old),
	}
}

// SubMenu returns the screen: the title bar; the three sections; Return
// alone; "Option". It has no item: the screen only reads.
func (a Accounts) SubMenu() SubMenu {
	return SubMenu{
		Title:    trail(usersTitle, accountsTitle),
		Sections: a.sections(),
		Menu:     component.Menu{Exit: returnItem()},
		Prompt:   menuPrompt,
	}
}
