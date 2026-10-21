// Package users holds the users area's control (refactor plan Phase 4,
// next/p4-check-users): Figma's User row ("non-root, sudo"). In v0.4 it is
// check-only: Check reads the host, and Plan returns engine.ErrNotMigrated
// (engine.CheckOnly) until slice 5 migrates it.
//
// Configured is a user account other than root (uid MinUID or more) with a
// login shell, whom an installed elevation tool lets run every command as
// root: sudo or sudo-rs, or doas on Alpine. Check reads the accounts
// (accounts.Read), each candidate's shell, and, once the tool's own check
// passes its files (sudoers.CheckInstalled: visudo -c, doas -C), each
// tool's rules in the order the tool reads them (sudoers.Choose and
// sudoers.Read, then the files). The last rule that matches decides, as
// in sudo and doas; hardn reads one form of grant per tool (rules.go), and
// any other rule after it leaves the answer Unknown. A read that fails is
// the check's error, which the engine reports as Unknown; what it read but
// cannot establish a state from is Unknown with findings: never a guess.
// docs/engine/check-users.md has every status line the row can show.
package users

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"

	"github.com/abbott/hardn/pkg/engine"
	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
)

// The finding codes of this package (pkg/output's registry holds them).
const (
	// CodeNoPassword: a login user becomes root without a password
	// (sudo's NOPASSWD, doas's nopass).
	CodeNoPassword = "USERS_NOPASSWD"
	// CodeRuleNotRead: a rule in a form hardn does not read comes after
	// the last one it reads that matches a login user, so whether that
	// user becomes root is not established.
	CodeRuleNotRead = "USERS_RULE_NOT_READ"
	// CodeUID0: an account other than root has uid 0, so it is root.
	CodeUID0 = "USERS_UID0"
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
	login  detect.Tri // Yes: a login shell; Unknown: its stat failed (err)
	err    error
	grants []grant // one per tool, in readTools' order
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
	st.Evidence = evidence(tools, cs)
	return verdict(st, tools, cs)
}

// loginShell is whether shell lets its user log in: not nologin or false,
// and an executable regular file, which sshd requires [I]. An empty shell
// is /bin/sh (passwd(5)).
func loginShell(ctx context.Context, sys host.System, shell string) (detect.Tri, error) {
	if shell == "" {
		shell = "/bin/sh"
	}
	if b := path.Base(shell); b == "nologin" || b == "false" || !path.IsAbs(shell) {
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
func verdict(st engine.Status, tools []tool, cs []candidate) (engine.Status, error) {
	logins := 0
	for _, c := range cs {
		if c.login != detect.Yes {
			continue
		}
		logins++
		for i, g := range c.grants {
			switch {
			case g.tri != detect.Yes:
				continue
			case st.State != engine.Configured:
				st.State, st.Detail = engine.Configured, "non-root, "+tools[i].name
			}
			if g.nopass {
				st.Findings = append(st.Findings, engine.Finding{Code: CodeNoPassword, Detail: c.Name + ": " + tools[i].name + " without password", Source: g.by.at})
			}
		}
	}
	switch {
	case st.State == engine.Configured:
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
		if c.login == detect.Unknown && (e != detect.No || logins == 0) && err == nil {
			err, st.Detail = c.err, "can't read"
		}
		for i, g := range c.grants {
			switch {
			case e != detect.Unknown || g.tri != detect.Unknown:
			case g.err != nil && err == nil:
				err, st.Detail = g.err, tools[i].detail
			case g.err == nil:
				st.Findings = append(st.Findings, engine.Finding{Code: CodeRuleNotRead, Detail: "may decide " + tools[i].name + " for " + c.Name, Source: g.by.at})
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
	st.State, st.Detail = engine.Partial, "non-root, no "+installed(tools)
	return st, nil
}

// installed names the tools installed, for Partial's detail; sudo when
// none is (Figma's word).
func installed(tools []tool) string {
	var s string
	for _, t := range tools {
		if t.state == detect.Yes {
			if s != "" {
				s += " or "
			}
			s += t.name
		}
	}
	if s == "" {
		return "sudo"
	}
	return s
}

// evidence is one line per candidate and per tool's answer for it.
func evidence(tools []tool, cs []candidate) []string {
	var ev []string
	for _, t := range tools {
		switch t.state {
		case detect.No:
			ev = append(ev, t.name+": not installed")
		case detect.Unknown:
			ev = append(ev, t.name+": "+t.err.Error())
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
			l += ": " + c.err.Error()
		}
		ev = append(ev, l)
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
				ev = append(ev, n+": "+c.Name+" not established, "+g.by.at+": "+g.by.text)
			}
		}
	}
	return ev
}
