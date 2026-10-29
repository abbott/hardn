package hangup

import (
	"os"
	"strconv"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal and returns its master, which the test
// never reads, as sudo-rs stops reading its pty once the SSH session has
// dropped, and its slave as hardn's stdout is: a blocking file, outside
// Go's poller (os.NewFile), whose Write blocks in the kernel once the
// pty's buffer is full (n_tty_write). It skips the test when the system
// has no pseudo-terminal to give, as in a container without devpts. Both
// are closed when the test ends, the master first: closing it hangs the
// slave up, and a write blocked on it ends.
//
// Written, not run: the tests have run on macOS only.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	fd := int(m.Fd())
	// unlockpt, then ptsname from the slave's number.
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = m.Close()
		t.Fatalf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = m.Close()
		t.Fatalf("ptsname: %v", err)
	}
	name := "/dev/pts/" + strconv.Itoa(n)
	sfd, err := syscall.Open(name, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		t.Skipf("no pseudo-terminal: %v", err)
	}
	s := os.NewFile(uintptr(sfd), name)
	t.Cleanup(func() { _ = m.Close(); _ = s.Close() })
	return m, s
}
