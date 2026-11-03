package users

import (
	"context"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine/steps"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/accounts"
	"github.com/abbott/hardn/pkg/host/detect"
	"github.com/abbott/hardn/pkg/host/sudoers"
)

// stillRounds is the most rules StillAdmin hides in turn.
const stillRounds = 32

// Still is what would still make a user an admin once hardn's grant is
// gone: every rule that would grant it, in the order found; or what is not
// read, where that is not established ("" when it is).
type Still struct {
	By      []Rule
	Unknown string // a rule's file:line, "sudo's rules", "the accounts"
}

// StillAdmin reads, with the real reader (ReadElevation), what still makes
// user an admin once hardn's grant is gone (decision 2 (a)): hardn's file
// hidden, then each deciding rule in turn (a file, or a group's line), at
// most stillRounds rounds, until none grants. A rule that asks a password
// the user does not have (r's Password not Set) is not counted, as it
// cannot be used, but is hidden all the same. A rule not read, or rules
// not read at all, is Unknown. It changes nothing.
func StillAdmin(ctx context.Context, sys host.System, r Reading, user string) Still {
	rules := r.toolWord() + "'s rules"
	n, err := sudoers.FileNames(r.Choice.Tool, user)
	if err != nil {
		return Still{Unknown: rules}
	}
	pw := accounts.PasswordNotRead
	if u, ok := r.user(user); ok {
		pw = u.Password
	}
	hs := hiding{System: sys, files: []string{n.Grant}, lines: map[string][]int{}}
	var out Still
	for range stillRounds {
		db, err := accounts.Read(ctx, hs)
		if err != nil {
			return Still{Unknown: "the accounts"}
		}
		i := slices.IndexFunc(db.Users, func(u accounts.User) bool { return u.Name == user })
		if i < 0 {
			return out
		}
		decided := Grant{Grants: detect.No}
		for _, g := range ReadElevation(ctx, hs, r.facts).GrantsOf(db, db.Users[i]) {
			if g.Grants == detect.Yes || (g.Grants == detect.Unknown && decided.Grants == detect.No) {
				decided = g
			}
		}
		switch d := decided.Rule; {
		case decided.Grants == detect.No:
			return out
		case decided.Grants == detect.Unknown && d.Kind != RuleNone:
			return Still{Unknown: steps.SourceAt(d.File, d.Line)}
		case decided.Grants == detect.Unknown, d.File == "":
			return Still{Unknown: rules}
		case d.Kind == RuleGroup:
			hs.lines[d.File] = append(hs.lines[d.File], d.Line)
		default:
			hs.files = append(hs.files, d.File)
		}
		if decided.NoPassword || pw == accounts.PasswordSet {
			out.By = append(out.By, decided.Rule)
		}
	}
	return Still{Unknown: rules}
}

// hiding is a System with files and lines hidden: the host as a plan that
// removes them would leave it, for the reader. It writes nothing.
type hiding struct {
	host.System
	files []string
	lines map[string][]int // a file's lines, from 1, read as blank
}

func (h hiding) hidden(name string) bool { return slices.Contains(h.files, name) }

// blank is data with name's hidden lines blank, their numbers kept.
func (h hiding) blank(name string, data []byte) []byte {
	if len(h.lines[name]) == 0 {
		return data
	}
	ls := strings.Split(string(data), "\n")
	for _, n := range h.lines[name] {
		if n >= 1 && n <= len(ls) {
			ls[n-1] = ""
		}
	}
	return []byte(strings.Join(ls, "\n"))
}

func (h hiding) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if h.hidden(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	data, err := h.System.ReadFile(ctx, name)
	return h.blank(name, data), err
}

func (h hiding) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if h.hidden(name) {
		return nil, nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	data, fi, err := h.System.ReadFileNoFollow(ctx, name)
	return h.blank(name, data), fi, err
}

func (h hiding) ScanLines(ctx context.Context, name string, yield func(host.Line) bool) (fs.FileInfo, error) {
	if h.hidden(name) {
		return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
	}
	n := 0
	return h.System.ScanLines(ctx, name, func(l host.Line) bool {
		n++
		if slices.Contains(h.lines[name], n) {
			l = host.Line{}
		}
		return yield(l)
	})
}

func (h hiding) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if h.hidden(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return h.System.Stat(ctx, name)
}

func (h hiding) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if h.hidden(name) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrNotExist}
	}
	return h.System.Lstat(ctx, name)
}

func (h hiding) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	es, err := h.System.ReadDir(ctx, name)
	return slices.DeleteFunc(es, func(e fs.DirEntry) bool { return h.hidden(path.Join(name, e.Name())) }), err
}
