package prompt

import "golang.org/x/sys/unix"

// terminalFd reports whether fd is a terminal: whether TIOCGETA reads its
// attributes, as x/term's IsTerminal asks on macOS (term_unix_bsd.go).
func terminalFd(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	return err == nil
}

// getTermios reads fd's terminal attributes (TIOCGETA).
func getTermios(fd int) (*unix.Termios, error) { return unix.IoctlGetTermios(fd, unix.TIOCGETA) }

// setTermios sets fd's terminal attributes at once (TIOCSETA).
func setTermios(fd int, t *unix.Termios) error { return unix.IoctlSetTermios(fd, unix.TIOCSETA, t) }
