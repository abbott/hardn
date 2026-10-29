package hangup

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal and returns its master, which the test
// never reads, as sudo-rs stops reading its pty once the SSH session has
// dropped, and its slave as hardn's stdout is: a blocking file, outside
// Go's poller (os.NewFile), whose Write blocks in the kernel once the
// pty's buffer is full. It skips the test when the system has no
// pseudo-terminal to give. Both are closed when the test ends, the master
// first: the slave then reads as hung up, and a write blocked on it ends.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	fd := int(m.Fd())
	// grantpt and unlockpt; ptsname with TIOCPTYGNAME, which fills a
	// 128-byte buffer (<sys/ttycom.h>).
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		_ = m.Close()
		t.Fatalf("grantpt: %v", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		_ = m.Close()
		t.Fatalf("unlockpt: %v", err)
	}
	var name [128]byte
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); e != 0 {
		_ = m.Close()
		t.Fatalf("ptsname: %v", e)
	}
	n, _, _ := bytes.Cut(name[:], []byte{0})
	sfd, err := syscall.Open(string(n), syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		_ = m.Close()
		t.Fatalf("open %s: %v", n, err)
	}
	s := os.NewFile(uintptr(sfd), string(n))
	t.Cleanup(func() { _ = m.Close(); _ = s.Close() })
	return m, s
}
