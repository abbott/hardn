// pkg/menu/dry_run_menu.go
package menu

import (
	"fmt"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// DryRunMenu handles the dry-run mode configuration
type DryRunMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
}

// NewDryRunMenu creates a new DryRunMenu
func NewDryRunMenu(
	menuManager *application.MenuManager,
	config *config.Config,
) *DryRunMenu {
	return &DryRunMenu{
		menuManager: menuManager,
		config:      config,
	}
}

// Show displays the dry-run mode menu and handles user input
func (m *DryRunMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Dry-Run Mode Settings", style.Blue))
	
	// Create a formatter with just the label we need
	formatter := style.NewStatusFormatter([]string{"Dry-run Mode"}, 2)

	// Display current status
	fmt.Println()
	if m.config.DryRun {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Enabled", style.Green, "", "bold"))
		fmt.Println(style.Dimmed("\nIn this mode, the script will preview changes without applying them."))
		
		// Create menu options
		menuOptions := []style.MenuOption{
			{Number: 1, Title: "Disable dry-run mode", Description: "Apply changes to the system for real"},
		}
		
		// Create and customize menu
		menu := style.NewMenu("Select an option", menuOptions)
		menu.SetExitOption(style.MenuOption{
			Number:      0,
			Title:       "Return to main menu",
			Description: "Keep dry-run mode enabled",
		})
		
		// Display the menu
		menu.Print()
		
		choiceStr := ReadInput()
		
		switch choiceStr {
		case "1":
			m.config.DryRun = false
			fmt.Println("\n" + formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Disabled", style.Yellow, "", "bold"))
			fmt.Println(style.Dimmed("\nChanges will now be applied to the system. Proceed with caution."))
		case "0":
			fmt.Println("\nDry-run remains " + style.Bolded("Enabled", style.Green) + " - changes will only be simulated.")
		default:
			fmt.Printf("\n%s Invalid option. Dry-run mode remains enabled.\n", style.Colored(style.Yellow, style.SymWarning))
		}
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Disabled", style.Yellow, "", "bold"))
		fmt.Println(style.Dimmed("\nIn this mode, changes will be applied to the system. Proceed with caution."))
		
		// Create menu options
		menuOptions := []style.MenuOption{
			{Number: 1, Title: "Enable dry-run mode", Description: "Preview changes without applying them"},
		}
		
		// Create and customize menu
		menu := style.NewMenu("Select an option", menuOptions)
		menu.SetExitOption(style.MenuOption{
			Number:      0,
			Title:       "Return to main menu",
			Description: "Keep dry-run mode disabled",
		})
		
		// Display the menu
		menu.Print()
		
		choiceStr := ReadInput()
		
		switch choiceStr {
		case "1":
			m.config.DryRun = true
			fmt.Println("\n" + formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Enabled", style.Green, "", "bold"))
			fmt.Println(style.Dimmed("\nChanges will be simulated without affecting the system."))
		case "0":
			fmt.Println("\nDry-run remains " + style.Bolded("Disabled", style.Yellow) + " - proceed with caution.")
		default:
			fmt.Printf("\n%s Invalid option. Dry-run mode remains disabled.\n", style.Colored(style.Yellow, style.SymWarning))
		}
	}

	// Save config changes
	// In a future iteration, this could use m.menuManager.SaveConfiguration()
	// For now, we'll use the direct approach
	configFile := "hardn.yml" // Default config file
	if err := config.SaveConfig(m.config, configFile); err != nil {
		fmt.Printf("\n%s Failed to save configuration: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	}

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}