package menu

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/version"
)

// TestStartupMakesNoUpdateCheck makes the update calls of hardn's
// interactive start, cmd/hardn/main.go's menu branch (--test-update,
// --test-security-update, HARDN_FORCE_UPDATE, HARDN_FORCE_SECURITY, else
// CheckForUpdates) and then ShowMainMenu's CheckForUpdatesWithEnvVars,
// with the cache an unprivileged user can plant (side-effects D2) where
// HARDN_CACHE_PATH points. With update checks disabled (V26) none sets an
// update or security notice, so the menu is never replaced by the "Update
// Binary" screen that exits, and the cache is neither read into a result
// nor changed, nor joined by another file.
func TestStartupMakesNoUpdateCheck(t *testing.T) {
	if version.UpdateCheckEnabled() {
		t.Fatal("update checks are enabled (pkg/version updateCheckEnabled; V26)")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hardn-version-cache.json")
	planted, err := json.Marshal(version.VersionCache{
		LastCheck: time.Now().Add(240 * time.Hour),
		LatestRelease: version.GitHubRelease{
			TagName: "v99.0.0", Name: "[security] fix",
			Body:    "Critical security vulnerability CVE-2026-0001",
			HTMLURL: "https://attacker.example/hardn",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, planted, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARDN_CACHE_PATH", path)
	t.Setenv("HARDN_DEBUG", "")
	t.Setenv("HARDN_CLEAR_CACHE", "")

	for _, env := range []map[string]string{
		{"HARDN_FORCE_UPDATE": "", "HARDN_FORCE_SECURITY": ""},
		{"HARDN_FORCE_UPDATE": "1", "HARDN_FORCE_SECURITY": ""},
		{"HARDN_FORCE_UPDATE": "", "HARDN_FORCE_SECURITY": "1"},
	} {
		for k, v := range env {
			t.Setenv(k, v)
		}
		m := NewMainMenu(config.DefaultConfig(), version.NewService("0.4.0", "", ""))
		m.SetTestUpdateAvailable("99.0.0")
		m.SetTestSecurityUpdate("Critical security vulnerability CVE-2023-1234: Privilege escalation in SSH component")
		m.CheckForUpdatesWithEnvVars()
		m.CheckForUpdates()
		if m.updateAvailable || m.securityUpdateAvailable || m.latestVersion != "" || m.updateURL != "" || m.installURL != "" || m.securityUpdateDetails != "" {
			t.Errorf("env %v: update state set: %+v", env, *m)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(planted) {
		t.Errorf("the planted cache changed (%v)", err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 {
		t.Errorf("files beside the cache: %v (%v)", entries, err)
	}
}
