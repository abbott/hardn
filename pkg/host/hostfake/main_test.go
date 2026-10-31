package hostfake_test

import (
	"os"
	"syscall"
	"testing"
)

// TestMain pins the umask, as pkg/host's does: TestContract's real side reads a
// guarded refusal it did not ask for where t.TempDir's directories are
// group-writable.
func TestMain(m *testing.M) {
	syscall.Umask(0o022)
	os.Exit(m.Run())
}
