package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Real is the System of the machine hardn runs on.
type Real struct {
	log  *RunLog
	tail int

	// euid is the user hardn runs as: directories it owns are trusted by
	// the guarded operations (see System), as root's are.
	euid int

	// Seams for the tests in this package; NewReal sets the real ones.
	dirs  []string                          // SearchPath, split
	stat  func(string) (fs.FileInfo, error) // resolves a command candidate
	exec  func(*exec.Cmd) (int, error)      // runs cmd: exit code, error
	spawn func(*exec.Cmd) (int, error)      // starts cmd, detached: process id
	// fail, when set, is asked before each step of WriteFileAtomic; an
	// error makes that step fail with it (crash-safety tests).
	fail func(step string) error
	// interactiveWait is how long RunInteractive waits after SIGTERM.
	interactiveWait time.Duration
}

var _ LegacySystem = (*Real)(nil)

// commandEnv is the whole environment of every command Real runs, but
// RunInteractive's, which is what its caller passes and this PATH. Nothing is
// inherited from hardn's own environment: no PATH, IFS, ENV, BASH_ENV,
// LD_PRELOAD or locale from whoever started hardn.
var commandEnv = []string{"PATH=" + SearchPath, "LC_ALL=C"}

// pipeWaitDelay bounds how long Run waits, once the command has exited or
// been killed, for output pipes a child it started (a daemon) still holds.
// After it, os/exec closes them: the holder's later writes fail with EPIPE,
// or kill it with SIGPIPE when it is a Go program writing to fd 1 or 2.
const pipeWaitDelay = 10 * time.Second

// NewReal returns the System of this machine. It does no I/O.
func NewReal(opts Options) *Real {
	return &Real{
		log:   NewRunLog(opts.RunLog),
		tail:  opts.Tail(),
		euid:  os.Geteuid(),
		dirs:  strings.Split(SearchPath, ":"),
		stat:  os.Stat,
		exec:  runCmd,
		spawn: spawnCmd,

		interactiveWait: interactiveWait,
	}
}

// LogErr returns the first error the run log's writer returned, if any.
func (r *Real) LogErr() error { return r.log.Err() }

func runCmd(cmd *exec.Cmd) (int, error) {
	err := cmd.Run()
	if cmd.ProcessState != nil {
		return cmd.ProcessState.ExitCode(), err
	}
	return -1, err
}

// Run runs name with args, with stdin empty, and waits for it.
//
//   - name without a slash is looked up only in SearchPath; an absolute name is
//     used as is; a relative path is refused. Either way ErrNotFound when it
//     is not an executable file, and nothing runs.
//   - args are passed as they are, one argv element each, through exec(3):
//     never a shell, so spaces, quotes and ';' carry no meaning.
//   - The environment is exactly PATH=SearchPath and LC_ALL=C, and the
//     working directory is "/".
//   - A non-zero exit returns *ExitError; ctx done kills the process Run
//     started (os/exec's default Cancel, Process.Kill) and returns an error
//     wrapping ctx.Err(). Processes that one started are not killed: the
//     command stays in hardn's process group, or in a session of its own
//     while OwnSessions holds, and a pipeline's or a wrapper's children
//     can outlive the cancellation.
//   - Once the command has exited or been killed, Run waits at most 10 s for
//     output pipes a child it started still holds open, so a daemon cannot
//     hang it; then the pipes are closed under the child (pipeWaitDelay). A
//     process meant to outlive Run must not inherit them.
//
// The Result is filled in on every return, and the run log gets an entry for
// every call.
func (r *Real) Run(ctx context.Context, name string, args ...string) (Result, error) {
	return r.run(ctx, nil, name, args)
}

// RunInput is Run with stdin as the command's standard input.
func (r *Real) RunInput(ctx context.Context, stdin []byte, name string, args ...string) (Result, error) {
	if stdin == nil {
		stdin = []byte{}
	}
	return r.run(ctx, stdin, name, args)
}

func (r *Real) run(ctx context.Context, stdin []byte, name string, args []string) (Result, error) {
	argv := append([]string{name}, args...)
	res := Result{Argv: argv, ExitCode: -1}
	stdinLen := -1
	if stdin != nil {
		stdinLen = len(stdin)
	}
	id := r.log.Begin(argv, stdinLen)
	err := r.start(ctx, &res, stdin, name, args)
	r.log.End(id, res, err)
	return res, err
}

func (r *Real) start(ctx context.Context, res *Result, stdin []byte, name string, args []string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("host: run %s: %w", FormatArgv(res.Argv), err)
	}
	path, err := r.resolve(name)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Args[0] = name // as exec.Command sets it; busybox applets dispatch on it
	cmd.Env = append([]string(nil), commandEnv...)
	cmd.Dir = "/"
	cmd.WaitDelay = pipeWaitDelay
	if inOwnSession() {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	combined := &lockedBuffer{}
	cmd.Stdout = multiWriter{&stdout, combined}
	cmd.Stderr = multiWriter{&stderr, combined}

	code, runErr := r.exec(cmd)

	res.Stdout, res.Stderr, res.Combined = stdout.Bytes(), stderr.Bytes(), combined.Bytes()
	res.ExitCode = code
	res.Tail = TailLines(res.Combined, r.tail)
	return runOutcome(ctx, res, code, runErr)
}

// runOutcome is what a command that was started returns: nil, or an
// *ExitError, or why it did not finish.
func runOutcome(ctx context.Context, res *Result, code int, runErr error) error {
	var xe *exec.ExitError
	switch {
	case code == 0 && (runErr == nil || errors.Is(runErr, exec.ErrWaitDelay)):
		// Exit 0 is success even when a child kept the pipes open past
		// pipeWaitDelay; its later writes to them fail (pipeWaitDelay).
		return nil
	case ctx.Err() != nil:
		return fmt.Errorf("host: run %s: %w (%v)", FormatArgv(res.Argv), ctx.Err(), runErr)
	case runErr == nil, errors.As(runErr, &xe):
		return &ExitError{Argv: res.Argv, ExitCode: code, Tail: res.Tail, Err: runErr}
	default:
		return fmt.Errorf("host: run %s: %w", FormatArgv(res.Argv), runErr)
	}
}

// resolve finds the executable for name; see Run.
func (r *Real) resolve(name string) (string, error) {
	var candidates []string
	switch {
	case name == "":
		return "", fmt.Errorf("host: empty command name: %w", ErrNotFound)
	case strings.Contains(name, "/"):
		if !filepath.IsAbs(name) {
			return "", fmt.Errorf("host: %q: relative command path refused: %w", name, ErrNotFound)
		}
		candidates = []string{name}
	default:
		for _, d := range r.dirs {
			candidates = append(candidates, d+"/"+name)
		}
	}
	for _, c := range candidates {
		if fi, err := r.stat(c); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return c, nil
		}
	}
	if len(candidates) == 1 && strings.Contains(name, "/") {
		return "", fmt.Errorf("host: %q: not an executable file: %w", name, ErrNotFound)
	}
	return "", fmt.Errorf("host: %q: not in %s: %w", name, strings.Join(r.dirs, ":"), ErrNotFound)
}

// lockedBuffer is the combined-output buffer stdout's and stderr's copying
// goroutines both write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Bytes()
}

// multiWriter writes p to its own buffer, then to the combined one. Stdout
// and stderr get different multiWriters, so os/exec gives each its own pipe
// and copying goroutine: the combined buffer keeps each stream's order, but
// not the exact order across them.
type multiWriter struct {
	own      *bytes.Buffer
	combined *lockedBuffer
}

func (w multiWriter) Write(p []byte) (int, error) {
	w.own.Write(p)
	return w.combined.Write(p)
}

// ReadFile is os.ReadFile.
func (r *Real) ReadFile(ctx context.Context, name string) ([]byte, error) {
	if err := ctxErr(ctx, "read", name); err != nil {
		return nil, err
	}
	return os.ReadFile(name)
}

// ReadFileNoFollow reads what openRegular opens.
func (r *Real) ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error) {
	if err := ctxErr(ctx, "read", name); err != nil {
		return nil, nil, err
	}
	f, fi, err := r.openRegular(name)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	return data, fi, nil
}

// openRegular checks what name is before opening it (a FIFO or a device is
// never opened), then opens it with O_NOFOLLOW|O_NONBLOCK in its guarded
// parent and returns it with the FileInfo of that one descriptor, which is
// asked again, as name could have been swapped between the look and the open.
func (r *Real) openRegular(name string) (*os.File, fs.FileInfo, error) {
	dir, base, err := r.openParent("open", name)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = dir.Close() }()
	f, err := openNoFollow(dir, base, name, regularOnly)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err == nil {
		err = regularOnly(name, fi.Mode())
	}
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	return f, fi, nil
}

// openNoFollow opens base in dir read-only with O_NOFOLLOW|O_NONBLOCK, after
// accept has passed what lstat says it is.
func openNoFollow(dir *os.File, base, name string, accept func(string, fs.FileMode) error) (*os.File, error) {
	t, err := typeAt(int(dir.Fd()), base)
	if err != nil {
		return nil, pathErr("lstat", name, err)
	}
	if err := accept(name, t); err != nil {
		return nil, err
	}
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	switch {
	case isNoFollow(err):
		return nil, pathErr("open", name, ErrSymlink)
	case err != nil:
		return nil, pathErr("open", name, err)
	}
	return os.NewFile(uintptr(fd), name), nil
}

// regularOnly refuses what ReadFileNoFollow does not read.
func regularOnly(name string, m fs.FileMode) error {
	switch {
	case m&fs.ModeSymlink != 0:
		return pathErr("open", name, ErrSymlink)
	case m.IsDir():
		return pathErr("read", name, syscall.EISDIR)
	case !m.IsRegular():
		return pathErr("open", name, fs.ErrInvalid)
	}
	return nil
}

// Stat is os.Stat.
func (r *Real) Stat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := ctxErr(ctx, "stat", name); err != nil {
		return nil, err
	}
	return os.Stat(name)
}

// Lstat is os.Lstat.
func (r *Real) Lstat(ctx context.Context, name string) (fs.FileInfo, error) {
	if err := ctxErr(ctx, "lstat", name); err != nil {
		return nil, err
	}
	return os.Lstat(name)
}

// ReadDir is os.ReadDir.
func (r *Real) ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error) {
	if err := ctxErr(ctx, "readdir", name); err != nil {
		return nil, err
	}
	return os.ReadDir(name)
}

// MkdirAll is os.MkdirAll (perm before the umask).
func (r *Real) MkdirAll(ctx context.Context, name string, perm fs.FileMode) error {
	if err := ctxErr(ctx, "mkdir", name); err != nil {
		return err
	}
	return os.MkdirAll(name, perm)
}

// Remove is os.Remove (unlink, then rmdir) relative to name's guarded
// parent.
func (r *Real) Remove(ctx context.Context, name string) error {
	if err := ctxErr(ctx, "remove", name); err != nil {
		return err
	}
	dir, base, err := r.openParent("remove", name)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	e := unix.Unlinkat(int(dir.Fd()), base, 0)
	if e == nil {
		return nil
	}
	e1 := unix.Unlinkat(int(dir.Fd()), base, unix.AT_REMOVEDIR)
	if e1 == nil {
		return nil
	}
	if e1 != unix.ENOTDIR {
		e = e1 // os.Remove's choice of error
	}
	return pathErr("remove", name, e)
}

// RemoveAll is os.RemoveAll.
func (r *Real) RemoveAll(ctx context.Context, name string) error {
	if err := ctxErr(ctx, "removeall", name); err != nil {
		return err
	}
	return os.RemoveAll(name)
}

// Rename is os.Rename (rename(2), after os.Rename's refusal of an existing
// directory at newname).
func (r *Real) Rename(ctx context.Context, oldname, newname string) error {
	if err := ctxErr(ctx, "rename", oldname); err != nil {
		return err
	}
	return os.Rename(oldname, newname)
}

// Readlink is os.Readlink.
func (r *Real) Readlink(ctx context.Context, name string) (string, error) {
	if err := ctxErr(ctx, "readlink", name); err != nil {
		return "", err
	}
	return os.Readlink(name)
}

// ResolveSymlink walks to name's directory with the guarded walk, requires
// that directory to be trusted, and reads the link with readlinkat there. The
// walk gives the directory's path with no symlink in it, which a relative
// target is joined to (JoinLinkTarget).
func (r *Real) ResolveSymlink(ctx context.Context, name string) (target, resolved string, err error) {
	if err := ctxErr(ctx, "readlink", name); err != nil {
		return "", "", err
	}
	dir, base, at, err := r.walkParent("readlink", name)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = dir.Close() }()
	dfd := int(dir.Fd())
	switch t, err := typeAt(dfd, base); {
	case err != nil:
		return "", "", pathErr("lstat", name, err)
	case t != fs.ModeSymlink:
		return "", "", pathErr("readlink", name, syscall.EINVAL)
	}
	switch ok, err := r.trusted(dfd); {
	case err != nil:
		return "", "", pathErr("readlink", name, err)
	case !ok:
		return "", "", pathErr("readlink", name, ErrSymlinkParent)
	}
	if target, err = readlinkat(dfd, base); err != nil {
		return "", "", pathErr("readlink", name, err)
	}
	if resolved, err = JoinLinkTarget(at, target); err != nil {
		return "", "", pathErr("readlink", name, err)
	}
	return target, resolved, nil
}

// Chmod refuses a symlink at name, then changes the mode through a
// descriptor opened with O_NOFOLLOW in name's guarded parent, so a symlink
// planted after the check is refused too (open fails with ELOOP) rather than
// followed. chmod(2) follows symlinks, and fchmodat's AT_SYMLINK_NOFOLLOW
// depends on the kernel (it needs fchmodat2; Go's syscall.Fchmodat returns
// EOPNOTSUPP without it). Opening is why only regular files and directories
// are changed (a FIFO or a device can block or act on open; O_NONBLOCK guards
// the swap of one in), and why, unprivileged, a file the process cannot read
// cannot be changed. A regular file with other hard links is refused in a
// directory that is not trusted (ErrHardLink); the check is on the open
// descriptor, so the file checked is the file changed.
func (r *Real) Chmod(ctx context.Context, name string, perm fs.FileMode) error {
	if err := ctxErr(ctx, "chmod", name); err != nil {
		return err
	}
	return r.change("chmod", name, func(f *os.File) error { return f.Chmod(perm) })
}

// Chown changes the owner through a descriptor, as Chmod changes the mode,
// with the same refusals: a symlink at name (planted after the check too), a
// user-controlled ~/.ssh that is a link elsewhere (V21), anything that is not
// a regular file or directory, and a hard-linked file in a directory that is
// not trusted. Unprivileged, a file the process cannot read cannot be
// changed.
func (r *Real) Chown(ctx context.Context, name string, uid, gid int) error {
	if err := ctxErr(ctx, "chown", name); err != nil {
		return err
	}
	return r.change("chown", name, func(f *os.File) error { return f.Chown(uid, gid) })
}

// change opens name for Chmod and Chown, checks it, and calls do with it.
func (r *Real) change(op, name string, do func(*os.File) error) error {
	dir, base, err := r.openParent(op, name)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	accept := func(name string, m fs.FileMode) error { return changeable(op, name, m) }
	f, err := openNoFollow(dir, base, name, accept)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := accept(name, fi.Mode()); err != nil {
		return err
	}
	if err := r.notHardLinked(int(dir.Fd()), fi, op, name); err != nil {
		return err
	}
	return do(f)
}

// changeable refuses what Chmod and Chown do not change.
func changeable(op, name string, m fs.FileMode) error {
	switch {
	case m&fs.ModeSymlink != 0:
		return pathErr(op, name, ErrSymlink)
	case !m.IsRegular() && !m.IsDir():
		return pathErr(op, name, fs.ErrInvalid)
	}
	return nil
}

// The steps of WriteFileAtomic, in order, as the failure-injection seam
// (Real.fail) names them.
const (
	stepOpenDir = "open-dir" // the guarded walk to name's directory
	stepCheck   = "check"    // name is absent or a regular file
	stepCreate  = "create"   // the temporary file, O_CREAT|O_EXCL, 0600
	stepWrite   = "write"
	stepChown   = "chown" // before chmod: chown(2) clears setuid and setgid
	stepChmod   = "chmod"
	stepSync    = "fsync"
	stepClose   = "close"
	stepRecheck = "recheck"   // name is still not a symlink
	stepRename  = "rename"    // over name: the one step readers can see
	stepSyncDir = "fsync-dir" // makes the rename durable
)

// WriteFileAtomic walks to name's directory with the guarded walk (see
// System) and works relative to that open directory throughout:
//
//  1. refuse a symlink (ErrSymlink), a directory (EISDIR) or anything else
//     that is not a regular file (fs.ErrInvalid) at name;
//  2. create a temporary file ".<name>.hardn-<random>" beside it with
//     O_CREAT|O_EXCL|O_NOFOLLOW and mode 0600, so it never exists with
//     looser permissions than 0600, and write data to it;
//  3. set its owner (uid or gid -1 leaves the process's), then its mode
//     (chown(2) clears setuid and setgid bits, so the other order could
//     lose them), then fsync and close it;
//  4. check again that name is not a symlink, and rename the temporary file
//     over name: readers see the old file or the new, never a mix;
//  5. fsync the directory, so the rename survives a crash.
//
// It fails closed: an error in steps 1 to 4 leaves name as it was and
// removes the temporary file. An error in step 5 comes after the rename: the
// new file is in place but may not be durable, and the error wraps
// ErrNotDurable. A crash (power loss) before step 4 can leave the temporary
// file behind; its name ends in ".hardn-<random>", which no ".conf" Include
// glob matches and which sudo skips (it contains a dot).
//
// Between the re-check and rename(2) a symlink could still be planted at
// name; rename(2) then replaces the link itself, never its target, and only
// a user who can write the directory can plant one.
func (r *Real) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) (err error) {
	if err := ctxErr(ctx, "write", name); err != nil {
		return err
	}
	var dir *os.File
	var base string
	if err = r.step(stepOpenDir, name, func() (e error) { dir, base, e = r.openParent("write", name); return e }); err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	dfd := int(dir.Fd())
	check := func() error { return writable(dfd, base, name) }
	if err = r.step(stepCheck, name, check); err != nil {
		return err
	}
	var f *os.File
	var tmp string
	if err = r.step(stepCreate, name, func() (e error) { f, tmp, e = createTemp(dir, base); return e }); err != nil {
		return err
	}
	defer func() {
		if f != nil {
			_ = f.Close()
		}
		if err != nil && tmp != "" {
			_ = unix.Unlinkat(dfd, tmp, 0)
		}
	}()
	steps := []struct {
		name string
		do   func() error
	}{
		{stepWrite, func() error { _, e := f.Write(data); return e }},
		{stepChown, func() error {
			if uid == -1 && gid == -1 {
				return nil
			}
			return f.Chown(uid, gid)
		}},
		{stepChmod, func() error { return f.Chmod(perm) }},
		{stepSync, f.Sync},
		{stepClose, func() error { e := f.Close(); f = nil; return e }},
		{stepRecheck, check},
		{stepRename, func() error {
			if e := unix.Renameat(dfd, tmp, dfd, base); e != nil {
				return &os.LinkError{Op: "rename", Old: filepath.Join(dir.Name(), tmp), New: name, Err: e}
			}
			tmp = "" // renamed: nothing left to remove
			return nil
		}},
	}
	for _, s := range steps {
		if err = r.step(s.name, name, s.do); err != nil {
			return err
		}
	}
	if err = r.step(stepSyncDir, name, dir.Sync); err != nil {
		return fmt.Errorf("host: write %s: %w: %w", name, ErrNotDurable, err)
	}
	return nil
}

// step runs do, unless the failure-injection seam fails the step first.
func (r *Real) step(step, name string, do func() error) error {
	if r.fail != nil {
		if err := r.fail(step); err != nil {
			return pathErr(step, name, err)
		}
	}
	return do()
}

// writable refuses what WriteFileAtomic does not replace: anything at base in
// dirfd that is not a regular file. A missing base is fine.
func writable(dirfd int, base, name string) error {
	t, err := typeAt(dirfd, base)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return pathErr("lstat", name, err)
	case t == fs.ModeSymlink:
		return pathErr("write", name, ErrSymlink)
	case t == fs.ModeDir:
		return pathErr("write", name, syscall.EISDIR)
	case t != 0:
		return pathErr("write", name, fs.ErrInvalid)
	}
	return nil
}

// createTemp creates ".<base>.hardn-<random>" in dir, exclusively, with mode
// 0600 (before the umask, which can only remove bits).
func createTemp(dir *os.File, base string) (*os.File, string, error) {
	for range 100 {
		tmp := "." + base + ".hardn-" + strconv.FormatUint(rand.Uint64(), 36)
		fd, err := unix.Openat(int(dir.Fd()), tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, "", pathErr("create", filepath.Join(dir.Name(), tmp), err)
		}
		return os.NewFile(uintptr(fd), filepath.Join(dir.Name(), tmp)), tmp, nil
	}
	return nil, "", pathErr("create", filepath.Join(dir.Name(), "."+base+".hardn-*"), fs.ErrExist)
}

// WriteFileInPlace is os.WriteFile; see LegacySystem.
func (r *Real) WriteFileInPlace(ctx context.Context, name string, data []byte, perm fs.FileMode) error {
	if err := ctxErr(ctx, "write", name); err != nil {
		return err
	}
	return os.WriteFile(name, data, perm)
}

// LookupUser uses os/user. Release builds have no cgo, so it reads
// /etc/passwd itself; accounts only NSS knows (LDAP, sssd) are not found.
func (r *Real) LookupUser(ctx context.Context, name string) (User, error) {
	if err := ctxErr(ctx, "lookup user", name); err != nil {
		return User{}, err
	}
	u, err := user.Lookup(name)
	if err != nil {
		var unknown user.UnknownUserError
		if errors.As(err, &unknown) {
			return User{}, fmt.Errorf("host: lookup user %q: %w", name, ErrUnknownUser)
		}
		return User{}, fmt.Errorf("host: lookup user %q: %w", name, err)
	}
	uid, uerr := strconv.Atoi(u.Uid)
	gid, gerr := strconv.Atoi(u.Gid)
	if err := errors.Join(uerr, gerr); err != nil {
		return User{}, fmt.Errorf("host: lookup user %q: %w", name, err)
	}
	return User{Name: u.Username, UID: uid, GID: gid, HomeDir: u.HomeDir}, nil
}

// LookupGroup uses os/user, as LookupUser does with /etc/group.
func (r *Real) LookupGroup(ctx context.Context, name string) (Group, error) {
	if err := ctxErr(ctx, "lookup group", name); err != nil {
		return Group{}, err
	}
	g, err := user.LookupGroup(name)
	if err != nil {
		var unknown user.UnknownGroupError
		if errors.As(err, &unknown) {
			return Group{}, fmt.Errorf("host: lookup group %q: %w", name, ErrUnknownGroup)
		}
		return Group{}, fmt.Errorf("host: lookup group %q: %w", name, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return Group{}, fmt.Errorf("host: lookup group %q: %w", name, err)
	}
	return Group{Name: g.Name, GID: gid}, nil
}
