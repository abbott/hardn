package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/ui/prompt"
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
	// ConfirmTimeout is the confirm window of a lockout-risk change
	// (decision §3.14): whole seconds ("180") or a duration ("3m"), read
	// with revert.ParseTimeout, at least revert.MinTimeout; "" is the
	// default, 180 s. The --confirm-timeout flag wins over it
	// (revert.ResolveTimeout; engine.ConfirmTimeout). Not written when
	// unset, so a legacy save writes no new key.
	ConfirmTimeout string `yaml:"confirmTimeout,omitempty"`

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
	DisableRootSSH           bool `yaml:"disableRootSSH"`

	// Localization
	Lang             string `yaml:"lang"`
	Language         string `yaml:"language"`
	LcAll            string `yaml:"lcAll"`
	Tz               string `yaml:"tz"`
	PythonUnbuffered string `yaml:"pythonUnbuffered"`

	// Logs Configuration (embedded for easy access to LogFile)
	LogsConfig struct {
		LogFilePath string
	}

	// loaded is what the YAML said beyond the fields: permitRootLogin's
	// string value and the deprecated forms read, and the path LoadConfig
	// read (rootlogin.go).
	loaded loaded
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
		SshConfigFile:    "/etc/ssh/sshd_config.d/hardn.conf",

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
		DisableRootSSH:           false,

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
	return defaultSearchPath()
}

// defaultSearchPath is where hardn looks for its configuration when neither
// -f nor HARDN_CONFIG names it, in order: system-wide, the two home files,
// the working directory. FindConfigFile and FileDryRun (through
// ConfigFileSearchPath) both walk it, so they find the same file.
func defaultSearchPath() []string {
	searchPaths := []string{
		"/etc/hardn/hardn.yml", // System-wide config
	}

	homeDir, err := os.UserHomeDir()
	if err == nil {
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".config/hardn/hardn.yml"))
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".hardn.yml"))
	}
	return append(searchPaths, "./hardn.yml")
}

// Direct replacement for the FindConfigFile function in pkg/config/config.go
// This ensures environment variables have the highest priority

func FindConfigFile(explicitPath string) (string, bool) {
	// Log environment variable for debugging
	envPath := os.Getenv("HARDN_CONFIG")
	// if envPath != "" {
	// 	logging.LogInfo("HARDN_CONFIG environment variable is set to: %s", envPath)
	// }

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
	for _, path := range defaultSearchPath() {
		if _, err := os.Stat(path); err == nil {
			logging.LogInfo("Using configuration from: %s", path)
			return path, true
		}
	}

	// No configuration file found
	logging.LogInfo("No configuration file found in any location")
	return "", false
}

// helper function to use with LoadConfig
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
			config.setPath(path)
			if data, err := os.ReadFile(path); err == nil {
				config.setLoaded(path, data) // what was created, for LoadedFrom
			}
			return config, nil
		}

		// If we're not creating a default config, just return the default
		logging.LogInfo("Using default configuration (no config file found)")
		return config, nil
	}

	return readConfigFile(configPath)
}

// readConfigFile reads the configuration file at path as LoadConfig reads
// the file it finds: the defaults, then the file's keys, and the path and
// bytes read recorded (setLoaded).
func readConfigFile(path string) (*Config, error) {
	config := DefaultConfig()

	// Read the found config file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML in config file %s: %w", path, err)
	}
	if err := checkLoaded(config); err != nil {
		return nil, fmt.Errorf("config file %s: %w", path, err)
	}
	config.setLoaded(path, data)

	return config, nil
}

// LoadConfig loads hardn's configuration as LoadConfigWithEnvPriority
// finds it (-f, HARDN_CONFIG, then the search path), its LogsConfig set
// from its logFile. It runs nothing: origin/main's su - probe of the sudo
// user's login shell for HARDN_CONFIG, and the notice after it, are gone
// (refactor plan slice 9, unit (d), decision 4; side-effects row 5): the
// Environment screen's state row says whether sudo, or doas, keeps
// HARDN_CONFIG, read from hardn's files (pkg/screens/environment).
func LoadConfig(filePath string) (*Config, error) {
	cfg, err := LoadConfigWithEnvPriority(filePath)
	if err != nil {
		return nil, err
	}

	// Initialize LogsConfig
	cfg.LogsConfig.LogFilePath = cfg.LogFile

	return cfg, nil
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
	// On a terminal, discard what was typed before the question shows
	// (brief 1C). It can follow a long step: LoadConfig runs mid-session
	// too (getConfigForSSHKeyPath in pkg/adapter/secondary, from the main
	// menu's status and the user screens) and asks again while no file
	// exists. The questions after it are asked back to back, so
	// readAnswer keeps what is typed ahead of them.
	_ = input().Flush() // a terminal that cannot be flushed keeps its typeahead
	fmt.Println("No configuration file found. Would you like to create one? [Y/n]")
	response := readAnswer()
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
		// Ask for username
		fmt.Print("Enter default username [george]: ")
		username := readAnswer()
		username = strings.TrimSpace(username)
		if username != "" {
			config.Username = username
		} else {
			config.Username = "george" // Default if nothing provided
		}

		// Ask for SSH port
		fmt.Printf("Enter SSH port [%d]: ", config.SshPort)
		portStr := readAnswer()
		portStr = strings.TrimSpace(portStr)
		if portStr != "" {
			var port int
			if _, err := fmt.Sscanf(portStr, "%d", &port); err == nil {
				config.SshPort = port
			}
		}

		// Ask for automatic updates
		fmt.Print("Enable automatic security updates? [y/N]: ")
		autoUpdates := readAnswer()
		autoUpdates = strings.TrimSpace(strings.ToLower(autoUpdates))
		config.EnableUnattendedUpgrades = (autoUpdates == "y" || autoUpdates == "yes")

		// Ask for SSH key
		fmt.Print("Add SSH public key (optional, press Enter to skip): ")
		sshKey := readAnswer()
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

// SaveConfig saves configuration to the specified file: the whole
// configuration, with dryRun as loaded, never the session's mode (slice 2b
// decision 8), so that -n, --dry-run=false and a toggle inside the session
// are never saved; only an edit of the file sets it.
func SaveConfig(config *Config, filePath string) error {
	saved := *config
	saved.DryRun = config.loaded.dryRun

	// Marshal YAML
	data, err := yaml.Marshal(&saved)
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

var (
	// input returns the Reader the prompts above read from: the
	// process-wide prompt.Stdin, which pkg/menu shares, so stdin has one
	// buffer (refactor plan risk R17). Tests replace it.
	input = prompt.Stdin
	// exit ends hardn. Tests replace it.
	exit = os.Exit
	// reraise ends hardn by SIGINT after Ctrl-C. Tests replace it.
	reraise = prompt.Reraise
)

// readAnswer reads one answer line through input. As with the bufio
// reader each prompt made before, the end of the input and a read error
// read as an empty answer, so the default applies, and nothing more is
// written. Nor does it discard typeahead, as the bufio reader did not:
// CreateDefaultConfig's questions follow one another with no long step
// between them, so an answer typed or pasted ahead is read by the
// question it was meant for (ShouldCreateDefaultConfig discards what was
// typed before the first question). Ctrl-C ends hardn as Ctrl-C ends any
// program, by SIGINT (prompt.Reraise), as the bufio reader's Ctrl-C did:
// a waiting parent sees it killed by the signal, so a shell running hardn
// in a loop or a script stops, and reports status 130 for it (brief
// Phase 3). If SIGINT cannot end it, it exits with 130.
func readAnswer() string {
	line, err := input().ReadLine()
	if errors.Is(err, prompt.ErrInterrupted) {
		reraise()
		exit(prompt.ExitInterrupted)
	}
	return line
}
