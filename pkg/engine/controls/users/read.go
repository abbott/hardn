package users

import (
	"context"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// UIDMax is the last uid of a user the User Management screen lists:
// login.defs' UID_MAX as Debian 13 and Ubuntu 26.04 ship it
// (s5/00-inspect logs, 2026-11-03), not read from the host
// (gallery-users.md §5, Made here).
const UIDMax = 60000

// Reading is the host as the User Management screen draws it, as facts, no
// words: the tool Choose picks, the tools whose rules are not read, the
// session user, the User row (Check's), and each user.
type Reading struct {
	Choice  sudoers.Choice
	NotRead []NotRead
	// Invoker is who ran hardn through the tool (sudoers.Invoker: SUDO_USER,
	// DOAS_USER); "" when not known: a root login, su -, a name hardn does
	// not write into a rule, or no tool.
	Invoker string
	// Status and StatusErr are Check's, the main menu's User row.
	Status    engine.Status
	StatusErr error
	// Users are the accounts of uid MinUID to UIDMax with a login shell (or
	// one whose stat failed), the first line of each name, in
	// /etc/passwd's order.
	Users []User
	facts detect.Facts
}

// User is one user as the screen reads it: whether a tool makes it an
// admin (Tool, the one that decides; Decided, its rule; NoPassword, without
// a password: GrantsOf's), its password (accounts.Shadow.PasswordOf), and
// hardn's own grant for it (sudoers.Read and PolicyOf; PolicyErr why not
// read).
type User struct {
	accounts.User
	Admin      detect.Tri
	Tool       string
	Decided    Rule
	NoPassword bool
	Password   accounts.Password
	Policy     sudoers.Policy
	PolicyErr  error
}

// Read reads what the screen draws, changing nothing: Check's row, the
// accounts, the tools' rules (ReadElevation), /etc/shadow's password states,
// hardn's files for each user, and the session user getenv names (os.Getenv
// in hardn). An error where the accounts are not read.
func Read(ctx context.Context, sys host.System, f detect.Facts, getenv func(string) string) (Reading, error) {
	return read(ctx, sys, f, getenv, true)
}

// read is Read, with Check's row (status) or without, as Plan reads.
func read(ctx context.Context, sys host.System, f detect.Facts, getenv func(string) string, status bool) (Reading, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	r := Reading{facts: f, Choice: sudoers.Choose(f, sudoers.ToolUnknown)}
	if status {
		r.Status, r.StatusErr = Admin{}.Check(ctx, sys, f)
	}
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		return r, err
	}
	elev := ReadElevation(ctx, sys, f)
	r.NotRead = elev.NotRead()
	t := r.Choice.Tool
	known := t == sudoers.ToolSudo || t == sudoers.ToolDoas
	if known {
		r.Invoker, _ = sudoers.Invoker(t, getenv) // a name hardn does not write is not known
	}
	sh, shErr := accounts.ReadShadow(ctx, sys)
	var names []string
	seen := map[string]bool{}
	for _, u := range db.Users {
		first := !seen[u.Name]
		seen[u.Name] = true
		if !first || u.UID < MinUID || u.UID > UIDMax {
			continue
		}
		if login, _ := loginShell(ctx, sys, u.Shell); login == detect.No {
			continue
		}
		uu := User{User: u, Admin: detect.No}
		for _, g := range elev.GrantsOf(db, u) {
			if g.Grants == detect.Yes || (g.Grants == detect.Unknown && uu.Admin == detect.No) {
				uu.Admin, uu.Tool, uu.Decided, uu.NoPassword = g.Grants, g.Tool, g.Rule, g.NoPassword
			}
		}
		if shErr == nil {
			uu.Password = sh.PasswordOf(u)
		}
		r.Users = append(r.Users, uu)
		if sudoers.CheckUser(t, u.Name) == nil {
			names = append(names, u.Name)
		}
	}
	if known && len(r.NotRead) == 0 {
		st, err := sudoers.Read(ctx, sys, r.Choice, names)
		for i := range r.Users {
			if err != nil {
				r.Users[i].PolicyErr = err
				continue
			}
			r.Users[i].Policy, r.Users[i].PolicyErr = sudoers.PolicyOf(st, r.Users[i].Name)
		}
	}
	return r, nil
}

// user is the user named name.
func (r Reading) user(name string) (User, bool) {
	for _, u := range r.Users {
		if u.Name == name {
			return u, true
		}
	}
	return User{}, false
}

// toolWord is the tool the words name: doas where Choose picks it, else
// sudo.
func (r Reading) toolWord() string {
	if r.Choice.Tool == sudoers.ToolDoas {
		return "doas"
	}
	return "sudo"
}
