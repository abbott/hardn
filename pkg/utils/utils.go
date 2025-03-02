package utils

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
)

var (
	logger  *log.Logger
	logFile *os.File
)

// InitLogging initializes the logger for the application
func InitLogging(logPath string) {
	// Create log directory if it doesn't exist
	dir := filepath.Dir(logPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("Failed to create log directory: %v\n", err)
		}
	}

	// Open log file
	var err error
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("Failed to open log file: %v\n", err)
		logFile = nil
	}

	// Create logger
	if logFile != nil {
		logger = log.New(logFile, "", log.LstdFlags)
	} else {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
}

// CloseLogging closes the log file
func CloseLogging() {
	if logFile != nil {
		logFile.Close()
	}
}

// PrintHeader prints a standard header
func PrintHeader() {
	// Clear screen
	fmt.Print("\033[H\033[2J")

	// Print header without an extra newline
	fmt.Print(style.Colored(style.Green, "#########################################################################"))
}

// PrintLogo prints the script logo
func PrintLogo() {
	fmt.Print(`
       _   _               _            _     _
      | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __
      | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /
      |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <
      |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\
`)
	// Add blank line after separator
	fmt.Println()
}

// LogError logs an error message
func LogError(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	color.Red("[ERROR] %s", msg)
	if logger != nil {
		logger.Printf("ERROR: %s", msg)
	}
}

// LogInfo logs an info message
func LogInfo(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	color.Blue("[INFO] %s", msg)
	if logger != nil {
		logger.Printf("INFO: %s", msg)
	}
}

// LogSuccess logs a success message
func LogSuccess(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	color.Green("[SUCCESS] %s", msg)
	if logger != nil {
		logger.Printf("SUCCESS: %s", msg)
	}
}

// LogInstall logs a package installation
func LogInstall(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	color.Cyan("[INSTALLED] %s", msg)
	if logger != nil {
		logger.Printf("INSTALLED: %s", msg)
	}
}

// BackupFile backs up a file
func BackupFile(filePath string, cfg *config.Config) error {
	if !cfg.EnableBackups {
		LogInfo("Backups disabled. Skipping backup of %s", filePath)
		return nil
	}

	if cfg.DryRun {
		LogInfo("[DRY-RUN] Backup %s to %s", filePath, cfg.BackupPath)
		return nil
	}

	// Create backup directory if it doesn't exist
	backupDir := filepath.Join(cfg.BackupPath, time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		LogInfo("File %s does not exist, no backup needed", filePath)
		return nil
	}

	// Get filename without path
	filename := filepath.Base(filePath)

	// Create backup with timestamp
	backupFile := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", filename, time.Now().Format("150405")))

	// Read original file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file for backup: %w", err)
	}

	// Write backup file
	if err := os.WriteFile(backupFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	LogInfo("Backed up %s to %s", filePath, backupFile)
	return nil
}

// RunCommand runs a command and returns its output
func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// CheckSubnet checks if the specified subnet is present in the system's interfaces
func CheckSubnet(subnet string) (bool, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil {
				continue
			}

			// Check if IP matches subnet
			if strings.HasPrefix(ip.String(), subnet+".") {
				LogInfo("Target IP subnet %s.x found: %s", subnet, ip.String())
				return true, nil
			}
		}
	}

	// Get all available IPs for logging
	var availableIPs []string
	for _, iface := range interfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil || strings.HasPrefix(ip.String(), "127.") {
				continue
			}

			availableIPs = append(availableIPs, ip.String())
		}
	}

	LogInfo("Target IP subnet %s.x not found. Available subnets: %s", subnet, strings.Join(availableIPs, ", "))
	return false, nil
}

// SetupHushlogin creates a .hushlogin file in the home directory
func SetupHushlogin(cfg *config.Config) error {
	if cfg.DryRun {
		LogInfo("[DRY-RUN] Write ~/.hushlogin")
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	hushloginPath := filepath.Join(homeDir, ".hushlogin")
	if _, err := os.Stat(hushloginPath); os.IsNotExist(err) {
		file, err := os.Create(hushloginPath)
		if err != nil {
			return fmt.Errorf("failed to create .hushlogin file: %w", err)
		}
		defer file.Close()

		LogInfo("Created ~/.hushlogin")
	}

	return nil
}

// PrintLogs prints the content of the log file
func PrintLogs(logPath string) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		LogError("Failed to read log file: %v", err)
		return
	}

	fmt.Printf("\n# Contents of %s:\n\n", logPath)
	fmt.Println(string(data))
}
