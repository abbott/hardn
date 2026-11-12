package version

import (
	"fmt"
	"io"
	"os"
	"time"
)

// UpdateOptions controls the behavior of the update checker
type UpdateOptions struct {
	// Force an update to be available (for testing)
	ForceUpdate bool
	// Version to use if forcing an update
	ForcedVersion string
	// Show debug output
	Debug bool
	// Skip cache and force a fresh check
	SkipCache bool
	// Custom cache file location
	CacheFilePath string
	// Force immediate cache expiration
	ClearCache bool
	// Force a security update (for testing)
	ForceSecurityUpdate bool
	// Custom security details for testing
	SecurityDetails string
}

// Service provides version checking functionality
type Service struct {
	CurrentVersion string
	BuildDate      string
	GitCommit      string
}

// NewService creates a version service instance
func NewService(currentVersion, buildDate, gitCommit string) *Service {
	return &Service{
		CurrentVersion: currentVersion,
		BuildDate:      buildDate,
		GitCommit:      gitCommit,
	}
}

// CheckForUpdates checks if a newer version is available
func (s *Service) CheckForUpdates(options *UpdateOptions) CheckResult {
	// Default options if nil
	if options == nil {
		options = &UpdateOptions{}
	}
	if !updateCheckEnabled {
		// No check, not even a forced one (updateCheckEnabled).
		return CheckResult{CurrentVersion: s.CurrentVersion}
	}

	// For testing purposes, we can force an update to be available
	if options.ForceUpdate {
		return CheckResult{
			CurrentVersion:          s.CurrentVersion,
			LatestVersion:           options.ForcedVersion,
			UpdateAvailable:         true,
			ReleaseURL:              "https://github.com/abbott/hardn/releases/latest",
			InstallURL:              "curl -fsSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh",
			SecurityUpdateAvailable: options.ForceSecurityUpdate,
			SecurityUpdateDetails:   options.SecurityDetails,
		}
	}

	// Set up environment variables for the underlying check function
	if options.Debug {
		os.Setenv("HARDN_DEBUG", "1")
		defer os.Unsetenv("HARDN_DEBUG")
	}

	if options.ClearCache {
		os.Setenv("HARDN_CLEAR_CACHE", "1")
		defer os.Unsetenv("HARDN_CLEAR_CACHE")
	}

	if options.CacheFilePath != "" {
		os.Setenv("HARDN_CACHE_PATH", options.CacheFilePath)
		defer os.Unsetenv("HARDN_CACHE_PATH")
	}

	// Perform the actual check
	return CheckForUpdates(s.CurrentVersion, options.Debug)
}

// PrintVersionInfo writes hardn --version's lines to w: the version, the
// build date and the commit, from the ldflags (main.Version,
// main.BuildDate, main.GitCommit), as v0.3.2 printed them, each "unknown"
// when the build did not set it (refactor plan §3.11, approved
// 2026-11-04: the build info restored), then the license and that it
// carries no warranty (the LICENSE's sections 15 and 16).
func (s *Service) PrintVersionInfo(w io.Writer) error {
	known := func(v string) string {
		if v == "" {
			return "unknown"
		}
		return v
	}
	_, err := fmt.Fprintf(w, "hardn - Linux hardening tool\nVersion:    %s\nBuild Date: %s\nGit Commit: %s\nLicense:    GNU AGPL v3, no warranty\n",
		known(s.CurrentVersion), known(s.BuildDate), known(s.GitCommit))
	return err
}

// GetCacheStatus returns information about the update cache
func (s *Service) GetCacheStatus() (bool, time.Time, error) {
	if !updateCheckEnabled {
		return false, time.Time{}, fmt.Errorf("update checks are disabled")
	}
	cache, valid := loadCache()
	if !valid {
		return false, time.Time{}, fmt.Errorf("no valid cache found")
	}
	return true, cache.LastCheck, nil
}

// CheckForSecurityUpdates checks specifically for security-related updates
func (s *Service) CheckForSecurityUpdates(options *UpdateOptions) (bool, string, error) {
	// Use existing update check mechanism
	result := s.CheckForUpdates(options)

	if result.Error != nil {
		return false, "", result.Error
	}

	return result.SecurityUpdateAvailable, result.SecurityUpdateDetails, nil
}
