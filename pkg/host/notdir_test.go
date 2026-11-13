package host_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/abbott/hardn/pkg/host"
)

// TestNotDir: a path under a regular file is ENOTDIR, a missing one is not.
func TestNotDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(filepath.Join(f, "below")); !host.NotDir(err) {
		t.Errorf("under a file: %v", err)
	}
	if _, err := os.ReadFile(f + "-missing"); host.NotDir(err) || !os.IsNotExist(err) {
		t.Errorf("missing: %v", err)
	}
	if host.NotDir(nil) || host.NotDir(fs.ErrNotExist) {
		t.Error("nil or ErrNotExist is NotDir")
	}
}
