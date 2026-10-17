package host

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// maxSymlinks is Linux's limit on symlinks followed in one lookup, past which
// it returns ELOOP (path_resolution(7)); openParent keeps to it.
const maxSymlinks = 40

// openParent opens the directory holding the last component of the absolute
// path name, and returns it with that component, for the guarded operations
// (see System). It walks from "/" one component at a time with
// openat(O_NOFOLLOW|O_DIRECTORY), so a directory it has opened cannot be
// swapped for a symlink under it. It follows a symlink on the way only when
// the directory holding the link is trusted (see trusted); a link anywhere
// else is refused with ErrSymlinkParent. ".." and relative link targets are
// resolved by the kernel against the directory reached so far.
func (r *Real) openParent(op, name string) (*os.File, string, error) {
	clean := filepath.Clean(name)
	if !filepath.IsAbs(name) || clean == "/" {
		return nil, "", pathErr(op, name, fs.ErrInvalid)
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", pathErr(op, name, err)
	}
	rest := components(filepath.Dir(clean))
	for links := 0; len(rest) > 0; {
		c := rest[0]
		rest = rest[1:]
		next, err := unix.Openat(fd, c, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err == nil {
			_ = unix.Close(fd)
			fd = next
			continue
		}
		// Linux reports a symlink opened with O_DIRECTORY|O_NOFOLLOW as
		// ENOTDIR, other kernels as ELOOP: ask what it is.
		if t, terr := typeAt(fd, c); terr != nil || t != fs.ModeSymlink {
			_ = unix.Close(fd)
			return nil, "", pathErr(op, name, err)
		}
		target, err := r.followable(fd, c, &links)
		if err != nil {
			_ = unix.Close(fd)
			return nil, "", pathErr(op, name, err)
		}
		if filepath.IsAbs(target) {
			_ = unix.Close(fd)
			if fd, err = unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0); err != nil {
				return nil, "", pathErr(op, name, err)
			}
		}
		rest = append(components(target), rest...)
	}
	return os.NewFile(uintptr(fd), filepath.Dir(clean)), filepath.Base(clean), nil
}

// followable returns the target of the symlink c in the directory dirfd if
// that directory is trusted, counting the link against maxSymlinks.
func (r *Real) followable(dirfd int, c string, links *int) (string, error) {
	ok, err := r.trusted(dirfd)
	switch {
	case err != nil:
		return "", err
	case !ok:
		return "", ErrSymlinkParent
	}
	if *links++; *links > maxSymlinks {
		return "", syscall.ELOOP
	}
	return readlinkat(dirfd, c)
}

// trusted reports whether the open directory dirfd is owned by root or by
// the user hardn runs as, and is writable by neither its group nor others:
// then no other user can replace an entry in it.
func (r *Real) trusted(dirfd int) (bool, error) {
	var st unix.Stat_t
	if err := unix.Fstat(dirfd, &st); err != nil {
		return false, err
	}
	return (st.Uid == 0 || int(st.Uid) == r.euid) && st.Mode&0o022 == 0, nil
}

// components splits a path into its non-empty components; "." and ".." are
// kept for the kernel to resolve.
func components(p string) []string {
	var out []string
	for _, c := range strings.Split(p, "/") {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// typeAt is lstat of c in dirfd, as the type bits of an fs.FileMode: 0 for a
// regular file, fs.ModeIrregular for anything that is not a regular file,
// directory or symlink.
func typeAt(dirfd int, c string) (fs.FileMode, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(dirfd, c, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return 0, err
	}
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		return 0, nil
	case unix.S_IFDIR:
		return fs.ModeDir, nil
	case unix.S_IFLNK:
		return fs.ModeSymlink, nil
	}
	return fs.ModeIrregular, nil
}

// readlinkat returns the target of the symlink c in dirfd, as stored.
func readlinkat(dirfd int, c string) (string, error) {
	for size := 256; size <= 1<<16; size *= 2 {
		buf := make([]byte, size)
		n, err := unix.Readlinkat(dirfd, c, buf)
		if err != nil {
			return "", err
		}
		if n < size {
			return string(buf[:n]), nil
		}
	}
	return "", syscall.ENAMETOOLONG
}

// isNoFollow reports the error open(2) gives for a symlink and O_NOFOLLOW:
// ELOOP on Linux and macOS (EMLINK on FreeBSD).
func isNoFollow(err error) bool {
	return errors.Is(err, unix.ELOOP) || errors.Is(err, unix.EMLINK)
}
