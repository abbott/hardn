package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// No step line of an engine scenario is cut: every change's title fits
// one line in every state, at every width of the core set
// (engine.TitleMax, engine.Change.Title). The Phase 3 VM run found the
// clean-up's step cut ("Drop port 22 from /etc/ssh/sshd_config.d/00-hardn...").
func TestPlayEngineStepsNotCut(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "golden", "play", "engine", "*.golden"))
	if err != nil || len(files) == 0 {
		t.Fatalf("goldens: %v, %d files", err, len(files))
	}
	escape := regexp.MustCompile(`␛\[[0-9;]*m`)
	step := regexp.MustCompile(`(Pending|Running|Done|Failed|Rolled back|Skipped): `)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			l := strings.TrimSuffix(escape.ReplaceAllString(line, ""), "$")
			if step.MatchString(l) && (strings.HasSuffix(l, "...") || strings.HasSuffix(l, "…")) {
				t.Errorf("%s:%d: a step line is cut: %q", filepath.Base(f), i+1, l)
			}
		}
	}
}
