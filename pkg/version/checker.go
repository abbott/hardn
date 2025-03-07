// pkg/version/checker.go
package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// GitHubAPIURL is the endpoint for checking the latest release
	GitHubAPIURL = "https://api.github.com/repos/abbott/hardn/releases/latest"
	
	// CacheFile is where we store the last check results
	CacheFileName = ".hardn-version-cache.json"
	
	// CacheTTL defines how long the cache is valid (24 hours)
	CacheTTL = 24 * time.Hour
)

// GitHubRelease represents the JSON structure of a GitHub release
type GitHubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// VersionCache stores the cached check results
type VersionCache struct {
	LastCheck   time.Time    `json:"last_check"`
	LatestRelease GitHubRelease `json:"latest_release"`
}

// CheckResult contains the result of a version check
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	UpdateAvailable bool
	ReleaseURL     string
	Error          error
}

// CheckForUpdates checks if a newer version is available on GitHub
func CheckForUpdates(currentVersion string) CheckResult {
	result := CheckResult{
		CurrentVersion: currentVersion,
	}
	
	// Skip check if running without version info
	if currentVersion == "" {
		return result
	}
	
	// Try to load from cache first
	cache, cacheValid := loadCache()
	if cacheValid {
		return compareVersions(currentVersion, cache.LatestRelease)
	}
	
	// Fetch from GitHub API with a short timeout
	client := &http.Client{
		Timeout: 3 * time.Second,
	}
	
	req, err := http.NewRequest("GET", GitHubAPIURL, nil)
	if err != nil {
		result.Error = fmt.Errorf("failed to create request: %w", err)
		return result
	}
	
	// Add User-Agent header to be a good API citizen
	req.Header.Set("User-Agent", "hardn-version-checker")
	
	resp, err := client.Do(req)
	if err != nil {
		result.Error = fmt.Errorf("failed to check for updates: %w", err)
		return result
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Errorf("GitHub API returned non-OK status: %s", resp.Status)
		return result
	}
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		result.Error = fmt.Errorf("failed to read response: %w", err)
		return result
	}
	
	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		result.Error = fmt.Errorf("failed to parse GitHub response: %w", err)
		return result
	}
	
	// Save to cache
	saveCache(release)
	
	return compareVersions(currentVersion, release)
}

// compareVersions compares the current version with the latest release
func compareVersions(currentVersion string, release GitHubRelease) CheckResult {
	result := CheckResult{
		CurrentVersion: currentVersion,
		LatestVersion: strings.TrimPrefix(release.TagName, "v"),
		ReleaseURL: release.HTMLURL,
	}
	
	// Clean version strings (remove 'v' prefix if present)
	current := strings.TrimPrefix(currentVersion, "v")
	latest := strings.TrimPrefix(release.TagName, "v")
	
	// Handle pre-release suffixes
	currentBase := current
	latestBase := latest
	
	if strings.Contains(current, "-") {
		parts := strings.SplitN(current, "-", 2)
		currentBase = parts[0]
	}
	
	if strings.Contains(latest, "-") {
		parts := strings.SplitN(latest, "-", 2)
		latestBase = parts[0]
	}
	
	// Compare base versions
	currentBaseParts := strings.Split(currentBase, ".")
	latestBaseParts := strings.Split(latestBase, ".")
	
	// Ensure we have at least 3 components (major.minor.patch)
	for len(currentBaseParts) < 3 {
		currentBaseParts = append(currentBaseParts, "0")
	}
	
	for len(latestBaseParts) < 3 {
		latestBaseParts = append(latestBaseParts, "0")
	}
	
	// Compare version components
	for i := 0; i < 3; i++ {
		currentNum, _ := strconv.Atoi(currentBaseParts[i])
		latestNum, _ := strconv.Atoi(latestBaseParts[i])
		
		if latestNum > currentNum {
			result.UpdateAvailable = true
			return result
		} else if currentNum > latestNum {
			result.UpdateAvailable = false
			return result
		}
	}
	
	// If base versions are equal, compare pre-release status
	// A version without a pre-release suffix is considered newer than one with it
	isCurrentPreRelease := strings.Contains(current, "-")
	isLatestPreRelease := strings.Contains(latest, "-")
	
	if isCurrentPreRelease && !isLatestPreRelease {
		result.UpdateAvailable = true
	} else if !isCurrentPreRelease && isLatestPreRelease {
		result.UpdateAvailable = false
	}
	
	return result
}

// loadCache tries to load the cached version check results
func loadCache() (VersionCache, bool) {
	var cache VersionCache
	
	// Get cache file path
	cacheFile := getCacheFilePath()
	
	// Try to read cache file
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return cache, false
	}
	
	// Parse JSON
	if err := json.Unmarshal(data, &cache); err != nil {
		return cache, false
	}
	
	// Check if cache is still valid
	if time.Since(cache.LastCheck) > CacheTTL {
		return cache, false
	}
	
	return cache, true
}

// saveCache saves the version check results to cache
func saveCache(release GitHubRelease) {
	cache := VersionCache{
		LastCheck:     time.Now(),
		LatestRelease: release,
	}
	
	// Convert to JSON
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	
	// Write to cache file
	cacheFile := getCacheFilePath()
	os.WriteFile(cacheFile, data, 0644)
}

// getCacheFilePath returns the path to the cache file
func getCacheFilePath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to system temp directory if home dir not available
		return filepath.Join(os.TempDir(), CacheFileName)
	}
	
	return filepath.Join(homeDir, CacheFileName)
}