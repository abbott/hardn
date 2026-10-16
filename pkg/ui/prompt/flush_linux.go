package prompt

import "golang.org/x/sys/unix"

// flushInput discards the terminal fd's input queue: what was typed and
// not yet read. TCFLSH takes its queue selector by value, as glibc's
// tcflush(fd, TCIFLUSH) passes it (x/sys unix zerrors_linux_*.go: TCFLSH
// per architecture, TCIFLUSH 0).
//
// Written, not run on a terminal: tests run on macOS and have no terminal;
// they check only that the ioctl is made (it fails on a pipe).
func flushInput(fd int) error {
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}
