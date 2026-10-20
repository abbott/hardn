//go:build darwin

package hold

import (
	"syscall"
	"unsafe"
)

// restoreDefault sets sig's action to the default (SIG_DFL) where it is
// ignored (SIG_IGN), with the sigaction system call. Its new action is a
// struct __sigaction (the handler, the trampoline, a 4-byte mask and the
// flags, 24 bytes) and its old one a struct sigaction (the handler, the
// mask, the flags, 16 bytes) (Go 1.27 runtime/defs_darwin_arm64.go,
// sigactiont and usigactiont): zero is SIG_DFL with no flags and an empty
// mask, and the handler comes first in both. A handler that is not SIG_IGN
// is left: someone installed it since the hold began. hardn runs on Linux;
// this is for the tests on a macOS development host.
func restoreDefault(sig syscall.Signal) error {
	var cur, dfl [4]uint64
	if _, _, e := syscall.RawSyscall(syscall.SYS_SIGACTION, uintptr(sig), 0, uintptr(unsafe.Pointer(&cur))); e != 0 {
		return e
	}
	if cur[0] != sigIgn {
		return nil
	}
	if _, _, e := syscall.RawSyscall(syscall.SYS_SIGACTION, uintptr(sig), uintptr(unsafe.Pointer(&dfl)), 0); e != 0 {
		return e
	}
	return nil
}
