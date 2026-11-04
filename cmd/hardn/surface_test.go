package main

// The surface changes of refactor plan §3.11 and §3.14 (Phase 7, step 5;
// adopted by the maintainer 2026-11-04: "adopt all").

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/config"
)

// TestDisableRootWarning: hardn.yml's disableRoot, v0.3.2's name, is read
// as disableRootSSH and warned about on stderr where the configuration
// loads, with the file and line, naming disableRootSSH; with both set,
// disableRootSSH wins, and the warning says so.
func TestDisableRootWarning(t *testing.T) {
	for _, tc := range []struct {
		name, file, words string
		want              bool
	}{
		{"disableRoot alone", "username: alice\ndisableRoot: true\n", "hardn.yml: disableRoot (line 2) is now disableRootSSH", true},
		{"both", "disableRoot: true\ndisableRootSSH: false\n",
			"hardn.yml: disableRoot (line 1) is now disableRootSSH, which this file sets too; disableRootSSH is used", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runHardnWith(t, asRoot, func() {
				onDNSHost(t)
				loadConfig = func(string) (*config.Config, error) {
					path := filepath.Join(t.TempDir(), "hardn.yml")
					if err := os.WriteFile(path, []byte(tc.file), 0o600); err != nil {
						t.Fatal(err)
					}
					c, err := config.LoadConfigWithEnvPriority(path)
					if err == nil {
						cfg = c
					}
					return c, err
				}
			}, "-n", "-r")
			if got := flat(r.stderr); !strings.Contains(got, tc.words) || strings.Count(got, "disableRoot (line") != 1 {
				t.Errorf("stderr lacks %q once:\n%s", tc.words, r.stderr)
			}
			if cfg == nil || cfg.DisableRootSSH != tc.want {
				t.Errorf("disableRootSSH read %v, want %t", cfg, tc.want)
			}
		})
	}
}
