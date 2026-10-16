package prompt

import "golang.org/x/sys/unix"

// terminalFd reports whether fd is a terminal: whether TCGETS reads its
// attributes, as x/term's IsTerminal asks on Linux (term_unix_other.go).
func terminalFd(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}
