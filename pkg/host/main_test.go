package host

import (
	"os"
	"syscall"
	"testing"
)

// TestMain pins the umask. t.TempDir's directories follow it, and under 0002, a
// user's own on the Debian and Ubuntu VMs, they are group-writable, so not trusted
// (guard.go), and a guarded test reads a refusal it did not ask for.
func TestMain(m *testing.M) {
	syscall.Umask(0o022)
	os.Exit(m.Run())
}
