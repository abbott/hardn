package users

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// New is the user Create makes: Name; Admin, hardn's grant for it, asking
// no password; Password, a password asked for admin rights, which a new
// user does not have (refused, decision 3 (a)); Keys, its public keys,
// each a pasted key or one of hardn.yml's sshKeys, none for no key.
type New struct {
	Name            string
	Admin, Password bool
	Keys            []string
}

// CodeNoKey: Create plans a user with no key, who has no password either,
// so cannot log in.
const CodeNoKey = "USERS_NO_KEY"

// nameForm is useradd's default NAME_REGEX on Debian
// (^[a-z][-a-z0-9_]*\$?$), with shadow's leading "_" (is_valid_user_name);
// nameMax its length, in bytes.
var nameForm = regexp.MustCompile(`^[a-z_][-a-z0-9_]*\$?$`)

const nameMax = 32

// homeOf is a new user's home, which Create states (--home-dir).
func homeOf(name string) string { return "/home/" + name }

// CheckName refuses a new user's name: one useradd would refuse (nameForm,
// nameMax), or hardn does not write into a rule (sudoers.CheckUser, doas's
// the stricter); one a user or group has (accounts.Read); one whose
// /home/<name> is there, as undoing Create runs userdel --remove, which
// would delete it. nil where Create may use it.
func (r Reading) CheckName(ctx context.Context, sys host.System, name string) error {
	if len(name) > nameMax || !nameForm.MatchString(name) || sudoers.CheckUser(sudoers.ToolDoas, name) != nil {
		return refuse(codeNameForm, ErrNameForm, nil, "Use lower-case letters, digits, - and _, starting with a letter or _, up to 32", "")
	}
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		return err
	}
	if slices.ContainsFunc(db.Users, func(u accounts.User) bool { return u.Name == name }) ||
		slices.ContainsFunc(db.Groups, func(g accounts.Group) bool { return g.Name == name }) {
		return refuse(codeNameTaken, ErrNameTaken, nil, name+" already exists", "")
	}
	switch _, err := sys.Lstat(ctx, homeOf(name)); {
	case err == nil:
		return refuse(codeHomeThere, ErrHomeThere, nil, homeOf(name)+" is already there, and undoing Create would delete it", "choose another name")
	case !errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("users: %w", err)
	}
	return nil
}

// CheckNew is why Plan refuses Create of n, in its order: CheckName's;
// Password (decision 3 (a)); with Admin, ToolRefusal's; with Keys,
// KeysRefusal's and newKeys'. nil where Create may go on. Create without
// admin rights works with no tool, as on Proxmox VE.
func (r Reading) CheckNew(ctx context.Context, sys host.System, n New) error {
	if err := r.CheckName(ctx, sys, n.Name); err != nil {
		return err
	}
	if n.Password {
		return refuse(codeNewPassword, ErrNewPassword, nil, "A new user has no password, so "+r.toolWord()+" couldn't ask for one", "")
	}
	if n.Admin {
		if err := r.ToolRefusal(); err != nil {
			return err
		}
	}
	if len(n.Keys) > 0 {
		if err := r.KeysRefusal(); err != nil {
			return err
		}
		if _, err := newKeys(n); err != nil {
			return err
		}
	}
	return nil
}

// newKeys are n's Keys parsed, in order, the same key (authkeys.Key.Same)
// once. One that is not a key is refused: in ParseKey's words where n has
// one, else naming which, by its place in Keys from 1 (proposed words).
func newKeys(n New) ([]authkeys.Key, error) {
	var ks []authkeys.Key
	for i, s := range n.Keys {
		k, err := authkeys.ParsePublic(s)
		if err != nil {
			why := pasteOneKey
			if len(n.Keys) > 1 {
				why = fmt.Sprintf("Key %d isn't an OpenSSH public key, such as ssh-ed25519 AAAA… name", i+1)
			}
			return nil, refuse(codeNotKey, err, nil, why, "")
		}
		if !slices.ContainsFunc(ks, k.Same) {
			ks = append(ks, k)
		}
	}
	return ks, nil
}

// planCreate plans Create of n, after CheckNew's refusals, as one plan
// (y/N): useradd with its home stated and created, the release's shell
// and, where sshd's PAM is not established as on, the password field "*"
// in place of useradd's "!", which sshd without PAM refuses a key login to
// (decision 3 (a); [I] not run on a VM), undone by userdel --remove; then,
// with Admin, hardn's grant asking no password, as Grant plans it; then,
// with Keys, every key in ~/.ssh/authorized_keys, one change (each
// authkeys.PlanAdd's line appended after the keys planned before it, a key
// a file sshd reads holds already left out), ~/.ssh made by install with
// the user's name, its own group (USERGROUPS_ENAB yes on all three VMs,
// s5/00-inspect logs; CheckName refuses a group of the name already
// there). With no key, a finding: the user cannot log in.
func (r Reading) planCreate(ctx context.Context, sys host.System, n New) ([]engine.Change, error) {
	if err := r.CheckNew(ctx, sys, n); err != nil {
		return nil, err
	}
	shell := "/bin/bash"
	if r.facts.Platform.Distro == detect.DistroAlpine {
		shell = "/bin/ash"
	}
	home := homeOf(n.Name)
	argv := []string{"useradd", "--home-dir", home, "--create-home", "--shell", shell}
	if r.PAM != detect.Yes {
		argv = append(argv, "--password", "*")
	}
	argv = append(argv, n.Name)
	undo := []string{"userdel", "--remove", n.Name}
	add := engine.Change{ID: ChangeCreate, Summary: "Create user " + n.Name, Target: accounts.PasswdFile, Commands: []string{host.FormatArgv(argv)},
		Stage: engine.StageAccounts, Resources: []string{"accounts"}, StateBefore: "absent", StateAfter: "no password, key login",
		Rollback: engine.Undo{Steps: []revert.Step{{Op: revert.OpRun, Argv: undo, Why: "accounts"}}},
		// The engine keeps Rollback (userdel --remove) unless Ran's replaces
		// it: where the name or its home has appeared since the plan
		// (CheckName on the live host), or useradd fails, nothing of
		// useradd's is there to undo, so an empty undo, and userdel never
		// removes what hardn did not make.
		Apply: func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
			if err := r.CheckName(ctx, sys, n.Name); err != nil {
				return engine.Ran{Rollback: &engine.Undo{}}, err
			}
			if _, err := sys.Run(ctx, argv[0], argv[1:]...); err != nil {
				return engine.Ran{Detail: host.FormatArgv(argv), Rollback: &engine.Undo{}}, err
			}
			return engine.Ran{Detail: host.FormatArgv(argv)}, nil
		}}
	if len(n.Keys) == 0 {
		add.Findings = []engine.Finding{{Code: CodeNoKey, Detail: "Without a key " + n.Name + " can't log in: there's no password"}}
	}
	out := []engine.Change{add}
	if n.Admin {
		g, err := grantChange(ctx, sys, r, n.Name, sudoers.GrantNoPassword, "Let "+n.Name+" use "+r.toolWord())
		if err != nil {
			return nil, err
		}
		if g != nil {
			g.Risk, g.After = engine.RiskPlain, []string{ChangeCreate}
			out = append(out, *g)
		}
	}
	if len(n.Keys) > 0 {
		ks, _ := newKeys(n) // CheckNew parsed them
		u := accounts.User{Name: n.Name, Home: home, Shell: shell}
		var p authkeys.Plan
		added := 0
		for _, k := range ks {
			q, err := authkeys.PlanAdd(ctx, sys, u, r.KeyEntries, k)
			switch {
			case err != nil:
				return nil, err
			case q.Empty(): // a file sshd reads holds it
				continue
			case p.Empty():
				p = q
			default:
				p.After += k.Line + "\n"
			}
			added++
		}
		if !p.Empty() {
			summary := "Add " + n.Name + "'s key"
			if added > 1 {
				summary += "s" // proposed words
			}
			c := keysChange(n.Name, p, []string{"install", "-d", "-m", "700", "-o", n.Name, "-g", n.Name, home + "/.ssh"}, summary)
			c.After = []string{ChangeCreate}
			out = append(out, c)
		}
	}
	return out, nil
}
