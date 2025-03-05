package config

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/abbott/hardn/pkg/logging"
)

// UfwAppProfile represents a UFW application profile
type UfwAppProfile struct {
	Name        string   `yaml:"name"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Ports       []string `yaml:"ports"`
}

// Config represents the main configuration structure
type Config struct {
	// Basic Configuration
	Username      string `yaml:"username"`
	LogFile       string `yaml:"logFile"`
	DryRun        bool   `yaml:"dryRun"`
	EnableBackups bool   `yaml:"enableBackups"`
	BackupPath    string `yaml:"backupPath"`

	// Network Configuration
	DmzSubnet   string   `yaml:"dmzSubnet"`
	Nameservers []string `yaml:"nameservers"`

	// SSH Configuration
	SshPort          int      `yaml:"sshPort"`
	PermitRootLogin  bool     `yaml:"permitRootLogin"`
	SshAllowedUsers  []string `yaml:"sshAllowedUsers"`
	SshListenAddress string   `yaml:"sshListenAddress"`
	SshKeyPath       string   `yaml:"sshKeyPath"`
	SshConfigFile    string   `yaml:"sshConfigFile"`

	// User Configuration
	SudoNoPassword bool     `yaml:"sudoNoPassword"`
	SshKeys        []string `yaml:"sshKeys"`

	// Package Configuration
	LinuxCorePackages    []string `yaml:"linuxCorePackages"`
	LinuxDmzPackages     []string `yaml:"linuxDmzPackages"`
	LinuxLabPackages     []string `yaml:"linuxLabPackages"`
	PythonPackages       []string `yaml:"pythonPackages"`
	NonWslPythonPackages []string `yaml:"nonWslPythonPackages"`
	PythonPipPackages    []string `yaml:"pythonPipPackages"`
	AlpineCorePackages   []string `yaml:"alpineCorePackages"`
	AlpineDmzPackages    []string `yaml:"alpineDmzPackages"`
	AlpineLabPackages    []string `yaml:"alpineLabPackages"`
	AlpinePythonPackages []string `yaml:"alpinePythonPackages"`

	// Repository Configuration
	DebianRepos            []string `yaml:"debianRepos"`
	ProxmoxSrcRepos        []string `yaml:"proxmoxSrcRepos"`
	ProxmoxCephRepo        []string `yaml:"proxmoxCephRepo"`
	ProxmoxEnterpriseRepo  []string `yaml:"proxmoxEnterpriseRepo"`
	ProxmoxPackagePatterns []string `yaml:"proxmoxPackagePatterns"`
	AlpineTestingRepo      bool     `yaml:"alpineTestingRepo"`

	// Firewall Configuration
	// UfwAppProfiles represents UFW application profiles
	UfwAppProfiles           []UfwAppProfile `yaml:"ufwAppProfiles"`
	UfwDefaultIncomingPolicy string          `yaml:"ufwDefaultIncomingPolicy"`
	UfwDefaultOutgoingPolicy string          `yaml:"ufwDefaultOutgoingPolicy"`
	UfwAllowedPorts          []int           `yaml:"ufwAllowedPorts"`

	// Feature Toggles
	UseUvPackageManager      bool `yaml:"useUvPackageManager"`
	EnableAppArmor           bool `yaml:"enableAppArmor"`
	EnableLynis              bool `yaml:"enableLynis"`
	EnableUnattendedUpgrades bool `yaml:"enableUnattendedUpgrades"`
	EnableUfwSshPolicy       bool `yaml:"enableUfwSshPolicy"`
	ConfigureDns             bool `yaml:"configureDns"`
	DisableRoot              bool `yaml:"disableRoot"`

	// Localization
	Lang             string `yaml:"lang"`
	Language         string `yaml:"language"`
	LcAll            string `yaml:"lcAll"`
	Tz               string `yaml:"tz"`
	PythonUnbuffered string `yaml:"pythonUnbuffered"`
}

// Default configuration
func DefaultConfig() *Config {
	return &Config{
		// Basic Configuration
		// Username:      "george",
		LogFile:       "/var/log/hardn.log",
		DryRun:        false,
		EnableBackups: true,
		BackupPath:    "/var/backups/hardn",

		// Network Configuration
		// DmzSubnet:   "192.168.4",
		// Nameservers: []string{"1.1.1.1", "1.0.0.1"},

		// SSH Configuration
		SshPort:         22,
		PermitRootLogin: false,
		// SshAllowedUsers:  []string{"george"},
		SshListenAddress: "0.0.0.0",
		SshKeyPath:       ".ssh_%u",
		SshConfigFile:    "/etc/ssh/sshd_config.d/manage.conf",

		// User Configuration
		SudoNoPassword: true,
		SshKeys:        []string{},

		// Firewall Configuration
		UfwAppProfiles: []UfwAppProfile{},
		// UfwDefaultIncomingPolicy: "deny",
		// UfwDefaultOutgoingPolicy: "allow",
		// UfwAllowedPorts:          []int{22},

		// Feature Toggles
		UseUvPackageManager:      false,
		EnableAppArmor:           false,
		EnableLynis:              false,
		EnableUnattendedUpgrades: false,
		EnableUfwSshPolicy:       false,
		ConfigureDns:             false,
		DisableRoot:              false,

		// Localization
		// Lang:             "en_US.UTF-8",
		// Language:         "en_US:en",
		// LcAll:            "en_US.UTF-8",
		// Tz:               "America/New_York",
		PythonUnbuffered: "1",

		// Package configuration with common defaults
		LinuxCorePackages:  []string{},
		LinuxDmzPackages:   []string{},
		LinuxLabPackages:   []string{},
		AlpineCorePackages: []string{},
		AlpineDmzPackages:  []string{},
		AlpineLabPackages:  []string{},
		// LinuxCorePackages:        []string{"apt-transport-https", "dstat", "gawk", "git", "jq", "htop", "iputils-clockdiff", "sed", "strace", "sudo", "sysstat"},
		// LinuxDmzPackages:         []string{"dnsutils", "fail2ban", "nethogs"},
		// LinuxLabPackages:         []string{"aria2", "arping", "fping", "iperf3", "lshw",  "mosh", "net-tools", "tree"},
		// AlpineCorePackages:       []string{"bash", "openssh", "shadow", "sudo", "ca-certificates"},
		// AlpineDmzPackages:        []string{"bind-tools", "fail2ban", "git", "htop", "jq", "sudo"},
		// AlpineLabPackages:        []string{"iperf3", "mosh", "net-tools", "tree"},
	}
}

// ConfigFileSearchPath returns an ordered list of paths to search for the config file
// Modifications for pkg/config/config.go

// Update the ConfigFileSearchPath function to be more explicit about priority
func ConfigFileSearchPath(explicitPath string) []string {
	// If an explicit path is provided via command line, that takes precedence
	if explicitPath != "" {
		return []string{explicitPath}
	}

	// Check for environment variable - this should have second highest priority
	envPath := os.Getenv("HARDN_CONFIG")
	if envPath != "" {
		// Return only this path - no fallback if using environment variable
		return []string{envPath}
	}

	// If no explicit path or environment variable, use default search paths
	searchPaths := []string{
		"/etc/hardn/hardn.yml", // System-wide config
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".config/hardn/hardn.yml"))
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".hardn.yml"))
	}
	searchPaths = append(searchPaths, "./hardn.yml")

	return searchPaths
}

// Direct replacement for the FindConfigFile function in pkg/config/config.go
// This ensures environment variables have the highest priority

func FindConfigFile(explicitPath string) (string, bool) {
	// Log environment variable for debugging
	envPath := os.Getenv("HARDN_CONFIG")
	if envPath != "" {
		logging.LogInfo("HARDN_CONFIG environment variable is set to: %s", envPath)
	}

	// First priority: explicit path from command line
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			logging.LogInfo("Using configuration from command-line flag: %s", explicitPath)
			return explicitPath, true
		}
		logging.LogError("Configuration file specified by command-line flag not found: %s", explicitPath)
		return "", false // Don't fall back if explicit path is specified but doesn't exist
	}

	// Second priority: environment variable
	if envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			logging.LogInfo("Using configuration from HARDN_CONFIG environment variable: %s", envPath)
			return envPath, true
		}
		logging.LogError("Configuration file specified by HARDN_CONFIG environment variable not found: %s", envPath)
		return "", false // Don't fall back if env var is specified but doesn't exist
	}

	// Third priority: default search paths
	searchPaths := []string{
		"/etc/hardn/hardn.yml", // System-wide config
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".config/hardn/hardn.yml"))
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".hardn.yml"))
	}

	searchPaths = append(searchPaths, "./hardn.yml")

	// Search through default paths
	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			logging.LogInfo("Using configuration from: %s", path)
			return path, true
		}
	}

	// No configuration file found
	logging.LogInfo("No configuration file found in any location")
	return "", false
}

// Additional helper function to use with LoadConfig
func LoadConfigWithEnvPriority(filePath string) (*Config, error) {
	// Start with default config
	config := DefaultConfig()

	// Find config file with proper priority
	configPath, found := FindConfigFile(filePath)

	if !found {
		// No config file found, check if we should create one
		if ShouldCreateDefaultConfig() {
			path := GetDefaultConfigLocation()
			if err := CreateDefaultConfig(path, config); err != nil {
				return nil, fmt.Errorf("failed to create default config at %s: %w", path, err)
			}
			return config, nil
		}

		// If we're not creating a default config, just return the default
		logging.LogInfo("Using default configuration (no config file found)")
		return config, nil
	}

	// Read the found config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML in config file %s: %w", configPath, err)
	}

	return config, nil
}

// DetectEnvVarLoss checks if the HARDN_CONFIG environment variable
// is present in the original environment but lost in the sudo environment
func DetectEnvVarLoss() bool {
	// Check if we're running under sudo
	sudoUID := os.Getenv("SUDO_UID")
	if sudoUID == "" {
		// Not running under sudo
		return false
	}

	// Check if HARDN_CONFIG is in the current environment
	if os.Getenv("HARDN_CONFIG") != "" {
		// We have the variable, no loss detected
		return false
	}

	// Check if the variable was in the original environment
	// This requires the SUDO_USER environment variable to be set
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		return false
	}

	// Try to check the user's environment
	// This is a simplified approach - a complete solution would
	// need to parse the user's shell profile which is complex
	cmd := exec.Command("su", "-", sudoUser, "-c", "echo $HARDN_CONFIG")
	output, err := cmd.Output()
	if err != nil {
		// Couldn't check, assume no loss
		return false
	}

	// If we get a non-empty value, the variable exists in the user's environment
	return len(strings.TrimSpace(string(output))) > 0
}

// Replace the LoadConfig function with this implementation
func LoadConfig(filePath string) (*Config, error) {
	// Check for environment variable loss
	if DetectEnvVarLoss() {
		fmt.Println("\nNOTICE: The HARDN_CONFIG environment variable is set in your user environment")
		fmt.Println("but is not preserved when using sudo. To fix this, run:")
		fmt.Println("  sudo hardn setup-sudo-env")
		fmt.Println("Then run your command again.")
		fmt.Println()
	}

	return LoadConfigWithEnvPriority(filePath)
}

// GetDefaultConfigLocation returns the appropriate location for a new config file
// based on whether the user is root or not
func GetDefaultConfigLocation() string {
	// Check if running as root
	if os.Geteuid() == 0 {
		// Create in system location if root
		return "/etc/hardn/hardn.yml"
	}

	// Otherwise, create in user's home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to current directory if can't determine home
		return "./hardn.yml"
	}

	// Use XDG config directory
	configDir := filepath.Join(homeDir, ".config/hardn")
	return filepath.Join(configDir, "hardn.yml")
}

// ShouldCreateDefaultConfig determines if we should offer to create a default config
func ShouldCreateDefaultConfig() bool {
	fmt.Println("No configuration file found. Would you like to create one? [Y/n]")
	reader := bufio.NewReader(os.Stdin)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))

	return response == "" || response == "y" || response == "yes"
}

// CreateDefaultConfig creates a default configuration file at the specified path
// with optional interactive configuration
func CreateDefaultConfig(path string, config *Config) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory for config file at %s: %w", dir, err)
		}
	}

	// Ask for basic configuration if interactive
	if isInteractive() {
		reader := bufio.NewReader(os.Stdin)

		// Ask for username
		fmt.Print("Enter default username [george]: ")
		username, _ := reader.ReadString('\n')
		username = strings.TrimSpace(username)
		if username != "" {
			config.Username = username
		} else {
			config.Username = "george" // Default if nothing provided
		}

		// Ask for SSH port
		fmt.Printf("Enter SSH port [%d]: ", config.SshPort)
		portStr, _ := reader.ReadString('\n')
		portStr = strings.TrimSpace(portStr)
		if portStr != "" {
			var port int
			if _, err := fmt.Sscanf(portStr, "%d", &port); err == nil {
				config.SshPort = port
			}
		}

		// Ask for automatic updates
		fmt.Print("Enable automatic security updates? [y/N]: ")
		autoUpdates, _ := reader.ReadString('\n')
		autoUpdates = strings.TrimSpace(strings.ToLower(autoUpdates))
		config.EnableUnattendedUpgrades = (autoUpdates == "y" || autoUpdates == "yes")

		// Ask for SSH key
		fmt.Print("Add SSH public key (optional, press Enter to skip): ")
		sshKey, _ := reader.ReadString('\n')
		sshKey = strings.TrimSpace(sshKey)
		if sshKey != "" {
			config.SshKeys = []string{sshKey}
		}
	}

	// Save config
	if err := SaveConfig(config, path); err != nil {
		return fmt.Errorf("failed to save default config to %s: %w", path, err)
	}

	fmt.Printf("Created configuration file at %s\n", path)

	// Ensure the example config exists and tell the user about it
	examplePath := "/etc/hardn/hardn.yml.example"
	if err := EnsureExampleConfigExists(); err == nil {
		fmt.Printf("A complete example configuration with all options is available at %s\n", examplePath)
	}

	return nil
}

// SaveConfig saves configuration to the specified file
func SaveConfig(config *Config, filePath string) error {
	// Marshal YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s for config: %w", dir, err)
		}
	}

	// Write file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file to %s: %w", filePath, err)
	}

	return nil
}

// isInteractive checks if we're running in an interactive terminal
func isInteractive() bool {
	fileInfo, _ := os.Stdin.Stat()
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}