// Package host is the one seam between hardn and the machine it hardens.
//
// Every Phase 2 safety primitive and the Phase 3 engine reach the host through
// System: commands, files, ownership and account lookups. Real is the
// implementation on the running machine; package hostfake is a recording,
// in-memory one for tests. The legacy interfaces.Commander and
// interfaces.FileSystem have adapters over a System, so legacy and new code
// can share one execution path (refactor plan P2, next/p2-system); whether
// legacy production calls take it is the interfaces package's
// legacyThroughHost gate.
//
// Commands run as one argv, never through a shell, resolved only from
// SearchPath, with a pinned environment. A command that exits non-zero
// returns an *ExitError, so a failed check cannot read as a pass. Service
// control is expressed as Run calls (systemctl, rc-service); which call fits
// which init system is the business of the reload and detection units.
//
// docs/safety/system.md describes the guarantees, the platform facts they rely
// on and what is written but not yet run on a Linux host.
package host

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
)

// SearchPath is the only place Real looks for a command named without a
// slash, in this order, and the PATH every command runs with. /usr/local is
// left out on purpose: hardn runs packaged tools only. hardn itself is the
// exception (install.sh puts it in /usr/local/bin): re-invoke it by its
// absolute path (os.Executable), never by name, from Run or from a launcher
// such as setsid or systemd-run that the PATH above is passed to.
const SearchPath = "/usr/sbin:/usr/bin:/sbin:/bin"

// DefaultTailLines is how many lines of output Result.Tail keeps when
// Options.TailLines is zero.
const DefaultTailLines = 20

// System is the host as hardn's safety primitives and engine see it.
//
// Paths should be absolute. Symlinks in the parent directories of a path are
// followed by every method except the guarded ones (WriteFileAtomic, Chown,
// Chmod, Remove, ReadFileNoFollow, ResolveSymlink): those take an absolute
// path, and follow a symlink on the way only when the directory holding it is
// trusted: owned by root (or by hardn's own user) and writable by neither its
// group nor others. Otherwise they refuse with ErrSymlinkParent, so a user who
// controls a directory on the path cannot redirect them (V21). The other
// no-follow rules below are about the last component. File operations honour
// ctx only on entry; Run kills the process it started when ctx is done, but not
// processes that one started.
// Every method refuses a ctx that is already done, so a rollback after
// cancellation (Ctrl-C) must run under context.WithoutCancel with its own
// timeout.
type System interface {
	// Run runs name with args as one argv and waits for it, with stdin
	// empty. See Real.Run for resolution, environment and errors.
	Run(ctx context.Context, name string, args ...string) (Result, error)
	// RunInput is Run with stdin as the command's standard input. The run
	// log records its length only, never its content.
	RunInput(ctx context.Context, stdin []byte, name string, args ...string) (Result, error)
	// StartDetached starts name with args as a process meant to outlive
	// hardn, and returns its process id without waiting for it. It is
	// resolved as Run resolves name, with Run's environment and working
	// directory, but in a new session (setsid(2)), so it has no
	// controlling terminal and the hangup of hardn's terminal or SSH
	// session does not reach it, and with stdin, stdout and stderr on the
	// null device, so it holds none of hardn's pipes. ctx is honoured on
	// entry only: it never kills the process. The run log records the
	// start and the process id. The OpenRC revert timer (p2-revert).
	StartDetached(ctx context.Context, name string, args ...string) (pid int, err error)

	// ReadFile returns the content of name, following symlinks.
	ReadFile(ctx context.Context, name string) ([]byte, error)
	// ReadFileNoFollow returns the content of the regular file name and its
	// FileInfo, both read from one open file, so they describe the same
	// file. A symlink at name is refused with ErrSymlink, a directory with
	// EISDIR, anything else that is not a regular file with fs.ErrInvalid;
	// it is guarded (see System). Backups read through it.
	ReadFileNoFollow(ctx context.Context, name string) ([]byte, fs.FileInfo, error)
	// Stat describes name, following a symlink at name.
	Stat(ctx context.Context, name string) (fs.FileInfo, error)
	// Lstat describes name itself: a symlink at name is not followed.
	Lstat(ctx context.Context, name string) (fs.FileInfo, error)
	// ReadDir lists the directory name, sorted by file name.
	ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error)
	// MkdirAll creates name and any missing parents with perm.
	MkdirAll(ctx context.Context, name string, perm fs.FileMode) error
	// Remove removes a file, a symlink (not its target) or an empty
	// directory. It is guarded (see System).
	Remove(ctx context.Context, name string) error
	// RemoveAll removes name and everything under it; a missing name is not
	// an error.
	RemoveAll(ctx context.Context, name string) error
	// Rename renames oldname to newname, replacing newname. A symlink at
	// either name is renamed or replaced itself, never its target. An
	// existing directory at newname is refused, as os.Rename refuses it.
	Rename(ctx context.Context, oldname, newname string) error
	// Readlink returns the target of the symlink name, as stored. It is not
	// guarded: to act on what a link points to, use ResolveSymlink.
	Readlink(ctx context.Context, name string) (string, error)
	// ResolveSymlink returns the target of the symlink name, as stored, and
	// resolved, the absolute path it points to, for writing through a link
	// on purpose (WriteFileThroughSymlink). It is guarded (see System), and
	// the directory holding the link must be trusted too, else
	// ErrSymlinkParent: a link a user planted in a directory they control
	// is never resolved. A relative target is joined to the path of the
	// link's directory as the walk reached it, with no symlink in it, so a
	// ".." in the target means what it means to the kernel; links in the
	// target itself are followed, under the same rule, by the guarded
	// operation resolved is then given. A target with ".." after another
	// component is refused with fs.ErrInvalid (JoinLinkTarget). Not a
	// symlink: EINVAL, as readlink(2).
	ResolveSymlink(ctx context.Context, name string) (target, resolved string, err error)
	// Chmod sets the mode of the regular file or directory name. A symlink
	// at name is refused with ErrSymlink, never followed; anything else that
	// is not a regular file or directory is refused with fs.ErrInvalid; a
	// regular file with more than one hard link, in a directory that is not
	// trusted (see System), with ErrHardLink. It is guarded.
	Chmod(ctx context.Context, name string, perm fs.FileMode) error
	// Chown sets the owner of the regular file or directory name, with the
	// same refusals as Chmod. A uid or gid of -1 is left unchanged. It is
	// guarded.
	Chown(ctx context.Context, name string, uid, gid int) error
	// WriteFileAtomic replaces name with a new file holding data, with mode
	// perm and owner uid:gid (-1 keeps the owner the process gives a new
	// file; WriteFileAtomicKeep keeps an existing file's). Readers see the
	// old content or the new, never a mix. A symlink at name is refused
	// with ErrSymlink, never written through or replaced; so is a directory
	// (EISDIR) or anything else that is not a regular file (fs.ErrInvalid).
	// It is guarded (see System). On an error, name is as it was, except
	// for one wrapping ErrNotDurable: see Real.WriteFileAtomic.
	WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error
	// AppendFile appends data to the regular file name, creating it with
	// perm (and the process's owner) when it is absent: one write on a
	// descriptor opened with O_APPEND, so each call's data lands whole
	// after what is there, whoever else appends, then fsync. It never
	// replaces or truncates the file. A symlink at name is refused with
	// ErrSymlink, never written through; so is a directory (EISDIR) or
	// anything else that is not a regular file (fs.ErrInvalid). The
	// directory holding name must be trusted (see System), checked on the
	// directory it opens, else ErrUntrustedDir: what the file receives
	// goes where no other user can rename, replace or link in front of it.
	// It is guarded (see System). The audit log (p3-output).
	AppendFile(ctx context.Context, name string, data []byte, perm fs.FileMode) error
	// Lock takes an exclusive lock on the regular file name, creating it
	// 0600 when absent, writes holder into it, and returns the function
	// that empties it and releases the lock. It never waits: a lock held
	// through another open of the file, by this process or another, is
	// ErrLocked. The lock goes with the open file (flock(2)), so the
	// kernel releases it when its holder exits, however it exits: a lock
	// file a dead process left is not held. A symlink at name is refused
	// with ErrSymlink, anything else that is not a regular file with
	// fs.ErrInvalid; it is guarded (see System). Never remove a lock file:
	// one opened before the removal would lock a file no one else sees.
	// The engine's run lock (p3-engine).
	Lock(ctx context.Context, name string, holder []byte) (unlock func() error, err error)

	// LookupUser finds a user account by name; ErrUnknownUser when absent.
	LookupUser(ctx context.Context, name string) (User, error)
	// LookupGroup finds a group by name; ErrUnknownGroup when absent.
	LookupGroup(ctx context.Context, name string) (Group, error)
}

// LegacySystem is System plus the in-place write the legacy
// interfaces.FileSystem still offers its callers. New code takes a System,
// which has no in-place write; nothing but review keeps new code from
// asserting a System to LegacySystem.
type LegacySystem interface {
	System
	// WriteFileInPlace is os.WriteFile: it truncates and rewrites name,
	// follows a symlink at name, keeps an existing file's mode and owner, and
	// creates a missing file with perm. It exists for interfaces.FileSystem
	// alone, until its last caller migrates.
	WriteFileInPlace(ctx context.Context, name string, data []byte, perm fs.FileMode) error
}

// Result is what a command did. Run fills it in whether or not it returns an
// error.
type Result struct {
	Argv   []string // name and args, as passed to Run
	Stdout []byte
	Stderr []byte
	// Combined is stdout and stderr together. Each stream keeps its own
	// order; across the two, Real's interleaving is approximate, because it
	// reads them through two pipes (exec.Cmd.CombinedOutput used one).
	Combined []byte
	// ExitCode is the command's exit status; -1 when it did not start or
	// was ended by a signal.
	ExitCode int
	Tail     []string // the last lines of Combined (Options.TailLines)
}

// User is a user account.
type User struct {
	Name    string
	UID     int
	GID     int // primary group
	HomeDir string
}

// Group is a group.
type Group struct {
	Name string
	GID  int
}

// Ownership is the owner a fake file system reports in fs.FileInfo.Sys(). The
// real file system reports *syscall.Stat_t; Owner reads either.
type Ownership struct {
	UID, GID int
}

// Owner returns the owner of the file fi describes, as Real's or the fake's
// Stat, Lstat and ReadDir report it. ok is false when fi carries no owner.
func Owner(fi fs.FileInfo) (uid, gid int, ok bool) {
	if o, isOwn := fi.Sys().(*Ownership); isOwn && o != nil {
		return o.UID, o.GID, true
	}
	return sysOwner(fi)
}

// WriteFileAtomicKeep is sys.WriteFileAtomic keeping an existing regular
// file's permission bits and owner: perm, uid and gid apply only when name
// does not exist. Setuid, setgid and sticky bits are never carried over. The
// existing file is read with Lstat just before the write, so use it for files
// in directories only root can change (a configuration file, sshd_config);
// pass explicit values for a file in a user's home directory.
func WriteFileAtomicKeep(ctx context.Context, sys System, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	fi, err := sys.Lstat(ctx, name)
	switch {
	case err == nil && fi.Mode().IsRegular():
		perm = fi.Mode().Perm()
		var ok bool
		if uid, gid, ok = Owner(fi); !ok {
			return pathErr("write", name, fs.ErrInvalid)
		}
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return err
	}
	// Anything else that exists (a symlink, a directory) is refused by
	// WriteFileAtomic itself.
	return sys.WriteFileAtomic(ctx, name, data, perm, uid, gid)
}

// WriteFileThroughSymlink writes data to the file the symlink name points
// to, with WriteFileAtomic, and leaves the link as it is: the explicit way to
// edit a file through a link (p2-pve: /root/.ssh/authorized_keys into
// /etc/pve/priv). The link is resolved with ResolveSymlink, so one in a
// directory another user can change is refused (ErrSymlinkParent); the file
// it points to must be absent or a regular file, as for WriteFileAtomic.
func WriteFileThroughSymlink(ctx context.Context, sys System, name string, data []byte, perm fs.FileMode, uid, gid int) error {
	_, resolved, err := sys.ResolveSymlink(ctx, name)
	if err != nil {
		return err
	}
	return sys.WriteFileAtomic(ctx, resolved, data, perm, uid, gid)
}

// JoinLinkTarget returns the absolute path a symlink holding target points
// to, when the link is in dir, an absolute path with no symlink in it. ".."
// at the start of target is resolved against dir, which is what the kernel
// does when dir has no link in it. A ".." after any other component is
// refused with fs.ErrInvalid: the component before it may be a symlink,
// which only a lookup could tell. ResolveSymlink uses it.
func JoinLinkTarget(dir, target string) (string, error) {
	other := false
	for _, c := range strings.Split(target, "/") {
		switch {
		case c == "..":
			if other {
				return "", fmt.Errorf("host: symlink target %q: \"..\" after another component: %w", target, fs.ErrInvalid)
			}
		case c != "" && c != ".":
			other = true
		}
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target), nil
	}
	return filepath.Join(dir, target), nil
}

// Errors a System returns, testable with errors.Is.
var (
	// ErrNotFound: the command is not an executable file in SearchPath, or
	// was named by a relative path.
	ErrNotFound = errors.New("command not found")
	// ErrSymlink: the operation refuses a symlink at the path.
	ErrSymlink = errors.New("refusing a symlink")
	// ErrSymlinkParent: a guarded operation met a symlink on the way to the
	// path, or ResolveSymlink one at the path, in a directory a user other
	// than root (or hardn's own user) can change (see System).
	// errors.Is(err, ErrSymlink) holds for it too, so test for it first: a
	// caller that writes through a link on ErrSymlink must not on this one.
	ErrSymlinkParent = fmt.Errorf("%w in a directory another user can change", ErrSymlink)
	// ErrHardLink: Chmod or Chown met a regular file with more than one hard
	// link in a directory another user can change: the file may be a root
	// file that user linked there.
	ErrHardLink = errors.New("refusing a file with other hard links, in a directory another user can change")
	// ErrUntrustedDir: AppendFile met a directory, the one it opened to
	// hold the file, that a user other than root (or hardn's own user) can
	// change.
	ErrUntrustedDir = errors.New("refusing to append in a directory another user can change")
	// ErrNotDurable: WriteFileAtomic replaced the file, but could not sync
	// its directory, so the replacement may not survive a crash. The new
	// content is in place; roll the change back like any failed step.
	ErrNotDurable = errors.New("replaced, but the directory could not be synced")
	// ErrLocked: Lock found the file locked by another open of it.
	ErrLocked = errors.New("locked by another process")
	// ErrUnknownUser: no such user account.
	ErrUnknownUser = errors.New("unknown user")
	// ErrUnknownGroup: no such group.
	ErrUnknownGroup = errors.New("unknown group")
)

// ExitError reports a command that ran and did not exit 0. Run returns it
// with the Result filled in; errors.As finds it.
type ExitError struct {
	Argv     []string
	ExitCode int // -1 when ended by a signal
	Tail     []string
	// Err is the error behind it: the os/exec error (Real), or one whose
	// text is os/exec's "exit status N" (the fake).
	Err error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("host: %s: %v", FormatArgv(e.Argv), e.Err)
	}
	return fmt.Sprintf("host: %s: exit status %d", FormatArgv(e.Argv), e.ExitCode)
}

// Unwrap returns the os/exec error, so errors.As still finds *exec.ExitError.
func (e *ExitError) Unwrap() error { return e.Err }

// Options configure Real and the fake.
type Options struct {
	// RunLog receives every command's argv, output and exit status; nil
	// discards them. Its path and rotation are p3-output's business.
	RunLog io.Writer
	// TailLines is how many lines Result.Tail keeps; 0 means
	// DefaultTailLines, a negative value none.
	TailLines int
}

// Tail returns the number of lines o keeps in Result.Tail.
func (o Options) Tail() int {
	switch {
	case o.TailLines == 0:
		return DefaultTailLines
	case o.TailLines < 0:
		return 0
	}
	return o.TailLines
}

// pathErr builds the *fs.PathError the os package would return.
func pathErr(op, name string, err error) error {
	return &fs.PathError{Op: op, Path: name, Err: err}
}

// ctxErr returns a wrapped ctx.Err() when ctx is done, for the file operations.
func ctxErr(ctx context.Context, op, name string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("host: %s %s: %w", op, name, err)
	}
	return nil
}
