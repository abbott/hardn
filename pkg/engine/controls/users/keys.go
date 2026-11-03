package users

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/authkeys"
	"github.com/abbott/hardn/pkg/host/revert"
)

// The SSH keys (slice 5, unit (b)): a key is added to, or removed from,
// ~/.ssh/authorized_keys alone (authkeys.Planned), and only where sshd
// reads that file; keys are counted in every file sshd reads
// (AuthorizedKeysFile), so a key in another one is never reported gone.
// docs/safety/authkeys.md has what authkeys reads and plans.

// KeysRefusal is why no key can be planned on the host, before anything is
// asked: sshd's AuthorizedKeysFile not read (KeyErr), or leaving out the
// one file hardn writes (authkeys.Plannable). nil when one can.
func (r Reading) KeysRefusal() error {
	switch {
	case r.KeyErr != nil:
		return refuse(codeKeyFilesNotRead, ErrKeyFilesNotRead, r.KeyErr, "hardn can't read which files sshd takes keys from: "+r.KeyErr.Error(), "")
	case !authkeys.Plannable(r.KeyEntries):
		return refuse(codeKeysNotPlanned, authkeys.ErrNotPlanned, nil,
			"sshd doesn't read ~/.ssh/authorized_keys here (AuthorizedKeysFile "+strings.Join(r.KeyEntries, " ")+"), so hardn can't change keys", "")
	}
	return nil
}

// keyRefusal is why a key action refuses for u before the key is asked:
// KeysRefusal's; for Add, a home or ~/.ssh sshd's StrictModes would ignore
// a key in (authkeys.CheckStrict); for Remove, the session user's last key,
// counted across every file sshd reads (decision 4).
func (r Reading) keyRefusal(ctx context.Context, sys host.System, action Action, u User) error {
	if err := r.KeysRefusal(); err != nil {
		return err
	}
	switch {
	case action == ActionAddKey:
		if err := authkeys.CheckStrict(ctx, sys, u.User); err != nil {
			why := strings.TrimSuffix(err.Error(), ": "+authkeys.ErrStrictModes.Error())
			return refuse(codeStrictModes, err, nil, "sshd would ignore a key for "+u.Name+": "+why,
				"with StrictModes it takes keys only where the home and ~/.ssh are the user's own and no one else can write to them")
		}
	case u.Name == r.Invoker && len(u.Keys) == 1:
		return refuse(codeLastKey, ErrLastKey, nil, fmt.Sprintf("This is %s's last key, and you're logged in as %s", u.Name, u.Name), "add another key first")
	}
	return nil
}

// ParseKey is a pasted public key (authkeys.ParsePublic), refused in the
// decided words where it is not one.
func ParseKey(s string) (authkeys.Key, error) {
	k, err := authkeys.ParsePublic(s)
	if err != nil {
		return authkeys.Key{}, refuse(codeNotKey, err, nil, "Paste one OpenSSH public key, such as ssh-ed25519 AAAA… name", "")
	}
	return k, nil
}

// planKey plans Add or Remove SSH key for a.User, after Refusal's: Add
// refuses a key that is not one (ParseKey) and plans nothing for one sshd
// reads already (authkeys.PlanAdd); a Locked user's plan has a finding, as
// sshd refuses its key login. Remove refuses a key that is also in another
// file sshd reads, which removing it here would leave working; it is a
// lockout risk where keyRisk says.
func (r Reading) planKey(ctx context.Context, sys host.System, a Admin) ([]engine.Change, error) {
	if err := r.Refusal(ctx, sys, a); err != nil {
		return nil, err
	}
	u, _ := r.user(a.User)
	if a.Action == ActionAddKey {
		k, err := ParseKey(a.Key)
		if err != nil {
			return nil, err
		}
		p, err := authkeys.PlanAdd(ctx, sys, u.User, r.KeyEntries, k)
		if err != nil || p.Empty() {
			return nil, err
		}
		c := keysChange(u.Name, p, p.MakeDir, "Add a key for "+u.Name)
		if u.Locked {
			c.Findings = append(c.Findings, engine.Finding{Code: CodeLocked, Source: u.lockAt,
				Detail: "sshd refuses " + u.Name + "'s keys: the account is locked (!) and sshd doesn't use PAM"})
		}
		return []engine.Change{c}, nil
	}
	planned := authkeys.Path(authkeys.Planned, u.User)
	for _, f := range u.Files {
		if f.Path != planned && slices.ContainsFunc(f.Keys, func(k authkeys.Key) bool { return k.Fingerprint == a.Fingerprint }) {
			return nil, refuse(codeKeyElsewhere, ErrKeyElsewhere, nil, fmt.Sprintf("%s's key is also in %s, which sshd reads too", u.Name, f.Path), "remove it there, then try again")
		}
	}
	p, err := authkeys.PlanRemove(ctx, sys, u.User, r.KeyEntries, a.Fingerprint)
	if err != nil {
		return nil, err
	}
	c := keysChange(u.Name, p, nil, "Remove a key from "+u.Name)
	if r.keyRisk(u) {
		c.Risk = engine.RiskLockout
	}
	return []engine.Change{c}, nil
}

// keyRisk is whether removing one of u's keys is a lockout risk (decision
// 4): the session user's (sshd's log, which says which key it logged in
// with, is not read), anyone's with the session user not known, or
// anyone's last key.
func (r Reading) keyRisk(u User) bool {
	return r.Invoker == "" || u.Name == r.Invoker || len(u.Keys) == 1
}

// keysChange is p as a change: mkdir run first where set (~/.ssh made,
// 0700, user's), then the file written 0600 with user's uid and gid read
// at apply, so a user Create makes in the same plan has them. The engine
// backs the file up, so the rollback restores it, or removes it where it
// was not there; then ~/.ssh, where made, is removed.
func keysChange(user string, p authkeys.Plan, mkdir []string, summary string) engine.Change {
	c := engine.Change{ID: ChangeKeys, Summary: summary, Target: p.Path, Stage: engine.StageAccounts, Resources: []string{"accounts"},
		Backups: []string{p.Path}, Diff: engine.FileDiff(p.Path, p.Before, &p.After)}
	if mkdir != nil {
		c.Commands = []string{host.FormatArgv(mkdir)}
		c.Rollback = engine.Undo{Steps: []revert.Step{{Op: revert.OpRemoveDir, Path: path.Dir(p.Path), Why: "accounts"}}}
	}
	c.Apply = func(ctx context.Context, sys host.System, _ engine.Run) (engine.Ran, error) {
		if mkdir != nil {
			if _, err := sys.Run(ctx, mkdir[0], mkdir[1:]...); err != nil {
				return engine.Ran{}, err
			}
		}
		us, err := accounts.ReadPasswd(ctx, sys)
		if err != nil {
			return engine.Ran{}, err
		}
		i := slices.IndexFunc(us, func(u accounts.User) bool { return u.Name == user })
		if i < 0 {
			return engine.Ran{}, fmt.Errorf("users: no user %s: %w", user, fs.ErrNotExist)
		}
		return engine.Ran{Detail: p.Path}, sys.WriteFileAtomic(ctx, p.Path, []byte(p.After), 0o600, us[i].UID, us[i].GID)
	}
	return c
}
