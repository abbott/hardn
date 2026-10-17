//go:build !linux && !darwin

package prompt

import (
	"os"
	"syscall"
)

// interruptible reports whether ReadLine can wait on a file here. It
// cannot: ReadLine reads directly, and Ctrl-C keeps its default action.
const interruptible = false

// wait is never called here (interruptible is false).
func wait(syscall.Conn, *os.File) error { return nil }
