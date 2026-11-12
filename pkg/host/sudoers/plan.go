package sudoers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/detect"
)

// File is a file as Read found it.
type File struct {
	Path     string
	Exists   bool
	Data     []byte
	Mode     fs.FileMode // permission bits
	UID, GID int
}

// RootsAlone is whether f, read where it exists, is a file only root owns
// and can write: owner and group 0, no group or other write bit, no bit
// beyond the permissions. Plan.Check refuses a change whose old file is
// not (a rollback would write it back so); a caller that says that
// refusal in words finds the file with it.
func (f File) RootsAlone() bool {
	return f.UID == 0 && f.GID == 0 && f.Mode&^fs.ModePerm == 0 && f.Mode&0o022 == 0
}

// same: f has exactly this content, mode and owner (root).
func (f File) same(data []byte, mode fs.FileMode) bool {
	return f.Exists && bytes.Equal(f.Data, data) && f.Mode == mode && f.UID == 0 && f.GID == 0
}

// FindingKind names a finding. Codes are stable (String).
type FindingKind uint8

const (
	// FindingLater: a file the tool reads after hardn's, so a rule there
	// that matches the user wins (the last matching rule wins).
	FindingLater FindingKind = iota + 1
	// FindingAfterInclude: the main sudoers file has lines after its
	// includedir; sudo reads them after the drop-ins.
	FindingAfterInclude
	// FindingLegacy: origin/main's /etc/sudoers.d/<user>, which held the
	// grant and the env_keep line in one file (V16). Reported, not changed.
	FindingLegacy
)

func (k FindingKind) String() string {
	switch k {
	case FindingLater:
		return "sudoers-later-file"
	case FindingAfterInclude:
		return "sudoers-after-include"
	case FindingLegacy:
		return "sudoers-legacy-file"
	}
	return "unknown"
}

// Finding is something to report; it does not stop a plan.
type Finding struct {
	Kind   FindingKind
	File   string
	Line   int
	Detail string
}

// State is what PlanFor plans from.
type State struct {
	Choice Choice
	// Main is, for sudo, the file it reads first, which includes the
	// drop-in directory: MainConfig, or SudoersRS for sudo-rs when that
	// exists. visudo -c checks the same file.
	Main string
	// Entries are the base names in the drop-in directory the tool reads,
	// in the order it reads them (C byte order): for sudo, the names with
	// no '.' and no trailing '~'; for doas, the names ending in ".conf".
	Entries  []string
	Files    map[string]File // hardn's files for the users read, by path
	Findings []Finding
}

// Read reads, for the users named, what PlanFor needs: whether the tool
// reads its drop-in directory, the directory's entries, and hardn's files.
// It changes nothing. Errors: Choice.Err for a choice that is not sudo or
// doas, ErrUserName, ErrNotIncluded, host.ErrSymlink for a symlink at one
// of hardn's files, and the host's own (EISDIR for a directory there).
func Read(ctx context.Context, sys host.System, c Choice, users []string) (State, error) {
	st := State{Choice: c, Files: map[string]File{}}
	var err error
	switch c.Tool {
	case ToolSudo:
		err = st.readSudo(ctx, sys)
	case ToolDoas:
		err = st.readDoas(ctx, sys)
	default:
		err = c.Err
		if err == nil {
			err = fmt.Errorf("sudoers: tool %s: %w", c.Tool, fs.ErrInvalid)
		}
	}
	if err != nil {
		return st, err
	}
	for _, u := range users {
		n, err := FileNames(c.Tool, u)
		if err != nil {
			return st, err
		}
		for _, p := range []string{n.Grant, n.Env} {
			if p == "" {
				continue
			}
			if st.Files[p], err = readFile(ctx, sys, p); err != nil {
				return st, err
			}
		}
	}
	return st, nil
}

func readFile(ctx context.Context, sys host.System, name string) (File, error) {
	f := File{Path: name}
	// ReadFileNoFollow refuses a symlink, a directory and any other file
	// that is not regular.
	data, fi, err := sys.ReadFileNoFollow(ctx, name)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return f, nil
	case err != nil:
		return f, fmt.Errorf("sudoers: %w", err)
	}
	f.Exists, f.Data, f.Mode = true, data, fi.Mode().Perm()
	f.UID, f.GID = -1, -1
	if uid, gid, ok := host.Owner(fi); ok {
		f.UID, f.GID = uid, gid
	}
	return f, nil
}

// readSudo: the main file must include /etc/sudoers.d, or a drop-in has no
// effect, and a grant hardn reports would not exist.
func (st *State) readSudo(ctx context.Context, sys host.System) error {
	var err error
	if st.Main, err = sudoMain(ctx, sys, st.Choice.Flavor); err != nil {
		return err
	}
	main, err := sys.ReadFile(ctx, st.Main)
	if err != nil {
		return fmt.Errorf("sudoers: %w", err)
	}
	line := 0
	lines := strings.Split(string(main), "\n")
	for i, l := range lines {
		f := strings.Fields(l)
		if len(f) == 2 && (f[0] == "@includedir" || f[0] == "#includedir") && strings.TrimSuffix(f[1], "/") == DropInDir {
			line = i + 1
		}
	}
	if line == 0 {
		return fmt.Errorf("sudoers: %s has no @includedir %s: %w", st.Main, DropInDir, ErrNotIncluded)
	}
	for i, l := range lines[line:] {
		if t := strings.TrimSpace(l); t != "" && !comment(t) {
			st.Findings = append(st.Findings, Finding{FindingAfterInclude, st.Main, line + 1 + i,
				"read after the drop-ins: a rule here that matches the user wins over hardn's"})
		}
	}
	names, err := entries(ctx, sys, DropInDir)
	for _, n := range names {
		if !strings.Contains(n, ".") && !strings.HasSuffix(n, "~") {
			st.Entries = append(st.Entries, n)
		}
	}
	return err
}

// sudoMain is the file sudo reads first. sudo-rs reads SudoersRS when it
// exists (a stat: a symlink to a file counts) and MainConfig otherwise; the
// sudo project's sudo reads MainConfig. With the flavour Unknown and
// SudoersRS present, which one is read is not established: ErrNotIncluded.
func sudoMain(ctx context.Context, sys host.System, flavor detect.SudoFlavor) (string, error) {
	if flavor == detect.SudoOriginal {
		return MainConfig, nil
	}
	_, err := sys.Stat(ctx, SudoersRS)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return MainConfig, nil
	case err != nil:
		return "", fmt.Errorf("sudoers: %w", err)
	case flavor == detect.SudoRS:
		return SudoersRS, nil
	}
	return "", fmt.Errorf("sudoers: %s exists and the sudo flavour is not established, so whether sudo reads it or %s is not known: %w",
		SudoersRS, MainConfig, ErrNotIncluded)
}

// comment: a trimmed sudoers line that is a comment. '#' starts one, except
// in "#include"/"#includedir" and in "#<digits>", which at the start of a
// line is a user ID: a rule.
func comment(t string) bool {
	return strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#include") && (len(t) == 1 || t[1] < '0' || t[1] > '9')
}

// readDoas: Alpine's doas reads /etc/doas.conf, then, when /etc/doas.d is
// a directory (lstat: not a symlink to one), its names ending in ".conf".
func (st *State) readDoas(ctx context.Context, sys host.System) error {
	fi, err := sys.Lstat(ctx, DoasDir)
	switch {
	case errors.Is(err, fs.ErrNotExist) || err == nil && !fi.IsDir():
		return fmt.Errorf("sudoers: %s is not a directory: %w", DoasDir, ErrNotIncluded)
	case err != nil:
		return fmt.Errorf("sudoers: %w", err)
	}
	names, err := entries(ctx, sys, DoasDir)
	for _, n := range names {
		if strings.HasSuffix(n, doasSuffix) {
			st.Entries = append(st.Entries, n)
		}
	}
	return err
}

// entries lists dir, which must be a directory: hardn does not create it.
func entries(ctx context.Context, sys host.System, dir string) ([]string, error) {
	fi, err := sys.Stat(ctx, dir)
	switch {
	case errors.Is(err, fs.ErrNotExist) || err == nil && !fi.IsDir():
		return nil, fmt.Errorf("sudoers: %s is not a directory: %w", dir, ErrNotIncluded)
	case err != nil:
		return nil, fmt.Errorf("sudoers: %w", err)
	}
	des, err := sys.ReadDir(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("sudoers: %w", err)
	}
	var names []string
	for _, d := range des {
		names = append(names, d.Name())
	}
	slices.Sort(names) // C byte order
	return names, nil
}

// Risk is what a change can cost the user.
type Risk uint8

const (
	RiskNone Risk = iota
	// RiskLockout: the change removes a grant, or makes one ask for the
	// user's password, which a user created without one (V17) cannot give.
	RiskLockout
)

func (r Risk) String() string {
	if r == RiskLockout {
		return "lockout"
	}
	return "none"
}

// Change is one file of a plan.
type Change struct {
	Path   string
	Old    File   // as Read found it; Apply refuses if it changed since
	Data   []byte // the new content; nil with Remove
	Mode   fs.FileMode
	Remove bool
	Risk   Risk
	// Check is the validation of the candidate, for the plan view; Apply
	// runs it on a copy in Options.RunDir, not on Path.
	Check []string
	Why   string
}

// Plan is the changes for a set of policies. No Changes: nothing to do.
type Plan struct {
	Tool Tool
	// Main is State.Main: for sudo, the file visudo -c checks, named in a
	// whole-configuration CheckError ("" is MainConfig).
	Main     string
	Changes  []Change
	Findings []Finding
}

// PlanFor plans policies against st, which Read made for the same users.
// It is pure. Errors: ErrUserName, ErrVarName, ErrEnvNeedsGrant (doas),
// fs.ErrInvalid for a user Read did not read, a repeated user or an
// unknown Grant.
func PlanFor(st State, policies []Policy) (Plan, error) {
	t := st.Choice.Tool
	p := Plan{Tool: t, Main: st.Main, Findings: slices.Clone(st.Findings)}
	var users []string
	later := map[string][]string{}
	for _, pol := range policies {
		n, err := FileNames(t, pol.User)
		if err != nil {
			return Plan{Tool: t}, err
		}
		if err := checkVars(pol.Env); err != nil {
			return Plan{Tool: t}, err
		}
		if slices.Contains(users, pol.User) || pol.Grant > GrantNoPassword {
			return Plan{Tool: t}, fmt.Errorf("sudoers: policy for %q repeated or of unknown grant: %w", pol.User, fs.ErrInvalid)
		}
		users = append(users, pol.User)
		var cs []Change
		switch t {
		case ToolSudo:
			var g, e []byte
			if pol.Grant != GrantNone {
				g = sudoGrant(pol.User, pol.Grant)
			}
			if len(pol.Env) > 0 {
				e = sudoEnv(pol.User, pol.Env)
			}
			cs = append(change(st, n.Grant, g, SudoMode), change(st, n.Env, e, SudoMode)...)
			if len(cs) > 0 && cs[0].Path == n.Grant {
				cs[0].Risk = risk(cs[0], pol.Grant)
			}
		default: // ToolDoas
			if pol.Grant == GrantNone && len(pol.Env) > 0 {
				return Plan{Tool: t}, fmt.Errorf("sudoers: %q: %w", pol.User, ErrEnvNeedsGrant)
			}
			var r []byte
			if pol.Grant != GrantNone {
				r = doasRule(pol.User, pol.Grant, pol.Env)
			}
			cs = change(st, n.Grant, r, DoasMode)
			if len(cs) > 0 {
				cs[0].Risk = risk(cs[0], pol.Grant)
			}
		}
		for _, c := range cs {
			if c.Path == "" {
				return Plan{Tool: t}, fmt.Errorf("sudoers: %q was not read: %w", pol.User, fs.ErrInvalid)
			}
		}
		p.Changes = append(p.Changes, cs...)
		// first is the first file, in the tool's order, that will hold a
		// rule of hardn's for the user; "" when hardn removes them all.
		var rules []string
		if pol.Grant != GrantNone {
			rules = append(rules, path.Base(n.Grant))
		}
		if t == ToolSudo && len(pol.Env) > 0 {
			rules = append(rules, path.Base(n.Env))
		}
		first := ""
		if len(rules) > 0 {
			first = slices.Min(rules)
		}
		for _, e := range st.Entries {
			switch {
			case t == ToolSudo && e == pol.User:
				removed := slices.ContainsFunc(cs, func(c Change) bool { return c.Path == n.Grant && c.Remove })
				p.Findings = append(p.Findings, Finding{FindingLegacy, DropInDir + "/" + e, 0, legacy(pol, e, first, removed)})
			case first != "" && e > first && !owned(t, e):
				later[e] = append(later[e], pol.User)
			}
		}
	}
	dir := DropInDir
	if t == ToolDoas {
		dir = DoasDir
	}
	for _, e := range st.Entries {
		if us := later[e]; len(us) > 0 {
			p.Findings = append(p.Findings, Finding{FindingLater, dir + "/" + e, 0,
				"read after hardn's rule for " + strings.Join(us, ", ") + ": a rule there that matches wins (the last match wins)"})
		}
	}
	return p, nil
}

// legacy is the detail of a sudoers-legacy-file finding for origin/main's
// file e, in the user's words, since the plan's review shows it as it is
// (next/s9-control; gallery-environment.md §7 item 22; proposed): read
// after first, hardn's first rule for the user, it may override that
// rule, as any later file may (FindingLater); where the policy grants
// nothing, a grant in it applies too: "also", or "still" where the plan
// removes hardn's grant (removed; adopted 2026-10-29); otherwise sudo
// reads hardn's rule after it, so hardn's wins.
func legacy(pol Policy, e, first string, removed bool) string {
	file := DropInDir + "/" + e
	after, none := first != "" && e > first, pol.Grant == GrantNone
	let := " also let "
	if removed {
		let = " still let "
	}
	switch {
	case after && none:
		return file + " may override hardn's rule and" + let + pol.User + " use sudo"
	case after:
		return file + " may override hardn's rule"
	case none:
		return file + " may" + let + pol.User + " use sudo"
	}
	return file + " is from an earlier hardn; hardn's rule wins"
}

// risk is the risk of a change to the file that carries a user's grant.
func risk(c Change, g Grant) Risk {
	if c.Path != "" && (c.Remove || g == GrantPassword) {
		return RiskLockout
	}
	return RiskNone
}

// change plans one file: written with data, or removed when data is nil.
// A path Read did not read gives a Change with no Path.
func change(st State, name string, data []byte, mode fs.FileMode) []Change {
	old, ok := st.Files[name]
	switch {
	case !ok:
		return []Change{{}}
	case data == nil && !old.Exists, data != nil && old.same(data, mode):
		return nil
	}
	c := Change{Path: name, Old: old, Data: data, Mode: mode}
	switch {
	case data == nil:
		c.Remove, c.Why = true, "remove hardn's file"
	case st.Choice.Tool == ToolSudo:
		c.Check, c.Why = []string{"visudo", "-cf", name}, "write hardn's file, checked by visudo first"
	default:
		c.Check, c.Why = []string{"doas", "-C", name}, "write hardn's file, checked by doas first"
	}
	return []Change{c}
}
