package users

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/alpine"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/revert"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// New is the user Create makes: Name; Admin, hardn's grant for it, asking
// the user's password; NoPassword, with Admin, the grant asking none, the
// opt-in (hardn.yml's adminNoPassword, or the screen's answer; the
// maintainer, 2026-11-11); Keys, its public keys, each a pasted key or one
// of hardn.yml's sshKeys, none for no key.
type New struct {
	Name              string
	Admin, NoPassword bool
	Keys              []string
}

// CodeNoKey: Create plans a user with no key, who has no password either,
// so cannot log in.
const CodeNoKey = "USERS_NO_KEY"

// CodePasswordNotSet: hardn's grant asks the password of a user who has
// none (a new user's, Create's; Grant's for a user without one), so the
// user can't use the tool until one is set. hardn sets none itself.
const CodePasswordNotSet = "USERS_PASSWORD_NOT_SET"

// passwordNotSet is CodePasswordNotSet's finding for name and the tool's
// word.
func passwordNotSet(name, tool string) engine.Finding {
	return engine.Finding{Code: CodePasswordNotSet, Detail: name + " can't use " + tool + " until a password is set: run passwd " + name}
}

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
// UseraddRefusal's, one refusal naming shadow and doas where, with Admin,
// ToolRefusal's is no tool too; with Admin, ToolRefusal's;
// with Keys, KeysRefusal's and newKeys'; then bypassRefusal's, the host's.
// nil where Create may go on. Create without admin rights works with no
// tool, as on Proxmox VE.
func (r Reading) CheckNew(ctx context.Context, sys host.System, n New) error {
	_, err := r.checkNew(ctx, sys, n)
	return err
}

// checkNew is CheckNew, with the finding planCreate puts on the new
// account where bypassRefusal does not establish the fix (nil for none).
func (r Reading) checkNew(ctx context.Context, sys host.System, n New) (*engine.Finding, error) {
	if err := r.CheckName(ctx, sys, n.Name); err != nil {
		return nil, err
	}
	if err := r.UseraddRefusal(ctx, sys); err != nil {
		if tool := r.ToolRefusal(); n.Admin && errors.Is(err, ErrNoUseradd) && errors.Is(tool, ErrNoTool) {
			// Both missing on Alpine (doas, ToolRefusal's word there): one
			// refusal naming both packages (proposed words), so that -c, -u
			// and Run All don't say the second only on the next run.
			return nil, refuse(codeNoUseradd, ErrNoUseradd, r.Choice.Err, "useradd and doas aren't installed, so hardn can't create an admin", "run apk add shadow doas, then try again")
		}
		return nil, err
	}
	if n.Admin {
		if err := r.ToolRefusal(); err != nil {
			return nil, err
		}
	}
	if len(n.Keys) > 0 {
		if err := r.KeysRefusal(); err != nil {
			return nil, err
		}
		if _, err := newKeys(n); err != nil {
			return nil, err
		}
	}
	return r.bypassRefusal(ctx, sys, n.Name)
}

// UseraddRefusal refuses Create on Alpine where useradd, which Create runs
// (planCreate), is not installed: it is the shadow package's, and BusyBox
// has adduser, not useradd (BusyBox 1.37 on the Alpine 3.23 VM:
// vm-runs/cve/busybox-useradd-alpine323.log; an Alpine 3.23.2 LXC without
// shadow planned useradd, 2026-11-07). Looked for as host.Real
// finds a command (detect.FindTool); a lookup that fails is its error.
// Off Alpine it is not looked for. The User Management screen asks it
// before Create's first question, and offers Install shadow in its place
// (installShadow; the maintainer, 2026-11-11); -c, -u and Run All refuse.
func (r Reading) UseraddRefusal(ctx context.Context, sys host.System) error {
	if r.facts.Platform.Distro != detect.DistroAlpine {
		return nil
	}
	switch t := detect.FindTool(ctx, sys, "useradd"); t.Present {
	case detect.Yes:
		return nil
	case detect.No:
		return refuse(codeNoUseradd, ErrNoUseradd, nil, "useradd isn't installed, so hardn can't create users", "run apk add shadow, then try again")
	default:
		return fmt.Errorf("users: %w", t.Err)
	}
}

// shadowPackage is the package that brings useradd on Alpine, in its
// community repository (apk policy shadow on the Alpine 3.23 VM,
// 2026-11-11).
const shadowPackage = "shadow"

// installShadow is Install shadow's plan (the maintainer, 2026-11-11), the
// User Management screen's alone, modelled on Firewall's Install iproute2:
// on Alpine where useradd is not installed (UseraddRefusal), the shadow
// package as one change (steps.PlanPackage: simulated first, RiskPackageInstall,
// y/N, its undo apk del shadow, run as it is), its state after naming the
// other new packages the simulation lists. Where useradd is there, and off
// Alpine, where Create does not look for it, nothing to do. Where shadow is
// installed and useradd still missing (a file removed), there is nothing to
// install, and saying so would leave Create offering it again: that is a
// refusal naming useradd.
func installShadow(ctx context.Context, sys host.System, f detect.Facts) ([]engine.Change, error) {
	if err := (Reading{facts: f}).UseraddRefusal(ctx, sys); !errors.Is(err, ErrNoUseradd) {
		return nil, err
	}
	changes, err := steps.PlanPackage(ctx, sys, f, steps.Package{ID: ChangeShadow, Name: shadowPackage, After: "installed", Why: "accounts", Community: true})
	if err == nil && len(changes) == 0 {
		return nil, refuse(codeNoUseradd, ErrNoUseradd, nil, "shadow is installed, but its useradd isn't there", "reinstall shadow, then try again")
	}
	return changes, err
}

// bypassRefusal is CVE-2026-123158's refusal of Create (docs/engine/
// control-users.md §7): on Alpine, where the new user's field would be "*"
// (sshd's PAM not established as on, planCreate), sshd as the SSH rows
// read it (ssh.ReadBypass): with the openssh package established to lack
// the fix and a password path not established off, sshd would let anyone
// log in as the new user with a long password, so Create is refused
// before anything is written; with the fix not established and a password
// path not established off, Create goes on with a finding
// (ssh.CodeFixUnknown), as CodeNoKey's. Whether a donor (a $5$ or $6$
// hash) exists is not asked: one may be set later. A read of sshd that
// fails leaves the password path Unknown, so that it refuses or warns,
// except where it establishes that no sshd can run (ssh.Bypass.NoSSHD: not
// installed, or no sshd_config), which leaves nothing to refuse or warn
// of: seen as a false warning in an Alpine 3.23 LXC with no openssh,
// 2026-11-07.
func (r Reading) bypassRefusal(ctx context.Context, sys host.System, name string) (*engine.Finding, error) {
	if r.facts.Platform.Distro != detect.DistroAlpine || r.PAM == detect.Yes {
		return nil, nil
	}
	b, _ := ssh.ReadBypass(ctx, sys, r.facts)
	switch {
	case !b.Applies || b.NoSSHD || b.Password == detect.No || b.Fix.Fixed == detect.Yes:
		return nil, nil
	case b.Fix.Fixed == detect.No:
		return nil, refuse(codePasswordBypass, ErrPasswordBypass, nil,
			"sshd would let anyone log in as "+name+" with a long password, since "+name+" has no password ("+ssh.CVE+")",
			"upgrade openssh in SSH Login (5), or turn password login off there (2)")
	}
	pkg := b.Fix.Package
	if pkg == "" {
		pkg = "openssh"
	}
	return &engine.Finding{Code: ssh.CodeFixUnknown, Detail: "can't tell if " + pkg + " has the fix for " + ssh.CVE + ": " + b.Fix.Why, Source: alpine.InstalledDB}, nil
}

// newKeys are n's Keys parsed, in order, the same key (authkeys.Key.Same)
// once. One that is not a key is refused naming which, by its place in Keys
// from 1, one key or several (proposed words): -c's come from hardn.yml's
// sshKeys, where "Paste" does not fit, and the screen refuses a pasted one
// at its field (ParseKey) before CheckNew sees it.
func newKeys(n New) ([]authkeys.Key, error) {
	var ks []authkeys.Key
	for i, s := range n.Keys {
		k, err := authkeys.ParsePublic(s)
		if err != nil {
			return nil, refuse(codeNotKey, err, nil, fmt.Sprintf("Key %d isn't an OpenSSH public key, such as ssh-ed25519 AAAA… name", i+1), "")
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
// (decision 3 (a); run on the Alpine 3.23 VM, 2026-11-07), undone by
// userdel --remove; then, with Admin, hardn's grant asking the user's
// password, with a finding that none is set yet (CodePasswordNotSet), or,
// with NoPassword, asking none; then, with Keys, every key in ~/.ssh/authorized_keys, one change (each
// authkeys.PlanAdd's line appended after the keys planned before it, a key
// a file sshd reads holds already left out), ~/.ssh made by install with
// the user's name, its own group (USERGROUPS_ENAB yes on all three VMs,
// s5/00-inspect logs; CheckName refuses a group of the name already
// there). With no key, a finding: the user cannot log in; with
// CVE-2026-123158's fix not established, a finding too (bypassRefusal).
func (r Reading) planCreate(ctx context.Context, sys host.System, n New) ([]engine.Change, error) {
	unknownFix, err := r.checkNew(ctx, sys, n)
	if err != nil {
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
		add.Findings = []engine.Finding{{Code: CodeNoKey, Detail: "Without a key, " + n.Name + " can't log in: there's no password"}}
	}
	if unknownFix != nil {
		add.Findings = append(add.Findings, *unknownFix)
	}
	out := []engine.Change{add}
	if n.Admin {
		grant := sudoers.GrantPassword
		if n.NoPassword {
			grant = sudoers.GrantNoPassword
		}
		g, err := grantChange(ctx, sys, r, n.Name, grant, "Let "+n.Name+" use "+r.toolWord())
		if err != nil {
			return nil, err
		}
		if g != nil {
			g.Risk, g.After = engine.RiskPlain, []string{ChangeCreate}
			if !n.NoPassword {
				g.Findings = append(g.Findings, passwordNotSet(n.Name, r.toolWord()))
			}
			out = append(out, *g)
		} else if !n.NoPassword {
			// hardn's file is there already (a user removed by hand, its
			// file left): no grant to plan, and the finding still holds.
			out[0].Findings = append(out[0].Findings, passwordNotSet(n.Name, r.toolWord()))
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
