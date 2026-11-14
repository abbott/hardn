package host

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// AppendFile opens name's parent through the guarded walk and refuses it
// unless that open directory is trusted (ErrUntrustedDir), so the check
// and the write are on the same directory: one that another user could
// rename or replace between a check by path and the open would not be.
// After lstat has shown name absent or a regular file (writable), it opens
// it there with O_WRONLY|O_APPEND|O_CREAT|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC
// and perm (before the umask, which can only remove bits), checks the open
// file again (regular), writes data in one call, syncs and closes it. A
// file with other hard links is accepted: in a trusted directory no other
// user can have linked it there (the rule Chmod and Chown keep). With
// O_APPEND "the file offset shall be set to the end of the file prior to
// each write and no intervening file modification operation shall occur
// between changing the file offset and the write operation" (POSIX
// write(2)), so a record appended by another process at the same time is
// never overwritten. O_NONBLOCK keeps a FIFO swapped in after the lstat
// from blocking the open (it fails with ENXIO without a reader, and is
// refused after it). A newly created file's directory entry is not synced:
// the audit log is a record, not the journal a rollback reads. An open
// that finds the file renamed away under it, as RotateFile in another
// process renames it, is tried again (openAppend).
func (r *Real) AppendFile(ctx context.Context, name string, data []byte, perm fs.FileMode) error {
	if err := ctxErr(ctx, "append", name); err != nil {
		return err
	}
	dir, base, err := r.openParent("append", name)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	dfd := int(dir.Fd())
	switch ok, err := r.trusted(dfd); {
	case err != nil:
		return pathErr("append", name, err)
	case !ok:
		return pathErr("append", name, ErrUntrustedDir)
	}
	if err := writable(dfd, base, name); err != nil {
		return err
	}
	fd, err := openAppend(dfd, base, perm)
	switch {
	case isNoFollow(err):
		return pathErr("append", name, ErrSymlink)
	case err != nil:
		return pathErr("append", name, err)
	}
	f := os.NewFile(uintptr(fd), name)
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	switch {
	case err != nil:
		return pathErr("append", name, err)
	case !fi.Mode().IsRegular():
		return pathErr("append", name, fs.ErrInvalid)
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

// openAppend opens base in dirfd to append to, creating it with perm. With
// O_CREAT, "no such file" from an open directory is a file renamed away
// between the kernel's lookup and its open, seen while another rotates the
// file (TestRotateFileRace; not run on Linux): the open is tried again,
// a few times, and then creates the new file.
func openAppend(dirfd int, base string, perm fs.FileMode) (fd int, err error) {
	for range 5 {
		fd, err = unix.Openat(dirfd, base, unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(perm.Perm()))
		if !errors.Is(err, unix.ENOENT) {
			break
		}
	}
	return fd, err
}
