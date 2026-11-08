package prompt

import "golang.org/x/sys/unix"

// terminalFd reports whether fd is a terminal: whether TCGETS reads its
// attributes, as x/term's IsTerminal asks on Linux (term_unix_other.go).
func terminalFd(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}

// getTermios reads fd's terminal attributes (TCGETS).
func getTermios(fd int) (*unix.Termios, error) { return unix.IoctlGetTermios(fd, unix.TCGETS) }

// setTermios sets fd's terminal attributes at once (TCSETS).
func setTermios(fd int, t *unix.Termios) error { return unix.IoctlSetTermios(fd, unix.TCSETS, t) }
