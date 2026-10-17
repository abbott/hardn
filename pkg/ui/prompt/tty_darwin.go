package prompt

import "golang.org/x/sys/unix"

// terminalFd reports whether fd is a terminal: whether TIOCGETA reads its
// attributes, as x/term's IsTerminal asks on macOS (term_unix_bsd.go).
func terminalFd(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	return err == nil
}
