// pkg/menu/main_menu.go
package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/status"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
	// "github.com/abbott/hardn/pkg/version"
)

// MainMenu is the main menu of the application
type MainMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo

	// Add fields for update notification
	updateAvailable bool
	latestVersion   string
	updateURL       string
	// Additional fields for menu state management could be added here
	// For example:
	// lastRefreshTime time.Time
	// statusFormatter *style.StatusFormatter
}

// NewMainMenu creates a new MainMenu
func NewMainMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *MainMenu {
	return &MainMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
		// Initialize any additional fields here
	}
}

// refreshConfig refreshes any configuration values that might have been changed
// by sub-menus like RunAllMenu or DryRunMenu
func (m *MainMenu) refreshConfig() {
	// If we added ways for sub-menus to notify the main menu of changes,
	// we would handle them here

	// For now, we're using a shared config pointer, so changes are automatically visible
	// This method is a placeholder for future extensibility
}

// isDryRunEnabled returns the current dry-run mode status
// func (m *MainMenu) isDryRunEnabled() bool {
// 	// Return the status and update UI elements
// 	isDryRun := m.config.DryRun

// 	// Update menu appearance based on dry-run status
// 	m.dryRunFormatter = style.NewStatusFormatter(
// 			[]string{"Operation Mode"},
// 			2,
// 	)

// 	if isDryRun {
// 			logging.LogInfo("Operating in dry-run mode")
// 			m.menuThemeColor = style.Green
// 			m.statusMessage = "SIMULATION MODE"
// 	} else {
// 			m.menuThemeColor = style.Red
// 			m.statusMessage = "PRODUCTION MODE"
// 	}

// 	return isDryRun
// }

// CheckForUpdates checks for new versions and updates the menu state
// func (m *MainMenu) CheckForUpdates(currentVersion string) {
// 	// Run in a goroutine to avoid blocking the menu display
// 	go func() {
// 		// Import the version package in the file
// 		result := version.CheckForUpdates(currentVersion)
// 		if result.Error != nil {
// 			// Silently ignore errors - we don't want to bother users with API issues
// 			return
// 		}

// 		if result.UpdateAvailable {
// 			m.updateAvailable = true
// 			m.latestVersion = result.LatestVersion
// 			m.updateURL = result.ReleaseURL
// 		}
// 	}()
// }

// showDryRunMenu creates and displays the dry-run configuration menu
func (m *MainMenu) showDryRunMenu() {
	// Display contextual information about dry-run mode
	utils.PrintHeader()
	fmt.Println(style.Bolded("Dry-Run Mode Configuration", style.Blue))

	fmt.Println()
	fmt.Println(style.Dimmed("Dry-run mode allows you to preview changes without applying them to your system."))
	fmt.Println(style.Dimmed("This is useful for testing and understanding what actions will be performed."))

	// Check if any critical operations have been performed
	// This is just an example - you'd need to track this information
	criticalChanges := false // Placeholder for tracking if changes have been made

	if criticalChanges && m.config.DryRun {
		fmt.Printf("\n%s You've already performed operations in dry-run mode.\n",
			style.Colored(style.Yellow, style.SymInfo))
		fmt.Printf("%s Disabling dry-run mode will apply future changes for real.\n",
			style.BulletItem)
	}

	fmt.Println()
	fmt.Printf("%s Press any key to continue to dry-run configuration...", style.BulletItem)
	ReadKey()

	// Create and show the dry-run menu
	dryRunMenu := NewDryRunMenu(m.menuManager, m.config)
	dryRunMenu.Show()

	// After returning from the dry-run menu, inform about the status
	utils.PrintHeader()

	// Quick feedback on the configuration change before returning to main menu
	fmt.Printf("\n%s Dry-run mode is now %s\n",
		style.Colored(style.Cyan, style.SymInfo),
		style.Bolded(map[bool]string{
			true:  "ENABLED - Changes will only be simulated",
			false: "DISABLED - Changes will be applied to the system",
		}[m.config.DryRun], map[bool]string{
			true:  style.Green,
			false: style.Yellow,
		}[m.config.DryRun]))

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// ShowMainMenu displays the main menu and handles user input
func (m *MainMenu) ShowMainMenu(version, buildDate, gitCommit string) {
	for {
		// Refresh any configuration that might have been changed
		m.refreshConfig()

		utils.PrintLogo()

		// Define separator line
		separator := "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
		sepWidth := len(separator)

		// Get security status - this would need to be adapted to use the new architecture
		securityStatus, err := status.CheckSecurityStatus(m.config, m.osInfo)
		var riskLevel, riskDescription, riskColor string
		if err == nil {
			riskLevel, riskDescription, riskColor = status.GetSecurityRiskLevel(securityStatus)
		}

		// Prepare OS display information
		var osDisplay string
		if m.osInfo != nil {
			if m.osInfo.IsProxmox {
				osDisplay = " Proxmox "
			} else {
				osName := strings.Title(m.osInfo.OsType)
				osCodename := strings.Title(m.osInfo.OsCodename)

				if m.osInfo.OsType == "alpine" {
					osDisplay = fmt.Sprintf(" %s Linux %s ", osName, m.osInfo.OsVersion)
				} else {
					osDisplay = fmt.Sprintf(" %s %s ", osName, osCodename)
				}
			}

			// Remove ANSI codes for accurate length calculation
			osDisplayStripped := style.StripAnsi(osDisplay)
			osDisplayWidth := len(osDisplayStripped)

			// Calculate padding for centering OS display, accounting for spaces
			leftPadding := (sepWidth - osDisplayWidth) / 2
			rightPadding := sepWidth - osDisplayWidth - leftPadding

			// Print centered OS display within the separator line
			var envLine = separator[:leftPadding] + osDisplay + separator[:rightPadding]

			fmt.Println(style.Colored(style.Green, envLine))
		} else {
			// Print separator without OS info
			fmt.Println(style.Bolded(separator, style.Green))
		}

		// Display version information after the OS display
		// if version != "" {
		// 	versionDisplay := fmt.Sprintf(" Version %s ", version)

		// 	// Center version information just like OS display
		// 	versionDisplayStripped := style.StripAnsi(versionDisplay)
		// 	versionDisplayWidth := len(versionDisplayStripped)

		// 	leftPadding := (sepWidth - versionDisplayWidth) / 2
		// 	rightPadding := sepWidth - versionDisplayWidth - leftPadding

		// 	// Print centered version within the separator line
		// 	versionLine := separator[:leftPadding] + versionDisplay + separator[:rightPadding]
		// 	fmt.Println(style.Colored(style.BrightCyan, versionLine))

		// 	// Show build information if available
		// 	if buildDate != "" || gitCommit != "" {
		// 		fmt.Println()
		// 		if buildDate != "" {
		// 			fmt.Printf("%s Build Date: %s\n", style.BulletItem, style.Dimmed(buildDate))
		// 		}
		// 		if gitCommit != "" {
		// 			fmt.Printf("%s Git Commit: %s\n", style.BulletItem, style.Dimmed(gitCommit))
		// 		}
		// 	}
		// }

		// Display update notification if a newer version is available
		// if m.updateAvailable {
		// 	fmt.Println()
		// 	updateMsg := fmt.Sprintf(" Update available: %s → %s ", version, m.latestVersion)

		// 	// Center the update message
		// 	updateMsgStripped := style.StripAnsi(updateMsg)
		// 	msgWidth := len(updateMsgStripped)

		// 	leftPadding := (sepWidth - msgWidth) / 2
		// 	rightPadding := sepWidth - msgWidth - leftPadding

		// 	updateLine := separator[:leftPadding] + updateMsg + separator[:rightPadding]
		// 	fmt.Println(style.Colored(style.Yellow, updateLine))

		// 	// Show update instructions
		// 	fmt.Printf("%s Visit: %s\n",
		// 		style.BulletItem,
		// 		style.Colored(style.BrightCyan, m.updateURL))
		// }

		fmt.Println()
		// 2 spaces buffer

		// Format and print risk status using the same formatter, with bold label
		if riskLevel != "" {

			// Create a formatter that includes all labels (including Risk)
			formatter := style.NewStatusFormatter([]string{
				"Risk",
				"SSH Root Login",
				"Firewall",
				"Users",
				"SSH Port",
				"SSH Auth",
				"AppArmor",
				"Auto Updates",
			}, 2)

			boldRiskLabel := style.Bold + "Risk Level" + style.Reset
			riskDescription = style.SymApprox + " " + riskDescription
			fmt.Println(formatter.FormatLine(style.SymDotTri, riskColor, boldRiskLabel, riskLevel, riskColor, riskDescription, "light"))
		}

		fmt.Println()

		// Display detailed security status if available
		if err == nil {
			// Create formatter here since it wasn't created in the risk level section
			formatter := style.NewStatusFormatter([]string{
				"Risk",
				"SSH Root Login",
				"Firewall",
				"Users",
				"SSH Port",
				"SSH Auth",
				"AppArmor",
				"Auto Updates",
			}, 2) // 2 spaces buffer

			status.DisplaySecurityStatus(m.config, securityStatus, formatter)
		}

		// Display dry-run mode if active
		fmt.Println()

		// Format the dry-run mode status like other status lines
		formatter := style.NewStatusFormatter([]string{
			"Dry-run Mode",
		}, 2)

		if m.config.DryRun {
			fmt.Println(formatter.FormatLine(style.SymAsterisk, style.BrightGreen, "Dry-run Mode", "Enabled", style.BrightGreen, "", "light"))
		} else {
			fmt.Println(formatter.FormatLine(style.SymAsterisk, style.BrightYellow, "Dry-run Mode", "Disabled", style.BrightYellow, "", "light"))
		}

		// Create menu options
		menuOptions := []style.MenuOption{
			{Number: 1, Title: "Sudo User", Description: "Create non-root user with sudo access"},
			{Number: 2, Title: "Root SSH", Description: "Disable SSH access for root user"},
			{Number: 3, Title: "DNS", Description: "Configure DNS settings"},
			{Number: 4, Title: "Firewall", Description: "Configure UFW rules"},
			{Number: 5, Title: "Run All", Description: "Run all hardening operations"},
			{Number: 6, Title: "Dry-Run", Description: "Preview changes without applying them"},
			{Number: 7, Title: "Linux Packages", Description: "Install specified Linux packages"},
			{Number: 8, Title: "Python Packages", Description: "Install specified Python packages"},
			{Number: 9, Title: "Package Sources", Description: "Configure package source"},
			{Number: 10, Title: "Backup", Description: "Configure backup settings"},
			{Number: 11, Title: "Environment", Description: "Configure environment variable support"},
			{Number: 12, Title: "Logs", Description: "View log file"},
			{Number: 13, Title: "Help", Description: "View usage information"},
		}

		// Create and customize menu
		menu := style.NewMenu("Select an option", menuOptions)
		// Set custom exit option
		menu.SetExitOption(style.MenuOption{
			Number:      0,
			Title:       "Exit",
			Description: "Tip: Press 'q' to exit immediately",
		})

		// Display the menu
		menu.Print()

		choice := ReadMenuInput()

		// Handle the special exit case for main menu
		if choice == "q" {
			utils.PrintHeader()
			fmt.Println("Hardn has exited.")
			fmt.Println()
			return
		}

		// Process the menu choice - using menuManager instead of direct calls
		switch choice {
		case "1": // Sudo User
			// Create and show user menu
			userMenu := NewUserMenu(m.menuManager, m.config, m.osInfo)
			userMenu.Show()

		case "2": // Root SSH
			// Create and show disable root menu
			disableRootMenu := NewDisableRootMenu(m.menuManager, m.config, m.osInfo)
			disableRootMenu.Show()

		case "3": // DNS
			// ConfigureDnsMenu(m.config, m.osInfo)
			dnsMenu := NewDNSMenu(m.menuManager, m.config, m.osInfo)
			dnsMenu.Show()

		case "4": // Firewall
			// UfwMenu(m.config, m.osInfo)
			firewallMenu := NewFirewallMenu(m.menuManager, m.config, m.osInfo)
			firewallMenu.Show()

		case "5": // Run All
			// Check for prerequisites
			if m.config.Username == "" && !m.config.DryRun {
				// For actual runs (not dry-run), having a username is essential
				fmt.Printf("\n%s No username defined for user creation\n",
					style.Colored(style.Yellow, style.SymWarning))
				fmt.Printf("%s Would you like to set a username now? (y/n): ", style.BulletItem)

				confirm := ReadInput()
				if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
					// Launch the user menu to set a username first
					userMenu := NewUserMenu(m.menuManager, m.config, m.osInfo)
					userMenu.Show()

					// If still no username, abort Run All
					if m.config.Username == "" {
						fmt.Printf("\n%s Run All requires a username for user creation. Operation cancelled.\n",
							style.Colored(style.Red, style.SymCrossMark))
						fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
						ReadKey()
						break
					}
				} else {
					// User chose not to set a username, continue with warning
					fmt.Printf("\n%s Continuing without user creation\n",
						style.Colored(style.Yellow, style.SymWarning))
				}
			}

			// Create and show the Run All menu
			runAllMenu := NewRunAllMenu(m.menuManager, m.config, m.osInfo)
			runAllMenu.Show()

			// After returning from Run All menu, check if the dry-run mode was toggled
			// This affects how the main menu status is displayed
			// Note: This would automatically be handled on the next menu refresh

		case "6": // Dry-Run
			m.showDryRunMenu()

		case "7": // Linux Packages
			// LinuxPackagesMenu(m.config, m.osInfo)
			// This needs a packages manager in application layer
			linuxMenu := NewLinuxPackagesMenu(m.menuManager, m.config, m.osInfo)
			linuxMenu.Show()

		case "8": // Python Packages
			// PythonPackagesMenu(m.config, m.osInfo)
			pythonMenu := NewPythonPackagesMenu(m.menuManager, m.config, m.osInfo)
			pythonMenu.Show()

		case "9": // Package Sources
			// UpdateSourcesMenu(m.config, m.osInfo)
			sourcesMenu := NewSourcesMenu(m.menuManager, m.config, m.osInfo)
			sourcesMenu.Show()

		case "10": // Backup
			// BackupOptionsMenu(m.config)
			backupMenu := NewBackupMenu(m.menuManager, m.config)
			backupMenu.Show()

		case "11": // Environment
			// EnvironmentSettingsMenu(m.config)
			envMenu := NewEnvironmentSettingsMenu(m.menuManager, m.config)
			envMenu.Show()

		case "12": // Logs
			// Viewing logs doesn't need to go through menuManager
			// ViewLogsMenu(m.config)
			logsMenu := NewLogsMenu(m.menuManager, m.config)
			logsMenu.Show()

		case "13": // Helpcase "13": // Help
			helpMenu := NewHelpMenu()
			helpMenu.Show()
			// helpMenu := menuFactory.CreateHelpMenu()
			// helpMenu.Show()

		case "0": // Exit
			utils.PrintHeader()
			fmt.Println("Hardn has exited.")
			fmt.Println()
			return

		default:
			utils.PrintHeader()
			fmt.Printf("%s Invalid option. Please try again.\n",
				style.Colored(style.Red, style.SymCrossMark))
			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
		}
	}
}
