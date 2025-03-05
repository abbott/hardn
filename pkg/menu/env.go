// pkg/menu/env.go

package menu

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// EnvironmentSettingsMenu displays and handles environment variable configuration
func EnvironmentSettingsMenu(cfg *config.Config) {
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
	sudoPreservation := checkSudoEnvPreservation()
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
		Title:       "Return to main menu",
		Description: "",
	})

	// Display the menu
	menu.Print()

	choice := ReadInput()

	switch choice {
	case "1":
		// Run sudo env setup
		fmt.Printf("\n%s Setting up sudo environment preservation...\n", style.BulletItem)

		// Check if running as root
		if os.Geteuid() != 0 {
			fmt.Printf("\n%s This operation requires sudo privileges.\n", style.Colored(style.Red, style.SymWarning))
			fmt.Printf("%s Please run: sudo hardn setup-sudo-env\n", style.BulletItem)
		} else {
			err := utils.SetupSudoEnvPreservation()
			if err != nil {
				fmt.Printf("\n%s Failed to configure sudo: %v\n", style.Colored(style.Red, style.SymCrossMark), err)
			}
		}

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		EnvironmentSettingsMenu(cfg)

	case "2":
		// Show environment guide
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

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		EnvironmentSettingsMenu(cfg)

	case "0":
		return

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", style.Colored(style.Red, style.SymCrossMark))
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		EnvironmentSettingsMenu(cfg)
	}
}

// Helper function to check if sudo preservation is enabled
func checkSudoEnvPreservation() bool {
	// First check for SUDO_USER which is the original user when using sudo
	username := os.Getenv("SUDO_USER")

	// If that's empty, fall back to USER
	if username == "" {
		username = os.Getenv("USER")

		// If that's still empty, try to get username another way
		if username == "" {
			currentUser, err := user.Current()
			if err != nil {
				return false
			}
			username = currentUser.Username
		}
	}

	// Check if sudoers file exists
	sudoersFile := filepath.Join("/etc/sudoers.d", username)
	if _, err := os.Stat(sudoersFile); os.IsNotExist(err) {
		return false
	}

	// Check file content
	data, err := os.ReadFile(sudoersFile)
	if err != nil {
		return false
	}

	return strings.Contains(string(data), "env_keep += \"HARDN_CONFIG\"")
}