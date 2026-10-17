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

// Options configure Apply.
type Options struct {
	// RunDir is an existing absolute directory, reached with no symlink,
	// whose every directory from / is owned by root and writable by neither
	// group nor others, as sshd.Request.RunDir. The copies are written to
	// RunDir/WorkDir, which must not exist yet.
	RunDir string
}

// Result is what Apply did.
type Result struct {
	Installed []string // the paths written or removed, in order
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
// context.WithoutCancel, and checked again (StageRestored). An empty plan
// runs nothing.
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
	defer func() { res.Cleanup = sys.RemoveAll(context.WithoutCancel(ctx), work) }()
	if err := sys.MkdirAll(ctx, work, 0o700); err != nil {
		return res, fmt.Errorf("sudoers: %w", err)
	}

	if err := checkAll(ctx, sys, p.Tool, StageBefore); err != nil {
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
		if err = checkAll(ctx, sys, p.Tool, StageAfter); err == nil {
			return res, nil
		}
	}
	// A failed write may have left its file either way: put it back too.
	if len(done) < len(p.Changes) {
		done = append(done, p.Changes[len(done)])
	}
	return res, errors.Join(err, restore(context.WithoutCancel(ctx), sys, p.Tool, done))
}

// Rollback puts every file of p back as Read found it, last first, skipping
// those already so, then checks the whole configuration (StageRestored). It
// is idempotent. Run it under context.WithoutCancel after Ctrl-C: every
// host.System method refuses a done context.
func Rollback(ctx context.Context, sys host.System, p Plan) error {
	if len(p.Changes) == 0 {
		return nil
	}
	return restore(ctx, sys, p.Tool, p.Changes)
}

func restore(ctx context.Context, sys host.System, t Tool, cs []Change) error {
	var errs []error
	for i := len(cs) - 1; i >= 0; i-- {
		o := cs[i].Old
		now, err := readFile(ctx, sys, o.Path)
		switch {
		case err != nil:
		case sameFile(now, o):
			continue
		case o.Exists:
			err = sys.WriteFileAtomic(ctx, o.Path, o.Data, o.Mode, o.UID, o.GID)
		default:
			err = sys.Remove(ctx, o.Path)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("sudoers: restore %s: %w", o.Path, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return checkAll(ctx, sys, t, StageRestored)
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

// checkAll checks what the tool reads: for sudo, visudo -c (/etc/sudoers
// and the files it includes); for doas, which has no such check, each file
// it reads, as it reads them: /etc/doas.conf, then each regular *.conf in
// /etc/doas.d, owned by root and writable by neither group nor others, and
// accepted by doas -C.
func checkAll(ctx context.Context, sys host.System, t Tool, s Stage) error {
	if t == ToolSudo {
		return check(ctx, sys, s, MainConfig, []string{"visudo", "-c"})
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

// checkRunDir: as sshd's, every directory from / to dir is a real
// directory owned by root and writable by neither group nor others.
func checkRunDir(ctx context.Context, sys host.System, dir string) error {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || dir == "/" {
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
