// pkg/menu/environment_settings_menu.go
package menu

import (
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// EnvironmentSettingsMenu handles environment variable configuration
type EnvironmentSettingsMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
}

// NewEnvironmentSettingsMenu creates a new EnvironmentSettingsMenu
func NewEnvironmentSettingsMenu(
	menuManager *application.MenuManager,
	config *config.Config,
) *EnvironmentSettingsMenu {
	return &EnvironmentSettingsMenu{
		menuManager: menuManager,
		config:      config,
	}
}

// Show displays the environment settings menu and handles user input
func (m *EnvironmentSettingsMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Environment Variable Settings", style.Blue))

	// Check if HARDN_CONFIG is set
	configEnv := os.Getenv("HARDN_CONFIG")
	if configEnv != "" {
		fmt.Printf("\n%s Current HARDN_CONFIG: %s\n", style.BulletItem, style.Colored(style.Green, configEnv))
	} else {
		fmt.Printf("\n%s HARDN_CONFIG environment variable is not set\n", style.BulletItem)
	}

	// Check sudo preservation status
	sudoPreservation := m.checkSudoEnvPreservation()
	if sudoPreservation {
		fmt.Printf("%s Sudo preservation: %s\n", style.BulletItem, style.Colored(style.Green, "Enabled"))
	} else {
		fmt.Printf("%s Sudo preservation: %s\n", style.BulletItem, style.Colored(style.Red, "Disabled"))
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Setup sudo environment preservation", Description: "Configure sudo to preserve HARDN_CONFIG"},
		{Number: 2, Title: "Show environment variables guide", Description: "Learn how to set up environment variables"},
	}

	// Create and customize menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return",
		Description: "",
	})

	// Display the menu
	menu.Print()

	choice := ReadMenuInput()

	// Handle 'q' as a special exit case
	if choice == "q" {
		return
	}

	switch choice {
	case "1":
		// Run sudo env setup
		fmt.Printf("\n%s Setting up sudo environment preservation...\n", style.BulletItem)

		// Check if running as root
		if os.Geteuid() != 0 {
			fmt.Printf("\n%s This operation requires sudo privileges.\n", style.Colored(style.Red, style.SymWarning))
			fmt.Printf("%s Please run: sudo hardn setup-sudo-env\n", style.BulletItem)
		} else {
			if m.config.DryRun {
				fmt.Printf("%s [DRY-RUN] Would configure sudo to preserve HARDN_CONFIG environment variable\n", style.BulletItem)
			} else {
				// Use application layer through menuManager
				err := m.menuManager.SetupSudoPreservation()
				if err != nil {
					fmt.Printf("\n%s Failed to configure sudo: %v\n", style.Colored(style.Red, style.SymCrossMark), err)
				} else {
					fmt.Printf("\n%s Successfully configured sudo to preserve HARDN_CONFIG\n", style.Colored(style.Green, style.SymCheckMark))
				}
			}
		}

		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()

	case "2":
		// Show environment guide
		m.showEnvironmentGuide()
		m.Show()

	case "0":
		return

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", style.Colored(style.Red, style.SymCrossMark))
		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()
	}
}

// showEnvironmentGuide displays a guide on how to set up environment variables
func (m *EnvironmentSettingsMenu) showEnvironmentGuide() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Environment Variables Guide", style.Blue))

	fmt.Printf("\n%s HARDN_CONFIG Environment Variable\n", style.Bolded("", style.Blue))
	fmt.Println(style.Dimmed("------------------------------------"))
	fmt.Println("Set this variable to specify a custom config file location:")
	fmt.Println(style.Colored(style.Cyan, "  export HARDN_CONFIG=/path/to/your/config.yml"))

	fmt.Printf("\n%s Using with sudo\n", style.Bolded("", style.Blue))
	fmt.Println(style.Dimmed("------------------------------------"))
	fmt.Println("To preserve the variable when using sudo, run:")
	fmt.Println(style.Colored(style.Cyan, "  sudo hardn setup-sudo-env"))

	fmt.Printf("\n%s For persistent configuration:\n", style.Bolded("", style.Blue))
	fmt.Println(style.Colored(style.Cyan, "  echo 'export HARDN_CONFIG=/path/to/config.yml' >> ~/.bashrc"))

	fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
	ReadKey()
}

// checkSudoEnvPreservation checks if sudo preservation is enabled
func (m *EnvironmentSettingsMenu) checkSudoEnvPreservation() bool {
	// Use application layer through menuManager
	isEnabled, err := m.menuManager.IsSudoPreservationEnabled()
	if err != nil {
		// If there's an error checking, assume disabled
		return false
	}
	return isEnabled
}
