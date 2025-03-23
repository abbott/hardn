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

		// Display information about status but continue to show menu
		fmt.Println(style.Colored(style.Green, "\nNo further action needed to disable root SSH access."))
	}

	// Security warning
	fmt.Println(style.Colored(style.Yellow, "\nBefore disabling root SSH access, ensure that:"))
	fmt.Printf("%s You have created at least one non-root user with sudo privileges\n", style.BulletItem)
	fmt.Printf("%s You have tested SSH access with this non-root user\n", style.BulletItem)
	fmt.Printf("%s You have a backup method to access this system if SSH fails\n", style.BulletItem)

	// Create menu options
	menuOptions := []style.MenuOption{}

	// Always show option 1, but dim it when already disabled
	if rootAccessEnabled {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1,
			Title:       "Disable root SSH access",
			Description: "Modify SSH config to prevent root login",
		})
	} else {
		// For dimmed text, we need to store just the plain text in the Title field
		// and then apply the dimming in the description to maintain proper spacing
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1,
			Title:       "Disable root SSH access",
			Description: "ALREADY DISABLED",
			Style:       "strike",
		})
	}

	// Add options to view SSH configuration
	menuOptions = append(menuOptions, style.MenuOption{
		Number:      2,
		Title:       "View current SSH configuration",
		Description: "Show details of SSH security settings",
	})

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return",
		Description: "Keep current settings",
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
		// Handle the case where root access is already disabled
		if !rootAccessEnabled {
			fmt.Printf("\n%s Root SSH access is already disabled\n",
				style.Colored(style.Green, style.SymCheckMark))
			fmt.Println(style.Dimmed("\nNo action needed."))
			break
		}

		// Confirmation step
		fmt.Printf("\n%s Are you sure you want to disable root SSH access? (y/n): ",
			style.Colored(style.Yellow, style.SymWarning))
		confirm := ReadInput()

		if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
			fmt.Println("\nOperation cancelled. Root SSH access remains enabled.")
			break
		}

		fmt.Println("\nDisabling root SSH access...")

		if m.config.DryRun {
			fmt.Printf("%s [DRY-RUN] Would disable root SSH access\n", style.BulletItem)
		} else {
			// Call application layer to disable root SSH access
			err := m.menuManager.DisableRootSSH()
			if err != nil {
				fmt.Printf("\n%s Failed to disable root SSH access: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s Root SSH access has been disabled\n",
					style.Colored(style.Green, style.SymCheckMark))

				// Restart SSH service
				fmt.Println(style.Dimmed("Restarting SSH service..."))
				var restartErr error
				if m.osInfo.OsType == "alpine" {
					restartErr = exec.Command("rc-service", "sshd", "restart").Run()
				} else {
					restartErr = exec.Command("systemctl", "restart", "ssh").Run()
				}

				if restartErr != nil {
					fmt.Printf("%s Failed to restart SSH service: %v\n",
						style.Colored(style.Yellow, style.SymWarning), restartErr)
					fmt.Println(style.Dimmed("You may need to restart the SSH service manually."))
				} else {
					fmt.Printf("%s SSH service restarted successfully\n",
						style.Colored(style.Green, style.SymCheckMark))
				}
			}
		}
	case "2":
		// View current SSH configuration
		fmt.Println("\nCurrent SSH Configuration:")
		fmt.Println(style.Dimmed("-------------------------------------"))

		// Display root login status
		rootStatus := "Enabled"
		if !rootAccessEnabled {
			rootStatus = "Disabled"
		}

		var color string
		if rootAccessEnabled {
			color = style.Red
		} else {
			color = style.Green
		}
		fmt.Printf("%s Root SSH login: %s\n", style.BulletItem,
			style.Colored(color, rootStatus))

		// Display SSH port
		fmt.Printf("%s SSH port: %d\n", style.BulletItem, m.config.SshPort)

		// Display additional SSH settings if available
		fmt.Printf("%s Allowed users: %s\n", style.BulletItem,
			strings.Join(m.config.SshAllowedUsers, ", "))
	case "0":
		return
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))
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
