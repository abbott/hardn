package host

import (
	"context"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// AppendFile opens name in its guarded parent, after lstat has shown it
// absent or a regular file (writable), with
// O_WRONLY|O_APPEND|O_CREAT|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC and perm (before
// the umask, which can only remove bits), checks the open file again
// (regular, and not hard-linked in an untrusted directory: notHardLinked),
// writes data in one call, syncs and closes it. With O_APPEND "the file
// offset shall be set to the end of the file prior to each write and no
// intervening file modification operation shall occur between changing the
// file offset and the write operation" (POSIX write(2)), so a record
// appended by another process at the same time is never overwritten.
// O_NONBLOCK keeps a FIFO swapped in after the lstat from blocking the open
// (it fails with ENXIO without a reader, and is refused after it). A newly
// created file's directory entry is not synced: the audit log is a record,
// not the journal a rollback reads.
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
	if err := writable(dfd, base, name); err != nil {
		return err
	}
	fd, err := unix.Openat(dfd, base, unix.O_WRONLY|unix.O_APPEND|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, uint32(perm.Perm()))
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
	if err := r.notHardLinked(dfd, fi, "append", name); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}
