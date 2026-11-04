package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// UfwAppProfile represents a UFW application profile
type UfwAppProfile struct {
	Name        string   `yaml:"name"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Ports       []string `yaml:"ports"`
}

// defaultLogFile is the log hardn writes, and logFile's default.
const defaultLogFile = "/var/log/hardn.log"

// Config represents the main configuration structure
type Config struct {
	// Basic Configuration
	Username string `yaml:"username"`
	// LogFile is retired (decision 6 (a)): hardn always writes /var/log/hardn.log, and
	// warns where this names another path. SaveConfig writes it only then.
	LogFile       string `yaml:"logFile,omitempty"`
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
	Nameservers []string `yaml:"nameservers"`

	// SSH Configuration
	SshPort         int    `yaml:"sshPort"`
	PermitRootLogin bool   `yaml:"permitRootLogin"`
	SshConfigFile   string `yaml:"sshConfigFile"`

	// User Configuration
	SudoNoPassword bool     `yaml:"sudoNoPassword"`
	SshKeys        []string `yaml:"sshKeys"`

	// Firewall Configuration
	// UfwAppProfiles represents UFW application profiles
	UfwAppProfiles []UfwAppProfile `yaml:"ufwAppProfiles"`

	// Feature Toggles
	EnableUnattendedUpgrades bool `yaml:"enableUnattendedUpgrades"`
	EnableUfwSshPolicy       bool `yaml:"enableUfwSshPolicy"`
	ConfigureDns             bool `yaml:"configureDns"`
	DisableRootSSH           bool `yaml:"disableRootSSH"`

	// loaded is what the YAML said beyond the fields: permitRootLogin's
	// string value and the deprecated forms read, and the path LoadConfig
	// read (rootlogin.go).
	loaded loaded
}

// Default configuration
func DefaultConfig() *Config {
	return &Config{
		// Basic Configuration: no username (refactor plan §3.11)
		LogFile:       defaultLogFile,
		DryRun:        false,
		EnableBackups: true,
		BackupPath:    "/var/backups/hardn",

		// Network Configuration
		// Nameservers: []string{"1.1.1.1", "1.0.0.1"},

		// SSH Configuration
		SshPort:         22,
		PermitRootLogin: false,
		SshConfigFile:   "/etc/ssh/sshd_config.d/hardn.conf",

		// User Configuration
		SudoNoPassword: true,
		SshKeys:        []string{},

		// Firewall Configuration
		UfwAppProfiles: []UfwAppProfile{},

		// Feature Toggles
		EnableUnattendedUpgrades: false,
		EnableUfwSshPolicy:       false,
		ConfigureDns:             false,
		DisableRootSSH:           false,
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
// the working directory. FindConfigFile and ConfigFileSearchPath both walk
// it, so they find the same file.
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

// FoundBy is how FindConfigFile looked for hardn.yml.
type FoundBy uint8

const (
	FoundNone   FoundBy = iota // neither -f nor HARDN_CONFIG, and no file on the search path
	FoundFlag                  // -f/--config named it
	FoundEnv                   // HARDN_CONFIG named it
	FoundSearch                // the first file of the search path (defaultSearchPath)
)

// Found is what FindConfigFile found: the file and how it was looked for.
// Exists is false for FoundNone, and for a file -f or HARDN_CONFIG names
// that is not there, as no other place is looked at then.
type Found struct {
	Path   string
	By     FoundBy
	Exists bool
}

// String is what hardn says of f once, on stderr (cmd/hardn), in the words
// FindConfigFile printed itself before (docs/engine/legacy-left.md,
// decision 4 (a), adopted 2026-11-04).
func (f Found) String() string {
	switch {
	case f.By == FoundFlag && f.Exists:
		return "Using configuration from command-line flag: " + f.Path
	case f.By == FoundFlag:
		return "Configuration file specified by command-line flag not found: " + f.Path
	case f.By == FoundEnv && f.Exists:
		return "Using configuration from HARDN_CONFIG environment variable: " + f.Path
	case f.By == FoundEnv:
		return "Configuration file specified by HARDN_CONFIG environment variable not found: " + f.Path
	case f.By == FoundSearch:
		return "Using configuration from: " + f.Path
	}
	return "No configuration file found in any location"
}

// NoneLoaded is what hardn says once a run goes on with the defaults, the
// offer to create hardn.yml declined.
const NoneLoaded = "Using default configuration (no config file found)"

// FindConfigFile finds hardn.yml: explicitPath (-f) when given, else
// HARDN_CONFIG when set, neither then falling back elsewhere when its file
// is not there, else the first file of the search path. It says nothing:
// cmd/hardn says what it found (Found.String).
func FindConfigFile(explicitPath string) Found {
	if explicitPath != "" {
		_, err := os.Stat(explicitPath)
		return Found{Path: explicitPath, By: FoundFlag, Exists: err == nil}
	}
	if envPath := os.Getenv("HARDN_CONFIG"); envPath != "" {
		_, err := os.Stat(envPath)
		return Found{Path: envPath, By: FoundEnv, Exists: err == nil}
	}
	for _, path := range defaultSearchPath() {
		if _, err := os.Stat(path); err == nil {
			return Found{Path: path, By: FoundSearch, Exists: true}
		}
	}
	return Found{}
}

// helper function to use with LoadConfig
func LoadConfigWithEnvPriority(filePath string) (*Config, error) {
	// Start with default config
	config := DefaultConfig()

	// Find config file with proper priority
	found := FindConfigFile(filePath)

	if !found.Exists {
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

		// Not creating one: the defaults (cmd/hardn says NoneLoaded).
		return config, nil
	}

	return readConfigFile(found.Path)
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
	config.setLoaded(path, data)

	return config, nil
}

// LoadConfig loads hardn's configuration as LoadConfigWithEnvPriority
// finds it (-f, HARDN_CONFIG, then the search path). It runs nothing: origin/main's su - probe of the sudo
// user's login shell for HARDN_CONFIG, and the notice after it, are gone
// (refactor plan slice 9, unit (d), decision 4; side-effects row 5): the
// Environment screen's state row says whether sudo, or doas, keeps
// HARDN_CONFIG, read from hardn's files (pkg/screens/environment).
func LoadConfig(filePath string) (*Config, error) {
	return LoadConfigWithEnvPriority(filePath)
}

// LoadExisting loads hardn's configuration as LoadConfig does where a file
// is found (-f, HARDN_CONFIG, then the search path), and is the defaults
// where none is: it never offers to create one, and so writes nothing. A
// file -f or HARDN_CONFIG names that is not there is an error wrapping
// fs.ErrNotExist, as FindConfigFile does not look elsewhere for it.
// setup-sudo-env's (refactor plan slice 9, unit (d)): the command keeps
// HARDN_CONFIG, so root finding no file is its usual case, and under -n it
// writes nothing (gallery-environment.md §5, Made here, adopted
// 2026-10-29).
func LoadExisting(filePath string) (*Config, error) {
	cfg := DefaultConfig()
	found := FindConfigFile(filePath)
	switch {
	case found.Exists:
		var err error
		if cfg, err = readConfigFile(found.Path); err != nil {
			return nil, err
		}
	case filePath != "":
		return nil, fmt.Errorf("config: %s (-f): %w", filePath, fs.ErrNotExist)
	case os.Getenv("HARDN_CONFIG") != "":
		return nil, fmt.Errorf("config: %s (HARDN_CONFIG): %w", os.Getenv("HARDN_CONFIG"), fs.ErrNotExist)
	}
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
		// Ask for username: none by default (refactor plan §3.11, "george"
		// dropped), so that -c asks for -u and the screens for a name.
		fmt.Print("Enter default username (optional, press Enter to skip): ")
		if username := strings.TrimSpace(readAnswer()); username != "" {
			config.Username = username
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
	// logFile is retired: only a path the file itself named is written back, so that
	// its warning stays until the person removes it.
	if filepath.Clean(saved.LogFile) == defaultLogFile {
		saved.LogFile = ""
	}

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
