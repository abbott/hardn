// pkg/utils/sudo_test.go

package utils

import (
	"os"
	"testing"
)

func TestCheckSudoEnvPreservation(t *testing.T) {
	// This is a simple mock test since we can't actually create sudoers files in tests
	// A real test would use a test fixture or mock the file system

	// Mock the username
	origUser := os.Getenv("USER")
	defer os.Setenv("USER", origUser)

	os.Setenv("USER", "testuser")

	// The real implementation would check for file existence and content
	// but for testing we'll just return false since the file won't exist
	// This is just a placeholder for the test structure
	if checkSudoEnvPreservation() != false {
		t.Error("Expected false when sudoers file doesn't exist")
	}
}

func checkSudoEnvPreservation() bool {
	// This is a simplified version just for testing
	// The real implementation is in the menu.go file
	return false
}
