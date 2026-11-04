// pkg/config/embed.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ExampleConfigContent contains the example configuration file content
// This allows the example config to be embedded in the binary and written
// to the filesystem when necessary
const ExampleConfigContent = `# Hardn - Linux Hardening Configuration

#################################################
# Basic Configuration
#################################################
username: ""                      # The user -c creates, and User Management's and Run All's default; none by default
dryRun: false                     # Preview changes without applying them
enableBackups: true               # Backup files before modifying them
backupPath: "/var/backups/hardn"  # Path to store backups

#################################################
# Network Configuration
#################################################
nameservers:                      # DNS servers to configure
  - "1.1.1.1"
  - "1.0.0.1"

#################################################
# SSH Configuration
#################################################
sshPort: 22                       # SSH port (this is the authoritative SSH port used throughout the configuration)
                                  # Consider using a non-standard port (e.g., 2208) as a security measure

permitRootLogin: "no"             # Root SSH login: "no" or "yes"
                                  # (true and false still work but are deprecated: write "no" or "yes")
sshConfigFile: "/etc/ssh/sshd_config.d/hardn.conf"  # SSH config file location

#################################################
# User Configuration
#################################################
sshKeys:                          # SSH public keys to add for created users
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... george@example.com"
  # Add more keys as needed

#################################################
# Firewall Configuration
#################################################
# UFW application profiles - these will be written to /etc/ufw/applications.d/hardn
ufwAppProfiles:
  - name: LabHTTPS
    title: Lab Web Server (HTTPS)
    description: Lab Web server secure port
    ports:
      - "30443/tcp" # non-standard 443

#################################################
# Feature Toggles
#################################################
enableUfwSshPolicy: false         # Configure UFW with SSH rules
configureDns: false               # Configure DNS settings
disableRootSSH: false             # Disable root SSH access
`

// EnsureExampleConfigExists checks if the example configuration file exists
// and creates it if it doesn't. This function is called during initialization.
func EnsureExampleConfigExists() error {
	exampleConfigPath := "/etc/hardn/hardn.yml.example"

	// Check if the example config file already exists
	if _, err := os.Stat(exampleConfigPath); err == nil {
		// File exists, no need to create it
		return nil
	}

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(exampleConfigPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory for example config: %w", err)
	}

	// Write the example config file
	if err := os.WriteFile(exampleConfigPath, []byte(ExampleConfigContent), 0644); err != nil {
		return fmt.Errorf("failed to write example config file: %w", err)
	}

	return nil
}
