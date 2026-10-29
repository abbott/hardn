package sudoers

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// ErrEdited: a file of hardn's for a user holds what PlanFor writes for no
// Policy (edited by hand, or written by something else), so the user's
// Policy is not known (EditedError).
var ErrEdited = errors.New("hardn's file holds a rule hardn did not write")

// EditedError names a file of hardn's that PolicyOf could not read back as
// a Policy (ErrEdited). Its text is "sudoers: <path>: <ErrEdited>".
type EditedError struct {
	Path string
}

func (e *EditedError) Error() string { return "sudoers: " + e.Path + ": " + ErrEdited.Error() }

// Unwrap returns ErrEdited.
func (e *EditedError) Unwrap() error { return ErrEdited }

// PolicyOf is user's whole Policy, grant and variables, as hardn's files in
// st hold it: the Policy PlanFor wrote them for, so that planning it again
// plans nothing, and a caller that changes one part of it (slice 9 its
// variables, slice 5 its grant) carries the other. Pure: Read reads the
// files, for the same user.
//
// A file is read back only when its content is exactly what PlanFor
// writes, byte for byte, for some grant and variable list (sudoGrant,
// sudoEnv, doasRule): any other content, a comment or a rule added, the
// list empty, a name repeated or not a variable's, is an *EditedError
// naming the file, one per file, joined when there are two; never a
// guess. The mode and owner are not read here: a wrong one is rewritten by
// the next plan, as PlanFor rewrites it for any Policy. A file that is not
// there contributes nothing: no grant, no variables.
//
// Errors besides: ErrUserName, and fs.ErrInvalid for a tool that is not
// sudo or doas or a user Read did not read.
func PolicyOf(st State, user string) (Policy, error) {
	n, err := FileNames(st.Choice.Tool, user)
	if err != nil {
		return Policy{}, err
	}
	pol := Policy{User: user}
	var errs []error
	for _, name := range []string{n.Grant, n.Env} {
		if name == "" {
			continue
		}
		f, ok := st.Files[name]
		switch {
		case !ok:
			return Policy{}, fmt.Errorf("sudoers: %q was not read: %w", user, fs.ErrInvalid)
		case !f.Exists:
			continue
		}
		switch {
		case st.Choice.Tool == ToolDoas:
			pol.Grant, pol.Env, ok = readDoasRule(user, f.Data)
		case name == n.Grant:
			pol.Grant, ok = readSudoGrant(user, f.Data)
		default:
			pol.Env, ok = readSudoEnv(user, f.Data)
		}
		if !ok {
			errs = append(errs, &EditedError{Path: name})
		}
	}
	switch len(errs) {
	case 0:
		return pol, nil
	case 1:
		return Policy{}, errs[0]
	}
	return Policy{}, errors.Join(errs...)
}

// grants are the grants a file of hardn's holds, in the order tried.
var grants = []Grant{GrantPassword, GrantNoPassword}

// readSudoGrant is the grant of hardn's sudo grant file data, ok when data
// is sudoGrant's for it.
func readSudoGrant(user string, data []byte) (Grant, bool) {
	for _, g := range grants {
		if bytes.Equal(data, sudoGrant(user, g)) {
			return g, true
		}
	}
	return GrantNone, false
}

// readSudoEnv is the variables of hardn's env_keep file data, ok when
// data is sudoEnv's for them.
func readSudoEnv(user string, data []byte) ([]string, bool) {
	const suffix = "\"\n"
	list, ok := between(data, strings.TrimSuffix(string(sudoEnv(user, []string{""})), suffix), suffix)
	if !ok {
		return nil, false
	}
	vars, ok := varList(list)
	return vars, ok && bytes.Equal(data, sudoEnv(user, vars))
}

// readDoasRule is the grant and variables of hardn's doas rule data, ok
// when data is doasRule's for them: the rule with no variables, or with
// " setenv { <list> }" before the user.
func readDoasRule(user string, data []byte) (Grant, []string, bool) {
	suffix := " " + user + "\n"
	for _, g := range grants {
		opts, ok := between(data, strings.TrimSuffix(string(doasRule(user, g, nil)), suffix), suffix)
		if !ok {
			continue
		}
		var vars []string
		if opts != "" {
			list, ok := between([]byte(opts), " setenv { ", " }")
			if !ok {
				continue
			}
			if vars, ok = varList(list); !ok {
				continue
			}
		}
		if bytes.Equal(data, doasRule(user, g, vars)) {
			return g, vars, true
		}
	}
	return GrantNone, nil, false
}

// between is data with prefix and suffix taken off, ok when it has both
// and they do not overlap.
func between(data []byte, prefix, suffix string) (string, bool) {
	s := string(data)
	if len(s) < len(prefix)+len(suffix) || !strings.HasPrefix(s, prefix) || !strings.HasSuffix(s, suffix) {
		return "", false
	}
	return s[len(prefix) : len(s)-len(suffix)], true
}

// varList is list, the variables as PlanFor joins them (one space
// between), ok when PlanFor takes each (checkVars): none empty, so at
// least one, each once.
func varList(list string) ([]string, bool) {
	vars := strings.Split(list, " ")
	if checkVars(vars) != nil {
		return nil, false
	}
	return vars, true
}
