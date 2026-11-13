package host

import (
	"errors"
	"syscall"
)

// NotDir reports whether err is the kernel's ENOTDIR: the path passes
// through a file that is no directory, as one under /dev/null does. For a
// reader that is "no such file": the units that may make no system call of
// their own ask it here.
func NotDir(err error) bool { return errors.Is(err, syscall.ENOTDIR) }
