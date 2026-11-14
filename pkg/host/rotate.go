package host

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strconv"
	"time"

	"golang.org/x/sys/unix"
)

// RotateFile waits rotateLockWait for the file's lock, asking again each
// rotateLockStep.
const (
	rotateLockWait = 300 * time.Millisecond
	rotateLockStep = 10 * time.Millisecond
)

// RotateFile renames the regular file name to name.1 once it holds more
// than limit bytes, after moving name.1 to name.2 and so on up to
// name.keep, whose content is replaced, so the oldest copy is deleted
// (docs/engine/design-log-rotation.md). They are renames in the one
// directory, opened through the guarded walk, so a copy keeps its mode and
// owner, nothing is copied or compressed, and a process that has the file
// open goes on writing to it under its new name. It creates no file: the
// next append does.
//
// Nothing is renamed, and the error says why, where name or a copy is a
// symlink (ErrSymlink) or anything else that is not a regular file
// (fs.ErrInvalid), and, with trusted, where the directory it opened is not
// trusted (ErrUntrustedDir, as AppendFile's). No name is ever followed:
// each is looked at with lstat and renamed with renameat(2), which moves a
// symlink itself.
//
// Two processes may ask at once. The rotation runs under flock(2)'s
// LOCK_EX|LOCK_NB on the file itself, taken on a descriptor opened here
// and released when it is closed: no lock file is left beside the log. The
// one that finds the lock held asks again for rotateLockWait; once it
// holds it and finds another file at name by then or one within limit, it
// rotates nothing and returns false with no error: the other got there
// first. A lock still held after the wait is ErrLocked: a file
// others can read, as hardn.log, is one any user can lock, and that must
// not pass unsaid.
func (r *Real) RotateFile(ctx context.Context, name string, limit int64, keep int, trusted bool) (bool, error) {
	if err := ctxErr(ctx, "rotate", name); err != nil {
		return false, err
	}
	if limit <= 0 || keep < 1 {
		return false, pathErr("rotate", name, fs.ErrInvalid)
	}
	dir, base, err := r.openParent("rotate", name)
	if err != nil {
		return false, err
	}
	defer func() { _ = dir.Close() }()
	dfd := int(dir.Fd())
	if trusted {
		switch ok, err := r.trusted(dfd); {
		case err != nil:
			return false, pathErr("rotate", name, err)
		case !ok:
			return false, pathErr("rotate", name, ErrUntrustedDir)
		}
	}
	if st, err := rotatable(dfd, base, name); err != nil || st == nil || st.Size <= limit {
		return false, err
	}
	fd, err := unix.Openat(dfd, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	switch {
	case errors.Is(err, unix.ENOENT):
		return false, nil // rotated by another since the lstat
	case isNoFollow(err):
		return false, pathErr("rotate", name, ErrSymlink)
	case err != nil:
		return false, pathErr("rotate", name, err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }() // releases the lock
	for waited := time.Duration(0); ; waited += rotateLockStep {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EINTR) {
			return false, pathErr("rotate", name, err)
		}
		if waited >= rotateLockWait {
			return false, pathErr("rotate", name, ErrLocked)
		}
		select {
		case <-ctx.Done():
			return false, pathErr("rotate", name, ctx.Err())
		case <-time.After(rotateLockStep):
		}
	}
	var held unix.Stat_t
	if err := unix.Fstat(fd, &held); err != nil {
		return false, pathErr("rotate", name, err)
	}
	// Under the lock: the file at name is still the one locked, and still
	// over the limit, or another rotated it first.
	st, err := rotatable(dfd, base, name)
	if err != nil || st == nil || st.Dev != held.Dev || st.Ino != held.Ino || st.Size <= limit {
		return false, err
	}
	copyOf := func(i int) string { return "." + strconv.Itoa(i) }
	for i := 1; i <= keep; i++ {
		if _, err := rotatable(dfd, base+copyOf(i), name+copyOf(i)); err != nil {
			return false, err
		}
	}
	for i := keep - 1; i >= 0; i-- {
		from, to := copyOf(i), copyOf(i+1)
		if i == 0 {
			from = ""
		}
		err := r.step("rotate-rename", name+from, func() error {
			if err := unix.Renameat(dfd, base+from, dfd, base+to); err != nil && (i == 0 || !errors.Is(err, unix.ENOENT)) {
				return &os.LinkError{Op: "rename", Old: name + from, New: name + to, Err: err}
			}
			return nil
		})
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

// rotatable is lstat of c in dirfd for RotateFile: nil, with no error,
// where there is nothing there; ErrSymlink for a symlink and fs.ErrInvalid
// for anything else that is not a regular file, as name's.
func rotatable(dirfd int, c, name string) (*unix.Stat_t, error) {
	var st unix.Stat_t
	err := unix.Fstatat(dirfd, c, &st, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case errors.Is(err, unix.ENOENT):
		return nil, nil
	case err != nil:
		return nil, pathErr("lstat", name, err)
	case st.Mode&unix.S_IFMT == unix.S_IFLNK:
		return nil, pathErr("rotate", name, ErrSymlink)
	case st.Mode&unix.S_IFMT != unix.S_IFREG:
		return nil, pathErr("rotate", name, fs.ErrInvalid)
	}
	return &st, nil
}
