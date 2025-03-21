// pkg/menu/ssh_keys_menu.go
package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// Initialize SSH Key Management display
func (m *UserMenu) SSHKeysMenu() {
	utils.ClearScreen()

	// Initialize status formatter w/specific fields for consistency
	formatter := style.NewStatusFormatter([]string{
		"Key 1",
	}, 2)

	// Display SSH Key management box
	m.displaySSHKeysBox(formatter)

	// Keep showing the SSH keys menu until told to exit
	continueShowing := true
	for continueShowing {
		continueShowing = m.HandleSSHKeysOptions()
	}
}

// format the header for the SSH Keys Management box
func (m *UserMenu) formatSSHKeysBoxHeader() string {
	showLabel := false

	head := "SSH Key Management"
	label := "Create and manage users"

	boldHead := style.Bolded(head)
	dimHead := style.Dimmed(boldHead, style.Gray15)
	dimLabel := style.Dimmed(label, style.Gray15)

	if !showLabel {
		return dimHead
	}

	return fmt.Sprintf("%s %s", dimHead, dimLabel)
}

// formats title and subtitle for the SSH Keys Management box
func (m *UserMenu) formatSSHKeysBoxTitle(formatter *style.StatusFormatter) (string, string) {
	showDescription := true
	showSubtext := false

	label := m.config.Username

	paddedLabel := " " + label + " "
	title := style.Colored(style.BgDarkBlue, paddedLabel)

	// Format title line
	formattedTitle := " " + style.Bolded(title)
	meta := "Last login: 2023-10-01"
	titleLine := formatter.FormatLine("", "", formattedTitle, meta, "", "", "no-indent")

	if !showDescription {
		titleLine = formatter.FormatLine("", "", formattedTitle, "", "", "", "no-indent")
	}

	// Format subtext line
	subtext := "Manage user accounts and permissions"
	formattedSubtext := style.Dimmed(subtext)

	subtextLine := ""

	if showSubtext {
		subtextLine = formatter.FormatLine("", "", "", formattedSubtext, style.Gray10, "", "no-indent")
	}

	return titleLine, subtextLine
}

// displays SSH Key Management box
func (m *UserMenu) displaySSHKeysBox(formatter *style.StatusFormatter) {
	// One line padding before the box
	fmt.Println()

	// Set box title and format lines for display
	titleLine, subtextLine := m.formatSSHKeysBoxTitle(formatter)

	// Format box header
	boxHeader := m.formatSSHKeysBoxHeader()

	// Define primary content box w/standardized settings
	contentBox := style.NewBox(style.BoxConfig{
		Width:          64,
		ShowEmptyRow:   true,
		ShowTopBorder:  true,
		ShowLeftBorder: false,
		Indentation:    0,
		Title:          boxHeader,
		TitleColor:     style.Bold,
	})

	// Draw primary box w/content
	contentBox.DrawBox(func(printLine func(string)) {
		// Box content settings
		showTopNotice := true
		showBottomNotice := true
		// showPriority := false
		indentSpaces := 2
		printIndent := style.IndentPrinter(printLine, indentSpaces)

		// Display top notice
		topLine := formatter.FormatConfigured("SSH Keys", "Configured", "ssh-ed25519", "dark")
		if showTopNotice {
			printIndent(topLine)
			printIndent("")
		}

		// Display box title and supporting line
		if titleLine != "" {
			printLine(titleLine)
			if subtextLine != "" {
				printLine(subtextLine)
			}
			printLine("")
		}

		// Display user details w/standardized formatting
		// userLabel := "Username"
		// userColor := style.Cyan
		// userValue := m.config.Username
		// boldUserValue := style.Bolded(userValue)

		// Get user ID
		// userIdentifier := m.getUserId()

		// priorityLine := ""
		// if m.config.Username != "" {
		// 	priorityLine = formatter.FormatLine(style.SymDotTri, userColor, userLabel, boldUserValue, style.Green, userIdentifier, "dark")
		// } else {
		// 	priorityLine = formatter.FormatWarning(userLabel, "Not set", "Please provide a username")
		// }

		// // Display priority line
		// if showPriority {
		// 	printIndent(priorityLine)
		// 	printIndent("")
		// }

		// Display primary user ssh keys configuration details
		m.DisplaySSHKeysConfiguration(m.config, formatter, printIndent, 0)

		// Display bottom notice
		bottomLine := "This is a bottom line example."
		if showBottomNotice {
			printIndent("")
			printIndent(bottomLine)
		}
	})
}

// Display user SSH Key details
// This function can be reused by other menus that need to display SSH key information
func (m *UserMenu) DisplaySSHKeysConfiguration(
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

	// Display current keys
	if len(m.config.SshKeys) == 0 {

		// fmt.Printf("%s No SSH keys configured\n", style.BulletItem)
		printIndent(formatter.FormatBullet("Key 1", "No SSH keys configured", "", "dark"))

	} else {
		for i, key := range m.config.SshKeys {

			// Try to extract comment from key (usually contains email or identifier)
			keyParts := strings.Fields(key)
			keyInfo := ""
			if len(keyParts) >= 3 {
				keyInfo = keyParts[2]
			}

			// Truncate the key for display
			keyTruncated := key
			if len(key) > 30 {
				keyTruncated = key[:15] + "..."
				// keyTruncated = key[:15] + "..." + key[len(key)-15:]
			}

			keyLabel := fmt.Sprintf("Key %d", i+1)

			printIndent(formatter.FormatBullet(keyLabel, keyTruncated, keyInfo, "dark"))

			// if keyInfo != "" {
			// 	fmt.Printf(" (%s)", keyInfo)
			// }
		}
	}

	// Display sudo access with standardized formatting
	// sudoStatus := "No password required"
	// if !cfg.SudoNoPassword {
	// 	sudoStatus = "Password required"
	// }

	// printIndent(formatter.FormatBullet("Access", sudoStatus, "", "dark"))

	// // Display SSH key status with standardized formatting
	// keyCount := len(cfg.SshKeys)
	// keyStatus := "None configured"
	// if keyCount > 0 {
	// 	keyStatus = fmt.Sprintf("%d key(s) configured", keyCount)
	// }
	// printIndent(formatter.FormatBullet("SSH Keys", keyStatus, "", "dark"))

	// meta := m.getUserId() // descriptor

	// if meta != "" {
	// 	printIndent(formatter.FormatBullet("UID", meta, "", "dark"))
	// }
}
