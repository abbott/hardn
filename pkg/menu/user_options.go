// pkg/menu/user_menu_options.go
package menu

import (
	"fmt"
	osuser "os/user"
	"regexp"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// validateUsername checks if the given username is valid for Linux systems
// Returns isValid, errorMessage
func validateUsername(username string) (bool, string) {
	// 1. Check for empty username
	if username == "" {
		return false, "Username cannot be empty"
	}

	// 2. Check for spaces
	if strings.Contains(username, " ") {
		return false, "Username cannot contain spaces"
	}

	// 3. Check username length
	if len(username) < 1 || len(username) > 32 {
		return false, "Username must be between 1 and 32 characters"
	}

	// 4. Check for valid characters (Linux username restrictions)
	// Linux usernames can contain lowercase letters, numbers, underscores, and hyphens
	// Usernames must start with a letter
	validUsernameRegex := "^[a-z][a-z0-9_-]*$"
	match, err := regexp.MatchString(validUsernameRegex, username)
	if err != nil || !match {
		return false, "Username must start with a lowercase letter and contain only lowercase letters, numbers, hyphens, and underscores"
	}

	// 5. Check against reserved/dangerous usernames
	reservedUsernames := []string{
		"root", "admin", "administrator", "sudo", "system",
		"daemon", "bin", "sys", "sync", "games", "man", "lp", "mail",
		"news", "uucp", "proxy", "www-data", "backup", "list", "irc",
		"gnats", "nobody", "systemd-network", "systemd-resolve", "messagebus",
		"sshd", "postfix", "ntp", "_apt", "tss", "uuidd", "tcpdump",
		"landscape", "pollinate", "syslog", "usbmux", "pulse",
	}

	for _, reserved := range reservedUsernames {
		if username == reserved {
			return false, fmt.Sprintf("'%s' is a reserved system username and cannot be used", username)
		}
	}

	return true, ""
}

// create and display the user menu options and handles user input
func (m *UserMenu) HandleUserMenuOptions() bool {
	// Check if configured user already exists in the system
	var userExists bool = false
	var username string = m.config.Username

	if username != "" {
		// Check if the user exists on the system
		_, err := osuser.Lookup(username)
		if err == nil {
			userExists = true
		}
	}

	// Create menu options
	var menuOptions []style.MenuOption

	// Different menu structure based on whether user exists and matches configuration
	if userExists && username != "" {
		// User exists and matches configuration - show simplified menu
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1,
			Title:       "Manage a user",
			Description: "Configure sudo, SSH Keys",
		})

		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2,
			Title:       "Create a user",
			Description: "Configure a new user",
		})
	} else {
		// Standard menu for when user doesn't exist or no username set
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
		if m.config.SudoNoPassword {
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

		// Create user option (only if username is set)
		if username != "" {
			menuOptions = append(menuOptions, style.MenuOption{
				Number:      4,
				Title:       "Create user",
				Description: fmt.Sprintf("Create user '%s' with current settings", username),
			})
		}
	}

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return",
		Description: "",
	})

	// Display menu

	menu.SetIndentation(2)

	menu.Print()

	choice := ReadMenuInput()

	// Handle 'q' as a special exit case
	if choice == "q" {
		return false // Exit the menu
	}

	switch choice {
	case "1":
		// Check if we're using the simplified menu for existing users
		if userExists && username != "" {
			// Option 1 in simplified menu: Manage a user
			// This submenu allows modifying sudo settings and SSH keys

			// Get list of non-system users to manage
			nonSysUsers, err := m.menuManager.GetNonSystemUsers()
			if err != nil {
				fmt.Printf("\n%s Error getting users: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
				style.PressAnyKey()
				ReadKey()
				return true
			}

			if len(nonSysUsers) == 0 {
				fmt.Printf("\n%s No non-system users found\n",
					style.Colored(style.Yellow, style.SymWarning))
				style.PressAnyKey()
				ReadKey()
				return true
			}

			// boxHeader := style.HeaderLabel("User Management")

			// Create user selection menu
			utils.ClearScreen()
			// Create a separate box for security status
			securityBox := style.NewBox(style.BoxConfig{
				Width:        64,
				ShowEmptyRow: true,
				ShowTopShade: true,
				Indentation:  0,
				Title:        "Manage User",
			})

			// Draw the security box with all content
			securityBox.DrawBox(func(printLine func(string)) {
				// fmt.Println(style.ScreenHeader("Manage User", 72))

				userOptions := []style.MenuOption{}
				for i, user := range nonSysUsers {
					userOptions = append(userOptions, style.MenuOption{
						Number:      i + 1,
						Title:       user.Username,
						Description: "",
					})
				}

				userMenu := style.NewMenu("Select a user", userOptions)

				userMenu.SetExitOption(style.MenuOption{
					Number:      0,
					Title:       "Return",
					Description: "",
				})

				userMenu.SetIndentation(2)
				userMenu.Print()
			})

			userChoice := ReadMenuInput()

			// Handle user selection
			if userChoice == "0" || userChoice == "q" {
				// User canceled selection
				return true
			}

			// Convert choice to index
			userIndex := -1
			_, err = fmt.Sscanf(userChoice, "%d", &userIndex)
			if err != nil || userIndex < 1 || userIndex > len(nonSysUsers) {
				fmt.Printf("\n%s Invalid selection. Please try again.\n",
					style.Colored(style.Red, style.SymCrossMark))
				style.PressAnyKey()
				ReadKey()
				return true
			}

			// Get the selected user
			selectedUser := nonSysUsers[userIndex-1]

			// Clear the screen before showing the management submenu
			utils.ClearScreen()
			// Create a separate box for security status
			manageUserBox := style.NewBox(style.BoxConfig{
				Width:        64,
				ShowEmptyRow: true,
				ShowTopShade: true,
				Indentation:  0,
				Title:        "Manage User",
			})

			// Draw the security box with all content
			manageUserBox.DrawBox(func(printLine func(string)) {

				usernameLabel := style.ColoredLabel(selectedUser.Username)

				fmt.Printf("  %s%s\n\n", style.Dimmed("Managing:"), usernameLabel)

				// Create a submenu for managing the user
				manageUserOptions := []style.MenuOption{
					{
						Number:      1,
						Title:       "Sudo Method",
						Description: "Toggle password requirement",
					},
					{
						Number:      2,
						Title:       "Manage SSH keys",
						Description: "Add or remove SSH keys",
					},
				}

				manageMenu := style.NewMenu("Select an option", manageUserOptions)
				manageMenu.SetExitOption(style.MenuOption{
					Number:      0,
					Title:       "Return",
					Description: "",
				})

				// Display management submenu

				manageMenu.SetIndentation(2)
				manageMenu.Print()

			})

			subChoice := ReadMenuInput()

			switch subChoice {
			case "1":

				// Create user selection menu
				utils.ClearScreen()

				formatter := style.NewStatusFormatter([]string{
					"Sudo Method",
				}, 2)

				// Create a separate box for security status
				sudoBox := style.NewBox(style.BoxConfig{
					Width:        64,
					ShowEmptyRow: true,
					ShowTopShade: true,
					Indentation:  0,
					Title:        "Manage Sudo",
				})

				// Draw the security box with all content
				sudoBox.DrawBox(func(printLine func(string)) {

					indentation := 2
					indentSpaces := strings.Repeat(" ", indentation)
					printIndent := style.IndentPrinter(printLine, indentation)

					// printIndent := printFn
					// if indent > 0 {
					// 	printIndent = style.IndentPrinter(printFn, indent)
					// }
					// fmt.Println(style.ScreenHeader("Manage User", 72))

					// Toggle sudo password requirement for selected user
					// Get current user settings
					userInfo, err := m.menuManager.GetExtendedUserInfo(selectedUser.Username)
					if err != nil {
						fmt.Printf("\n%s Error getting user info: %v\n",
							style.Colored(style.Red, style.SymCrossMark), err)
						style.PressAnyKey()
						ReadKey()
						return
					}

					// Toggle the setting
					sudoNoPassword := !userInfo.SudoNoPassword

					var passwordStatus, sudoDescStyle string

					// Set passwordStatus and sudoDescStyle based on sudoNoPassword value
					if !sudoNoPassword {
						// We're about to set sudo to require a password
						passwordStatus = "no password"
						sudoDescStyle = "warn"
						usernameLabel := style.ColoredLabel(selectedUser.Username)

						fmt.Printf("  %s%s\n\n", style.Dimmed("Managing sudo method for:"), usernameLabel)

						sudoBox.WarningNotice("", "A user password is required for sudo usage.")

						sudoBox.SectionHeader("User Configuration")

						printIndent(formatter.FormatBullet("Sudo", "Enabled", passwordStatus, sudoDescStyle))

						fmt.Printf("\n\n")

						fmt.Printf(indentSpaces + "Require password for sudo? (y/n): ")

						confirm := ReadInput()

						if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {

							fmt.Println()

							sudoBox.WarningNotice("Operation Cancelled", "")

							style.PressAnyKey()

							ReadKey()
							return
						}
					}

					// else {
					// 	// We're setting sudo to not require a password
					// 	passwordStatus = "password required"
					// 	sudoDescStyle = "dark"
					// }

					if sudoNoPassword {
						fmt.Printf("\n%s Sudo will %s for user '%s'\n",
							style.Colored(style.Green, style.SymCheckMark),
							style.Bolded("NOT require a password", style.Green),
							selectedUser.Username)
					} else {
						fmt.Printf("\n%s Sudo will %s for user '%s'\n",
							style.Colored(style.Green, style.SymCheckMark),
							style.Bolded("require a password", style.Green),
							selectedUser.Username)
					}

					// Update the user's sudo settings
					// We're reusing CreateUser which can also update existing users
					err = m.menuManager.CreateUser(selectedUser.Username, true, sudoNoPassword, userInfo.SshKeys)
					if err != nil {
						fmt.Printf("\n%s Failed to update user's sudo settings: %v\n",
							style.Colored(style.Red, style.SymCrossMark), err)
					} else if !m.config.DryRun {
						fmt.Printf("\n%s User sudo settings updated successfully\n",
							style.Colored(style.Green, style.SymCheckMark))
					}

					// Return to this menu after toggling sudo
					style.PressAnyKey()

				})

				ReadKey()

			case "2":
				// Manage SSH keys for the selected user
				// We need to modify SSHKeysMenu to work with the selected user
				// For now, let's implement a simpler version
				userInfo, err := m.menuManager.GetExtendedUserInfo(selectedUser.Username)
				if err != nil {
					fmt.Printf("\n%s Error getting user info: %v\n",
						style.Colored(style.Red, style.SymCrossMark), err)
					style.PressAnyKey()
					ReadKey()
					break
				}

				// Create submenu for SSH key management
				utils.ClearScreen()

				fmt.Printf("\n%s Managing SSH keys for user: %s\n",
					style.Colored(style.Blue, style.SymInfo),
					style.Bolded(selectedUser.Username))

				// Display current keys
				fmt.Println("\nCurrent SSH keys:")
				if len(userInfo.SshKeys) == 0 {
					fmt.Println("  No SSH keys configured")
				} else {
					for i, key := range userInfo.SshKeys {
						// Truncate the key for display
						keyTruncated := key
						if len(key) > 30 {
							keyTruncated = key[:15] + "..." + key[len(key)-15:]
						}
						fmt.Printf("  %d. %s\n", i+1, keyTruncated)
					}
				}

				// Create SSH key options
				keyOptions := []style.MenuOption{
					{
						Number:      1,
						Title:       "Add SSH key",
						Description: "Add a new SSH public key",
					},
				}

				// Only show remove option if keys exist
				if len(userInfo.SshKeys) > 0 {
					keyOptions = append(keyOptions, style.MenuOption{
						Number:      2,
						Title:       "Remove SSH key",
						Description: "Remove an existing SSH key",
					})
				}

				keyMenu := style.NewMenu("Select SSH key operation", keyOptions)
				keyMenu.SetExitOption(style.MenuOption{
					Number:      0,
					Title:       "Return",
					Description: "",
				})

				keyMenu.SetIndentation(2)
				keyMenu.Print()
				keyChoice := ReadMenuInput()

				switch keyChoice {
				case "1": // Add key
					fmt.Printf("\n%s Paste SSH public key: ", style.BulletItem)
					newKey := ReadInput()

					if newKey == "" {
						fmt.Printf("\n%s No key provided. Operation cancelled.\n",
							style.Colored(style.Yellow, style.SymWarning))
					} else {
						// Add the key using manager
						err := m.menuManager.AddSSHKey(selectedUser.Username, newKey)
						if err != nil {
							fmt.Printf("\n%s Failed to add SSH key: %v\n",
								style.Colored(style.Red, style.SymCrossMark), err)
						} else if !m.config.DryRun {
							fmt.Printf("\n%s SSH key added successfully\n",
								style.Colored(style.Green, style.SymCheckMark))
						}
					}

				case "2": // Remove key
					if len(userInfo.SshKeys) == 0 {
						fmt.Printf("\n%s No SSH keys to remove.\n",
							style.Colored(style.Yellow, style.SymWarning))
					} else {
						fmt.Println("\nSelect a key to remove:")

						for i, key := range userInfo.SshKeys {
							// Truncate the key for display
							keyTruncated := key
							if len(key) > 30 {
								keyTruncated = key[:15] + "..." + key[len(key)-15:]
							}
							fmt.Printf("  %d. %s\n", i+1, keyTruncated)
						}

						fmt.Printf("\n%s Enter number to remove (0 to cancel): ", style.BulletItem)
						keyIndexStr := ReadInput()
						keyIndex := -1

						_, err = fmt.Sscanf(keyIndexStr, "%d", &keyIndex)
						if err != nil {
							fmt.Printf("\n%s Invalid selection: Please enter a number.\n",
								style.Colored(style.Red, style.SymCrossMark))
						} else if keyIndex == 0 {
							fmt.Printf("\n%s Operation cancelled.\n",
								style.Colored(style.Yellow, style.SymInfo))
						} else if keyIndex < 1 || keyIndex > len(userInfo.SshKeys) {
							fmt.Printf("\n%s Invalid selection.\n",
								style.Colored(style.Red, style.SymCrossMark))
						} else {
							// Remove the key at the specified index
							// This is a little tricky - we need to rebuild the keys list without the specified key
							newKeys := []string{}
							for i, key := range userInfo.SshKeys {
								if i != keyIndex-1 {
									newKeys = append(newKeys, key)
								}
							}

							// Update the user with the new keys list
							err := m.menuManager.CreateUser(selectedUser.Username, userInfo.HasSudo, userInfo.SudoNoPassword, newKeys)
							if err != nil {
								fmt.Printf("\n%s Failed to update SSH keys: %v\n",
									style.Colored(style.Red, style.SymCrossMark), err)
							} else if !m.config.DryRun {
								fmt.Printf("\n%s SSH key removed successfully\n",
									style.Colored(style.Green, style.SymCheckMark))
							}
						}
					}

				case "0", "q":
					// Return to user management menu
					break

				default:
					fmt.Printf("\n%s Invalid option. Please try again.\n",
						style.Colored(style.Red, style.SymCrossMark))
				}

				style.PressAnyKey()
				ReadKey()

			case "0", "q":
				// Return to main user menu
				break

			default:
				fmt.Printf("\n%s Invalid option. Please try again.\n",
					style.Colored(style.Red, style.SymCrossMark))
				style.PressAnyKey()
				ReadKey()
			}

			return true // Return to main menu

		} else {
			// Standard menu - Option 1: Set or change username
			if username == "" {
				fmt.Printf("\n%s Enter username to create: ", style.BulletItem)
			} else {
				fmt.Printf("\n%s Current username: %s\n", style.BulletItem, username)
				fmt.Printf("%s Enter new username (leave empty to keep current): ", style.BulletItem)
			}

			newUsername := ReadInput()

			// If empty and we already have a username, just keep current
			if newUsername == "" && username != "" {
				fmt.Printf("\n%s Username unchanged: %s\n", style.BulletItem, username)
			} else if newUsername != "" {
				// Validate the new username
				isValid, validationError := validateUsername(newUsername)
				if !isValid {
					fmt.Printf("\n%s Invalid username: %s\n",
						style.Colored(style.Red, style.SymCrossMark),
						validationError)
				} else {
					// Username is valid, proceed
					m.config.Username = newUsername

					// Check if new user exists
					_, err := osuser.Lookup(newUsername)
					if err == nil {
						fmt.Printf("\n%s User '%s' already exists on the system\n",
							style.Colored(style.Yellow, style.SymInfo), newUsername)
					}

					fmt.Printf("\n%s Username set to: %s\n",
						style.Colored(style.Green, style.SymCheckMark), newUsername)

					// Save config changes
					err = config.SaveConfig(m.config, "hardn.yml")
					if err != nil {
						fmt.Printf("\n%s Failed to save configuration: %v\n",
							style.Colored(style.Red, style.SymCrossMark), err)
					}
				}
			} else {
				// No username provided and none exists
				fmt.Printf("\n%s No username provided. Please enter a valid username.\n",
					style.Colored(style.Yellow, style.SymWarning))
			}

			// Return to this menu after changing username
			style.PressAnyKey()
			ReadKey()
			return true // Continue showing the menu
		}

	case "2":
		// Check if we're using the simplified menu for existing users
		if userExists && username != "" {
			// Option 2 in simplified menu: Create a user
			// Show dialog to create a new user in a new screen
			utils.ClearScreen()

			// Create a header for the new user creation screen
			// fmt.Println(style.BoxedTitle("Create New User", 72, style.Blue))

			fmt.Println(style.ScreenHeader("Create New User", 64, style.Gray10))

			fmt.Printf("\n%s Configure a new user\n",
				style.Colored(style.Blue, style.SymInfo))

			// Get new username
			fmt.Printf("\n%s Enter username to create: ", style.BulletItem)
			newUsername := ReadInput()

			// Validate the username
			if newUsername == "" {
				fmt.Printf("\n%s No username provided. Operation cancelled.\n",
					style.Colored(style.Yellow, style.SymWarning))
				style.PressAnyKey()
				ReadKey()
				return true
			}

			isValid, validationError := validateUsername(newUsername)
			if !isValid {
				fmt.Printf("\n%s Invalid username: %s\n",
					style.Colored(style.Red, style.SymCrossMark),
					validationError)
				style.PressAnyKey()
				ReadKey()
				return true
			}

			// Check if user already exists
			_, err := osuser.Lookup(newUsername)
			if err == nil {
				fmt.Printf("\n%s User '%s' already exists on the system\n",
					style.Colored(style.Red, style.SymCrossMark), newUsername)
				style.PressAnyKey()
				ReadKey()
				return true
			}

			// Display user settings section
			fmt.Println("\n" + style.SectionDivider("User Settings", 72))

			// Configure sudo options
			fmt.Printf("\n%s Allow sudo access? (y/n): ", style.BulletItem)
			hasSudoChoice := ReadInput()
			hasSudo := strings.EqualFold(hasSudoChoice, "y") || strings.EqualFold(hasSudoChoice, "yes")

			// Only ask about sudo password if sudo is enabled
			sudoNoPassword := false
			if hasSudo {
				fmt.Printf("\n%s Allow sudo without password? (y/n): ", style.BulletItem)
				sudoChoice := ReadInput()
				sudoNoPassword = strings.EqualFold(sudoChoice, "y") || strings.EqualFold(sudoChoice, "yes")
			}

			// SSH key section
			fmt.Println("\n" + style.SectionDivider("SSH Access", 72))

			// Add SSH key option
			fmt.Printf("\n%s Add SSH public key? (y/n): ", style.BulletItem)
			addKeyChoice := ReadInput()

			var sshKeys []string
			if strings.EqualFold(addKeyChoice, "y") || strings.EqualFold(addKeyChoice, "yes") {
				fmt.Printf("\n%s Paste SSH public key: ", style.BulletItem)
				sshKey := ReadInput()
				if sshKey != "" {
					sshKeys = append(sshKeys, sshKey)
				}
			}

			// Display summary section
			fmt.Println("\n" + style.SectionDivider("Summary", 72))

			// Show summary of settings
			fmt.Printf("\n  Username:          %s", style.Bolded(newUsername))
			if hasSudo {
				fmt.Printf("\n  Sudo Access:       %s", style.Colored(style.Green, "Enabled"))
				if sudoNoPassword {
					fmt.Printf("\n  Sudo Password:     %s", style.Colored(style.Yellow, "Not required"))
				} else {
					fmt.Printf("\n  Sudo Password:     %s", style.Colored(style.Green, "Required"))
				}
			} else {
				fmt.Printf("\n  Sudo Access:       %s", style.Colored(style.Red, "Disabled"))
			}

			if len(sshKeys) > 0 {
				fmt.Printf("\n  SSH Keys:          %d key(s) configured", len(sshKeys))
			} else {
				fmt.Printf("\n  SSH Keys:          %s", style.Colored(style.Yellow, "None configured"))
			}

			// Confirm creation
			fmt.Printf("\n\n%s Create user '%s'? (y/n): ", style.BulletItem, newUsername)
			confirm := ReadInput()
			if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
				fmt.Printf("\n%s Operation cancelled.\n",
					style.Colored(style.Yellow, style.SymInfo))
				style.PressAnyKey()
				ReadKey()
				return true
			}

			// Create the user
			fmt.Printf("\n%s Creating user '%s'...\n", style.BulletItem, newUsername)

			err = m.menuManager.CreateUser(newUsername, true, sudoNoPassword, sshKeys)
			if err != nil {
				fmt.Printf("\n%s Failed to create user: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else if !m.config.DryRun {
				fmt.Printf("\n%s User '%s' created successfully\n",
					style.Colored(style.Green, style.SymCheckMark),
					newUsername)
			}

			style.PressAnyKey()
			ReadKey()
			return true

		} else {
			// Standard menu - Option 2: Toggle sudo password requirement
			m.config.SudoNoPassword = !m.config.SudoNoPassword

			if m.config.SudoNoPassword {
				fmt.Printf("\n%s Sudo will %s for user '%s'\n",
					style.Colored(style.Green, style.SymCheckMark),
					style.Bolded("NOT require a password", style.Green),
					m.config.Username)
			} else {
				fmt.Printf("\n%s Sudo will %s for user '%s'\n",
					style.Colored(style.Green, style.SymCheckMark),
					style.Bolded("require a password", style.Green),
					m.config.Username)
			}

			// Save config changes
			err := config.SaveConfig(m.config, "hardn.yml")
			if err != nil {
				fmt.Printf("\n%s Failed to save configuration: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			}

			// Return to this menu after toggling sudo
			style.PressAnyKey()
			ReadKey()
			return true // Continue showing the menu
		}

	case "3":
		// Standard menu only - Manage SSH keys
		m.SSHKeysMenu()
		return true // Continue showing the menu

	case "4":
		// Standard menu only - Create or update user
		if username == "" {
			fmt.Printf("\n%s No username provided. Please enter a username first.\n",
				style.Colored(style.Red, style.SymCrossMark))

			// Return to this menu
			style.PressAnyKey()
			ReadKey()
			return true // Continue showing the menu
		}

		// Confirm keys are configured
		if len(m.config.SshKeys) == 0 {
			fmt.Printf("\n%s Warning: No SSH keys configured. User will not have SSH access.\n",
				style.Colored(style.Yellow, style.SymWarning))
			fmt.Printf("%s Would you like to continue anyway? (y/n): ", style.BulletItem)

			confirm := ReadInput()
			if !strings.EqualFold(confirm, "y") && !strings.EqualFold(confirm, "yes") {
				fmt.Printf("\n%s Operation cancelled. Please add SSH keys first.\n",
					style.Colored(style.Yellow, style.SymInfo))

				// Return to this menu
				style.PressAnyKey()
				ReadKey()
				return true // Continue showing the menu
			}
		}

		// Determine action based on whether user exists
		action := "Creating"
		if userExists {
			action = "Updating"
		}

		// Create or update user using menuManager
		fmt.Printf("\n%s %s user '%s'...\n", style.BulletItem, action, username)

		err := m.menuManager.CreateUser(username, true, m.config.SudoNoPassword, m.config.SshKeys)
		if err != nil {
			fmt.Printf("\n%s Failed to %s user: %v\n",
				style.Colored(style.Red, style.SymCrossMark), strings.ToLower(action), err)
		} else if !m.config.DryRun {
			fmt.Printf("\n%s User '%s' %s successfully\n",
				style.Colored(style.Green, style.SymCheckMark),
				username,
				strings.ToLower(action)+"d")
		}

		return false // Exit to main menu after user creation/update

	case "0":
		// Return to main menu
		return false // Exit to main menu

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))

		// Return to this menu
		style.PressAnyKey()
		ReadKey()
		return true // Continue showing the menu
	}

	// fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	// ReadKey()
	// return false // Exit to main menu as default behavior
}
