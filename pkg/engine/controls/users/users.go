// Package users holds the users area's control (refactor plan Phase 4,
// next/p4-check-users): Figma's User row ("non-root, sudo"). In v0.4 it is
// check-only: Check reads the host, and Plan returns engine.ErrNotMigrated
// (engine.CheckOnly) until slice 5 migrates it.
//
// Configured is a user account other than root (uid MinUID or more) with a
// login shell, whom an installed elevation tool, setuid root, lets run
// every command as root: sudo or sudo-rs, or doas on Alpine. Check reads
// the accounts (accounts.Read), each candidate's shell, and, once the
// tool's own check passes its files (sudoers.CheckInstalled: visudo -c,
// doas -C), each tool's rules in the order the tool reads them
// (sudoers.Choose and sudoers.Read, then the files). The last rule that
// matches decides, as in sudo and doas, for each request; hardn reads one
// form of grant per tool (rules.go), and any other rule after it that may
// be for the user, or a sudo Defaults that may change how it asks a
// password, leaves the answer Unknown. A read that fails is the check's
// error, which the engine reports as Unknown; what it read but cannot
// establish a state from is Unknown with findings: never a guess. Beside
// the grant that makes the row Configured, every grant without a password
// and every grant not established of an account the row counts (uid
// MinUID or more, that may log in) is a finding (exposure), which the
// risk level weighs: for those accounts, a grant hardn cannot read is
// never silence. Other accounts, and NSS's, are not looked at
// (check-users.md §6). An account whose password is locked ("!") cannot
// log in where sshd does not use PAM (next/p4-locked): it is not counted,
// and has a finding (CodeLocked); /etc/shadow is read for the first
// character of each password field alone (accounts.ReadShadow), and
// sshd's PAM as the SSH rows read it (ssh.ReadPAM), only where a lock may
// decide.
// docs/engine/check-users.md has every status line the row can show.
package users

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/controls/ssh"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// The finding codes of this package (pkg/output's registry holds them).
const (
	// CodeNoPassword: with Configured, an account that may log in (uid
	// MinUID or more; a login shell, or one whose stat failed) becomes
	// root without a password (sudo's NOPASSWD, doas's nopass), by the
	// rule that decides or by one before it that still decides some
	// requests (another group, another target user). Accounts below
	// MinUID are not looked at.
	CodeNoPassword = "USERS_NOPASSWD"
	// CodeRuleNotRead: whether an account that may log in becomes root,
	// or how sudo asks it a password, is not established: a rule in a
	// form hardn does not read that may be for it comes after the last
	// one it reads that matches, or a sudo Defaults may change how sudo
	// asks it a password; or, beside the grant that makes the row
	// Configured, a tool's rules are not read at all, or such a rule
	// comes before the one that decides and may still decide some
	// requests.
	CodeRuleNotRead = "USERS_RULE_NOT_READ"
	// CodeUID0: an account other than root has uid 0, so it is root.
	CodeUID0 = "USERS_UID0"
	// CodeLocked: an account the row would count (uid MinUID or more, a
	// login shell, or one whose stat failed) whose password is locked
	// ("!") while sshd does not use PAM, so that sshd refuses it before
	// any method, keys included (next/p4-locked): it is not counted.
	CodeLocked = "USERS_LOCKED"
)

// MinUID is the first uid of a user account: Figma's "UID ≥ 1000" and
// origin/main's own rule (pkg/adapter/secondary GetNonSystemUsers).
const MinUID = 1000

// Controls are the users controls in Figma's order: User.
func Controls() []engine.Control { return []engine.Control{Admin{}} }

// Admin is Figma's User row: an account other than root to log in with
// and become root from.
type Admin struct{ engine.CheckOnly }

// ID is "users.admin".
func (Admin) ID() string { return "users.admin" }

// Title is Figma's row label.
func (Admin) Title() string { return "User" }

// Applies is true everywhere, Proxmox VE included, where root stays for
// the cluster and the web UI but an account beside it is still the row's
// (docs/engine/check-users.md §2.10).
func (Admin) Applies(detect.Facts) (bool, string) { return true, "" }

// candidate is an account the row may count: uid MinUID or more, the first
// of its name (getpwnam's [I]), its shell not established as no login.
type candidate struct {
	accounts.User
	login  detect.Tri // Yes: a login shell; Unknown: its stat failed (err), or its lock decides and is not established (lockErr, or why)
	err    error
	grants []grant // one per tool, in readTools' order
	// lock is its password's lock as sshd reads it, read at lockAt;
	// refused is ssh.LockedOut's: whether sshd refuses it for it.
	lock    accounts.Lock
	lockAt  string
	refused detect.Tri
	lockErr error  // why refused is Unknown: ShadowFile, or sshd's configuration, not read
	why     string // or, with no lockErr, sshd's PAM not established (ssh.PAM.Why)
}

// elevates is Yes when a tool grants the candidate root, No when every
// tool is established not to.
func (c candidate) elevates() detect.Tri {
	t := detect.No
	for _, g := range c.grants {
		switch g.tri {
		case detect.Yes:
			return detect.Yes
		case detect.Unknown:
			t = detect.Unknown
		}
	}
	return t
}

// Check reads the accounts and the elevation rules.
// docs/engine/check-users.md lists each status line.
func (Admin) Check(ctx context.Context, sys host.System, f detect.Facts) (engine.Status, error) {
	db, err := accounts.Read(ctx, sys)
	if err != nil {
		return engine.Status{State: engine.Unknown, Detail: "can't read"}, err
	}
	var st engine.Status
	var cs []candidate
	seen := map[string]bool{}
	for _, u := range db.Users {
		first := !seen[u.Name]
		seen[u.Name] = true
		switch {
		case !first:
		case u.UID == 0 && u.Name != "root":
			st.Findings = append(st.Findings, engine.Finding{Code: CodeUID0, Detail: u.Name + " has uid 0", Source: steps.SourceAt(accounts.PasswdFile, u.Line)})
		case u.UID >= MinUID:
			c := candidate{User: u}
			if c.login, c.err = loginShell(ctx, sys, u.Shell); c.login != detect.No {
				cs = append(cs, c)
			}
		}
	}
	var tools []tool
	if len(cs) > 0 { // root only needs no rule read, nor visudo run
		tools = readTools(ctx, sys, f)
	}
	for i := range cs {
		for _, t := range tools {
			cs[i].grants = append(cs[i].grants, t.decide(db, cs[i].User))
		}
	}
	var lr locks
	if len(cs) > 0 {
		lr = readLocks(ctx, sys, f, cs)
	}
	st, err = verdict(st, f, tools, cs, lr)
	st.Evidence = evidence(tools, cs, lr, err)
	return st, err
}

// locks is what Check read of the candidates' locks: ShadowFile's error,
// and sshd's PAM where a lock is locked or not read.
type locks struct {
	err    error    // ShadowFile not read
	pam    *ssh.PAM // nil: not read, no lock decides
	pamErr error    // sshd's configuration not read
}

// readLocks reads each candidate's lock (ShadowFile, else its PasswdFile
// field: accounts.Shadow.LockOf) and, only where one is locked or not
// read, sshd's PAM as the SSH rows read it (ssh.ReadPAM); it sets each
// candidate's refused (ssh.LockedOut) and, where that is not established,
// its login Unknown with the reason.
func readLocks(ctx context.Context, sys host.System, f detect.Facts, cs []candidate) locks {
	var lr locks
	sh, err := accounts.ReadShadow(ctx, sys)
	lr.err = err
	uses := detect.Unknown
	for i := range cs {
		if err == nil {
			cs[i].lock, cs[i].lockAt = sh.LockOf(cs[i].User)
		}
		if ssh.LockedOut(cs[i].lock, uses) == detect.Unknown && lr.pam == nil {
			p, perr := ssh.ReadPAM(ctx, sys, f)
			lr.pam, lr.pamErr, uses = &p, perr, p.Uses
		}
	}
	for i := range cs {
		c := &cs[i]
		c.refused = ssh.LockedOut(c.lock, uses)
		if c.refused != detect.Unknown || c.login == detect.Unknown { // a shell not read keeps its own error
			continue
		}
		switch c.login = detect.Unknown; {
		case c.lock == accounts.LockNotRead:
			c.lockErr = lr.err
		case lr.pamErr != nil:
			c.lockErr = lr.pamErr
		default:
			c.why = lr.pam.Why
		}
	}
	return lr
}

// noShells are the base names of the shells that give no shell to run a
// command from: nologin, false and true exit, git-shell runs git's
// commands only.
var noShells = []string{"nologin", "false", "true", "git-shell"}

// loginShell is whether shell lets its user log in: not one of noShells,
// and an executable regular file, which sshd requires [I]. An empty shell
// is /bin/sh (passwd(5)). Any other program is taken for a shell.
func loginShell(ctx context.Context, sys host.System, shell string) (detect.Tri, error) {
	if shell == "" {
		shell = "/bin/sh"
	}
	if slices.Contains(noShells, path.Base(shell)) || !path.IsAbs(shell) {
		return detect.No, nil
	}
	fi, err := sys.Stat(ctx, shell)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return detect.No, nil
	case err != nil:
		return detect.Unknown, fmt.Errorf("users: %w", err)
	case fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0:
		return detect.Yes, nil
	}
	return detect.No, nil
}

// verdict decides the row from the candidates (see the package comment).
// One sshd refuses for its lock (refused Yes) cannot log in: it is not
// counted, and has a finding; with no other, "non-root, locked".
func verdict(st engine.Status, f detect.Facts, tools []tool, all []candidate, lr locks) (engine.Status, error) {
	var cs []candidate
	for _, c := range all {
		if c.refused == detect.Yes {
			st.Findings = append(st.Findings, engine.Finding{Code: CodeLocked, Detail: c.Name + ": locked, no login", Source: c.lockAt})
			continue
		}
		cs = append(cs, c)
	}
	logins := 0
	for _, c := range cs {
		if c.login != detect.Yes {
			continue
		}
		logins++
		for i, g := range c.grants {
			if g.tri == detect.Yes && st.State != engine.Configured {
				st.State, st.Detail = engine.Configured, "non-root, "+tools[i].name
			}
		}
	}
	switch {
	case st.State == engine.Configured:
		st.Findings = append(st.Findings, exposure(tools, cs)...)
		return st, nil
	case len(cs) == 0 && len(all) > 0:
		st.State, st.Detail = engine.NotConfigured, "non-root, locked"
		return st, nil
	case len(cs) == 0:
		st.State, st.Detail = engine.NotConfigured, "root only"
		return st, nil
	}
	// Partial needs every candidate that may log in established not to
	// become root, and one established to log in; otherwise Unknown, with
	// the first error, or the rules not read as findings.
	st.State = engine.Unknown
	var err error
	for _, c := range cs {
		e := c.elevates()
		cerr := c.err
		if cerr == nil {
			cerr = c.lockErr
		}
		switch decides := c.login == detect.Unknown && (e != detect.No || logins == 0); {
		case decides && cerr != nil && err == nil:
			err, st.Detail = cerr, "can't read"
		case decides && cerr == nil && st.Detail == "": // its lock, sshd's PAM not established
			st.Detail, st.Findings = c.why, append(st.Findings, lr.pam.Findings...)
		}
		for i, g := range c.grants {
			switch {
			case e != detect.Unknown || g.tri != detect.Unknown:
			case g.err != nil && err == nil:
				err, st.Detail = g.err, tools[i].detail
			case g.err == nil:
				st.Findings = append(st.Findings, notRead(tools[i], c, g.by.at))
				if st.Detail == "" {
					st.Detail = "can't tell who has " + tools[i].name
				}
			}
		}
	}
	switch {
	case err != nil:
		return st, err
	case st.Detail != "":
		return st, nil
	}
	st.State, st.Detail = engine.Partial, "non-root, no "+installed(f, tools)
	st.Findings = append(st.Findings, exposure(tools, cs)...) // none: every grant is No
	return st, nil
}

// exposure is what a Configured or Partial row reports beside its state,
// for the risk level: for every candidate that may log in (a login
// shell, or one whose stat failed) and every tool, a grant without a
// password (CodeNoPassword), by the deciding rule or one before it that
// still decides some requests (decide), and a grant not established, or
// such a rule before it not read (CodeRuleNotRead). They cannot undo the
// grant that makes the row Configured: another tool's rules, or a rule
// for another account, cannot take it away; a rule before it decides
// only requests it does not match (sudo's -g), and a rule not read after
// it that may be for its account leaves it Unknown. They say who else
// may become root, and whether without a password. A Partial row has
// every grant established No, so none.
func exposure(tools []tool, cs []candidate) []engine.Finding {
	var fs []engine.Finding
	for _, c := range cs {
		for i, g := range c.grants {
			switch g.tri {
			case detect.Yes:
				if g.nopass {
					fs = append(fs, engine.Finding{Code: CodeNoPassword, Detail: c.Name + ": " + tools[i].name + " without password", Source: g.nopassAt})
				}
				if g.openAt != "" {
					fs = append(fs, notRead(tools[i], c, g.openAt))
				}
			case detect.Unknown:
				fs = append(fs, notRead(tools[i], c, g.by.at))
			}
		}
	}
	return fs
}

// notRead is the finding for t's grant to c, not established: at the rule
// or Defaults that leaves it open, or with no source when t's rules are
// not read (the evidence says why).
func notRead(t tool, c candidate, at string) engine.Finding {
	return engine.Finding{Code: CodeRuleNotRead, Detail: "may decide " + t.name + " for " + c.Name, Source: at}
}

// installed names the tools installed, for Partial's detail; when none
// is, the distribution's: doas on Alpine, sudo elsewhere (Figma's word).
func installed(f detect.Facts, tools []tool) string {
	var s string
	for _, t := range tools {
		if t.state == detect.Yes {
			if s != "" {
				s += " or "
			}
			s += t.name
		}
	}
	switch {
	case s != "":
		return s
	case f.Platform.Distro == detect.DistroAlpine:
		return "doas"
	}
	return "sudo"
}

// evidence is one line per tool not read, per candidate and per tool's
// answer for it, with its lock where one was locked or not read, and
// then sshd's PAM. An error that is the check's (err) is in its errors,
// so the line says only what was not read; any other is shown short.
func evidence(tools []tool, cs []candidate, lr locks, err error) []string {
	var ev []string
	for _, t := range tools {
		switch t.state {
		case detect.No:
			ev = append(ev, t.name+": "+t.why)
		case detect.Unknown:
			ev = append(ev, t.name+": not read"+cause(t.err, err))
		}
	}
	if len(cs) == 0 {
		ev = append(ev, "passwd: no account with uid "+strconv.Itoa(MinUID)+" or more and a login shell")
	}
	for _, c := range cs {
		shell := c.Shell
		if shell == "" {
			shell = "/bin/sh"
		}
		l := "passwd: " + c.Name + ", uid " + strconv.Itoa(c.UID) + ", " + shell
		if c.err != nil {
			l += ", shell not read" + cause(c.err, err)
		}
		ev = append(ev, l)
		if lr.pam != nil && (c.lock == accounts.Locked || lr.err != nil) {
			ev = append(ev, ssh.LockEvidence(c.Name, c.lock, c.lockAt, lr.err, errors.Is(err, lr.err)))
		}
		for i, g := range c.grants {
			n := tools[i].name
			switch {
			case tools[i].state != detect.Yes:
			case g.tri == detect.Yes:
				ev = append(ev, n+": "+c.Name+" by "+g.by.at+": "+g.by.text)
			case g.tri == detect.No && g.by.deny:
				ev = append(ev, n+": "+c.Name+" denied by "+g.by.at+": "+g.by.text)
			case g.tri == detect.No:
				ev = append(ev, n+": no rule for "+c.Name)
			default:
				ev = append(ev, n+": "+c.Name+" may be decided by "+g.by.at+": "+g.by.text)
			}
		}
	}
	switch {
	case lr.pam == nil:
	case lr.pamErr != nil:
		ev = append(ev, "sshd: not read"+cause(lr.pamErr, err))
	default:
		ev = append(ev, lr.pam.Evidence...)
	}
	return ev
}

// cause is ": " and e short, or "" when e is the check's error, err.
func cause(e, err error) string {
	if errors.Is(err, e) {
		return ""
	}
	return ": " + short(e)
}

// short is err as the host put it: the tool's check and the last line it
// printed, the file operation that failed, or the innermost error.
func short(err error) string {
	var ce *sudoers.CheckError
	var pe *fs.PathError
	switch {
	case errors.As(err, &ce) && len(ce.Tail) > 0:
		return host.FormatArgv(ce.Argv) + ": " + ce.Tail[len(ce.Tail)-1]
	case errors.As(err, &pe):
		return pe.Error()
	case ce != nil:
		return ce.Path + ": " + short(ce.Err)
	}
	for u := errors.Unwrap(err); u != nil; u = errors.Unwrap(err) {
		err = u
	}
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}
