package ssh

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

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
// other; with PAM the test is skipped. PermitRootLogin applies to every
// account with uid 0 (auth2.c userauth_finish), so each must be locked.

// PAM is whether the sshd that runs uses PAM, read as SSH Auth reads it
// (check-ssh.md §2.11): Uses is Yes with UsePAM yes; No with UsePAM no, or
// a build without PAM where OpenRC starts sshd or no other build installed
// would use PAM (otherPAM); Unknown otherwise, with Why and Findings.
// Evidence is sshd -T's usepam line, or that it printed none, then each
// other build's that would use PAM.
type PAM struct {
	Uses     detect.Tri
	Evidence []string
	// Why is the row's detail with Uses Unknown: "can't tell which sshd
	// runs", or "can't read" with Findings; with ReadPAM's error, the SSH
	// rows' detail for it ("sshd not started yet", "sshd not installed",
	// "can't read", ...).
	Why      string
	Findings []engine.Finding // with "can't read" and no error: sshd's findings that UsePAM is not established
}

// ReadPAM reads sshd's effective configuration as the SSH rows do (sshd -T
// of the binary OpenRC's script runs) and returns PAM: the User row asks it
// of an account whose password is locked. A read that fails is its error,
// with Uses Unknown and Why the SSH rows' detail for it.
func ReadPAM(ctx context.Context, sys host.System, f detect.Facts) (PAM, error) {
	r, err := readEffective(ctx, sys, f)
	if err == nil {
		var p PAM
		if p, err = r.pam(ctx, sys, f); err == nil {
			return p, nil
		}
	}
	st, err := failed(f.SSH, err)
	return PAM{Why: st.Detail}, err
}

// otherBuilds reports whether which build runs is not established from
// sshd -T alone: its build has no PAM and OpenRC's init script does not
// start sshd, so pam asks the other builds (otherPAM, which runs them).
func (r reading) otherBuilds(f detect.Facts) bool {
	return r.eff.BuiltWithoutPAM && f.SSH.Mode != detect.SSHOpenRC
}

// pam is PAM from this reading, with otherPAM's commands where which build
// runs is not established (otherBuilds); otherwise it runs nothing.
func (r reading) pam(ctx context.Context, sys host.System, f detect.Facts) (PAM, error) {
	p := PAM{Evidence: r.pamEvidence()}
	if r.otherBuilds(f) {
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

// LockEvidence is the evidence of name's lock l, read at at
// ("/etc/shadow:1: root locked (!)"): never the password field.
func LockEvidence(name string, l accounts.Lock, at string) string {
	return at + ": " + name + " " + l.String()
}

// rootAccount is an account sshd treats as root, and its lock as sshd
// reads it (accounts.Shadow.LockOf), at at.
type rootAccount struct {
	accounts.User
	lock accounts.Lock
	at   string
}

// rootAccounts are the accounts PermitRootLogin applies to: sshd tests
// the uid, not the name (auth2.c userauth_finish: pw_uid == 0), so each
// name's first PasswdFile line, as getpwnam finds it [I], with uid 0,
// root among them; each with its lock, its ShadowFile line's, else its
// PasswdFile field's. With an error, file is the file not read: the
// accounts are then those read, with no lock.
func rootAccounts(ctx context.Context, sys host.System) (as []rootAccount, file string, err error) {
	us, err := accounts.ReadPasswd(ctx, sys)
	if err != nil {
		return nil, accounts.PasswdFile, err
	}
	seen := map[string]bool{}
	for _, u := range us {
		if !seen[u.Name] && u.UID == 0 {
			as = append(as, rootAccount{User: u})
		}
		seen[u.Name] = true
	}
	if !slices.ContainsFunc(as, func(a rootAccount) bool { return a.Name == "root" }) {
		return nil, accounts.PasswdFile, fmt.Errorf("ssh: root with uid 0 in %s: %w", accounts.PasswdFile, host.ErrUnknownUser)
	}
	sh, err := accounts.ReadShadow(ctx, sys)
	if err != nil {
		return as, accounts.ShadowFile, err
	}
	for i := range as {
		as[i].lock, as[i].at = sh.LockOf(as[i].User)
	}
	return as, "", nil
}

// detailBlocked is SSH Login's Partial detail on Proxmox VE where root's
// key login is kept out (loginState's peer in Check).
const detailBlocked = "root key login blocked"

// lockedOut is SSH Login's status st, read with error stErr from
// PermitRootLogin and the Match blocks, once the locks of the accounts it
// applies to are read (rootAccounts: root and any other with uid 0). Each
// of them locked and sshd not using PAM is Configured "root locked" off
// Proxmox VE, where the baseline is that root cannot log in, whatever
// PermitRootLogin says; on Proxmox VE, whose cluster logs in as root by
// key, Partial "root key login blocked". One not locked, or PAM in use:
// st and stErr, as read. Where that is not established and it decides (st
// not already Unknown or that Partial): Unknown, with the error that kept
// it so (the files' read, otherPAM's), or PAM's Why and Findings.
//
// sshd -T's own UsePAM yes decides first, with no file read; otherPAM's
// commands run only once the locks may decide.
func (r reading) lockedOut(ctx context.Context, sys host.System, f detect.Facts, st engine.Status, stErr error) (engine.Status, error) {
	var p PAM
	var perr error
	read := !r.otherBuilds(f)
	if read {
		if p, perr = r.pam(ctx, sys, f); p.Uses == detect.Yes { // no command: perr is nil
			return st, stErr
		}
	}
	as, file, lerr := rootAccounts(ctx, sys)
	lock := accounts.LockNotRead
	if lerr == nil {
		lock = accounts.Locked
		if slices.ContainsFunc(as, func(a rootAccount) bool { return a.lock != accounts.Locked }) {
			return st, stErr // that account logs in as root
		}
	}
	if !read {
		p, perr = r.pam(ctx, sys, f)
	}
	out := LockedOut(lock, p.Uses)
	if perr != nil {
		out = detect.Unknown
	}
	decides := out == detect.Yes || out == detect.Unknown && st.State != engine.Unknown && st.Detail != detailBlocked
	if lerr == nil {
		for _, a := range as {
			st.Evidence = append(st.Evidence, LockEvidence(a.Name, a.lock, a.at))
		}
	} else {
		l := file + ": " + accountNames(as) + " not read"
		if !decides { // else the check's error, which says it
			l += ": " + words(lerr)
		}
		st.Evidence = append(st.Evidence, l)
	}
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

// accountNames is the accounts' names, "root" when none was read.
func accountNames(as []rootAccount) string {
	if len(as) == 0 {
		return "root"
	}
	ns := make([]string, len(as))
	for i, a := range as {
		ns[i] = a.Name
	}
	return strings.Join(ns, ", ")
}

// words is err as the host put it: the file operation that failed, else
// the innermost error, without the packages' prefixes.
func words(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Error()
	}
	for u := errors.Unwrap(err); u != nil; u = errors.Unwrap(err) {
		err = u
	}
	return err.Error()
}

// ReadKeyFiles is sshd -T's AuthorizedKeysFile, read as ReadPAM reads sshd
// (sshd.Effective's: unexpanded, "none" kept, Match blocks not read): the
// files sshd takes a user's keys from, for the User Management screen
// (slice 5, unit (b)). An error where sshd -T is not read, or printed no
// authorizedkeysfile line.
func ReadKeyFiles(ctx context.Context, sys host.System, f detect.Facts) ([]string, error) {
	r, err := readEffective(ctx, sys, f)
	switch {
	case err != nil:
		return nil, err
	case len(r.eff.AuthorizedKeysFile) == 0:
		return nil, errors.New("sshd -T printed no authorizedkeysfile")
	}
	return r.eff.AuthorizedKeysFile, nil
}
