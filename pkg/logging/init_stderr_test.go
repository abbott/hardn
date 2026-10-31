package logging

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitLoggingCantOpen: where the log's directory cannot be made or the log cannot
// be opened, as for a user other than root, InitLogging says so on stderr and nothing
// on stdout, so that hardn system-details > file and --output json hold the report
// alone (defect 28).
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
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		want       []string
	}{
		{"a directory", t.TempDir(), []string{"Failed to open log file"}},
		{"no directory", filepath.Join(file, "dir", "hardn.log"), []string{"Failed to create log directory", "Failed to open log file"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr := stream(&os.Stdout), stream(&os.Stderr)
			InitLogging(tc.path)
			if got := stdout(); got != "" {
				t.Errorf("stdout %q, want nothing", got)
			}
			got := stderr()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("stderr %q, want %q", got, want)
				}
			}
		})
	}
}
