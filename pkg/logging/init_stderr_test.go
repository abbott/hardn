package logging

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitLoggingCantOpen: where the log cannot be opened, as for a user other than
// root, InitLogging says so on stderr and nothing on stdout, so that hardn
// system-details > file and --output json hold the report alone (defect 28).
func TestInitLoggingCantOpen(t *testing.T) {
	stream := func(f **os.File) func() string {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		was := *f
		*f = w
		return func() string {
			*f = was
			w.Close()
			b, _ := io.ReadAll(r)
			return string(b)
		}
	}
	stdout, stderr := stream(&os.Stdout), stream(&os.Stderr)
	InitLogging(filepath.Join(t.TempDir(), "file", "..")) // a directory
	if got := stdout(); got != "" {
		t.Errorf("stdout %q, want nothing", got)
	}
	if got := stderr(); !strings.Contains(got, "Failed to open log file") {
		t.Errorf("stderr %q", got)
	}
}
