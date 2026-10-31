package revert

import (
	"os"
	"syscall"
	"testing"
)

// TestMain pins the umask, as pkg/host's does: TestStateRealFiles reads "a user
// other than root can change it" where t.TempDir's directories are group-writable.
func TestMain(m *testing.M) {
	syscall.Umask(0o022)
	os.Exit(m.Run())
}
