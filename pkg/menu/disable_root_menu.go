// pkg/menu/disable_root_menu.go
package menu

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// DisableRootMenu handles disabling root SSH access
type DisableRootMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewDisableRootMenu creates a new DisableRootMenu
func NewDisableRootMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *DisableRootMenu {
	return &DisableRootMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the disable root menu and handles user input
func (m *DisableRootMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Disable Root SSH Access", style.Blue))

	// Check current status of root SSH access
	rootAccessEnabled, err := m.checkRootLoginEnabled()
	if err != nil {
		fmt.Printf("\n%s Error checking root SSH status: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)
		rootAccessEnabled = true // Assume vulnerable if can't check
	}

	fmt.Println()
	if rootAccessEnabled {
		fmt.Printf("%s %s Root SSH access is currently %s\n",
			style.Colored(style.Yellow, style.SymWarning),
			style.Bolded("WARNING:"),
			style.Bolded("ENABLED", style.Red))
	} else {
		fmt.Printf("%s Root SSH access is already %s\n",
			style.Colored(style.Green, style.SymCheckMark),
			style.Bolded("DISABLED", style.Green))

		fmt.Printf("\n%s Nothing to do. Press any key to return to the main menu...", style.BulletItem)
		ReadKey()
		return
	}

	// Security warning
	fmt.Println(style.Colored(style.Yellow, "\nBefore proceeding, ensure that:"))
	fmt.Printf("%s You have created at least one non-root user with sudo privileges\n", style.BulletItem)
	fmt.Printf("%s You have tested SSH access with this non-root user\n", style.BulletItem)
	fmt.Printf("%s You have a backup method to access this system if SSH fails\n", style.BulletItem)

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Disable root SSH access", Description: "Modify SSH config to prevent root login"},
	}

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to main menu",
		Description: "Keep root SSH access enabled",
	})

	// Display menu
	menu.Print()

	choice := ReadInput()

	switch choice {
	case "1":
		fmt.Println("\nDisabling root SSH access...")

		if m.config.DryRun {
			fmt.Printf("%s [DRY-RUN] Would disable root SSH access\n", style.BulletItem)
		} else {
			// Call application layer to disable root SSH access
			err := m.menuManager.DisableRootSsh()
			if err != nil {
				fmt.Printf("\n%s Failed to disable root SSH access: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s Root SSH access has been disabled\n",
					style.Colored(style.Green, style.SymCheckMark))

				// Restart SSH service
				fmt.Println(style.Dimmed("Restarting SSH service..."))
				if m.osInfo.OsType == "alpine" {
					exec.Command("rc-service", "sshd", "restart").Run()
				} else {
					exec.Command("systemctl", "restart", "ssh").Run()
				}
			}
		}
	case "0":
		fmt.Println("\nOperation cancelled. Root SSH access remains enabled.")
	default:
		fmt.Printf("\n%s Invalid option. No changes were made.\n",
			style.Colored(style.Yellow, style.SymWarning))
	}

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// checkRootLoginEnabled checks if SSH root login is enabled by asking the application layer
func (m *DisableRootMenu) checkRootLoginEnabled() (bool, error) {
	// In a full implementation, we would call through to the application layer
	// For now, we'll use a simple file check similar to the old implementation

	// This is temporary and should be replaced with a proper call to the application layer
	// as it becomes available
	var rootLoginEnabled bool

	// Check SSH config file - THIS SHOULD BE REPLACED with app layer method
	var sshConfigPaths []string

	if m.osInfo.OsType == "alpine" {
		sshConfigPaths = []string{"/etc/ssh/sshd_config"}
	} else {
		// For Debian/Ubuntu, check both main config and config.d
		sshConfigPaths = []string{
			"/etc/ssh/sshd_config.d/hardn.conf",
			"/etc/ssh/sshd_config",
		}
	}

	// Check each potential config file
	for _, configPath := range sshConfigPaths {
		// Check if the file exists and parse it
		content, err := os.ReadFile(configPath)
		if err != nil {
			continue // Try next config file if this one can't be read
		}

		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "PermitRootLogin") {
				parts := strings.Fields(line)
				if len(parts) >= 2 && (parts[1] == "no" || parts[1] == "No") {
					rootLoginEnabled = false
					return rootLoginEnabled, nil
				}
				rootLoginEnabled = true
				return rootLoginEnabled, nil
			}
		}
	}

	// If not explicitly set, assume it's enabled
	return true, nil
}
