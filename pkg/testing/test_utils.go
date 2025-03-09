package testing

import (
	"os"
	"strings"
)

// IsCI returns true if running in a CI environment
func IsCI() bool {
	// Check common CI environment variables
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return true
	}

	// Check if hostname contains CI-specific patterns
	hostname, err := os.Hostname()
	if err == nil {
		// GitHub Actions runners typically have hostnames like "fv-az..."
		if strings.HasPrefix(hostname, "fv-az") {
			return true
		}
	}

	return false
}

// GetTestHostname returns the hostname to use in tests
func GetTestHostname() string {
	// Try to get hostname from environment variable first
	if hostname := os.Getenv("TEST_HOSTNAME_OVERRIDE"); hostname != "" {
		return hostname
	}

	// Fall back to a default test hostname
	return "testhost"
}
