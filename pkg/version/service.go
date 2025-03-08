package version

import (
	"fmt"
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

	// For testing purposes, we can force an update to be available
	if options.ForceUpdate {
		return CheckResult{
			CurrentVersion:  s.CurrentVersion,
			LatestVersion:   options.ForcedVersion,
			UpdateAvailable: true,
			ReleaseURL:      "https://github.com/abbott/hardn/releases/latest",
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

// PrintVersionInfo prints version information to stdout
func (s *Service) PrintVersionInfo() {
	fmt.Println("hardn - Linux hardening tool")
	fmt.Printf("Version:    %s\n", s.CurrentVersion)
	if s.BuildDate != "" {
		fmt.Printf("Build Date: %s\n", s.BuildDate)
	}
	if s.GitCommit != "" {
		fmt.Printf("Git Commit: %s\n", s.GitCommit)
	}
}

// GetCacheStatus returns information about the update cache
func (s *Service) GetCacheStatus() (bool, time.Time, error) {
	cache, valid := loadCache()
	if !valid {
		return false, time.Time{}, fmt.Errorf("no valid cache found")
	}
	return true, cache.LastCheck, nil
}
