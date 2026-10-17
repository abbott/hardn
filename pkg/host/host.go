// Package host is the one seam between hardn and the machine it hardens.
//
// Every Phase 2 safety primitive and the Phase 3 engine reach the host through
// System: commands, files, ownership and account lookups. Real is the
// implementation on the running machine; package hostfake is a recording,
// in-memory one for tests. The legacy interfaces.Commander and
// interfaces.FileSystem are adapters over a System, so legacy and new code
// share one execution path (refactor plan P2, next/p2-system).
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
)

// SearchPath is the only place Real looks for a command named without a
// slash, in this order, and the PATH every command runs with. /usr/local is
// left out on purpose: hardn runs packaged tools only.
const SearchPath = "/usr/sbin:/usr/bin:/sbin:/bin"

// DefaultTailLines is how many lines of output Result.Tail keeps when
// Options.TailLines is zero.
const DefaultTailLines = 20

// System is the host as hardn's safety primitives and engine see it.
//
// Paths should be absolute. Methods honour ctx only on entry for file
// operations; Run kills the command when ctx is done.
type System interface {
	// Run runs name with args as one argv and waits for it, with stdin
	// empty. See Real.Run for resolution, environment and errors.
	Run(ctx context.Context, name string, args ...string) (Result, error)
	// RunInput is Run with stdin as the command's standard input. The run
	// log records its length only, never its content.
	RunInput(ctx context.Context, stdin []byte, name string, args ...string) (Result, error)

	// ReadFile returns the content of name, following symlinks.
	ReadFile(ctx context.Context, name string) ([]byte, error)
	// Stat describes name, following a symlink at name.
	Stat(ctx context.Context, name string) (fs.FileInfo, error)
	// Lstat describes name itself: a symlink at name is not followed.
	Lstat(ctx context.Context, name string) (fs.FileInfo, error)
	// ReadDir lists the directory name, sorted by file name.
	ReadDir(ctx context.Context, name string) ([]fs.DirEntry, error)
	// MkdirAll creates name and any missing parents with perm.
	MkdirAll(ctx context.Context, name string, perm fs.FileMode) error
	// Remove removes a file, a symlink (not its target) or an empty directory.
	Remove(ctx context.Context, name string) error
	// RemoveAll removes name and everything under it; a missing name is not
	// an error.
	RemoveAll(ctx context.Context, name string) error
	// Rename renames oldname to newname, replacing newname. A symlink at
	// either name is renamed or replaced itself, never its target.
	Rename(ctx context.Context, oldname, newname string) error
	// Chown sets the owner of name. It never follows a symlink: a symlink at
	// name is refused with ErrSymlink. A uid or gid of -1 is left unchanged.
	Chown(ctx context.Context, name string, uid, gid int) error
	// WriteFileAtomic replaces name with a new file holding data, with mode
	// perm and owner uid:gid (-1 keeps the owner the process gives a new
	// file). Readers see the old content or the new, never a mix. A symlink
	// at name is refused with ErrSymlink, never written through or replaced.
	WriteFileAtomic(ctx context.Context, name string, data []byte, perm fs.FileMode, uid, gid int) error

	// LookupUser finds a user account by name; ErrUnknownUser when absent.
	LookupUser(ctx context.Context, name string) (User, error)
	// LookupGroup finds a group by name; ErrUnknownGroup when absent.
	LookupGroup(ctx context.Context, name string) (Group, error)
}

// LegacySystem is System plus the in-place write the legacy
// interfaces.FileSystem still offers its callers. New code takes a System,
// which has no in-place write.
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
	Argv     []string // name and args, as passed to Run
	Stdout   []byte
	Stderr   []byte
	Combined []byte // stdout and stderr interleaved, in the order written
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

// Errors a System returns, testable with errors.Is.
var (
	// ErrNotFound: the command is not an executable file in SearchPath, or
	// was named by a relative path.
	ErrNotFound = errors.New("command not found")
	// ErrSymlink: the operation refuses a symlink at the path.
	ErrSymlink = errors.New("refusing a symlink")
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
	// Err is the os/exec error behind it, if any (Real); the fake has none.
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
