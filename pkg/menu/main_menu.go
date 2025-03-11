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
	installURL      string

	// Security update fields
	securityUpdateAvailable bool
	securityUpdateDetails   string
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
			m.installURL = result.InstallURL

			// Track security updates
			m.securityUpdateAvailable = result.SecurityUpdateAvailable
			m.securityUpdateDetails = result.SecurityUpdateDetails
		}
	}()
}

// SetTestUpdateAvailable sets test update information
func (m *MainMenu) SetTestUpdateAvailable(testVersion string) {
	if m.versionService != nil {
		result := m.versionService.CheckForUpdates(&version.UpdateOptions{
			ForceUpdate:   true,
			ForcedVersion: testVersion,
		})

		m.updateAvailable = result.UpdateAvailable
		m.latestVersion = result.LatestVersion
		m.updateURL = result.ReleaseURL
		m.installURL = result.InstallURL
	}
}

// SetTestSecurityUpdate sets test security update information
func (m *MainMenu) SetTestSecurityUpdate(details string) {
	if m.versionService != nil {
		// Use a shorter default message if none provided
		if details == "" {
			details = "CVE-2023-1234 fixed"
		} else if len(details) > 80 {
			// Truncate long security details to prevent layout issues
			details = details[:77] + "..."
		}

		result := m.versionService.CheckForUpdates(&version.UpdateOptions{
			ForceUpdate:         true,
			ForcedVersion:       m.versionService.CurrentVersion + ".1", // Just a minor increment
			ForceSecurityUpdate: true,
			SecurityDetails:     details,
		})

		m.updateAvailable = result.UpdateAvailable
		m.latestVersion = result.LatestVersion
		m.updateURL = result.ReleaseURL
		m.installURL = result.InstallURL
		m.securityUpdateAvailable = result.SecurityUpdateAvailable
		m.securityUpdateDetails = result.SecurityUpdateDetails
	}
}

func SafePadding(totalWidth int, contentLength int, offset int) string {
	paddingSize := totalWidth - contentLength + offset
	if paddingSize < 0 {
		paddingSize = 0 // Prevent negative padding
	}
	return strings.Repeat(" ", paddingSize)
}

// displaySecurityStatusWithBordersFixed is the corrected version that prevents negative padding
func (m *MainMenu) displaySecurityStatusWithBorders(securityStatus *status.SecurityStatus, formatter *style.StatusFormatter) {
	// Define box dimensions
	boxWidth := 64 // Total inner width of the box

	// Header info construction

	hardnBold := style.Bold + "hardn" + style.Reset
	hardnPad := " " + hardnBold + " "
	hardnLabel := style.Colored(style.BgGray02, hardnPad)
	currentVersion := "v" + m.versionService.CurrentVersion
	currentVersionPad := " " + currentVersion + " "
	currentVersionDim := style.Dimmed(currentVersionPad)
	currentVersionBg := style.Colored(style.BgGray03, currentVersionDim)
	latestVersion := "v" + m.latestVersion
	// hardnVersion = hardnLabel + currentVersionBg + " → " + style.DarkGreen + latestVersion + style.Reset
	hardnVersion := hardnLabel + currentVersionBg
	repoURL := "https://github.com/abbott/hardn"

	// Create borders and padding elements for the status box
	boxHorizontal := "─"  // U+2500 Box Drawings Light Horizontal
	boxVertical := "│"    // U+2502 Box Drawings Light Vertical
	boxTopLeft := "╭"     // U+256D Box Drawings Light Arc Down and Right
	boxTopRight := "╮"    // U+256E Box Drawings Light Arc Down and Left
	boxBottomLeft := "╰"  // U+256F Box Drawings Light Arc Up and Right
	boxBottomRight := "╯" // U+2570 Box Drawings Light Arc Up and Left
	boxSpace := " "       // U+0020 Space
	// boxNone := "" // intentional: no box character here

	topBorder := style.DarkBorder(boxTopLeft + strings.Repeat(boxHorizontal, boxWidth) + boxTopRight)
	bottomBorder := style.DarkBorder(boxBottomLeft + strings.Repeat(boxHorizontal, boxWidth) + boxBottomRight)
	// noHoritontalBorder := style.DarkBorder(boxNone) // intentional: no box character here
	leftBorder := style.DarkBorder(boxVertical)  // unique border for left side
	rightBorder := style.DarkBorder(boxVertical) // unique border for right side
	emptyLine := style.DarkBorder(boxVertical) + strings.Repeat(boxSpace, boxWidth) + style.DarkBorder(boxVertical)

	// Define a unified border printing function for regular status items
	printBorderedLine := func(content string) {
		// Get visible content length by removing ANSI escape codes
		visibleLen := len(style.StripAnsi(content))

		// Calculate padding needed for consistent alignment (with safety check)
		paddingSize := boxWidth - visibleLen - 1 // -1 for space adjustment
		if paddingSize < 0 {
			paddingSize = 0 // Safety check to prevent panic
		}
		padding := strings.Repeat(" ", paddingSize)

		fmt.Println(leftBorder + content + padding + rightBorder)
	}

	securityNoticeLine := ""
	securityUpdateLine := ""
	hardnLine := ""
	repoLine := ""
	notification := ""
	message := ""

	hardnLine = formatter.FormatLine(
		"",
		"",
		hardnVersion,
		repoURL,
		style.Gray06,
		"",
		"no-indent",
	)
	repoLine = formatter.FormatLine(
		"",
		"",
		"",
		repoURL,
		style.Gray06,
		"",
		"no-indent",
	)
	// Format and display the header line with version info
	if m.versionService != nil && m.versionService.CurrentVersion != "" {

		// Display security update alert if available (at the top of the box for high visibility)
		if m.securityUpdateAvailable {
			// Create security update alert with distinctive styling
			securityDetails := m.securityUpdateDetails
			message = latestVersion + " " + "available, update now!"

			// notification = style.Colored(style.Red, securityDetails)
			// Format the security alert line with alert styling
			securityNoticeLine = formatter.FormatLine(
				"",
				"",
				hardnVersion,
				"",
				"",
				"",
				"no-indent",
			)

			securityUpdateLine = formatter.FormatLine(
				"",
				"",
				"",
				message,
				style.Dim,
				"",
				"no-indent",
			)

			// noticeBold := style.Bold + "Critical Update" + style.Reset
			// noticePad := " " + noticeBold + " "
			// noticeLabel := style.Colored(style.BgDarkRed, noticePad)

			latestVersion := "v" + m.latestVersion
			// latestVersionPad := " " + latestVersion + " "
			// latestVersionRed := style.Colored(style.Red, latestVersionPad)
			// noticeVersion := noticeLabel + latestVersionRed

			// Create security update alert with distinctive styling
			securityHeader := style.Colored(style.BgDarkRed, " Update Binary ")
			// securityDetails := m.securityUpdateDetails
			// if securityDetails == "" {
			// 	securityDetails = "Security updates available in " + latestVersion
			// }

			// Format the security alert line with alert styling
			securityLine := formatter.FormatLine(
				"",
				style.Red,
				securityHeader,
				"",
				"",
				"",
				"",
				"no-indent",
				"no-spacing",
			)

			fmt.Println(hardnLine)
			fmt.Println()
			fmt.Println()

			fmt.Println("  " + securityLine)
			fmt.Println()
			fmt.Println("  " + securityDetails)
			// fmt.Println(securityUpdateLine)

			fmt.Printf("  %s\n",
				style.Colored(style.Royal, m.updateURL))
			fmt.Println()

			infoFormatter := style.NewStatusFormatter([]string{
				"Build Date",
				"Git Commit",
			}, 2) // 2 spaces buffer

			fmt.Println(infoFormatter.FormatBullet("Version", latestVersion, "", "no-indent"))
			fmt.Println(infoFormatter.FormatBullet("Build Date", m.versionService.BuildDate, "", "no-indent"))
			fmt.Println(infoFormatter.FormatBullet("Git Commit", m.versionService.GitCommit, "", "no-indent"))
			fmt.Println()
			fmt.Println(style.Bolded("  Installer Script:"))
			fmt.Println(style.Colored(style.Royal, "  "+m.installURL))
			fmt.Println()
			fmt.Println()
			fmt.Print(style.Dimmed("Press any key to exit... "))

			// Print the security alert safely
			// printBorderedLine(securityLine)
		} else if m.updateAvailable {

			message = latestVersion + " " + "available"

			notification = style.Colored(style.Royal, message)
			hardnLine = formatter.FormatLine(
				"",
				"",
				hardnVersion,
				notification,
				style.Royal,
				"",
				"no-indent",
			)
		}
	}
	// hardnLine := formatter.FormatLine("", "", hardnVersion, repoURL, style.Gray06, "", "no-indent")

	if !m.securityUpdateAvailable {
		// Calculate padding with safety check
		visibleHardnLength := len(style.StripAnsi(hardnLine))
		hardnSetPad := boxWidth - visibleHardnLength - 2
		if hardnSetPad < 0 {
			hardnSetPad = 0 // Safety check to prevent panic
		}
		hardnPadding := strings.Repeat(" ", hardnSetPad)

		fmt.Println(topBorder)
		if m.securityUpdateAvailable {
			fmt.Println(leftBorder + securityNoticeLine + hardnPadding + rightBorder)
			fmt.Println(leftBorder + securityUpdateLine + hardnPadding + rightBorder)
			fmt.Println(leftBorder + repoLine + hardnPadding + rightBorder)
		} else {
			fmt.Println(leftBorder + hardnLine + hardnPadding + rightBorder)
		}
		fmt.Println(bottomBorder)
		fmt.Println()
	}

	// Get host information
	hostInfo, err := m.menuManager.GetHostInfo()

	hostLine := ""
	uptimeLine := ""

	// Display host information if available
	if err == nil && hostInfo != nil {
		// Get IP address (first one if multiple are available)
		ipAddress := "Unknown"
		if len(hostInfo.IPAddresses) > 0 {
			ipAddress = hostInfo.IPAddresses[0]
		}

		// hostBold := style.Bold + hostInfo.Hostname + style.Reset
		hostPad := " " + hostInfo.Hostname + " "
		hostLabel := style.Colored(style.BgDarkBlue, hostPad)

		// Format host + IP address line
		// message := latestVersion + " " + "available"
		hostLine = formatter.FormatLine("", "", hostLabel, ipAddress, style.Dim, "", "bold", "no-indent")

		// Format uptime line
		uptime := m.menuManager.FormatUptime(hostInfo.Uptime)
		uptimeLine = formatter.FormatLine("", "", "", uptime, style.Gray06, "", "no-indent")

		// // Format host line
		// hostLine := formatter.FormatLine(style.SymInfo, style.Cyan, "Host",
		// 	fmt.Sprintf("%s (%s)", hostInfo.Hostname, ipAddress), style.Cyan, "", "light")
		// fmt.Println(hostLine)

		// // Format OS line
		// osLine := formatter.FormatLine(style.SymInfo, style.Cyan, "OS",
		// 	fmt.Sprintf("%s %s", hostInfo.OSName, hostInfo.OSVersion), style.Cyan, "", "light")
		// fmt.Println(osLine)

		// // Format uptime line
		// uptimeLine := formatter.FormatLine(style.SymInfo, style.Cyan, "Uptime",
		// 	m.menuManager.FormatUptime(hostInfo.Uptime), style.Cyan, "", "light")
		// fmt.Println(uptimeLine)

		// fmt.Println()
	}

	if !m.securityUpdateAvailable {

		// Print top border with OS info
		fmt.Println(topBorder)
		fmt.Println(emptyLine)
		fmt.Println(hostLine)
		fmt.Println(uptimeLine)
		fmt.Println(emptyLine)

		// First display risk level if available - with special handling
		if securityStatus != nil {
			riskLevel, riskDescription, riskColor := status.GetSecurityRiskLevel(securityStatus)
			boldRiskLabel := style.Bold + "Risk Level" + style.Reset
			riskDescription = style.SymApprox + " " + riskDescription

			// Format the risk level line
			formattedLine := formatter.FormatLine(style.SymDotTri, riskColor, boldRiskLabel, riskLevel, riskColor, riskDescription, "light")

			// Special handling for risk level line - with safe padding calculation
			formattedLen := len(style.StripAnsi(formattedLine))

			// Apply an adjustment factor but ensure we don't get negative padding
			adjustment := 3
			paddingSize := boxWidth - formattedLen - 2 + adjustment
			if paddingSize < 0 {
				paddingSize = 0 // Prevent negative padding
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
}

// ShowMainMenu displays the main menu and handles user input
func (m *MainMenu) ShowMainMenu(currentVersion, buildDate, gitCommit string) {
	// Initialize version service if not already done
	if m.versionService == nil && currentVersion != "" {
		m.versionService = version.NewService(currentVersion, buildDate, gitCommit)
	}

	// Check for updates when the menu starts
	if m.versionService != nil {
		// Check for different environment variables to trigger test modes
		if os.Getenv("HARDN_FORCE_UPDATE") != "" {
			m.updateAvailable = true
			m.latestVersion = "0.3.3"
			m.updateURL = "curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh"
		} else if os.Getenv("HARDN_FORCE_SECURITY") != "" {
			// Test mode for security updates
			m.updateAvailable = true
			m.latestVersion = "0.3.3"
			m.updateURL = "curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh"
			m.securityUpdateAvailable = true
			m.securityUpdateDetails = "Critical security vulnerability fixed - CVE-2023-1234"
		} else {
			m.CheckForUpdates()
		}
	}

	for {
		// Refresh any configuration that might have been changed
		m.refreshConfig()

		utils.ClearScreen()

		// Get security status
		securityStatus, err := status.CheckSecurityStatus(m.config, m.osInfo)

		// Display security status with borders if available
		if err == nil {
			// Create formatter for security status
			formatter := style.NewStatusFormatter([]string{
				"Host",   // Add new label for host info
				"OS",     // Add new label for OS info
				"Uptime", // Add new label for uptime info
				"Risk Level",
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
			{Number: 1, Title: "Sudo User", Description: "Create non-root sudo user"},
			{Number: 2, Title: "Root SSH", Description: "Disable SSH access for root user"},
			{Number: 3, Title: "DNS", Description: "Configure DNS settings"},
			{Number: 4, Title: "Firewall", Description: "Configure UFW rules"},
			{Number: 5, Title: "Run All", Description: "Run all hardening operations"},
			{Number: 6, Title: "Dry-Run", Description: "Simulate changes"},
			{Number: 7, Title: "Linux Packages", Description: "Install specified Linux packages"},
			{Number: 8, Title: "Package Sources", Description: "Configure package source"},
			{Number: 9, Title: "Backup", Description: "Configure backup settings"},
			{Number: 10, Title: "Environment", Description: "Configure environment variable"},
			{Number: 11, Title: "Host Info", Description: "View detailed system information"},
			{Number: 12, Title: "Logs", Description: "View log file"},
		}

		// Create and customize menu
		menu := style.NewMenu("Select an option", menuOptions)

		// Set indentation for menu options (4 spaces)
		menu.SetIndentation(2)

		// Set dry-run status to display alongside the title
		menu.SetDryRunStatus(true, m.config.DryRun)

		// Set custom exit option
		menu.SetExitOption(style.MenuOption{
			Number:      0,
			Title:       "Exit",
			Description: "Tip: Press 'q' to exit immediately",
		})

		if !m.securityUpdateAvailable {
			// Display the menu
			menu.Print()
		}

		choice := ReadMenuInput()

		// Handle the special exit case for main menu
		if choice == "q" {
			utils.ClearScreen()
			// utils.PrintHeader()
			// fmt.Println("Hardn has exited.")
			// fmt.Println()
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

		case "6": // Dry-Run
			m.showDryRunMenu()

		case "7": // Linux Packages
			linuxMenu := NewLinuxPackagesMenu(m.menuManager, m.config, m.osInfo)
			linuxMenu.Show()

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

		case "0": // Exit
			utils.ClearScreen()
			// utils.PrintHeader()
			// fmt.Println("Hardn has exited.")
			// fmt.Println()
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
