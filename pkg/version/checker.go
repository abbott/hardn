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

	// CacheFileName is where we store the last check results
	CacheFileName = ".hardn-version-cache.json"

	// CacheTTL defines how long the cache is valid (24 hours)
	CacheTTL = 24 * time.Hour
)

// GitHubRelease represents the JSON structure of a GitHub release
type GitHubRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"` // Add body field to check for security notices
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// VersionCache stores the cached check results
type VersionCache struct {
	LastCheck     time.Time     `json:"last_check"`
	LatestRelease GitHubRelease `json:"latest_release"`
}

// CheckResult contains the result of a version check
type CheckResult struct {
	CurrentVersion          string
	LatestVersion           string
	UpdateAvailable         bool
	ReleaseURL              string
	InstallURL              string
	Error                   error
	SecurityUpdateAvailable bool   // New field for security updates
	SecurityUpdateDetails   string // Details about the security update
}

// CheckForUpdates checks if a newer version is available on GitHub
func CheckForUpdates(currentVersion string, debug bool) CheckResult {
	result := CheckResult{
		CurrentVersion: currentVersion,
	}

	// Print debug info if enabled
	if debug {
		fmt.Println("DEBUG: Checking for updates...")
		fmt.Println("DEBUG: Current version:", currentVersion)
	}

	// Skip check if running without version info
	if currentVersion == "" {
		if debug {
			fmt.Println("DEBUG: No version information provided. Skipping update check.")
		}
		return result
	}

	// Use environment variable for cache control
	if os.Getenv("HARDN_CLEAR_CACHE") != "" {
		if debug {
			fmt.Println("DEBUG: Clearing cache file")
		}
		os.Remove(getCacheFilePath())
	}

	// Try to load from cache first
	cache, cacheValid := loadCache()
	if cacheValid {
		if debug {
			fmt.Println("DEBUG: Using cached version information")
			fmt.Println("DEBUG: Cached latest version:", cache.LatestRelease.TagName)
		}
		return compareVersions(currentVersion, cache.LatestRelease)
	}

	if debug {
		fmt.Println("DEBUG: No valid cache found. Fetching from GitHub API...")
	}

	// Fetch from GitHub API with a short timeout
	client := &http.Client{
		Timeout: 3 * time.Second,
	}

	req, err := http.NewRequest("GET", GitHubAPIURL, nil)
	if err != nil {
		if debug {
			fmt.Printf("DEBUG: Failed to create request: %v\n", err)
		}
		result.Error = fmt.Errorf("failed to create request: %w", err)
		return result
	}

	// Add User-Agent header to be a good API citizen
	req.Header.Set("User-Agent", "hardn-version-checker")

	if debug {
		fmt.Println("DEBUG: Sending request to GitHub API...")
	}

	resp, err := client.Do(req)
	if err != nil {
		if debug {
			fmt.Printf("DEBUG: Failed to check for updates: %v\n", err)
		}
		result.Error = fmt.Errorf("failed to check for updates: %w", err)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if debug {
			fmt.Printf("DEBUG: GitHub API returned non-OK status: %s\n", resp.Status)
		}
		result.Error = fmt.Errorf("GitHub API returned non-OK status: %s", resp.Status)
		return result
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if debug {
			fmt.Printf("DEBUG: Failed to read response: %v\n", err)
		}
		result.Error = fmt.Errorf("failed to read response: %w", err)
		return result
	}

	var release GitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		if debug {
			fmt.Printf("DEBUG: Failed to parse GitHub response: %v\n", err)
		}
		result.Error = fmt.Errorf("failed to parse GitHub response: %w", err)
		return result
	}

	if debug {
		fmt.Println("DEBUG: Received latest version:", release.TagName)
		fmt.Println("DEBUG: Saving to cache...")
	}

	// Save to cache
	saveCache(release)

	// Verify cache was written
	if debug {
		cacheFile := getCacheFilePath()
		if _, err := os.Stat(cacheFile); err == nil {
			fmt.Println("DEBUG: Successfully wrote cache file to:", cacheFile)
		} else {
			fmt.Printf("DEBUG: Failed to verify cache file: %v\n", err)
		}
	}

	return compareVersions(currentVersion, release)
}

// isSecurityUpdate checks if the release contains security-related updates
func isSecurityUpdate(release GitHubRelease) (bool, string) {
	// Check for security indicators in the release name
	nameLower := strings.ToLower(release.Name)
	if strings.Contains(nameLower, "security") ||
		strings.Contains(nameLower, "[security]") ||
		strings.Contains(nameLower, "cve-") {
		return true, release.Name
	}

	// Check for security indicators in the release body
	bodyLower := strings.ToLower(release.Body)
	if strings.Contains(bodyLower, "security") ||
		strings.Contains(bodyLower, "[security]") ||
		strings.Contains(bodyLower, "cve-") ||
		strings.Contains(bodyLower, "vulnerability") ||
		strings.Contains(bodyLower, "exploit") {

		// Try to extract relevant details from the body
		lines := strings.Split(release.Body, "\n")
		for _, line := range lines {
			lineLower := strings.ToLower(line)
			if strings.Contains(lineLower, "security") ||
				strings.Contains(lineLower, "cve-") ||
				strings.Contains(lineLower, "vulnerability") {

				// Clean up the line for display
				line = strings.TrimSpace(line)
				if len(line) > 100 {
					line = line[:97] + "..."
				}
				return true, line
			}
		}

		// Default security message if we couldn't extract a specific line
		return true, "Security updates available"
	}

	return false, ""
}

// compareVersions compares the current version with the latest release
func compareVersions(currentVersion string, release GitHubRelease) CheckResult {
	result := CheckResult{
		CurrentVersion: currentVersion,
		LatestVersion:  strings.TrimPrefix(release.TagName, "v"),
		ReleaseURL:     release.HTMLURL,
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

			// Check if this is a security update
			isSecurityUpdate, details := isSecurityUpdate(release)
			result.SecurityUpdateAvailable = isSecurityUpdate
			result.SecurityUpdateDetails = details

			return result
		} else if currentNum > latestNum {
			return result
		}
	}

	// If base versions are equal, compare pre-release status
	// A version without a pre-release suffix is considered newer than one with it
	isCurrentPreRelease := strings.Contains(current, "-")
	isLatestPreRelease := strings.Contains(latest, "-")

	if isCurrentPreRelease && !isLatestPreRelease {
		result.UpdateAvailable = true

		// Check if this is a security update
		isSecurityUpdate, details := isSecurityUpdate(release)
		result.SecurityUpdateAvailable = isSecurityUpdate
		result.SecurityUpdateDetails = details
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
		fmt.Printf("Warning: Failed to marshal version cache: %v\n", err)
		return
	}

	// Get cache file path
	cacheFile := getCacheFilePath()

	// Ensure directory exists
	dir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Warning: Failed to create directory for version cache: %v\n", err)
		return
	}

	// Write to cache file
	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		// Log the error but don't fail the operation since this is just cache
		fmt.Printf("Warning: Failed to write version cache: %v\n", err)
		return
	}
}

// getCacheFilePath returns the path to the cache file
func getCacheFilePath() string {
	// Check if HARDN_CACHE_PATH environment variable is set
	if cachePath := os.Getenv("HARDN_CACHE_PATH"); cachePath != "" {
		return cachePath
	}

	// Use /tmp for easier access across users
	return "/tmp/hardn-version-cache.json"
}
