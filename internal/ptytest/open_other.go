//go:build !linux && !darwin

package ptytest

import (
	"os"
	"testing"
)

// Open skips the test: pseudo-terminals are opened on Linux, which hardn
// runs on, and macOS, where it is developed.
func Open(t testing.TB) (master, slave *os.File) {
	t.Helper()
	t.Skip("no pseudo-terminal on this system")
	return nil, nil
}

func canonical(int) (bool, error) { return true, nil }
