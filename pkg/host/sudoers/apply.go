package sudoers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/host"
)

// Stage is when a check ran.
type Stage uint8

const (
	StageBefore    Stage = iota + 1 // the whole configuration, before any write
	StageCandidate                  // one candidate, as a copy in the run directory
	StageAfter                      // the whole configuration, with the plan installed
	StageRestored                   // the whole configuration, after the old files were put back
)

func (s Stage) String() string {
	switch s {
	case StageBefore:
		return "before"
	case StageCandidate:
		return "candidate"
	case StageAfter:
		return "after"
	case StageRestored:
		return "restored"
	}
	return "unknown"
}

// CheckError is a check that failed: the tool rejected a file (a
// *host.ExitError in Err), a doas file is not root's alone (ErrPerms), or
// the check could not run (the host's error, such as host.ErrNotFound or
// ctx.Err()). errors.Is(err, ErrRejected) holds for the first two only.
type CheckError struct {
	Stage Stage
	Path  string   // the real path checked; for a candidate, its target
	Argv  []string // the check that ran, naming the copy for a candidate
	Tail  []string
	Err   error
}

func (e *CheckError) Error() string {
	msg := fmt.Sprintf("sudoers: %s check of %s: %v", e.Stage, e.Path, e.Err)
	if len(e.Tail) > 0 {
		msg += ": " + e.Tail[len(e.Tail)-1]
	}
	return msg
}

// Is makes errors.Is(err, ErrRejected) hold when the tool, or doas's
// ownership rule, rejected the file.
func (e *CheckError) Is(target error) bool {
	var exit *host.ExitError
	return target == ErrRejected && (errors.As(e.Err, &exit) || errors.Is(e.Err, ErrPerms))
}

func (e *CheckError) Unwrap() error { return e.Err }

// DefaultRestoreTimeout bounds Apply's restore and the removal of its
// copies when Options.RestoreTimeout is not positive.
const DefaultRestoreTimeout = time.Minute

// Options configure Apply.
type Options struct {
	// RunDir is an existing absolute directory, reached with no symlink,
	// whose every directory from / is owned by root and writable by neither
	// group nor others, with no space, quote, backslash, '#' or glob
	// character: as sshd.Request.RunDir. The copies are written to
	// RunDir/WorkDir, which must not exist yet.
	RunDir string
	// RestoreTimeout bounds the restore after a failed write or StageAfter,
	// and the removal of the copies. Both run under
	// context.WithoutCancel(ctx), so a Ctrl-C does not stop them, and so
	// without ctx's deadline; this is theirs. Not positive:
	// DefaultRestoreTimeout.
	RestoreTimeout time.Duration
}

// Result is what Apply did.
type Result struct {
	// Installed are the paths written or removed, in order, including
	// those a restore then put back.
	Installed []string
	// Restored: a write or StageAfter failed, and every file is back as
	// Read found it and the configuration checked (StageRestored). When
	// that restore did not complete, the error wraps ErrNotRestored.
	Restored bool
	// Cleanup is the error removing the copies gave; it does not change
	// the outcome.
	Cleanup error
}

// Apply installs p. In order, it stops at the first failure: check the
// whole configuration (StageBefore); write each candidate to a copy in
// RunDir/WorkDir, mode and owner as installed, and check it
// (StageCandidate); re-read each file and refuse one that changed since
// Read (ErrChanged); write each file with WriteFileAtomic (root:root) or
// remove it; check the whole configuration again (StageAfter). Up to the
// writes nothing under /etc changes. When a write or StageAfter fails, the
// files written are put back as Read found them, under
// context.WithoutCancel with Options.RestoreTimeout, and checked again
// (StageRestored): Result.Restored, or an error wrapping ErrNotRestored. An
// empty plan runs nothing.
func Apply(ctx context.Context, sys host.System, p Plan, opts Options) (res Result, err error) {
	if len(p.Changes) == 0 {
		return res, nil
	}
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("sudoers: apply: %w", err)
	}
	if p.Tool != ToolSudo && p.Tool != ToolDoas {
		return res, fmt.Errorf("sudoers: tool %s: %w", p.Tool, fs.ErrInvalid)
	}
	if err := checkRunDir(ctx, sys, opts.RunDir); err != nil {
		return res, err
	}
	work := filepath.Join(opts.RunDir, WorkDir)
	if _, err := sys.Lstat(ctx, work); !errors.Is(err, fs.ErrNotExist) {
		if err == nil {
			err = fs.ErrExist
		}
		return res, fmt.Errorf("sudoers: %s: %w", work, err)
	}
	defer func() {
		cctx, cancel := opts.restoreContext(ctx)
		defer cancel()
		res.Cleanup = sys.RemoveAll(cctx, work)
	}()
	if err := sys.MkdirAll(ctx, work, 0o700); err != nil {
		return res, fmt.Errorf("sudoers: %w", err)
	}

	if err := checkAll(ctx, sys, p, StageBefore); err != nil {
		return res, err
	}
	for _, c := range p.Changes {
		if c.Remove {
			continue
		}
		cp := work + "/" + path.Base(c.Path)
		if err := sys.WriteFileAtomic(ctx, cp, c.Data, c.Mode, 0, 0); err != nil {
			return res, fmt.Errorf("sudoers: %w", err)
		}
		if err := check(ctx, sys, StageCandidate, c.Path, checkArgv(p.Tool, cp)); err != nil {
			return res, err
		}
	}
	for _, c := range p.Changes {
		now, err := readFile(ctx, sys, c.Path)
		if err != nil {
			return res, err
		}
		if !sameFile(now, c.Old) {
			return res, fmt.Errorf("sudoers: %s: %w", c.Path, ErrChanged)
		}
	}
	var done []Change
	for _, c := range p.Changes {
		if err = install(ctx, sys, c); err != nil {
			break
		}
		done = append(done, c)
		res.Installed = append(res.Installed, c.Path)
	}
	if err == nil {
		if err = checkAll(ctx, sys, p, StageAfter); err == nil {
			return res, nil
		}
	}
	// A failed write leaves its file as it was or as planned (an atomic
	// write): put it back too.
	if len(done) < len(p.Changes) {
		done = append(done, p.Changes[len(done)])
	}
	rctx, cancel := opts.restoreContext(ctx)
	defer cancel()
	rerr := restore(rctx, sys, p, done)
	res.Restored = rerr == nil
	return res, errors.Join(err, rerr)
}

// restoreContext is ctx without its cancellation and deadline, bounded by
// RestoreTimeout instead.
func (o Options) restoreContext(ctx context.Context) (context.Context, context.CancelFunc) {
	d := o.RestoreTimeout
	if d <= 0 {
		d = DefaultRestoreTimeout
	}
	return context.WithTimeout(context.WithoutCancel(ctx), d)
}

// Rollback undoes p: last first, a file as Read found it is left as it is,
// one holding what p wrote (or absent, for a Remove) is put back as Read
// found it, and any other is left alone and reported (ErrChanged), so a
// change another writer made, even one Apply refused with ErrChanged, is
// never undone. Then it checks the whole configuration (StageRestored).
// A plan Check refuses is refused before anything is read or written.
// Every failure wraps ErrNotRestored. It is idempotent. Run it under
// context.WithoutCancel, with a timeout, after Ctrl-C: every host.System
// method refuses a done context.
func Rollback(ctx context.Context, sys host.System, p Plan) error {
	if len(p.Changes) == 0 {
		return nil
	}
	if err := p.Check(); err != nil {
		return fmt.Errorf("sudoers: rollback refused, nothing undone: %w: %w", ErrNotRestored, err)
	}
	return restore(ctx, sys, p, p.Changes)
}

// Check refuses, with fs.ErrInvalid, a plan PlanFor would not make, so
// that a rollback from p2-revert's journal touches only hardn's own files
// (pkg/host/revert/plans): a tool that is not sudo or doas; a Main other
// than the file the tool reads first ("", MainConfig or SudoersRS for
// sudo, "" for doas); a change to a path that is not one of hardn's
// per-user files in the tool's drop-in directory (FileNames' names), or to
// one path twice; an Old read from another path, or holding data for a
// file that did not exist; a mode other than the tool's (SudoMode,
// DoasMode); a removal with content, or of a file that did not exist; a
// write whose content does not start with Header. It is pure.
func (p Plan) Check() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("sudoers: plan: %s: %w", fmt.Sprintf(format, a...), fs.ErrInvalid)
	}
	var dir string
	var mode fs.FileMode
	switch p.Tool {
	case ToolSudo:
		dir, mode = DropInDir, SudoMode
		if p.Main != "" && p.Main != MainConfig && p.Main != SudoersRS {
			return bad("main file %q", p.Main)
		}
	case ToolDoas:
		dir, mode = DoasDir, DoasMode
		if p.Main != "" {
			return bad("main file %q for doas", p.Main)
		}
	default:
		return bad("tool %s", p.Tool)
	}
	seen := map[string]bool{}
	for _, c := range p.Changes {
		switch {
		case c.Path != dir+"/"+path.Base(c.Path) || !owned(p.Tool, path.Base(c.Path)):
			return bad("%q is not one of hardn's files in %s", c.Path, dir)
		case seen[c.Path]:
			return bad("%s is changed twice", c.Path)
		case c.Old.Path != c.Path || !c.Old.Exists && len(c.Old.Data) > 0:
			return bad("%s: its old state is of %q, exists %v", c.Path, c.Old.Path, c.Old.Exists)
		case c.Mode != mode:
			return bad("%s: mode %v, want %v", c.Path, c.Mode, mode)
		case c.Remove && (c.Data != nil || !c.Old.Exists):
			return bad("%s: a removal with content, or of a file that did not exist", c.Path)
		case !c.Remove && !bytes.HasPrefix(c.Data, []byte(Header)):
			return bad("%s: not hardn's content (%q first)", c.Path, Header)
		}
		seen[c.Path] = true
	}
	return nil
}

// restore is Rollback over cs, a prefix of p.Changes.
func restore(ctx context.Context, sys host.System, p Plan, cs []Change) error {
	var errs []error
	for i := len(cs) - 1; i >= 0; i-- {
		c := cs[i]
		now, err := readFile(ctx, sys, c.Path)
		switch {
		case err != nil:
		case sameFile(now, c.Old):
			continue
		case !planned(now, c):
			err = ErrChanged
		case c.Old.Exists:
			err = sys.WriteFileAtomic(ctx, c.Path, c.Old.Data, c.Old.Mode, c.Old.UID, c.Old.GID)
		default:
			err = sys.Remove(ctx, c.Path)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", c.Path, err))
		}
	}
	err := errors.Join(errs...)
	if err == nil {
		err = checkAll(ctx, sys, p, StageRestored)
	}
	if err != nil {
		return fmt.Errorf("sudoers: %w: %w", ErrNotRestored, err)
	}
	return nil
}

// planned: f is what c installs (absent, for a Remove).
func planned(f File, c Change) bool {
	if c.Remove {
		return !f.Exists
	}
	return f.same(c.Data, c.Mode)
}

func install(ctx context.Context, sys host.System, c Change) error {
	var err error
	if c.Remove {
		err = sys.Remove(ctx, c.Path)
	} else {
		err = sys.WriteFileAtomic(ctx, c.Path, c.Data, c.Mode, 0, 0)
	}
	if err != nil {
		return fmt.Errorf("sudoers: %w", err)
	}
	return nil
}

func sameFile(a, b File) bool {
	return a.Exists == b.Exists && bytes.Equal(a.Data, b.Data) && a.Mode == b.Mode && a.UID == b.UID && a.GID == b.GID
}

func checkArgv(t Tool, name string) []string {
	if t == ToolSudo {
		return []string{"visudo", "-cf", name}
	}
	return []string{"doas", "-C", name}
}

func check(ctx context.Context, sys host.System, s Stage, real string, argv []string) error {
	res, err := sys.Run(ctx, argv[0], argv[1:]...)
	if err != nil {
		return &CheckError{Stage: s, Path: real, Argv: argv, Tail: res.Tail, Err: err}
	}
	return nil
}

// checkAll checks what the tool reads: for sudo, visudo -c (the main file,
// Plan.Main, and the files it includes); for doas, which has no such
// check, each file it reads, as it reads them: /etc/doas.conf, then each
// regular *.conf in /etc/doas.d, owned by root and writable by neither
// group nor others, and accepted by doas -C.
func checkAll(ctx context.Context, sys host.System, p Plan, s Stage) error {
	if p.Tool == ToolSudo {
		main := p.Main
		if main == "" {
			main = MainConfig
		}
		return check(ctx, sys, s, main, []string{"visudo", "-c"})
	}
	files := []string{DoasConfig}
	fi, err := sys.Lstat(ctx, DoasDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return &CheckError{Stage: s, Path: DoasDir, Err: err}
	}
	if err == nil && fi.IsDir() {
		names, err := entries(ctx, sys, DoasDir)
		if err != nil {
			return &CheckError{Stage: s, Path: DoasDir, Err: err}
		}
		for _, n := range names {
			if strings.HasSuffix(n, doasSuffix) {
				files = append(files, DoasDir+"/"+n)
			}
		}
	}
	for _, f := range files {
		fi, err := sys.Stat(ctx, f)
		switch {
		case err != nil:
			return &CheckError{Stage: s, Path: f, Err: err}
		case !fi.Mode().IsRegular() && f != DoasConfig:
			continue // doas skips it
		}
		if uid, _, ok := host.Owner(fi); !ok || uid != 0 || fi.Mode().Perm()&0o022 != 0 {
			return &CheckError{Stage: s, Path: f, Err: ErrPerms}
		}
		if err := check(ctx, sys, s, f, []string{"doas", "-C", f}); err != nil {
			return err
		}
	}
	return nil
}

// checkRunDir: as sshd's, dir is absolute and clean, with none of the
// characters sshd's check refuses (harmless here, where no argv is parsed,
// but one run directory passes both checks or neither), and every
// directory from / to dir is a real directory owned by root and writable
// by neither group nor others.
func checkRunDir(ctx context.Context, sys host.System, dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" || strings.ContainsAny(dir, " \t\r\n\"'\\#*?[]{}") {
		return fmt.Errorf("sudoers: run directory %q: %w", dir, fs.ErrInvalid)
	}
	for p := dir; ; p = filepath.Dir(p) {
		fi, err := sys.Lstat(ctx, p)
		if err != nil {
			return fmt.Errorf("sudoers: run directory: %w", err)
		}
		if uid, _, ok := host.Owner(fi); !fi.IsDir() || !ok || uid != 0 || fi.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("sudoers: %s: %w", p, ErrUnsafeRunDir)
		}
		if p == "/" {
			return nil
		}
	}
}
