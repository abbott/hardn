// Package characterization records what origin/main does today, as golden files.
//
// What is left: config.LoadConfig on fixture files (config/) and pkg/host/backup's
// run of one change on hostfake (backups/). Nothing here executes a command, and
// nothing writes outside t.TempDir() except -update rewriting testdata/golden.
//
// The goldens are a baseline, not a specification: origin/main is the behaviour
// baseline (maintainer decision 6). The operations goldens (ops/) went in
// refactor plan slice 8, unit (d), with the legacy layers they reached; the
// host_info golden with the host-info chain it read.
//
// Regenerate after an intended change:
//
//	go test ./pkg/characterization -run . -update
package characterization

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/ instead of comparing")

// baselineHeader is the first line of every golden in this package.
const baselineHeader = "# origin/main behaviour (baseline per maintainer decision 6)"

func errText(err error) string {
	if err == nil {
		return "ok"
	}
	return "error: " + err.Error()
}

// assertGolden compares got with testdata/golden/<rel>, or rewrites it under -update.
func assertGolden(t *testing.T, rel string, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", filepath.FromSlash(rel))
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create it)", path, err)
	}
	want := string(wantBytes)
	if got == want {
		return
	}
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(wl) || i < len(gl); i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		if w != g {
			t.Fatalf("golden %s differs at line %d\nwant: %q\ngot:  %q\n(if the change is intended, rerun with -update and explain the diff in the review description)", path, i+1, w, g)
		}
	}
	t.Fatalf("golden %s differs (line endings)", path)
}
