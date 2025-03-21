// pkg/menu/backup_menu.go
package menu

import (
	"fmt"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// BackupMenu handles backup configuration
type BackupMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
}

// NewBackupMenu creates a new BackupMenu
func NewBackupMenu(
	menuManager *application.MenuManager,
	config *config.Config,
) *BackupMenu {
	return &BackupMenu{
		menuManager: menuManager,
		config:      config,
	}
}

// Show displays the backup menu and handles user input
func (m *BackupMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Backup Settings", style.Blue))

	// Get backup status from application layer
	enabled, backupPath, err := m.menuManager.GetBackupStatus()
	if err != nil {
		fmt.Printf("\n%s Error retrieving backup status: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)
	}

	// Display current settings
	fmt.Println()
	fmt.Println(style.Bolded("Current Backup Configuration:", style.Blue))

	// Format backup status
	backupStatus := "Disabled"
	statusColor := style.Red
	if enabled {
		backupStatus = "Enabled"
		statusColor = style.Green
	}

	// Display status with formatter
	formatter := style.NewStatusFormatter([]string{"Backups", "Backup Path"}, 2)

	// Determine symbol and color based on backup status
	symbol := style.SymCrossMark
	color := style.Red
	if enabled {
		symbol = style.SymEnabled
		color = style.Green
	}

	fmt.Println(formatter.FormatLine(symbol, color, "Backups", backupStatus, statusColor, "", "bold"))

	// Display backup path
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Backup Path", backupPath, style.Cyan, ""))

	// Check backup path status
	if enabled {
		// Use application layer to check path status
		pathExists, err := m.menuManager.VerifyBackupPath()
		if err != nil {
			fmt.Printf("%s Error checking backup path: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if pathExists {
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
			Description: fmt.Sprintf("Current: %s", backupPath),
		},
	}

	// Add option to test backup directory if backups are enabled
	if enabled {
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

	choice := ReadMenuInput()

	// Handle 'q' as a special exit case
	if choice == "q" {
		return
	}

	switch choice {
	case "1":
		// Toggle backups using application layer
		err := m.menuManager.ToggleBackups()
		if err != nil {
			fmt.Printf("\n%s Error toggling backups: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
		} else {
			// Get the new status
			enabled, backupPath, _ = m.menuManager.GetBackupStatus()

			if enabled {
				fmt.Printf("\n%s Backups have been %s\n",
					style.Colored(style.Green, style.SymCheckMark),
					style.Bolded("enabled", style.Green))
				fmt.Printf("%s Modified files will be backed up to: %s\n",
					style.BulletItem,
					style.Colored(style.Cyan, backupPath))
			} else {
				fmt.Printf("\n%s Backups have been %s\n",
					style.Colored(style.Yellow, style.SymInfo),
					style.Bolded("disabled", style.Yellow))
				fmt.Printf("%s No automatic backups will be created\n", style.BulletItem)
			}

			// Update config to keep it in sync
			m.config.EnableBackups = enabled

			// Save config changes
			configFile := "hardn.yml" // Default config file
			if err := config.SaveConfig(m.config, configFile); err != nil {
				fmt.Printf("\n%s Failed to save configuration: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			}
		}

		// Return to this menu after changing setting
		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()

	case "2":
		// Change backup path
		fmt.Printf("\n%s Current backup path: %s\n",
			style.BulletItem,
			style.Colored(style.Cyan, backupPath))
		fmt.Printf("%s Enter new backup path: ", style.BulletItem)

		newPath := ReadInput()
		if newPath != "" {
			// Use application layer to set backup directory
			err := m.menuManager.SetBackupDirectory(newPath)
			if err != nil {
				fmt.Printf("\n%s Failed to set backup path: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				// Update local path for display
				_, updatedPath, _ := m.menuManager.GetBackupStatus()

				fmt.Printf("\n%s Backup path updated to: %s\n",
					style.Colored(style.Green, style.SymCheckMark),
					style.Colored(style.Cyan, updatedPath))

				// Update config to keep it in sync
				m.config.BackupPath = updatedPath

				// Save config
				configFile := "hardn.yml" // Default config file
				if err := config.SaveConfig(m.config, configFile); err != nil {
					fmt.Printf("\n%s Failed to save configuration: %v\n",
						style.Colored(style.Red, style.SymCrossMark), err)
				}
			}
		} else {
			fmt.Printf("\n%s Backup path unchanged\n", style.BulletItem)
		}

		// Return to this menu after changing path
		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()

	case "3":
		// Verify backup directory (only available if backups are enabled)
		if enabled {
			fmt.Printf("\n%s Verifying backup directory: %s\n",
				style.BulletItem,
				style.Colored(style.Cyan, backupPath))

			// Use application layer to verify directory
			err := m.menuManager.VerifyBackupDirectory()
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
		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()

	case "0":
		// Return to main menu
		return

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))

		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()
	}
}
