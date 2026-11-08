package ptytest

import (
	"os"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// Open opens a pseudo-terminal in its default, cooked mode and returns its
// master and its slave. It skips the test when the system has no
// pseudo-terminal to give, as in a container without devpts. Both are
// closed when the test ends; nothing reads the master's output (Drain).
func Open(t testing.TB) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	fd := int(m.Fd())
	// unlockpt, then ptsname from the slave's number.
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("ptsname: %v", err)
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.Itoa(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return m, s
}

// canonical reports whether fd's line editing (ICANON) is on.
func canonical(fd int) (bool, error) {
	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return false, err
	}
	return t.Lflag&unix.ICANON != 0, nil
}
