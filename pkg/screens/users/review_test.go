package users_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	usersctl "github.com/abbott/hardn/pkg/engine/controls/users"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/hostfake"
	usrscreen "github.com/abbott/hardn/pkg/screens/users"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// Account Review on the screen (actions.go's drawReview, rows.go's
// AccountsView): User Management's item 3 and its screen, which read only.
// The clock is 2026-11-03 (helpers_test.go).

// reviewed is debian13 with carol, whose password is empty, and deploy, who
// never logged in; with a login record where logins is not nil: george
// logged in yesterday, alice in March.
func reviewed(logins *hostfake.Response) *usrHost {
	deb := debian13()
	h := usersOn(deb, true, george(deb), alice(),
		simUser{name: "carol", uid: 1002, pw: "", shell: "/bin/bash"}, simUser{name: "deploy", uid: 1003, pw: "!", shell: "/bin/bash"})
	h.env = map[string]string{"SUDO_USER": "george"}
	if logins != nil {
		h.AddFile(accounts.LastlogFile, nil, 0o644, 0, 0)
		h.logins = logins
	}
	return h
}

var reviewLogins = &hostfake.Response{Stdout: `USER="root" LAST-LOGIN=""
USER="george" LAST-LOGIN="2026-11-02T09:00:00+00:00"
USER="alice" LAST-LOGIN="2026-03-02T10:00:00+00:00"
USER="carol" LAST-LOGIN="2026-11-01T10:00:00+00:00"
USER="deploy" LAST-LOGIN=""
`}

// TestReviewScreen: item 3 counts the accounts, each once; its screen lists
// each with the command to run; 0 returns to User Management; nothing on
// the host changes, and nothing is asked.
func TestReviewScreen(t *testing.T) {
	for _, dry := range []bool{false, true} {
		h := reviewed(reviewLogins)
		p := play(h, dry, "3", "0", "0")
		if p.err != nil || p.unread != 0 || p.engines != 0 || len(p.runs) != 0 {
			t.Fatalf("dry %v: err %v, unread %d, engines %d, runs %d", dry, p.err, p.unread, p.engines, len(p.runs))
		}
		for _, w := range []string{"3) Account Review", "4 accounts to review", "Account Review ####",
			"Empty password", "carol", "Run passwd -l carol",
			"Password never expires", "Run chage -M 365 alice",
			"No login in 90 days", "2026-03-02, run usermod -e 1 alice", "Never, run usermod -e 1 deploy"} {
			if !shows(p.out, w) {
				t.Errorf("dry %v: no %q in:\n%s", dry, w, p.out)
			}
		}
		for _, w := range []string{"usermod -e 1 george", "usermod -e 1 carol", "passwd -l deploy", "chage -M 365 george", "never logged in", "This host keeps no login record"} {
			if shows(p.out, w) {
				t.Errorf("dry %v: %q shown", dry, w)
			}
		}
		if w := writes(h, 0); len(w) != 0 {
			t.Errorf("dry %v: changed the host: %v", dry, w)
		}
		if u := h.unanswered(); len(u) != 0 {
			t.Errorf("dry %v: unanswered %v", dry, u)
		}
	}
	// q on the review screen quits hardn.
	if p := play(reviewed(reviewLogins), false, "3", "q"); !errors.Is(p.err, nav.ErrQuit) || p.unread != 0 {
		t.Errorf("q: %v, unread %d", p.err, p.unread)
	}
}

// TestReviewNoRecord: a host with no login record says so, lists no one
// under it, never says an account never logged in, and runs no lslogins for
// it: the screen's own call alone, for the last logins.
func TestReviewNoRecord(t *testing.T) {
	h := reviewed(nil)
	p := play(h, false, "3", "0", "0")
	if p.err != nil || !shows(p.out, "This host keeps no login record") || !shows(p.out, "3 accounts to review") || shows(p.out, "Never,") || shows(p.out, "usermod") {
		t.Errorf("%v:\n%s", p.err, p.out)
	}
	if ran(h, 0, "lslogins --export") {
		t.Error("ran lslogins --export")
	}
}

// TestReviewLoginsNotRead: a record that is there and not read says so in
// the section, and lists shadow's two as anywhere; with nothing else to
// list, the item says it too, never "Nothing to review".
func TestReviewLoginsNotRead(t *testing.T) {
	for name, r := range map[string]hostfake.Response{
		"lslogins missing": {Err: host.ErrNotFound},
		"malformed":        {Stdout: "USER LAST-LOGIN\nroot \x1b[31m\n"},
	} {
		p := play(reviewed(&r), false, "3", "0", "0")
		if p.err != nil || !shows(p.out, "Can't read the login record") || !shows(p.out, "3 accounts to review") || shows(p.out, "usermod") || strings.Contains(p.out, "\x1b[31m") {
			t.Errorf("%s: %v:\n%s", name, p.err, p.out)
		}
	}
	v := usrscreen.AccountsView(usersctl.Review{LoginErr: host.ErrNotFound})
	users := screen.Users{Review: &v}
	if out := flat(strip(users.SubMenu().Render(plainCtx()))); !strings.Contains(out, "Account Review Can't read the login record") || strings.Contains(out, "Nothing to review") {
		t.Errorf("the item:\n%s", out)
	}
}

// TestReviewNotRead: accounts hardn cannot read (/etc/tcb) are said so on
// the item and on the screen, with no list and no "None".
func TestReviewNotRead(t *testing.T) {
	v := usrscreen.AccountsView(usersctl.Review{Err: accounts.ErrTCB})
	if v.NotRead == "" || v.Count() != 0 {
		t.Fatalf("%+v", v)
	}
	out := flat(strip(v.SubMenu().Render(plainCtx())))
	if !strings.Contains(out, "/etc/tcb exists") || strings.Contains(out, "None") || strings.Contains(out, "Empty password") {
		t.Errorf("%s", out)
	}
	users := screen.Users{Review: &v}
	if out := flat(strip(users.SubMenu().Render(plainCtx()))); !strings.Contains(out, "Account Review Can't read the accounts") {
		t.Errorf("%s", out)
	}
}

// TestAccountsView: the mapping. Names from the host are made safe, a name
// the shell would not take as one word is quoted in its command, the last
// login is the day, and no login is "".
func TestAccountsView(t *testing.T) {
	v := usrscreen.AccountsView(usersctl.Review{
		EmptyPassword: []string{"a\x1b[31mb", "-rf"},
		NeverExpires:  []string{"al ice"},
		Stale:         []usersctl.Stale{{Name: "alice", Last: time.Date(2026, 3, 2, 23, 30, 0, 0, time.UTC)}, {Name: "deploy"}},
	})
	if len(v.Stale) != 2 || v.Stale[0] != (screen.AccountsStale{Name: "alice", Last: "2026-03-02"}) || v.Stale[1] != (screen.AccountsStale{Name: "deploy"}) {
		t.Errorf("stale: %+v", v.Stale)
	}
	out := v.SubMenu().Render(plainCtx())
	if strings.Contains(out, "\x1b[31m") || strings.Contains(strip(out), "\x1b") {
		t.Errorf("a control character drawn: %q", out)
	}
	for _, w := range []string{"Run passwd -l '-rf'", "Run chage -M 365 'al ice'"} {
		if !shows(out, w) {
			t.Errorf("no %q in:\n%s", w, out)
		}
	}
	if v := usrscreen.AccountsView(usersctl.Review{NoRecord: true, EmptyPassword: []string{"carol"}}); !v.NoLoginRecord || v.LoginNotRead || v.Count() != 1 {
		t.Errorf("no record: %+v", v)
	}
	if v := usrscreen.AccountsView(usersctl.Review{NoRecent: true, EmptyPassword: []string{"carol"}}); !v.NoRecentLogin || v.NoLoginRecord || v.LoginNotRead || v.Count() != 1 {
		t.Errorf("no recent login: %+v", v)
	}
	if v := usrscreen.AccountsView(usersctl.Review{LoginErr: accounts.ErrLogins}); v.NoLoginRecord || !v.LoginNotRead {
		t.Errorf("not read: %+v", v)
	}
}

// plainCtx draws plain at 120 columns, as play does.
func plainCtx() component.Ctx {
	return component.NewCtx(term.Resolve(term.Options{}, term.Facts{Width: 120}))
}

// strip is out without its styling.
func strip(out string) string { return escapes.ReplaceAllString(out, "") }
