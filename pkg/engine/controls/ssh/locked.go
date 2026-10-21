package ssh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/pve"
)

// Locked accounts without PAM (next/p4-locked, check-ssh.md §9). When sshd
// does not use PAM, auth.c's allowed_user() refuses an account whose
// password is locked ("!" on Linux, platform.c's platform_locked_account())
// before any authentication method runs, public keys included, root as any
// other; with PAM the test is skipped.

// PAM is whether the sshd that runs uses PAM, read as SSH Auth reads it
// (check-ssh.md §2.11): Uses is Yes with UsePAM yes; No with UsePAM no, or
// a build without PAM where OpenRC starts sshd or no other build installed
// would use PAM (otherPAM); Unknown otherwise, with Why and Findings.
// Evidence is sshd -T's usepam line, or that it printed none, then each
// other build's that would use PAM.
type PAM struct {
	Uses     detect.Tri
	Evidence []string
	Why      string           // with Unknown and no error: "can't tell which sshd runs", or "can't read"
	Findings []engine.Finding // with "can't read": sshd's findings that UsePAM is not established
}

// ReadPAM reads sshd's effective configuration as the SSH rows do (sshd -T
// of the binary OpenRC's script runs) and returns PAM: the User row asks it
// of an account whose password is locked. A read that fails is its error,
// with Uses Unknown.
func ReadPAM(ctx context.Context, sys host.System, f detect.Facts) (PAM, error) {
	r, err := readEffective(ctx, sys, f)
	if err != nil {
		return PAM{}, err
	}
	return r.pam(ctx, sys, f)
}

// pam is PAM from this reading, with otherPAM's commands where which build
// runs is not established.
func (r reading) pam(ctx context.Context, sys host.System, f detect.Facts) (PAM, error) {
	p := PAM{Evidence: r.pamEvidence()}
	if r.eff.BuiltWithoutPAM && f.SSH.Mode != detect.SSHOpenRC {
		other, err := otherPAM(ctx, sys)
		if err != nil {
			return p, err
		}
		if len(other) > 0 {
			p.Evidence, p.Why = append(p.Evidence, other...), "can't tell which sshd runs"
			return p, nil
		}
	}
	switch u := r.eff.UsePAM; {
	case u.Known && u.Value == "yes":
		p.Uses = detect.Yes
	case u.Known:
		p.Uses = detect.No
	default:
		p.Why, p.Findings = "can't read", r.unknowns("usepam")
	}
	return p, nil
}

// LockedOut is whether sshd refuses an account before any method for its
// password's lock (auth.c allowed_user(): !options.use_pam &&
// platform_locked_account(pw)): Yes when the password is locked and sshd
// does not use PAM; No when it is not locked, or sshd uses PAM; Unknown
// otherwise.
func LockedOut(l accounts.Lock, usesPAM detect.Tri) detect.Tri {
	switch {
	case l == accounts.NotLocked || l == accounts.Empty || usesPAM == detect.Yes:
		return detect.No
	case l == accounts.Locked && usesPAM == detect.No:
		return detect.Yes
	}
	return detect.Unknown
}

// LockEvidence is the evidence of name's lock as l and at say it
// ("/etc/shadow:1: root locked (!)"), or, with err, that it was not read:
// with the error's words unless it is the check's own, which its errors
// hold.
func LockEvidence(name string, l accounts.Lock, at string, err error, own bool) string {
	switch {
	case err == nil:
		return at + ": " + name + " " + l.String()
	case own:
		return accounts.ShadowFile + ": " + name + " not read"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return accounts.ShadowFile + ": " + name + " not read: " + pe.Error()
	}
	return accounts.ShadowFile + ": " + name + " not read: " + err.Error()
}

// rootLock is root's lock as sshd reads it: PasswdFile's first root line,
// and its lock in ShadowFile, else its PasswdFile field
// (accounts.Shadow.LockOf), with where it was read.
func rootLock(ctx context.Context, sys host.System) (accounts.Lock, string, error) {
	us, err := accounts.ReadPasswd(ctx, sys)
	if err != nil {
		return accounts.LockNotRead, "", err
	}
	i := slices.IndexFunc(us, func(u accounts.User) bool { return u.Name == "root" })
	if i < 0 {
		return accounts.LockNotRead, "", fmt.Errorf("ssh: root in %s: %w", accounts.PasswdFile, host.ErrUnknownUser)
	}
	sh, err := accounts.ReadShadow(ctx, sys)
	if err != nil {
		return accounts.LockNotRead, "", err
	}
	l, at := sh.LockOf(us[i])
	return l, at, nil
}

// detailBlocked is SSH Login's Partial detail on Proxmox VE where root's
// key login is kept out (loginState's peer in Check).
const detailBlocked = "root key login blocked"

// lockedOut is SSH Login's status st, read with error stErr from
// PermitRootLogin and the Match blocks, once root's lock is read: root's
// password locked and sshd not using PAM is Configured "root locked" off
// Proxmox VE, where the baseline is that root cannot log in, whatever
// PermitRootLogin says; on Proxmox VE, whose cluster logs in as root by
// key, Partial "root key login blocked". Not locked, or PAM in use: st and
// stErr, as read. Where that is not established and it decides (st not
// already Unknown or that Partial): Unknown, with the error that kept it
// so (the files' read, otherPAM's), or PAM's Why and Findings.
func (r reading) lockedOut(ctx context.Context, sys host.System, f detect.Facts, st engine.Status, stErr error) (engine.Status, error) {
	lock, at, lerr := rootLock(ctx, sys)
	if lerr == nil && lock != accounts.Locked {
		return st, stErr
	}
	p, perr := r.pam(ctx, sys, f)
	out := LockedOut(lock, p.Uses)
	if perr != nil {
		out = detect.Unknown
	}
	decides := out == detect.Yes || out == detect.Unknown && st.State != engine.Unknown && st.Detail != detailBlocked
	st.Evidence = append(st.Evidence, LockEvidence("root", lock, at, lerr, decides && lerr != nil))
	st.Evidence = append(st.Evidence, p.Evidence...)
	switch {
	case out == detect.Yes:
		onPVE, err := pve.Applies(f.Proxmox)
		switch {
		case err != nil:
			st.State, st.Detail = engine.Unknown, "can't tell if Proxmox"
			return st, err
		case onPVE:
			st.State, st.Detail = engine.Partial, detailBlocked
		default:
			st.State, st.Detail = engine.Configured, "root locked"
		}
		return st, nil
	case !decides:
		return st, stErr
	case lerr != nil:
		st.State, st.Detail = engine.Unknown, "can't read"
		return st, lerr
	case perr != nil:
		why, err := failed(f.SSH, perr)
		st.State, st.Detail = engine.Unknown, why.Detail
		return st, err
	}
	st.State, st.Detail = engine.Unknown, p.Why
	st.Findings = append(st.Findings, p.Findings...)
	return st, nil
}
