package screen

import (
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// The Account Review screen (accounts.go). Synthetic values only.
var accTestFound = Accounts{EmptyPassword: []string{"carol"}, NeverExpires: []string{"george", "alice"},
	Stale: []AccountsStale{{Name: "alice", Last: "2026-03-02"}, {Name: "deploy"}}}

// accText is vm as drawn at 80 columns, with no colour.
func accText(vm SubMenu) string {
	return ansi.Strip(vm.Render(component.NewCtx(term.Fixed(80, colorprofile.TrueColor, false, true))))
}

// TestAccountsRows: each account's row is the command that fixes it, the
// last login before a stale account's; a section with none says None; the
// screen has Return alone.
func TestAccountsRows(t *testing.T) {
	got := accText(accTestFound.SubMenu())
	for _, want := range []string{"Empty password", "Run passwd -l carol", "Password never expires", "Run chage -M 365 george", "Run chage -M 365 alice",
		"No login in 90 days", "2026-03-02, run usermod -e 1 alice", "Never, run usermod -e 1 deploy"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if vm := accTestFound.SubMenu(); len(vm.Menu.Items) != 0 || vm.Title.Title != accountsTitle || vm.Title.Parents[0] != usersTitle {
		t.Errorf("menu %+v, title %+v", vm.Menu.Items, vm.Title)
	}
	if clean := accText(Accounts{}.SubMenu()); strings.Count(clean, "-  None") != 3 {
		t.Errorf("a clean host:\n%s", clean)
	}
}

// TestAccountsNoLoginRecord: with no login record the last section says
// so, whatever Stale holds, and the other two are drawn as anywhere.
func TestAccountsNoLoginRecord(t *testing.T) {
	a := accTestFound
	a.NoLoginRecord = true
	got := accText(a.SubMenu())
	if !strings.Contains(got, accountsNoRecord) || strings.Contains(got, "usermod") || !strings.Contains(got, "Run passwd -l carol") {
		t.Errorf("no login record:\n%s", got)
	}
	b := accTestFound
	b.NoRecentLogin = true
	got = accText(b.SubMenu())
	if !strings.Contains(got, "The login record has no login in 90 days") || strings.Contains(got, accountsNoRecord) || strings.Contains(got, "usermod") || !strings.Contains(got, "Run passwd -l carol") || b.Count() != 3 {
		t.Errorf("no recent login:\n%s", got)
	}
}

// TestAccountsItem: the users' screen has Account Review only with Review
// set, as item 3 after its own, and it counts each account once.
func TestAccountsItem(t *testing.T) {
	if vm := usrTestTwo.SubMenu(); len(vm.Menu.Items) != 2 {
		t.Errorf("without Review: %+v", vm.Menu.Items)
	}
	one := Accounts{EmptyPassword: []string{"carol"}, Stale: []AccountsStale{{Name: "carol"}}}
	for _, tt := range []struct {
		a    Accounts
		want string
	}{{Accounts{}, "Nothing to review"}, {one, "1 account to review"}, {accTestFound, "4 accounts to review"}} {
		u := usrTestTwo
		u.Review = &tt.a
		items := u.SubMenu().Menu.Items
		if it := items[len(items)-1]; len(items) != 3 || it.Key != UsersReviewItem || it.Label != "Account Review" || it.Description != tt.want {
			t.Errorf("%+v: items %+v, want %q", tt.a, items, tt.want)
		}
	}
}

// TestAccountsNotRead: with the accounts not read the screen says why and
// lists nothing, and the item says so, never "Nothing to review"; a login
// record not read is said in its section, and by the item where nothing
// else is listed.
func TestAccountsNotRead(t *testing.T) {
	a := accTestFound
	a.NotRead = "/etc/tcb exists."
	got := accText(a.SubMenu())
	if !strings.Contains(got, "/etc/tcb exists.") || strings.Contains(got, "carol") || strings.Contains(got, accountsEmptySection) || a.Count() != 0 || a.item().Description != accountsCantRead {
		t.Errorf("accounts not read: %q\n%s", a.item().Description, got)
	}
	b := accTestFound
	b.LoginNotRead, b.NoLoginRecord = true, true
	got = accText(b.SubMenu())
	if !strings.Contains(got, accountsNoLogins) || strings.Contains(got, accountsNoRecord) || strings.Contains(got, "usermod") || b.Count() != 3 || b.item().Description != "3 accounts to review" {
		t.Errorf("login record not read: %q\n%s", b.item().Description, got)
	}
	if it := (Accounts{LoginNotRead: true, Stale: []AccountsStale{{Name: "alice"}}}).item(); it.Description != accountsNoLogins {
		t.Errorf("nothing listed, login record not read: %q", it.Description)
	}
	if it := (Accounts{NoLoginRecord: true}).item(); it.Description != accountsNothing {
		t.Errorf("nothing listed, no login record: %q", it.Description)
	}
}

// TestAccountsArg: a name that is not a plain one is quoted in its command.
func TestAccountsArg(t *testing.T) {
	for name, want := range map[string]string{"carol": "carol", "backup-operator": "backup-operator", "a.b_c": "a.b_c", "svc$": "'svc$'",
		"x; reboot": "'x; reboot'", "-l": "'-l'", "o'brien": `'o'\''brien'`, "": "''"} {
		if got := accountsArg(name); got != want {
			t.Errorf("%q: %q, want %q", name, got, want)
		}
	}
	got := accText(Accounts{EmptyPassword: []string{"x; reboot"}}.SubMenu())
	if !strings.Contains(got, "Run passwd -l 'x; reboot'") {
		t.Errorf("a quoted name:\n%s", got)
	}
}
