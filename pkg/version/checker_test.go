package version

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubIO replaces the checker's network and cache I/O with stubs that
// record each call and fail, until the test ends.
func stubIO(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	prev := struct {
		do    func(*http.Client, *http.Request) (*http.Response, error)
		read  func(string) ([]byte, error)
		write func(string, []byte, fs.FileMode) error
		rm    func(string) error
		mkdir func(string, fs.FileMode) error
		stat  func(string) (os.FileInfo, error)
	}{doRequest, readFile, writeFile, removeFile, mkdirAll, statFile}
	t.Cleanup(func() {
		doRequest, readFile, writeFile, removeFile, mkdirAll, statFile = prev.do, prev.read, prev.write, prev.rm, prev.mkdir, prev.stat
	})
	doRequest = func(_ *http.Client, req *http.Request) (*http.Response, error) {
		calls = append(calls, "GET "+req.URL.String())
		return nil, os.ErrPermission
	}
	readFile = func(name string) ([]byte, error) { calls = append(calls, "read "+name); return nil, os.ErrPermission }
	writeFile = func(name string, _ []byte, _ fs.FileMode) error {
		calls = append(calls, "write "+name)
		return os.ErrPermission
	}
	removeFile = func(name string) error { calls = append(calls, "remove "+name); return os.ErrPermission }
	mkdirAll = func(name string, _ fs.FileMode) error { calls = append(calls, "mkdir "+name); return os.ErrPermission }
	statFile = func(name string) (os.FileInfo, error) {
		calls = append(calls, "stat "+name)
		return nil, os.ErrPermission
	}
	return &calls
}

// TestUpdateCheckDisabled checks the update checker's gate (V26): every
// entry point, forced or not, reports no update without a network call or
// any cache I/O, and leaves the environment as it found it.
func TestUpdateCheckDisabled(t *testing.T) {
	if updateCheckEnabled {
		t.Fatal("updateCheckEnabled is set: hardn calls GitHub and trusts a /tmp cache anyone can plant (V26)")
	}
	t.Setenv("HARDN_DEBUG", "")
	t.Setenv("HARDN_CLEAR_CACHE", "1") // would remove the cache if a check ran
	t.Setenv("HARDN_CACHE_PATH", "")
	calls := stubIO(t)
	s := NewService("0.4.0", "", "")

	results := map[string]CheckResult{
		"CheckForUpdates":           CheckForUpdates("0.4.0", true),
		"Service, no options":       s.CheckForUpdates(nil),
		"Service, debug":            s.CheckForUpdates(&UpdateOptions{Debug: true, ClearCache: true, CacheFilePath: "/tmp/elsewhere.json"}),
		"Service, --test-update":    s.CheckForUpdates(&UpdateOptions{ForceUpdate: true, ForcedVersion: "99.0.0"}),
		"Service, security forced":  s.CheckForUpdates(&UpdateOptions{ForceUpdate: true, ForcedVersion: "0.4.0.1", ForceSecurityUpdate: true, SecurityDetails: "CVE-2023-1234"}),
		"Service, skip cache":       s.CheckForUpdates(&UpdateOptions{SkipCache: true}),
		"Service, unversioned host": NewService("", "", "").CheckForUpdates(nil),
	}
	for name, r := range results {
		if r.UpdateAvailable || r.SecurityUpdateAvailable || r.LatestVersion != "" || r.ReleaseURL != "" || r.InstallURL != "" || r.Error != nil {
			t.Errorf("%s: %+v, want no update", name, r)
		}
	}
	if sec, details, err := s.CheckForSecurityUpdates(&UpdateOptions{ForceUpdate: true, ForceSecurityUpdate: true}); sec || details != "" || err != nil {
		t.Errorf("CheckForSecurityUpdates: %v %q %v", sec, details, err)
	}
	if valid, _, err := s.GetCacheStatus(); valid || err == nil || err.Error() != "update checks are disabled" {
		t.Errorf("GetCacheStatus: %v, %v", valid, err)
	}
	if len(*calls) != 0 {
		t.Errorf("network or cache calls: %v", *calls)
	}
	for _, v := range []string{"HARDN_DEBUG", "HARDN_CACHE_PATH"} {
		if os.Getenv(v) != "" {
			t.Errorf("%s left set to %q", v, os.Getenv(v))
		}
	}
	if os.Getenv("HARDN_CLEAR_CACHE") != "1" {
		t.Errorf("HARDN_CLEAR_CACHE changed")
	}
}

// TestPlantedCacheIgnored plants the cache Phase 0 showed an unprivileged
// user can write (side-effects D2) where HARDN_CACHE_PATH points, then
// runs a real check with the network stubbed out: no update is reported,
// and the file is neither read into a result nor changed.
func TestPlantedCacheIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hardn-version-cache.json")
	planted, err := json.Marshal(VersionCache{
		LastCheck: time.Now().Add(240 * time.Hour),
		LatestRelease: GitHubRelease{
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
	t.Setenv("HARDN_CLEAR_CACHE", "")
	prevDo := doRequest
	t.Cleanup(func() { doRequest = prevDo })
	doRequest = func(*http.Client, *http.Request) (*http.Response, error) {
		t.Error("network call")
		return nil, os.ErrPermission
	}

	r := NewService("0.4.0", "", "").CheckForUpdates(nil)
	if r.UpdateAvailable || r.SecurityUpdateAvailable || strings.Contains(r.ReleaseURL, "attacker") {
		t.Errorf("the planted cache was used: %+v", r)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(planted) {
		t.Errorf("cache changed: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("files in the cache dir: %v, %v", entries, err)
	}
}

// TestUpdateFlagUsage checks the help of the update flags while checks
// are disabled.
func TestUpdateFlagUsage(t *testing.T) {
	if got, want := UpdateFlagUsage("Force update notification for testing"), "Force update notification for testing (no effect: update checks are disabled)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if UpdateCheckEnabled() {
		t.Error("UpdateCheckEnabled() = true")
	}
}
