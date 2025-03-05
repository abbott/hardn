package utils

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/logging"
)

// SetupSudoEnvPreservation configures sudoers to preserve the HARDN_CONFIG environment variable
func SetupSudoEnvPreservation() error {
	// Check if running as root
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must be run with sudo privileges")
	}

	// Get current username (the real user, not root)
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		// Fallback if SUDO_USER is not set
		currentUser, err := user.Current()
		if err != nil {
			return fmt.Errorf("failed to determine current user: %w", err)
		}

		// If we're still root and can't determine the real user, error out
		if currentUser.Username == "root" {
			return fmt.Errorf("cannot determine the real username; please run with sudo from a regular user account")
		}

		sudoUser = currentUser.Username
	}

	logging.LogInfo("Setting up sudo environment preservation for user: %s", sudoUser)

	// Ensure sudoers.d directory exists
	sudoersDir := "/etc/sudoers.d"
	if _, err := os.Stat(sudoersDir); os.IsNotExist(err) {
		return fmt.Errorf("sudoers.d directory does not exist; your system may not support sudo drop-in configurations: %w", err)
	}

	// Create/modify sudoers file for the user
	sudoersFile := filepath.Join(sudoersDir, sudoUser)

	// Check if file already exists
	var content string
	if _, err := os.Stat(sudoersFile); err == nil {
		// Read existing content
		data, err := os.ReadFile(sudoersFile)
		if err != nil {
			return fmt.Errorf("failed to read existing sudoers file %s: %w", sudoersFile, err)
		}
		content = string(data)

		// Check if HARDN_CONFIG is already in the file
		if strings.Contains(content, "env_keep += \"HARDN_CONFIG\"") {
			logging.LogInfo("HARDN_CONFIG is already preserved in your sudoers configuration")
			return nil
		}

		// Append to existing content
		content = strings.TrimSpace(content) + "\n"
	}

	// env_keep directive
	content += fmt.Sprintf("Defaults:%s env_keep += \"HARDN_CONFIG\"\n", sudoUser)

	// Create a temporary file for validation
	tempFile := filepath.Join(os.TempDir(), "hardn_sudoers_temp")
	if err := os.WriteFile(tempFile, []byte(content), 0440); err != nil {
		return fmt.Errorf("failed to create temporary sudoers file at %s: %w", tempFile, err)
	}
	defer os.Remove(tempFile)

	// Validate the sudoers file
	cmd := exec.Command("visudo", "-c", "-f", tempFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("invalid sudoers configuration: %w\nOutput: %s", err, string(output))
	}

	// Write the validated content to the actual sudoers file
	if err := os.WriteFile(sudoersFile, []byte(content), 0440); err != nil {
		return fmt.Errorf("failed to write sudoers file %s: %w", sudoersFile, err)
	}

	logging.LogSuccess("Successfully configured sudo to preserve HARDN_CONFIG environment variable for user: %s", sudoUser)
	logging.LogInfo("You can now set HARDN_CONFIG environment variable and it will be preserved when using sudo")
	return nil
}