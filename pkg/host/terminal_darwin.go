package host

import "golang.org/x/sys/unix"

// The ioctls of tcgetattr(3) and tcsetattr(3), TCSANOW.
const (
	getTermios = unix.TIOCGETA
	setTermios = unix.TIOCSETA
)
