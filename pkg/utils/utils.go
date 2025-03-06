package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/interfaces"
)

// PrintHeader prints a standard header
func PrintHeader() {
	// Clear screen
	fmt.Print("\033[H\033[2J")

	// Print header without an extra newline
	fmt.Print(style.Colored(style.Green, "#######################################################################"))
	fmt.Println()
}

// PrintLogo prints the script logo
func PrintLogo() {
	fmt.Print("\033[H\033[2J")
	fmt.Print(style.Colored(style.Green, "#######################################################################"))
	fmt.Print(`
       _   _               _            _     _
      | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __
      | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /
      |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <
      |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\
`)
	fmt.Println()
}

// BackupFile backs up a file
func BackupFile(filePath string, cfg *config.Config) error {
	if !cfg.EnableBackups {
		logging.LogInfo("Backups disabled. Skipping backup of %s", filePath)
		return nil
	}

	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Backup %s to %s", filePath, cfg.BackupPath)
		return nil
	}

	// Create backup directory if it doesn't exist
	backupDir := filepath.Join(cfg.BackupPath, time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory %s: %w", backupDir, err)
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logging.LogInfo("File %s does not exist, no backup needed", filePath)
		return nil
	}

	// Get filename without path
	filename := filepath.Base(filePath)

	// Create backup with timestamp
	backupFile := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", filename, time.Now().Format("150405")))

	// Read original file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s for backup: %w", filePath, err)
	}

	// Write backup file
	if err := os.WriteFile(backupFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup file to %s: %w", backupFile, err)
	}

	logging.LogInfo("Backed up %s to %s", filePath, backupFile)
	return nil
}

// RunCommand runs a command and returns its output
func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command %s %v failed: %w", name, args, err)
	}
	return string(output), nil
}

// CheckSubnet checks if the specified subnet is present in the system's interfaces
func CheckSubnet(subnet string, networkOps interfaces.NetworkOperations) (bool, error) {
	return networkOps.CheckSubnet(subnet)
}
// func CheckSubnet(subnet string) (bool, error) {
// 	interfaces, err := net.Interfaces()
// 	if err != nil {
// 		return false, fmt.Errorf("failed to get network interfaces: %w", err)
// 	}

// 	for _, iface := range interfaces {
// 		addrs, err := iface.Addrs()
// 		if err != nil {
// 			continue
// 		}

// 		for _, addr := range addrs {
// 			ipNet, ok := addr.(*net.IPNet)
// 			if !ok {
// 				continue
// 			}

// 			ip := ipNet.IP.To4()
// 			if ip == nil {
// 				continue
// 			}

// 			// Check if IP matches subnet
// 			if strings.HasPrefix(ip.String(), subnet+".") {
// 				logging.LogInfo("Target IP subnet %s.x found: %s", subnet, ip.String())
// 				return true, nil
// 			}
// 		}
// 	}

// 	// Get all available IPs for logging
// 	var availableIPs []string
// 	for _, iface := range interfaces {
// 		addrs, err := iface.Addrs()
// 		if err != nil {
// 			continue
// 		}

// 		for _, addr := range addrs {
// 			ipNet, ok := addr.(*net.IPNet)
// 			if !ok {
// 				continue
// 			}

// 			ip := ipNet.IP.To4()
// 			if ip == nil || strings.HasPrefix(ip.String(), "127.") {
// 				continue
// 			}

// 			availableIPs = append(availableIPs, ip.String())
// 		}
// 	}

// 	logging.LogInfo("Target IP subnet %s.x not found. Available subnets: %s", subnet, strings.Join(availableIPs, ", "))
// 	return false, nil
// }

// SetupHushlogin creates a .hushlogin file in the home directory
func SetupHushlogin(cfg *config.Config) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Write ~/.hushlogin")
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
			return fmt.Errorf("failed to create .hushlogin file at %s: %w", hushloginPath, err)
		}
		defer file.Close()

		logging.LogInfo("Created ~/.hushlogin")
	}

	return nil
}

// PrintLogs prints the content of the log file
func PrintLogs(logPath string) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		logging.LogError("Failed to read log file %s: %v", logPath, err)
		return
	}

	fmt.Printf("\n# Contents of %s:\n\n", logPath)
	fmt.Println(string(data))
}