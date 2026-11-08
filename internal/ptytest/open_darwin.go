package ptytest

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Open opens a pseudo-terminal in its default, cooked mode and returns its
// master and its slave. It skips the test when the system has no
// pseudo-terminal to give. Both are closed when the test ends; nothing reads
// the master's output (Drain).
func Open(t testing.TB) (master, slave *os.File) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = m.Close() })
	fd := int(m.Fd())
	// grantpt and unlockpt; ptsname with TIOCPTYGNAME, which fills a
	// 128-byte buffer (<sys/ttycom.h>).
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		t.Fatalf("grantpt: %v", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		t.Fatalf("unlockpt: %v", err)
	}
	var name [128]byte
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); e != 0 {
		t.Fatalf("ptsname: %v", e)
	}
	n, _, _ := bytes.Cut(name[:], []byte{0})
	s, err := os.OpenFile(string(n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", n, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return m, s
}

// canonical reports whether fd's line editing (ICANON) is on.
func canonical(fd int) (bool, error) {
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return false, err
	}
	return t.Lflag&unix.ICANON != 0, nil
}
