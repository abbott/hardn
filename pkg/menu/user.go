// pkg/menu/user.go

package menu

import (
	"fmt"
	osuser "os/user"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/ssh"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
	"github.com/abbott/hardn/pkg/user"
)

// UserCreationMenu handles creating a non-root user with sudo access
func UserCreationMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("User Creation", style.Blue))

	// Display current user settings
	fmt.Println()
	fmt.Println(style.Bolded("Current User Configuration:", style.Blue))
	
	// Format user status
	formatter := style.NewStatusFormatter([]string{"Username", "Sudo Access", "SSH Keys"}, 2)
	
	// Username status
	if cfg.Username != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Username", 
			cfg.Username, style.Cyan, "", "light"))
	} else {
		fmt.Println(formatter.FormatWarning("Username", "Not set", "Please provide a username"))
	}
	
	// Sudo access status
	sudoStatus := "No password required"
	if !cfg.SudoNoPassword {
		sudoStatus = "Password required"
	}
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Sudo Access", 
		sudoStatus, style.Cyan, "", "light"))
	
	// SSH key status
	keyCount := len(cfg.SshKeys)
	keyStatus := "None configured"
	if keyCount > 0 {
		keyStatus = fmt.Sprintf("%d key(s) configured", keyCount)
	}
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "SSH Keys", 
		keyStatus, style.Cyan, "", "light"))
		
	// Check if user already exists
	var userExists bool
	var username string = cfg.Username
	
	if username != "" {
		_, err := osuser.Lookup(username)
		userExists = (err == nil)
		
		if userExists {
			fmt.Printf("\n%s User '%s' already exists on the system\n", 
				style.Colored(style.Yellow, style.SymInfo), username)
		}
	}

	// Create menu options
	var menuOptions []style.MenuOption

	// Add or change username option
	if username == "" {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1, 
			Title:       "Enter username", 
			Description: "Specify username to create",
		})
	} else {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1, 
			Title:       "Change username", 
			Description: fmt.Sprintf("Current: %s", username),
		})
	}
	
	// Toggle sudo password option
	if cfg.SudoNoPassword {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2, 
			Title:       "Require sudo password", 
			Description: "Change sudo to require password",
		})
	} else {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2, 
			Title:       "Allow sudo without password", 
			Description: "Change sudo to not require password",
		})
	}
	
	// Manage SSH keys option
	menuOptions = append(menuOptions, style.MenuOption{
		Number:      3, 
		Title:       "Manage SSH keys", 
		Description: "Add or remove SSH public keys",
	})
	
	// Create user option (only if username is set and user doesn't exist)
	if username != "" && !userExists {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      4, 
			Title:       "Create user", 
			Description: fmt.Sprintf("Create user '%s' with current settings", username),
		})
	} else if username != "" && userExists {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      4, 
			Title:       "Update user", 
			Description: fmt.Sprintf("Update SSH configuration for user '%s'", username),
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
	
	choice := ReadInput()
	
	switch choice {
	case "1":
		// Set or change username
		if username == "" {
			fmt.Printf("\n%s Enter username to create: ", style.BulletItem)
		} else {
			fmt.Printf("\n%s Current username: %s\n", style.BulletItem, username)
			fmt.Printf("%s Enter new username (leave empty to keep current): ", style.BulletItem)
		}
		
		newUsername := ReadInput()
		if newUsername != "" {
			cfg.Username = newUsername
			
			// Check if new user exists
			_, err := osuser.Lookup(newUsername)
			if err == nil {
				fmt.Printf("\n%s User '%s' already exists on the system\n", 
					style.Colored(style.Yellow, style.SymInfo), newUsername)
			}
			
			fmt.Printf("\n%s Username set to: %s\n", 
				style.Colored(style.Green, style.SymCheckMark), newUsername)
				
			// Save config changes
			saveUserConfig(cfg)
		} else if username != "" {
			fmt.Printf("\n%s Username unchanged: %s\n", style.BulletItem, username)
		}
		
		// Return to this menu after changing username
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		UserCreationMenu(cfg, osInfo)
		
	case "2":
		// Toggle sudo password requirement
		cfg.SudoNoPassword = !cfg.SudoNoPassword
		
		if cfg.SudoNoPassword {
			fmt.Printf("\n%s Sudo will %s for user '%s'\n", 
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("NOT require a password", style.Green),
				cfg.Username)
		} else {
			fmt.Printf("\n%s Sudo will %s for user '%s'\n", 
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("require a password", style.Green),
				cfg.Username)
		}
		
		// Save config changes
		saveUserConfig(cfg)
		
		// Return to this menu after toggling sudo
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		UserCreationMenu(cfg, osInfo)
		
	case "3":
		// Manage SSH keys
		manageSshKeysMenu(cfg, osInfo)
		UserCreationMenu(cfg, osInfo)
		
	case "4":
		// Create or update user
		if username == "" {
			fmt.Printf("\n%s No username provided. Please enter a username first.\n", 
				style.Colored(style.Red, style.SymCrossMark))
			
			// Return to this menu
			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
			UserCreationMenu(cfg, osInfo)
			return
		}
		
		// Confirm keys are configured
		if len(cfg.SshKeys) == 0 {
			fmt.Printf("\n%s Warning: No SSH keys configured. User will not have SSH access.\n", 
				style.Colored(style.Yellow, style.SymWarning))
			fmt.Printf("%s Would you like to continue anyway? (y/n): ", style.BulletItem)
			
			confirm := ReadInput()
			if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
				fmt.Printf("\n%s Operation cancelled. Please add SSH keys first.\n", 
					style.Colored(style.Yellow, style.SymInfo))
				
				// Return to this menu
				fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
				ReadKey()
				UserCreationMenu(cfg, osInfo)
				return
			}
		}
		
		// Determine action based on whether user exists
		action := "Creating"
		if userExists {
			action = "Updating"
		}
		
		// Create or update user
		fmt.Printf("\n%s %s user '%s'...\n", style.BulletItem, action, username)
		
		if !userExists {
			err := user.CreateUser(username, cfg, osInfo)
			if err != nil {
				fmt.Printf("\n%s Failed to create user: %v\n", 
					style.Colored(style.Red, style.SymCrossMark), err)
				logging.LogError("Failed to create user: %v", err)
			} else if !cfg.DryRun {
				fmt.Printf("\n%s User '%s' created successfully\n", 
					style.Colored(style.Green, style.SymCheckMark), username)
			}
		}
		
		// Configure SSH
		fmt.Printf("\n%s Configuring SSH for user '%s'...\n", style.BulletItem, username)
		err := ssh.WriteSSHConfig(cfg, osInfo)
		if err != nil {
			fmt.Printf("\n%s Failed to configure SSH: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
			logging.LogError("Failed to configure SSH: %v", err)
		} else if !cfg.DryRun {
			fmt.Printf("\n%s SSH configured successfully\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
		
	case "0":
		// Return to main menu
		return
		
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", 
			style.Colored(style.Red, style.SymCrossMark))
		
		// Return to this menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		UserCreationMenu(cfg, osInfo)
	}
	
	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// Helper function to manage SSH keys
func manageSshKeysMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Manage SSH Keys", style.Blue))
	
	// Display current keys
	fmt.Println()
	fmt.Println(style.Bolded("Current SSH Keys:", style.Blue))
	
	if len(cfg.SshKeys) == 0 {
		fmt.Printf("%s No SSH keys configured\n", style.BulletItem)
	} else {
		for i, key := range cfg.SshKeys {
			// Try to extract comment from key (usually contains email or identifier)
			keyParts := strings.Fields(key)
			keyInfo := ""
			if len(keyParts) >= 3 {
				keyInfo = keyParts[2]
			}
			
			// Truncate the key for display
			truncatedKey := key
			if len(key) > 30 {
				truncatedKey = key[:15] + "..." + key[len(key)-15:]
			}
			
			fmt.Printf("%s Key %d: %s", style.BulletItem, i+1, 
				style.Colored(style.Cyan, truncatedKey))
			
			if keyInfo != "" {
				fmt.Printf(" (%s)", keyInfo)
			}
			fmt.Println()
		}
	}
	
	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Add SSH key", Description: "Add a new SSH public key"},
	}
	
	// Only add remove option if keys exist
	if len(cfg.SshKeys) > 0 {
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
	
	choice := ReadInput()
	
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
				cfg.SshKeys = append(cfg.SshKeys, newKey)
				fmt.Printf("\n%s SSH key added successfully\n", 
					style.Colored(style.Green, style.SymCheckMark))
				
				// Save config changes
				saveUserConfig(cfg)
			}
		}
		
		// Return to SSH keys menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		manageSshKeysMenu(cfg, osInfo)
		
	case "2":
		// Only process if keys exist
		if len(cfg.SshKeys) == 0 {
			fmt.Printf("\n%s No keys to remove\n", 
				style.Colored(style.Yellow, style.SymWarning))
			
			// Return to SSH keys menu
			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
			manageSshKeysMenu(cfg, osInfo)
			return
		}
		
		// Remove SSH key
		fmt.Printf("\n%s Enter key number to remove (1-%d): ", style.BulletItem, len(cfg.SshKeys))
		keyNumStr := ReadInput()
		keyNum := 0
		
		// Parse key number
		fmt.Sscanf(keyNumStr, "%d", &keyNum)
		
		if keyNum < 1 || keyNum > len(cfg.SshKeys) {
			fmt.Printf("\n%s Invalid key number. Please enter a number between 1 and %d\n", 
				style.Colored(style.Red, style.SymCrossMark), len(cfg.SshKeys))
		} else {
			// Remove key (adjusting for 0-based indexing)
			removedKey := cfg.SshKeys[keyNum-1]
			cfg.SshKeys = append(cfg.SshKeys[:keyNum-1], cfg.SshKeys[keyNum:]...)
			
			fmt.Printf("\n%s SSH key %d removed successfully\n", 
				style.Colored(style.Green, style.SymCheckMark), keyNum)
			
			// Show truncated key that was removed
			if len(removedKey) > 30 {
				removedKey = removedKey[:15] + "..." + removedKey[len(removedKey)-15:]
			}
			fmt.Printf("%s Removed: %s\n", style.BulletItem, 
				style.Colored(style.Yellow, removedKey))
			
			// Save config changes
			saveUserConfig(cfg)
		}
		
		// Return to SSH keys menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		manageSshKeysMenu(cfg, osInfo)
		
	case "0":
		// Return to user menu
		return
		
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", 
			style.Colored(style.Red, style.SymCrossMark))
		
		// Return to SSH keys menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		manageSshKeysMenu(cfg, osInfo)
	}
}

// Helper function to save user configuration
func saveUserConfig(cfg *config.Config) {
	// Save config changes
	configFile := "hardn.yml" // Default config file
	if err := config.SaveConfig(cfg, configFile); err != nil {
		logging.LogError("Failed to save configuration: %v", err)
		fmt.Printf("\n%s Failed to save configuration: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	}
}