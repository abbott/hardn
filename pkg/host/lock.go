package host

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// Lock opens name in its guarded parent with O_NOFOLLOW|O_NONBLOCK (after
// lstat has shown it absent or a regular file, so a FIFO or a device is
// never opened), creating it 0600, and takes flock(2)'s LOCK_EX|LOCK_NB on
// that open file: "the lock is released either by an explicit LOCK_UN
// operation on any of these duplicate file descriptors, or when all such
// file descriptors have been closed", and a process's descriptors are
// closed when it exits. The descriptor is close-on-exec, so no command Run
// or StartDetached starts keeps the lock after hardn exits. holder replaces
// the file's content once the lock is held; unlock empties it and closes
// the file, which releases the lock, and is safe to call more than once.
func (r *Real) Lock(ctx context.Context, name string, holder []byte) (func() error, error) {
	if err := ctxErr(ctx, "lock", name); err != nil {
		return nil, err
	}
	dir, base, err := r.openParent("lock", name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	switch t, err := typeAt(int(dir.Fd()), base); {
	case errors.Is(err, unix.ENOENT):
	case err != nil:
		return nil, pathErr("lstat", name, err)
	case t&fs.ModeSymlink != 0:
		return nil, pathErr("lock", name, ErrSymlink)
	case t != 0:
		return nil, pathErr("lock", name, fs.ErrInvalid)
	}
	fd, err := unix.Openat(int(dir.Fd()), base, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0o600)
	switch {
	case isNoFollow(err):
		return nil, pathErr("lock", name, ErrSymlink)
	case err != nil:
		return nil, pathErr("lock", name, err)
	}
	f := os.NewFile(uintptr(fd), name)
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		_ = f.Close()
		if err == nil {
			err = fs.ErrInvalid
		}
		return nil, pathErr("lock", name, err)
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			err = ErrLocked
		}
		return nil, pathErr("lock", name, err)
	}
	werr := f.Truncate(0)
	if werr == nil {
		_, werr = f.WriteAt(holder, 0)
	}
	if werr != nil {
		err = werr
		_ = f.Close()
		return nil, pathErr("lock", name, err)
	}
	var once sync.Once
	var cerr error
	return func() error {
		once.Do(func() {
			_ = f.Truncate(0)
			cerr = f.Close()
		})
		return cerr
	}, nil
}
