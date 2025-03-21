// pkg/menu/user_menu_ssh_keys_options.go
package menu

import (
	"fmt"
	osuser "os/user"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
)

// HandleSSHKeysOptions displays the SSH keys menu and processes a single selection
// Returns true if the menu should be shown again, false to exit to the parent menu
func (m *UserMenu) HandleSSHKeysOptions() bool {

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Add SSH key", Description: "Add a new SSH public key"},
	}

	// Only add remove option if keys exist
	if len(m.config.SshKeys) > 0 {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2,
			Title:       "Remove SSH key",
			Description: "Remove an existing SSH public key",
		})
	}

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to user menu",
		Description: "",
	})

	// Display menu
	menu.Print()

	choice := ReadMenuInput()

	// Handle 'q' as a special exit case or option 0 to exit
	if choice == "q" || choice == "0" {
		// Return to user menu
		return false
	}

	switch choice {
	case "1":
		// Add SSH key
		fmt.Printf("\n%s Paste SSH public key (e.g., ssh-ed25519 AAAAC3NzaC1lZDI1...): \n", style.BulletItem)
		newKey := ReadInput()

		if newKey != "" {
			// Validate key format
			if !strings.HasPrefix(newKey, "ssh-") && !strings.HasPrefix(newKey, "ecdsa-") {
				fmt.Printf("\n%s Invalid SSH key format. Key should start with 'ssh-' or 'ecdsa-'\n",
					style.Colored(style.Red, style.SymCrossMark))
			} else {
				// Add key
				m.config.SshKeys = append(m.config.SshKeys, newKey)
				fmt.Printf("\n%s SSH key added successfully\n",
					style.Colored(style.Green, style.SymCheckMark))

				// Save config changes
				err := config.SaveConfig(m.config, "hardn.yml")
				if err != nil {
					fmt.Printf("\n%s Failed to save configuration: %v\n",
						style.Colored(style.Red, style.SymCrossMark), err)
				}

				// If user already exists, add key to user
				if m.config.Username != "" {
					_, err := osuser.Lookup(m.config.Username)
					if err == nil {
						err = m.menuManager.AddSSHKey(m.config.Username, newKey)
						if err != nil {
							fmt.Printf("\n%s Failed to add SSH key to user: %v\n",
								style.Colored(style.Yellow, style.SymWarning), err)
						} else if !m.config.DryRun {
							fmt.Printf("%s Key added to user '%s'\n",
								style.BulletItem, m.config.Username)
						}
					}
				}
			}
		}

		// Wait for key press before continuing
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		return true // Continue showing the SSH keys menu

	case "2":
		// Only process if keys exist
		if len(m.config.SshKeys) == 0 {
			fmt.Printf("\n%s No keys to remove\n",
				style.Colored(style.Yellow, style.SymWarning))

			// Wait for key press before continuing
			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
			return true // Continue showing the SSH keys menu
		}

		// Remove SSH key
		fmt.Printf("\n%s Enter key number to remove (1-%d): ", style.BulletItem, len(m.config.SshKeys))
		keyNumStr := ReadInput()

		// Parse key number
		keyNum := 0
		n, err := fmt.Sscanf(keyNumStr, "%d", &keyNum)
		if err != nil || n != 1 {
			fmt.Printf("\n%s Invalid key number: not a valid number\n",
				style.Colored(style.Red, style.SymCrossMark))
		} else if keyNum < 1 || keyNum > len(m.config.SshKeys) {
			fmt.Printf("\n%s Invalid key number. Please enter a number between 1 and %d\n",
				style.Colored(style.Red, style.SymCrossMark), len(m.config.SshKeys))
		} else {
			// Remove key (adjusting for 0-based indexing)
			removedKey := m.config.SshKeys[keyNum-1]
			m.config.SshKeys = append(m.config.SshKeys[:keyNum-1], m.config.SshKeys[keyNum:]...)

			fmt.Printf("\n%s SSH key %d removed successfully\n",
				style.Colored(style.Green, style.SymCheckMark), keyNum)

			// Show truncated key that was removed
			if len(removedKey) > 30 {
				removedKey = removedKey[:15] + "..." + removedKey[len(removedKey)-15:]
			}
			fmt.Printf("%s Removed: %s\n", style.BulletItem,
				style.Colored(style.Yellow, removedKey))

			// Save config changes
			err := config.SaveConfig(m.config, "hardn.yml")
			if err != nil {
				fmt.Printf("\n%s Failed to save configuration: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			}
		}

		// Wait for key press before continuing
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		return true // Continue showing the SSH keys menu

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))

		// Wait for key press before continuing
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		return true // Continue showing the SSH keys menu
	}
}
