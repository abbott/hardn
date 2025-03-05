// pkg/menu/backup.go

package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// BackupOptionsMenu displays and handles backup configuration options
func BackupOptionsMenu(cfg *config.Config) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Backup Settings", style.Blue))

	// Display current settings
	fmt.Println()
	fmt.Println(style.Bolded("Current Backup Configuration:", style.Blue))
	
	// Format backup status
	backupStatus := "Disabled"
	statusColor := style.Red
	if cfg.EnableBackups {
		backupStatus = "Enabled"
		statusColor = style.Green
	}
	
	// Display status with formatter
	formatter := style.NewStatusFormatter([]string{"Backups", "Backup Path"}, 2)
	
	// Determine symbol and color based on backup status
	symbol := style.SymCrossMark
	color := style.Red
	if cfg.EnableBackups {
		symbol = style.SymEnabled
		color = style.Green
	}
	
	fmt.Println(formatter.FormatLine(
		symbol,
		color,
		"Backups",
		backupStatus,
		statusColor,
		"", 
		"bold"))
	
	// Display backup path
	fmt.Println(formatter.FormatLine(
		style.SymInfo,
		style.Cyan,
		"Backup Path",
		cfg.BackupPath,
		style.Cyan,
		"", 
		"light"))
	
	// Check backup path status
	if cfg.EnableBackups {
		pathExists := checkBackupPath(cfg.BackupPath)
		if pathExists {
			fmt.Printf("%s Backup directory exists and is writable\n", 
				style.Colored(style.Green, style.SymCheckMark))
		} else {
			fmt.Printf("%s Backup directory doesn't exist or isn't writable\n", 
				style.Colored(style.Yellow, style.SymWarning))
			fmt.Printf("%s Directory will be created when needed\n", style.BulletItem)
		}
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{
			Number:      1, 
			Title:       fmt.Sprintf("Toggle backups (currently: %s)", backupStatus), 
			Description: "Enable or disable automatic backups of modified files",
		},
		{
			Number:      2, 
			Title:       "Change backup path", 
			Description: fmt.Sprintf("Current: %s", cfg.BackupPath),
		},
	}
	
	// Add option to test backup directory if backups are enabled
	if cfg.EnableBackups {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      3, 
			Title:       "Verify backup directory", 
			Description: "Test if backup directory exists and is writable",
		})
	}
	
	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to main menu",
		Description: "",
	})
	
	// Display menu
	menu.Print()
	
	choiceStr := ReadInput()
	choice, _ := strconv.Atoi(choiceStr)
	
	switch choice {
	case 1:
		// Toggle backups
		cfg.EnableBackups = !cfg.EnableBackups
		if cfg.EnableBackups {
			fmt.Printf("\n%s Backups have been %s\n", 
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("enabled", style.Green))
			fmt.Printf("%s Modified files will be backed up to: %s\n", 
				style.BulletItem,
				style.Colored(style.Cyan, cfg.BackupPath))
		} else {
			fmt.Printf("\n%s Backups have been %s\n", 
				style.Colored(style.Yellow, style.SymInfo),
				style.Bolded("disabled", style.Yellow))
			fmt.Printf("%s No automatic backups will be created\n", style.BulletItem)
		}
		
		// Save config changes
		saveBackupConfig(cfg)
		
		// Return to this menu after changing setting
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		BackupOptionsMenu(cfg)
		
	case 2:
		// Change backup path
		fmt.Printf("\n%s Current backup path: %s\n", 
			style.BulletItem,
			style.Colored(style.Cyan, cfg.BackupPath))
		fmt.Printf("%s Enter new backup path: ", style.BulletItem)
		
		newPath := ReadInput()
		if newPath != "" {
			// Expand path if it starts with ~
			if newPath[:1] == "~" {
				home, err := os.UserHomeDir()
				if err == nil {
					newPath = filepath.Join(home, newPath[1:])
				}
			}
			
			cfg.BackupPath = newPath
			fmt.Printf("\n%s Backup path updated to: %s\n", 
				style.Colored(style.Green, style.SymCheckMark),
				style.Colored(style.Cyan, cfg.BackupPath))
				
			// Save config changes
			saveBackupConfig(cfg)
		} else {
			fmt.Printf("\n%s Backup path unchanged\n", style.BulletItem)
		}
		
		// Return to this menu after changing path
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		BackupOptionsMenu(cfg)
		
	case 3:
		// Verify backup directory (only available if backups are enabled)
		if cfg.EnableBackups {
			fmt.Printf("\n%s Verifying backup directory: %s\n", 
				style.BulletItem,
				style.Colored(style.Cyan, cfg.BackupPath))
				
			// Try to create directory and test write access
			err := verifyBackupDirectory(cfg.BackupPath)
			if err == nil {
				fmt.Printf("\n%s Backup directory is valid and writable\n", 
					style.Colored(style.Green, style.SymCheckMark))
			} else {
				fmt.Printf("\n%s Backup directory verification failed: %v\n", 
					style.Colored(style.Red, style.SymCrossMark),
					err)
				fmt.Printf("%s Please choose a different backup path\n", style.BulletItem)
			}
		}
		
		// Return to this menu after verification
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		BackupOptionsMenu(cfg)
		
	case 0:
		// Return to main menu
		return
		
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", 
			style.Colored(style.Red, style.SymCrossMark))
			
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		BackupOptionsMenu(cfg)
	}
}

// Helper function to save backup configuration
func saveBackupConfig(cfg *config.Config) {
	// Save config changes
	configFile := "hardn.yml" // Default config file
	if err := config.SaveConfig(cfg, configFile); err != nil {
		logging.LogError("Failed to save configuration: %v", err)
		fmt.Printf("\n%s Failed to save configuration: %v\n", 
			style.Colored(style.Red, style.SymCrossMark),
			err)
	}
}

// Helper function to check if backup path exists
func checkBackupPath(path string) bool {
	// Check if directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	
	// Check if directory is writable by writing a test file
	testFile := filepath.Join(path, ".write_test")
	err := os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		return false
	}
	
	// Clean up test file
	os.Remove(testFile)
	
	return true
}

// Helper function to verify backup directory
func verifyBackupDirectory(path string) error {
	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}
	
	// Check if directory is writable by writing a test file
	testFile := filepath.Join(path, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("backup directory is not writable: %w", err)
	}
	
	// Clean up test file
	os.Remove(testFile)
	
	return nil
}