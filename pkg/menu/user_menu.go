// pkg/menu/user_menu.go
package menu

import (
	"fmt"
	"os/user"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// UserMenu handles user-related operations through the menu system
type UserMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewUserMenu creates a new UserMenu
func NewUserMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *UserMenu {
	return &UserMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// ShowUserMenu displays the user menu and handles input
func (m *UserMenu) Show() {
	utils.ClearScreen()

	// Initialize status formatter w/specific fields for consistency
	formatter := style.NewStatusFormatter([]string{
		"Privileges",
		"Sudo Password",
		"SSH Keys",
		"UID:GID",
		"Home Directory",
	}, 2)

	// Display user configuration box
	m.displayUserBox(formatter)

	// Handle menu options
	continueShowing := m.HandleUserMenuOptions()

	// Recursive loop if needed
	if continueShowing {
		m.Show()
	}
}

// formats title and subtitle for the User Management box
func (m *UserMenu) formatUserInstanceUsername(username string, formatter *style.StatusFormatter) (string, string) {
	showMeta := true
	showSubtext := false

	usernameLabel := style.ColoredLabel(username)

	// Try to get last login from extended user info
	meta := ""
	userInfo, err := m.menuManager.GetExtendedUserInfo(username)
	if err == nil && userInfo != nil && userInfo.LastLogin != "" {
		// Split the LastLogin string into IP address and login time
		ipAddress := ""
		if userInfo.LastLoginIP != "" {
			ipAddress = style.Dimmed("(" + userInfo.LastLoginIP + ")")
		}
		loginTime := userInfo.LastLogin
		meta = fmt.Sprintf("%s %s", loginTime, ipAddress)
	}

	usernameLine := formatter.FormatLine("", "", usernameLabel, meta, "", "", "no-indent")

	if !showMeta {
		usernameLine = formatter.FormatLine("", "", usernameLabel, "", "", "", "no-indent")
	}

	// Format subtext line
	subtext := "Manage user accounts and permissions"
	formattedSubtext := style.Dimmed(subtext)

	subtextLine := ""

	if showSubtext {
		subtextLine = formatter.FormatLine("", "", "", formattedSubtext, style.Gray10, "", "no-indent")
	}

	return usernameLine, subtextLine
}

// displays User Management box
func (m *UserMenu) displayUserBox(formatter *style.StatusFormatter) {
	// One line padding before the box
	// Format box header
	boxHeader := style.HeaderLabel("User Management")

	// Define primary content box w/standardized settings
	contentBox := style.NewBox(style.BoxConfig{
		Width:               64,
		ShowEmptyRow:        true,
		ShowTopShade:        true,
		ShowBottomSeparator: true,
		Indentation:         0,
		Title:               boxHeader,
	})

	// Draw primary box w/content
	contentBox.DrawBox(func(printLine func(string)) {
		// Box content settings
		showTopNotice := true
		showBottomNotice := true
		indentSpaces := 2
		printIndent := style.IndentPrinter(printLine, indentSpaces)

		// Display top notice
		topLine := formatter.FormatConfigured("Non-root", "Configured", "UID ≥ 1000", "dark")

		if showTopNotice {
			printIndent(topLine)
			printIndent("")
		}

		contentBox.SectionHeader("Configuration")

		// Display user details
		m.DisplayUserDetails(m.config, formatter, printLine, indentSpaces)

		// Display bottom notice
		bottomLine := ""
		if showBottomNotice && bottomLine != "" {
			printIndent("")
			printIndent(bottomLine)
		}
	})
}

// getUserId returns the user ID for the given username
func (m *UserMenu) getUserId(username string) string {
	// Try using Go's standard library first
	u, err := user.Lookup(username)
	if err == nil {
		return u.Uid + ":" + u.Gid
	}

	// log.Printf("Failed to get user info via standard library for %s: %v", username, err)

	// Fall back to original method
	userInfo, err := m.menuManager.GetExtendedUserInfo(username)
	if err != nil {
		// log.Printf("Error getting extended user info for %s: %v", username, err)
		return "1000:1000" // Fallback
	}

	if userInfo == nil || userInfo.UID == "" || userInfo.GID == "" {
		// log.Printf("Incomplete user info for %s: %+v", username, userInfo)
		return "1000:1000" // Fallback
	}

	return userInfo.UID + ":" + userInfo.GID
}

// Display user configuration details
// This function can be reused by other menus that need to display user config
func (m *UserMenu) DisplayUserDetails(
	cfg *config.Config,
	formatter *style.StatusFormatter,
	printFn func(string),
	indent int,
) {
	// Apply additional indentation if requested
	printIndent := printFn
	if indent > 0 {
		printIndent = style.IndentPrinter(printFn, indent)
	}

	// Get non-system users (UID >= 1000)
	nonSysUsers, err := m.menuManager.GetNonSystemUsers()
	if err != nil {
		printIndent(formatter.FormatWarning(
			"System Users",
			"Error",
			fmt.Sprintf("Failed to retrieve non-system users: %v", err),
		))
	} else if len(nonSysUsers) == 0 {
		printIndent(formatter.FormatBullet("Status", "No non-system users found", "", "dark"))
	} else {
		// Display each non-system user with their UID and login status
		for i, user := range nonSysUsers {

			usernameLine, subtextLine := m.formatUserInstanceUsername(user.Username, formatter)

			// Display username  and supporting line
			if usernameLine != "" {
				printFn(usernameLine)
				if subtextLine != "" {
					printFn(subtextLine)
				}
				printFn("")
			}
			// Try to get extended user info
			userInfo, err := m.menuManager.GetExtendedUserInfo(user.Username)
			// Display sudo access w/standardized formatting
			passwordStatus := "Not required"
			if !cfg.SudoNoPassword {
				passwordStatus = "Required"
			}

			// Use extended info if available
			if err == nil && userInfo != nil {
				// Update sudo status based on extended info
				if userInfo.HasSudo {
					printIndent(formatter.FormatBullet("Privileges", "sudo", "", "dark"))

					// Use extended info for sudo password status
					if userInfo.SudoNoPassword {
						passwordStatus = "Not required"
					} else {
						passwordStatus = "Required"
					}
				} else {
					printIndent(formatter.FormatBullet("Privileges", "regular user", "", "dark"))
				}

				printIndent(formatter.FormatBullet("Sudo Password", passwordStatus, "", "dark"))

				// Display SSH keys from extended info
				keyCount := len(userInfo.SshKeys)
				keyStatus := "None configured"
				if keyCount > 0 {
					keyStatus = fmt.Sprintf("%d key(s) configured", keyCount)
				}
				printIndent(formatter.FormatBullet("SSH Keys", keyStatus, "", "dark"))

				// Display UID:GID from extended info
				if userInfo.UID != "" && userInfo.GID != "" {
					printIndent(formatter.FormatBullet("UID:GID", userInfo.UID+":"+userInfo.GID, "", "dark"))
				}

				// Display home directory from extended info
				if userInfo.HomeDirectory != "" {
					printIndent(formatter.FormatBullet("Directory", userInfo.HomeDirectory, "", "dark"))
				} else {
					printIndent(formatter.FormatBullet("Directory", "/home/"+user.Username, "", "dark"))
				}
			} else {
				// Fallback to config values if extended info isn't available
				printIndent(formatter.FormatBullet("Privileges", "sudo", "", "dark"))
				printIndent(formatter.FormatBullet("Sudo Password", passwordStatus, "", "dark"))

				// Display SSH key status from config
				keyCount := len(user.SshKeys)
				keyStatus := "None configured"
				if keyCount > 0 {
					keyStatus = fmt.Sprintf("%d key(s) configured", keyCount)
				}
				printIndent(formatter.FormatBullet("SSH Keys", keyStatus, "", "dark"))

				// Display UID:GID from the getUserId method
				meta := m.getUserId(user.Username)
				if meta != "" {
					printIndent(formatter.FormatBullet("UID:GID", meta, "", "dark"))
				}
				printIndent(formatter.FormatBullet("Directory", "/home/"+user.Username, "", "dark"))
			}

			// Add section divider for system users list
			if i < len(nonSysUsers)-1 {
				printFn("")
			}
		}
	}
}
