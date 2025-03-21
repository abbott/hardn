package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/style"
)

// PrintHeader prints a standard header
func PrintPounds() {
	fmt.Print(style.Colored(style.Green, "#######################################################################"))
}

func PrintTilda() {
	fmt.Print(style.Colored(style.Green, "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"))
}

func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}

// PrintHeader prints a standard header
func PrintHeader() {
	// Clear screen
	ClearScreen()

	// Print header without an extra newline
	PrintTilda()
	// PrintPounds()
	fmt.Println()
}

// PrintLogo prints the script logo
// func PrintLogo() {
// 	fmt.Print(style.Dimmed(`
// |      _   _               _            _     _                       |
// |     | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __     |
// |     | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /     |
// |     |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <      |
// |     |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\     |
// `))
// 	fmt.Println()
// }

func PrintLogo() {
	fmt.Print(style.Dimmed(`
       _   _               _            _     _
      | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __
      | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /
      |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <
      |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\
`))
	fmt.Println()
}

// func PrintLogo() {
// 	fmt.Print("\033[H\033[2J")
// 	fmt.Print(style.Colored(style.Green, "#######################################################################"))
// 	fmt.Print(`
//        _   _               _            _     _
//       | | | | __ _ _ __ __| |_ __      | |   (_)_ __  _   ___  __
//       | |_| |/ _  | '__/ _  | '_ \     | |   | | '_ \| | | \ \/ /
//       |  _  | (_| | | | (_| | | | |    | |___| | | | | |_| |>  <
//       |_| |_|\__,_|_|  \__,_|_| |_|    |_____|_|_| |_|\__,_/_/\_\
// `)
// 	fmt.Println()
// }

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

// run a command and returns its output
func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command %s %v failed: %w", name, args, err)
	}
	return string(output), nil
}

// check if the specified subnet is present in the system's interfaces
func CheckSubnet(subnet string, networkOps interfaces.NetworkOperations) (bool, error) {
	return networkOps.CheckSubnet(subnet)
}

// create a .hushlogin file in the home directory
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

// WriteToFile writes string content to a file
func WriteToFile(filePath string, content string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(filePath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Write the file
	return os.WriteFile(filePath, []byte(content), 0644)
}
