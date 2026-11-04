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

// TestVersionFlag: --version and -v print the version, the build date and
// the commit the ldflags set (main.Version, main.BuildDate,
// main.GitCommit), as v0.3.2 did, each "unknown" when the build left it
// empty, on stdout, before the root check, exit status 0; cobra's own
// "hardn version" line never.
func TestVersionFlag(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		version, date, commit string
		want                  string
	}{
		{"a release build", "0.4.0", "2026-11-04T12:00:00Z", "0123456789abcdef0123456789abcdef01234567",
			"hardn - Linux hardening tool\nVersion:    0.4.0\nBuild Date: 2026-11-04T12:00:00Z\nGit Commit: 0123456789abcdef0123456789abcdef01234567\n"},
		{"go build, no ldflags", "", "", "",
			"hardn - Linux hardening tool\nVersion:    unknown\nBuild Date: unknown\nGit Commit: unknown\n"},
	} {
		for _, flag := range []string{"--version", "-v"} {
			t.Run(tc.name+" "+flag, func(t *testing.T) {
				was := [3]string{Version, BuildDate, GitCommit}
				Version, BuildDate, GitCommit = tc.version, tc.date, tc.commit
				t.Cleanup(func() { Version, BuildDate, GitCommit = was[0], was[1], was[2] })
				r := runHardn(t, hostUnreachable, flag)
				if r.ended || r.stdout != tc.want || r.stderr != "" || len(r.reached) != 0 {
					t.Errorf("ended %v (%d), reached %v; stdout:\n%s\nstderr:\n%s", r.ended, r.code, r.reached, r.stdout, r.stderr)
				}
			})
		}
	}
}
