// pkg/menu/main_menu.go
package menu

import (
	"fmt"
	"os"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/status"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
	"github.com/abbott/hardn/pkg/version"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// MainMenu is the main menu of the application
type MainMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo

	// Version service for update checks
	versionService *version.Service

	// Update state fields
	updateAvailable bool
	latestVersion   string
	updateURL       string
}

// NewMainMenu creates a new MainMenu
func NewMainMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
	versionService *version.Service,
) *MainMenu {
	return &MainMenu{
		menuManager:    menuManager,
		config:         config,
		osInfo:         osInfo,
		versionService: versionService,
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

// CheckForUpdates checks for new versions and updates the menu state
func (m *MainMenu) CheckForUpdates() {
	if m.versionService == nil || m.versionService.CurrentVersion == "" {
		return
	}

	// Run in a goroutine to avoid blocking the menu display
	go func() {
		// Use the unified version service
		result := m.versionService.CheckForUpdates(&version.UpdateOptions{
			Debug: os.Getenv("HARDN_DEBUG") != "",
		})

		if result.Error != nil {
			return // Silently fail for menu updates
		}

		if result.UpdateAvailable {
			m.updateAvailable = true
			m.latestVersion = result.LatestVersion
			m.updateURL = result.ReleaseURL
		}
	}()
}

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

// displaySecurityStatusWithBorders displays security status info with borders
func (m *MainMenu) displaySecurityStatusWithBorders(securityStatus *status.SecurityStatus, formatter *style.StatusFormatter) {
	// Define box dimensions
	boxWidth := 68 // Total inner width of the box

	// Get OS display information for the border
	var osDisplay string
	if m.osInfo != nil {
		if m.osInfo.IsProxmox {
			osDisplay = " Proxmox "
		} else {
			osName := cases.Title(language.English).String(m.osInfo.OsType)
			osCodename := cases.Title(language.English).String(m.osInfo.OsCodename)

			if m.osInfo.OsType == "alpine" {
				osDisplay = fmt.Sprintf(" %s Linux %s ", osName, m.osInfo.OsVersion)
			} else {
				osDisplay = fmt.Sprintf(" %s %s ", osName, osCodename)
			}
		}
	}

	// Create top border with OS info
	var topBorder string
	if osDisplay != "" {
		// Remove ANSI codes for accurate length calculation
		osDisplayStripped := style.StripAnsi(osDisplay)
		osDisplayWidth := len(osDisplayStripped)

		// Calculate padding for centered OS display within border
		leftBorderLen := (boxWidth - osDisplayWidth) / 2
		rightBorderLen := boxWidth - osDisplayWidth - leftBorderLen

		// Create the border with OS info
		topBorder = style.DarkBorder("╭"+strings.Repeat("─", leftBorderLen)) +
			style.Colored(style.Green, osDisplay) +
			style.DarkBorder(strings.Repeat("─", rightBorderLen)+"╮")
	} else {
		// Standard border without OS info
		topBorder = style.DarkBorder("╭" + strings.Repeat("─", boxWidth) + "╮")
	}

	// Define other border elements
	bottomBorder := style.DarkBorder("╰" + strings.Repeat("─", boxWidth) + "╯")
	leftBorder := style.DarkBorder("│   ")
	rightBorder := style.DarkBorder(" │")
	// Empty line with consistent width
	emptyLine := style.DarkBorder("│") + strings.Repeat(" ", boxWidth) + style.DarkBorder("│")

	// Print top border with OS info
	fmt.Println(topBorder)

	// Add padding line after top border
	fmt.Println(emptyLine)

	// Define a unified border printing function for regular status items
	printBorderedLine := func(content string) {
		// Get visible content length by removing ANSI escape codes
		visibleLen := len(style.StripAnsi(content))

		// Calculate padding needed for consistent alignment
		paddingSize := boxWidth - visibleLen - 2 // -2 for the spaces in left border
		if paddingSize < 0 {
			paddingSize = 0 // Safety check
		}
		padding := strings.Repeat(" ", paddingSize)

		fmt.Println(leftBorder + content + padding + rightBorder)
	}

	// First display risk level if available - with special handling
	if securityStatus != nil {
		riskLevel, riskDescription, riskColor := status.GetSecurityRiskLevel(securityStatus)
		boldRiskLabel := style.Bold + "Risk Level" + style.Reset
		riskDescription = style.SymApprox + " " + riskDescription

		// Format the risk level line
		formattedLine := formatter.FormatLine(style.SymDotTri, riskColor, boldRiskLabel, riskLevel, riskColor, riskDescription, "light")

		// Special handling for risk level line
		// The risk level line needs specific calculation due to complex formatting
		formattedLen := len(style.StripAnsi(formattedLine))

		// Apply an adjustment factor specific to the risk level line
		adjustment := 2 // Fine-tune this value based on testing
		paddingSize := boxWidth - formattedLen - 2 + adjustment

		if paddingSize < 0 {
			paddingSize = 0
		}
		padding := strings.Repeat(" ", paddingSize)

		// Print the risk level line with adjusted padding
		fmt.Println(leftBorder + formattedLine + padding + rightBorder)

		// Add an empty line after risk level
		fmt.Println(emptyLine)
	}

	// Create a custom print function for status items
	borderPrinter := func(line string) {
		printBorderedLine(line)
	}

	// Use the existing DisplaySecurityStatus function with our border printer
	status.DisplaySecurityStatusWithCustomPrinter(m.config, securityStatus, formatter, borderPrinter)

	// Add padding line before bottom border
	fmt.Println(emptyLine)

	// Print bottom border
	fmt.Println(bottomBorder)
}

// ShowMainMenu displays the main menu and handles user input
func (m *MainMenu) ShowMainMenu(currentVersion, buildDate, gitCommit string) {
	// Initialize version service if not already done
	if m.versionService == nil && currentVersion != "" {
		m.versionService = version.NewService(currentVersion, buildDate, gitCommit)
	}

	// Check for updates when the menu starts
	if m.versionService != nil {
		// See if we should force an update notification for testing
		if os.Getenv("HARDN_FORCE_UPDATE") != "" {
			m.updateAvailable = true
			m.latestVersion = "0.3.3"
			// m.updateURL = "https://github.com/abbott/hardn/releases/latest"
			m.updateURL = "curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh"
		} else {
			m.CheckForUpdates()
		}
	}

	for {

		// Refresh any configuration that might have been changed
		m.refreshConfig()

		utils.ClearScreen()

		// Define separator line
		// separator := "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
		// sepWidth := len(separator)

		// Get security status - this would need to be adapted to use the new architecture
		securityStatus, err := status.CheckSecurityStatus(m.config, m.osInfo)
		// var riskLevel, riskDescription, riskColor string
		// if err == nil {
		// 	riskLevel, riskDescription, riskColor = status.GetSecurityRiskLevel(securityStatus)
		// }

		fmt.Println()

		hardnLabel := style.Colored(style.BgDarkGreen, " hardn ")
		currentVersion := "v" + m.versionService.CurrentVersion
		latestVersion := "v" + m.latestVersion

		// Display update notification if a newer version is available
		if m.versionService != nil && m.versionService.CurrentVersion != "" {

			if m.updateAvailable {
				hardnVersion := hardnLabel + " " + style.Dimmed(currentVersion) + " → " + style.DarkGreen + latestVersion + style.Reset
				fmt.Println(hardnVersion)

				fmt.Println()
				fmt.Printf("%s%s\n",
					style.Colored(style.Royal, style.BulletSpecial),
					style.Colored(style.Royal, m.updateURL))

				fmt.Println()
			} else {
				hardnVersion := hardnLabel + " " + style.Dimmed(currentVersion)
				fmt.Println(hardnVersion)

			}

			formatter := style.NewStatusFormatter([]string{
				"Build Date",
				"Git Commit",
			}, 2) // 2 spaces buffer

			fmt.Println(formatter.FormatBullet("Build Date", m.versionService.BuildDate, "", "no-indent"))
			fmt.Println(formatter.FormatBullet("Git Commit", m.versionService.GitCommit, "", "no-indent"))
		}

		// fmt.Println()

		// // Prepare OS display information
		// var osDisplay string
		// if m.osInfo != nil {
		// 	if m.osInfo.IsProxmox {
		// 		osDisplay = " Proxmox "
		// 	} else {
		// 		osName := cases.Title(language.English).String(m.osInfo.OsType)
		// 		osCodename := cases.Title(language.English).String(m.osInfo.OsCodename)

		// 		if m.osInfo.OsType == "alpine" {
		// 			osDisplay = fmt.Sprintf(" %s Linux %s ", osName, m.osInfo.OsVersion)
		// 		} else {
		// 			osDisplay = fmt.Sprintf(" %s %s ", osName, osCodename)
		// 		}
		// 	}

		// 	// Remove ANSI codes for accurate length calculation
		// 	osDisplayStripped := style.StripAnsi(osDisplay)
		// 	osDisplayWidth := len(osDisplayStripped)

		// 	// Calculate padding for centering OS display, accounting for spaces
		// 	leftPadding := (sepWidth - osDisplayWidth) / 2
		// 	rightPadding := sepWidth - osDisplayWidth - leftPadding

		// 	// Print centered OS display within the separator line
		// 	var envLine = separator[:leftPadding] + osDisplay + separator[:rightPadding]

		// 	// Calculate padding for centering OS display, accounting for spaces
		// 	// rightPadding := sepWidth - osDisplayWidth
		// 	// var envLine = osDisplay + separator[:rightPadding]

		// 	fmt.Println(style.Colored(style.Green, envLine))
		// } else {
		// 	// Print separator without OS info
		// 	fmt.Println(style.Bolded(separator, style.Green))
		// }

		fmt.Println()

		// Display security status with borders if available
		if err == nil {
			// Create formatter for security status
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

			// Use our helper function to display security status with proper borders
			m.displaySecurityStatusWithBorders(securityStatus, formatter)
		} else {
			fmt.Println()
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
			// {Number: 8, Title: "Python Packages", Description: "Install specified Python packages"},
			{Number: 8, Title: "Package Sources", Description: "Configure package source"},
			{Number: 9, Title: "Backup", Description: "Configure backup settings"},
			{Number: 10, Title: "Environment", Description: "Configure environment variable support"},
			{Number: 11, Title: "Host Info", Description: "View detailed system information"},
			{Number: 12, Title: "Logs", Description: "View log file"},
			// {Number: 13, Title: "Help", Description: "View usage information"},
		}

		// Create and customize menu
		menu := style.NewMenu("Select an option", menuOptions)

		// Set indentation for menu options (4 spaces)
		menu.SetIndentation(4)

		// Set dry-run status to display alongside the title
		menu.SetDryRunStatus(true, m.config.DryRun)

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
			linuxMenu := NewLinuxPackagesMenu(m.menuManager, m.config, m.osInfo)
			linuxMenu.Show()

		// case "8": // Python Packages
		// 	pythonMenu := NewPythonPackagesMenu(m.menuManager, m.config, m.osInfo)
		// 	pythonMenu.Show()

		case "8": // Package Sources
			sourcesMenu := NewSourcesMenu(m.menuManager, m.config, m.osInfo)
			sourcesMenu.Show()

		case "9": // Backup
			backupMenu := NewBackupMenu(m.menuManager, m.config)
			backupMenu.Show()

		case "10": // Environment
			envMenu := NewEnvironmentSettingsMenu(m.menuManager, m.config)
			envMenu.Show()

		case "11": // Host Info
			hostInfoMenu := NewHostInfoMenu(m.menuManager, m.config, m.osInfo)
			hostInfoMenu.Show()

		case "12": // Logs
			logsMenu := NewLogsMenu(m.menuManager, m.config)
			logsMenu.Show()

		// case "13": // Help
		// 	helpMenu := NewHelpMenu()
		// 	helpMenu.Show()

		// helpMenu := menuFactory.CreateHelpMenu()

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

func (m *MainMenu) SetTestUpdateAvailable(testVersion string) {
	if m.versionService != nil {
		result := m.versionService.CheckForUpdates(&version.UpdateOptions{
			ForceUpdate:   true,
			ForcedVersion: testVersion,
		})

		m.updateAvailable = result.UpdateAvailable
		m.latestVersion = result.LatestVersion
		m.updateURL = result.ReleaseURL
	}
}
