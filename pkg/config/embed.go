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
username: "george"                # Default username to create. (See: 'sshAllowedUsers')
logFile: "/var/log/hardn.log"     # Log file path
dryRun: false                     # Preview changes without applying them
enableBackups: true               # Backup files before modifying them
backupPath: "/var/backups/hardn"  # Path to store backups

#################################################
# Network Configuration
#################################################
dmzSubnet: "192.168.4"            # DMZ subnet for conditional package installation
nameservers:                      # DNS servers to configure
  - "1.1.1.1"
  - "1.0.0.1"

#################################################
# SSH Configuration
#################################################
sshPort: 22                       # SSH port (this is the authoritative SSH port used throughout the configuration)
                                  # Consider using a non-standard port (e.g., 2208) as a security measure

permitRootLogin: false            # Allow or deny root SSH access
sshAllowedUsers:                  # List of users allowed to access via SSH
  - "george"
sshListenAddress: "0.0.0.0"       # IP address to listen on
sshKeyPath: ".ssh_%u"             # Path to SSH keys (use %u for username substitution)
sshConfigFile: "/etc/ssh/sshd_config.d/hardn.conf"  # SSH config file location

#################################################
# User Configuration
#################################################
sudoNoPassword: true              # Whether to allow sudo without password
sshKeys:                          # SSH public keys to add for created users
  - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... george@example.com"
  # Add more keys as needed

#################################################
# Package Configuration
#################################################
# Debian/Ubuntu Packages

# linuxCorePackages: []         
linuxCorePackages:                # Always installed packages
  - "apt-transport-https"
  - "dstat"
  - "gawk"
  - "git"
  - "jq"
  - "htop"
  - "iputils-clockdiff"
  - "sed"
  - "strace"
  - "sudo"
  - "sysstat"

linuxDmzPackages:                 # Packages for DMZ environments 
  - "dnsutils"
  - "fail2ban"
  - "nethogs"

linuxLabPackages:                 # Additional packages for non-DMZ environments
  - "aria2"
  - "arping"
  - "fping"
  - "iperf3"
  - "mosh"
  - "net-tools"
  - "tree"

# Python packages (Debian/Ubuntu)
pythonPackages:                   # System python packages
  - "python3-dev"
  - "python3-pip"

nonWslPythonPackages:             # Python packages not for WSL environments
  - "build-essential"

pythonPipPackages:                # Python packages to install via pip
  - "pytest"
  - "requests"

# Alpine Packages
alpineCorePackages:               # Core Alpine packages
  - "bash"
  - "openssh"
  - "shadow"
  - "sudo"
  - "ca-certificates"

alpineDmzPackages:                # Alpine packages for DMZ environments
  - "bind-tools"
  - "fail2ban"
  - "git"
  - "htop"
  - "jq"
  - "sudo"

alpineLabPackages:                # Additional Alpine packages for non-DMZ environments
  - "iperf3"
  - "mosh"
  - "net-tools"
  - "tree"

alpinePythonPackages:             # Alpine Python packages
  - "python3-dev"
  - "py3-pip"

#################################################
# Repository Configuration
#################################################
# Replace CODENAME with the OS codename (e.g., bullseye, focal)
debianRepos:                      # Debian/Ubuntu repositories
  - "deb http://deb.debian.org/debian CODENAME main contrib non-free"
  - "deb http://security.debian.org/debian-security CODENAME-security main contrib non-free"
  - "deb http://deb.debian.org/debian CODENAME-updates main contrib non-free"

# Proxmox specific repositories
proxmoxSrcRepos:                  # Proxmox source repositories
  - "deb http://ftp.us.debian.org/debian CODENAME main contrib"
  - "deb http://ftp.us.debian.org/debian CODENAME-updates main contrib"
  - "deb http://security.debian.org CODENAME-security main contrib"
  - "deb http://download.proxmox.com/debian/pve CODENAME pve-no-subscription"

proxmoxCephRepo:                  # Proxmox Ceph repository
  - "#deb https://enterprise.proxmox.com/debian/ceph-quincy CODENAME enterprise"
  - "deb http://download.proxmox.com/debian/ceph-reef CODENAME no-subscription"

proxmoxEnterpriseRepo:            # Proxmox Enterprise repository
  - "#deb https://enterprise.proxmox.com/debian/pve CODENAME pve-enterprise"

proxmoxPackagePatterns:           # Package patterns to protect during operations
  - "proxmox-archive-keyring"
  - "proxmox-backup-client"
  - "proxmox-ve"
  - "pve-kernel"

# Alpine specific configuration
alpineTestingRepo: false          # Whether to enable Alpine testing repository

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
useUvPackageManager: false        # Use UV instead of pip for Python packages
enableAppArmor: false             # Set up and enable AppArmor
enableLynis: false                # Install and run Lynis security audit
enableUnattendedUpgrades: false   # Configure automatic security updates
enableUfwSshPolicy: false         # Configure UFW with SSH rules
configureDns: false               # Configure DNS settings
disableRoot: false                # Disable root SSH access

#################################################
# Localization
#################################################
lang: "en_US.UTF-8"               # System locale
language: "en_US:en"              # System language
lcAll: "en_US.UTF-8"              # Locale for all categories
tz: "America/New_York"            # Timezone
pythonUnbuffered: "1"             # Python unbuffered mode
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
