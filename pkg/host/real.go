package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Real is the System of the machine hardn runs on.
type Real struct {
	log  *RunLog
	tail int

	// Seams for the tests in this package; NewReal sets the real ones.
	dirs []string                          // SearchPath, split
	stat func(string) (fs.FileInfo, error) // resolves a command candidate
	exec func(*exec.Cmd) (int, error)      // runs cmd: exit code, error
}

var _ LegacySystem = (*Real)(nil)

// commandEnv is the whole environment of every command Real runs. Nothing is
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
		log:  NewRunLog(opts.RunLog),
		tail: opts.Tail(),
		dirs: strings.Split(SearchPath, ":"),
		stat: os.Stat,
		exec: runCmd,
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
//     command stays in hardn's process group, and a pipeline's or a
//     wrapper's children can outlive the cancellation.
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

// Remove is os.Remove.
func (r *Real) Remove(ctx context.Context, name string) error {
	if err := ctxErr(ctx, "remove", name); err != nil {
		return err
	}
	return os.Remove(name)
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

// Chmod refuses a symlink at name, then changes the mode through a
// descriptor opened with O_NOFOLLOW, so a symlink planted after the check is
// refused too (open fails with ELOOP) rather than followed. chmod(2) follows
// symlinks, and fchmodat's AT_SYMLINK_NOFOLLOW depends on the kernel (it needs
// fchmodat2; Go's syscall.Fchmodat returns EOPNOTSUPP without it).
// Opening is why only regular files and directories are changed (a FIFO or a
// device can block or act on open; O_NONBLOCK guards the swap of one in), and
// why, unprivileged, a file the process cannot read cannot be changed.
// Symlinks in the parent directories are followed, as everywhere in System.
func (r *Real) Chmod(ctx context.Context, name string, perm fs.FileMode) error {
	if err := ctxErr(ctx, "chmod", name); err != nil {
		return err
	}
	fi, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if err := chmodable(name, fi); err != nil {
		return err
	}
	f, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ELOOP) {
		return pathErr("chmod", name, ErrSymlink)
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if fi, err = f.Stat(); err != nil {
		return err
	}
	if err := chmodable(name, fi); err != nil {
		return err
	}
	return f.Chmod(perm)
}

// chmodable refuses what Chmod does not change.
func chmodable(name string, fi fs.FileInfo) error {
	switch {
	case fi.Mode()&fs.ModeSymlink != 0:
		return pathErr("chmod", name, ErrSymlink)
	case !fi.Mode().IsRegular() && !fi.IsDir():
		return pathErr("chmod", name, fs.ErrInvalid)
	}
	return nil
}

// Chown refuses a symlink at name and changes name itself with lchown(2), so
// a symlink planted after the check is not followed either. Symlinks in the
// parent directories are followed: a user-controlled ~/.ssh that is a link
// elsewhere (V21) is not refused here; p2-atomic owns that refusal.
func (r *Real) Chown(ctx context.Context, name string, uid, gid int) error {
	if err := ctxErr(ctx, "chown", name); err != nil {
		return err
	}
	fi, err := os.Lstat(name)
	if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		return pathErr("chown", name, ErrSymlink)
	}
	return os.Lchown(name, uid, gid)
}

// WriteFileAtomic writes data to a new file in name's directory, sets its
// owner, then its mode (chown(2) clears setuid and setgid bits, so the other
// order could lose them), and renames it over name. It refuses a symlink or a
// directory at name; rename(2) never writes through a symlink planted after
// the check, it replaces it. On any failure the temporary file is removed and
// name is untouched. Symlinks in the parent directories are followed.
//
// TODO(next/p2-atomic): this is the naive version. p2-atomic replaces it with
// the durable one: fsync of the file before the rename and of the directory
// after it, backups with a manifest, the symlink refusal re-checked against
// the open directory, and the refusal of a symlinked parent (V21).
func (r *Real) WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) (err error) {
	if err := ctxErr(ctx, "write", name); err != nil {
		return err
	}
	switch fi, lerr := os.Lstat(name); {
	case lerr == nil && fi.Mode()&fs.ModeSymlink != 0:
		return pathErr("write", name, ErrSymlink)
	case lerr == nil && fi.IsDir():
		return pathErr("write", name, syscall.EISDIR)
	case lerr != nil && !errors.Is(lerr, fs.ErrNotExist):
		return lerr
	}
	// filepath.Dir, not Split: a bare name's temporary file must land in
	// ".", not in os.TempDir().
	f, err := os.CreateTemp(filepath.Dir(name), "."+filepath.Base(name)+".hardn-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if uid != -1 || gid != -1 {
		if err = f.Chown(uid, gid); err != nil {
			return err
		}
	}
	if err = f.Chmod(perm); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, name)
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
