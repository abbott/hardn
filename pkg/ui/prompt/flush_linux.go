package prompt

import "golang.org/x/sys/unix"

// flushInput discards the terminal fd's input queue: what was typed and
// not yet read. TCFLSH takes its queue selector by value, as glibc's
// tcflush(fd, TCIFLUSH) passes it (x/sys unix zerrors_linux_*.go: TCFLSH
// per architecture, TCIFLUSH 0).
//
// Written, not run on a TTY: the tests have run on macOS only, where a
// pseudo-terminal exercises flush_darwin.go; this file is cross-compiled
// and vetted. On Linux, TestTerminalTypeahead runs it on a pseudo-terminal
// (pty_linux_test.go), and its pipe case expects ENOTTY.
func flushInput(fd int) error {
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIFLUSH)
}
