//go:build linux

package hold

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

// errNoRestore: this architecture has no restore here.
var errNoRestore = errors.New("no restore of the default action on this architecture")

// restoreDefault sets sig's action to the default (SIG_DFL) where it is
// ignored (SIG_IGN), with rt_sigaction(2). On amd64 and arm64 the kernel's
// struct sigaction is the handler, the flags, the restorer and an 8-byte
// mask (Go 1.27 runtime/defs_linux_amd64.go and defs_linux_arm64.go,
// sigactiont), whose zero value is SIG_DFL with no flags and an empty mask;
// other architectures lay it out otherwise and are left alone
// (errNoRestore). A handler that is not SIG_IGN is left: someone installed
// it since the hold began.
func restoreDefault(sig syscall.Signal) error {
	switch runtime.GOARCH {
	case "amd64", "arm64":
	default:
		return errNoRestore
	}
	const sigsetSize = 8
	var cur, dfl [4]uint64
	if _, _, e := syscall.RawSyscall6(syscall.SYS_RT_SIGACTION, uintptr(sig), 0, uintptr(unsafe.Pointer(&cur)), sigsetSize, 0, 0); e != 0 {
		return e
	}
	if cur[0] != sigIgn {
		return nil
	}
	if _, _, e := syscall.RawSyscall6(syscall.SYS_RT_SIGACTION, uintptr(sig), uintptr(unsafe.Pointer(&dfl)), 0, sigsetSize, 0, 0); e != 0 {
		return e
	}
	return nil
}
