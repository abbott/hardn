package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

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
	UfwDefaultIncomingPolicy string `yaml:"ufwDefaultIncomingPolicy"`
	UfwDefaultOutgoingPolicy string `yaml:"ufwDefaultOutgoingPolicy"`
	UfwAllowedPorts          []int  `yaml:"ufwAllowedPorts"`

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
		Username:      "sysadmin",
		LogFile:       "/var/log/hardn.log",
		DryRun:        false,
		EnableBackups: true,
		BackupPath:    "/var/backups/hardn",

		// Network Configuration
		DmzSubnet:   "192.168.4",
		Nameservers: []string{"1.1.1.1", "1.0.0.1"},

		// SSH Configuration
		SshPort:          2208,
		PermitRootLogin:  false,
		SshAllowedUsers:  []string{"sysadmin"},
		SshListenAddress: "0.0.0.0",
		SshKeyPath:       ".ssh_%u",
		SshConfigFile:    "/etc/ssh/sshd_config.d/manage.conf",

		// User Configuration
		SudoNoPassword: true,
		SshKeys:        []string{},

		// Firewall Configuration
		UfwDefaultIncomingPolicy: "deny",
		UfwDefaultOutgoingPolicy: "allow",
		UfwAllowedPorts:          []int{2208},

		// Feature Toggles
		UseUvPackageManager:      false,
		EnableAppArmor:           false,
		EnableLynis:              false,
		EnableUnattendedUpgrades: false,
		EnableUfwSshPolicy:       false,
		ConfigureDns:             false,
		DisableRoot:              false,

		// Localization
		Lang:             "en_US.UTF-8",
		Language:         "en_US:en",
		LcAll:            "en_US.UTF-8",
		Tz:               "America/New_York",
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
func ConfigFileSearchPath(explicitPath string) []string {
	// If an explicit path is provided via command line, that takes precedence
	if explicitPath != "" {
		return []string{explicitPath}
	}
	
	// Check for environment variable
	envPath := os.Getenv("HARDN_CONFIG")
	if envPath != "" {
		return []string{envPath}
	}
	
	// Initialize the search paths
	searchPaths := []string{
		"/etc/hardn/hardn.yml", // System-wide config (highest priority after explicit paths)
	}
	
	// Add user home directory based paths
	homeDir, err := os.UserHomeDir()
	if err == nil {
		// Add ~/.config/hardn/hardn.yml (XDG Base Directory)
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".config/hardn/hardn.yml"))
		// Add ~/.hardn.yml (traditional dot-file)
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".hardn.yml"))
	}
	
	// Add current directory (lowest priority)
	searchPaths = append(searchPaths, "./hardn.yml")
	
	return searchPaths
}

// FindConfigFile searches for a config file in the standard locations
func FindConfigFile(explicitPath string) (string, bool) {
	searchPaths := ConfigFileSearchPath(explicitPath)
	
	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	
	return "", false
}

// LoadConfig loads configuration from the specified file or searches for it
func LoadConfig(filePath string) (*Config, error) {
	// Start with default config
	config := DefaultConfig()
	
	// Find config file
	configPath, found := FindConfigFile(filePath)
	
	if !found {
		// No config file found, check if we should create one
		if ShouldCreateDefaultConfig() {
			path := GetDefaultConfigLocation()
			if err := CreateDefaultConfig(path, config); err != nil {
				return nil, fmt.Errorf("failed to create default config: %w", err)
			}
			return config, nil
		}
		
		// If we're not creating a default config, just return the default
		return config, nil
	}
	
	// Read the found config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}
	
	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", configPath, err)
	}
	
	return config, nil
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
			return fmt.Errorf("failed to create directory for config file: %w", err)
		}
	}

	// Ask for basic configuration if interactive
	if isInteractive() {
		reader := bufio.NewReader(os.Stdin)

		// Ask for username
		fmt.Print("Enter default username [sysadmin]: ")
		username, _ := reader.ReadString('\n')
		username = strings.TrimSpace(username)
		if username != "" {
			config.Username = username
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
		return fmt.Errorf("failed to save default config: %w", err)
	}

	fmt.Printf("Created configuration file at %s\n", path)
	fmt.Println("To modify more settings, edit this file directly.")

	return nil
}

// SaveConfig saves configuration to the specified file
func SaveConfig(config *Config, filePath string) error {
	// Marshal YAML
	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write file
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// isInteractive checks if we're running in an interactive terminal
func isInteractive() bool {
	fileInfo, _ := os.Stdin.Stat()
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
