package prompt

import (
	"os"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal in its default, cooked mode and returns
// its master, where the test types, and its slave, which hardn would read
// as stdin. It skips the test when the system has no pseudo-terminal to
// give, as in a container without devpts. Both are closed when the test
// ends; the master's echo is drained.
//
// Written, not run: the tests have run on macOS only.
func openPTY(t *testing.T) (master, slave *os.File) {
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
	go drain(m)
	return m, s
}

// drain reads the master's output, the slave's echo, until it is closed.
func drain(m *os.File) {
	b := make([]byte, 1024)
	for {
		if _, err := m.Read(b); err != nil {
			return
		}
	}
}
