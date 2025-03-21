package menu

import (
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/security"
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

// refresh any configuration values that might have been
// changed by sub-menus like RunAllMenu or DryRunMenu
func (m *MainMenu) refreshConfig() {
	// add ways for sub-menus to notify the main menu of changes,

	// For now, we're using a shared config pointer, so changes are automatically visible
}

const (
	repoURL             = "https://github.com/abbott/hardn"
	testVersionNumber   = "0.3.3"
	testReleaseURL      = "https://api.github.com/repos/abbott/hardn/releases/latest"
	testInstallScript   = "curl -sSL https://raw.githubusercontent.com/abbott/hardn/main/install.sh | sudo sh"
	testSecurityMessage = "Critical security vulnerability fixed - CVE-2023-1234"
)

// Check for new version
func (m *MainMenu) CheckForUpdates() {
	if m.versionService == nil || m.versionService.CurrentVersion == "" {
		return
	}

	// Use Go outine to Avoid blocking display
	go func() {
		result := m.versionService.CheckForUpdates(&version.UpdateOptions{
			Debug: os.Getenv("HARDN_DEBUG") != "",
		})

		if result.Error != nil {
			return
		}

		m.applyUpdateResult(result)
	}()
}

// Apply update check result
func (m *MainMenu) applyUpdateResult(result version.CheckResult) {
	if result.UpdateAvailable {
		m.updateAvailable = true
		m.latestVersion = result.LatestVersion
		m.updateURL = result.ReleaseURL
		m.installURL = result.InstallURL
		m.securityUpdateAvailable = result.SecurityUpdateAvailable
		m.securityUpdateDetails = result.SecurityUpdateDetails
	}
}

// Update checks respecting environment variables
func (m *MainMenu) CheckForUpdatesWithEnvVars() {
	if m.versionService == nil {
		return
	}

	// Check for environment variables that trigger test modes
	if os.Getenv("HARDN_FORCE_UPDATE") != "" {
		m.SetTestUpdateAvailable(testVersionNumber)
	} else if os.Getenv("HARDN_FORCE_SECURITY") != "" {
		m.SetTestSecurityUpdate(testSecurityMessage)
	} else {
		m.CheckForUpdates()
	}
}

// SetTestUpdateAvailable sets test update information
func (m *MainMenu) SetTestUpdateAvailable(testVersion string) {
	if m.versionService == nil {
		return
	}

	result := m.versionService.CheckForUpdates(&version.UpdateOptions{
		ForceUpdate:   true,
		ForcedVersion: testVersion,
	})

	m.applyUpdateResult(result)
}

// SetTestSecurityUpdate sets test security update information
func (m *MainMenu) SetTestSecurityUpdate(details string) {
	if m.versionService == nil {
		return
	}

	// Use a shorter default message if none provided
	if details == "" {
		details = "CVE-2023-1234 fixed"
	} else if len(details) > 80 {
		// Truncate long security details to prevent layout issues
		details = details[:77] + "..."
	}

	result := m.versionService.CheckForUpdates(&version.UpdateOptions{
		ForceUpdate:         true,
		ForcedVersion:       m.versionService.CurrentVersion + ".1",
		ForceSecurityUpdate: true,
		SecurityDetails:     details,
	})

	m.applyUpdateResult(result)
}

// displaySecurityStatus displays the security status with appropriate borders
func (m *MainMenu) displaySecurityStatus(securityStatus *security.SecurityStatus, formatter *style.StatusFormatter) {
	// If security update is available, display special alert and return
	if m.securityUpdateAvailable && m.versionService != nil && m.versionService.CurrentVersion != "" {
		m.displaySecurityUpdateAlert(formatter)
		return
	}

	// Otherwise display the normal security status
	m.displayNormalSecurityStatus(securityStatus, formatter)
}

// displaySecurityUpdateAlert displays the security update alert
// Preserves exact formatting and messaging for security updates
func (m *MainMenu) displaySecurityUpdateAlert(formatter *style.StatusFormatter) {
	// Format hardn version header
	hardnBold := style.Bold + "hardn" + style.Reset
	hardnPad := " " + hardnBold + " "
	hardnLabel := style.Colored(style.BgGray02, hardnPad)
	currentVersion := "v" + m.versionService.CurrentVersion
	currentVersionPad := " " + currentVersion + " "
	currentVersionDim := style.Dimmed(currentVersionPad)
	currentVersionBg := style.Colored(style.BgGray03, currentVersionDim)
	hardnVersion := hardnLabel + currentVersionBg

	// Format security related content
	latestVersion := "v" + m.latestVersion
	securityHeader := style.Colored(style.BgDarkRed, " Update Binary ")

	// Display the alert - exact formatting preserved from original
	fmt.Println(hardnVersion)
	fmt.Println()
	fmt.Println()

	fmt.Println("  " + securityHeader)
	fmt.Println()
	fmt.Println("  " + m.securityUpdateDetails)

	fmt.Printf("  %s\n", style.Colored(style.Royal, m.updateURL))
	fmt.Println()

	infoFormatter := style.NewStatusFormatter([]string{
		"Build Date",
		"Git Commit",
	}, 2) // 2 spaces buffer

	// Add "  " prefix to each line for consistent indentation
	fmt.Println("    " + infoFormatter.FormatBullet("Version", latestVersion, "", "no-indent"))
	fmt.Println("    " + infoFormatter.FormatBullet("Build Date", m.versionService.BuildDate, "", "no-indent"))
	fmt.Println("    " + infoFormatter.FormatBullet("Git Commit", m.versionService.GitCommit, "", "no-indent"))
	fmt.Println()
	fmt.Println(style.Bolded("  Installer Script:"))
	fmt.Println(style.Colored(style.Royal, "  "+m.installURL))
	fmt.Println()
	fmt.Println()
	fmt.Print(style.Dimmed("Press any key to exit... "))
}

// displayNormalSecurityStatus displays the normal security status in a box
func (m *MainMenu) displayNormalSecurityStatus(securityStatus *security.SecurityStatus, formatter *style.StatusFormatter) {
	// Format hardn version line with update info if available
	hardnLine := m.formatHardnVersionLine(formatter)

	// Display version information
	fmt.Println(hardnLine)

	repo := style.Dimmed(repoURL)

	// When update is available, also display the repo URL on a new line
	if m.updateAvailable {
		repoLine := formatter.FormatLine("", "", "", repo, style.Gray08, "", "no-indent")
		fmt.Println(repoLine)
	}

	fmt.Println()
	// Get host information and format lines for display
	hostInfo, err := m.menuManager.GetHostInfo()
	hostLine := ""
	uptimeLine := ""
	if err == nil && hostInfo != nil {
		hostLine, uptimeLine = m.formatHostInfoLines(hostInfo, formatter)
	}

	// Format OS information for the box title
	osTitle := m.formatOSTitle()

	// Create a separate box for security status
	securityBox := style.NewBox(style.BoxConfig{
		Width:          64,
		ShowEmptyRow:   true,
		ShowTopBorder:  true,
		ShowLeftBorder: false,
		Indentation:    0,
		Title:          osTitle,
		TitleColor:     style.Bold,
	})

	// Draw the security box with all content
	securityBox.DrawBox(func(printLine func(string)) {
		// Display host information if available
		if hostLine != "" {
			printLine(hostLine)
			printLine(uptimeLine)
			printLine("")
		}

		// Display security status if available
		if securityStatus != nil {
			// Define indentation for all security status items
			indentSpaces := 2
			indentedPrintLine := style.IndentPrinter(printLine, indentSpaces)

			// Display risk level with appropriate color
			riskLevel, riskDescription, riskColor := security.GetSecurityRiskLevel(securityStatus)
			boldRiskLabel := style.Bold + "Risk Level" + style.Reset
			riskDescription = style.SymApprox + " " + riskDescription
			riskLine := formatter.FormatLine(style.SymDotTri, riskColor, boldRiskLabel, riskLevel, riskColor, riskDescription, "dark")

			// Use indented print function for risk level as well
			indentedPrintLine(riskLine)

			// Add empty line after risk level
			indentedPrintLine("")

			// Display security status items using the same indentation
			security.DisplaySecurityStatusWithCustomPrinter(m.config, securityStatus, formatter, indentedPrintLine, 0)
		}
	})
}

// formatOSTitle formats the OS information into a pretty title string
func (m *MainMenu) formatOSTitle() string {
	if m.osInfo == nil {
		return ""
	}

	boldOsType := style.Bolded(utils.Capitalize(m.osInfo.OsType))
	dimOsType := style.Dimmed(boldOsType, style.Gray15)
	regularVersion := style.Dimmed(m.osInfo.OsVersion, style.Gray15)
	grayCodename := style.Dimmed("("+utils.Capitalize(m.osInfo.OsCodename+")"), style.Gray15)

	// Format based on OS type
	switch m.osInfo.OsType {
	case "debian":
		// For Debian, format as "Debian X (Codename)"
		return fmt.Sprintf("%s %s %s", dimOsType, regularVersion, grayCodename)
	case "ubuntu":
		// For Ubuntu, format as "Ubuntu X.Y (Codename)"
		return fmt.Sprintf("%s %s %s", dimOsType, regularVersion, grayCodename)
	case "alpine":
		// For Alpine, format as "Alpine Linux X.Y.Z" - but "Linux" should not be bold
		return fmt.Sprintf("%s Linux %s", dimOsType, regularVersion)
	default:
		// Generic format for other OS types
		return fmt.Sprintf("%s %s", dimOsType, regularVersion)
	}
}

// formatHardnVersionLine formats the hardn version line with update information if available
func (m *MainMenu) formatHardnVersionLine(formatter *style.StatusFormatter) string {
	// Create common elements
	hardnBold := style.Bold + "hardn" + style.Reset
	// hardnPad := " " + hardnBold + " "
	// hardnLabel := style.Colored(style.BgGray02, hardnPad)
	currentVersion := "v" + m.versionService.CurrentVersion
	currentVersionPad := " " + currentVersion + " "
	currentVersionDim := style.Dimmed(currentVersionPad)
	currentVersionBg := style.Colored(style.BgGray03, currentVersionDim)
	hardnVersion := hardnBold + " " + currentVersionBg
	repo := style.Dimmed(repoURL)

	// Format differently based on update availability
	if m.updateAvailable {
		latestVersion := "v" + m.latestVersion
		message := latestVersion + " " + "available"
		notification := style.Colored(style.Royal, message)
		return formatter.FormatLine(
			"",
			"",
			hardnVersion,
			notification,
			style.Royal,
			"",
			"no-indent",
		)
	} else {
		return formatter.FormatLine(
			"",
			"",
			hardnVersion,
			repo,
			style.Gray10,
			"",
			"no-indent",
		)
	}
}

// formatHostInfoLines formats host information lines for display
func (m *MainMenu) formatHostInfoLines(hostInfo *model.HostInfo, formatter *style.StatusFormatter) (string, string) {
	// Get IP address if available
	ipAddress := "Unknown"
	if len(hostInfo.IPAddresses) > 0 {
		ipAddress = hostInfo.IPAddresses[0]
	}

	// Format hostname as highlighted label
	hostPad := " " + hostInfo.Hostname + " "
	hostLabel := style.Colored(style.BgDarkBlue, hostPad)

	// Format host info line
	hostLine := formatter.FormatLine("", "", hostLabel, ipAddress, "", "", "no-indent")
	// hostLine := formatter.FormatLine("", "", hostLabel, ipAddress, style.Dim, "", "bold", "no-indent")

	// Format uptime line
	uptime := m.menuManager.FormatUptime(hostInfo.Uptime)
	uptime = style.Dimmed(uptime)
	uptimeLine := formatter.FormatLine("", "", "", uptime, style.Gray10, "", "no-indent")

	return hostLine, uptimeLine
}

// ShowMainMenu displays the main menu and handles user input
func (m *MainMenu) ShowMainMenu(currentVersion, buildDate, gitCommit string) {
	// Initialize version service if not already done
	if m.versionService == nil && currentVersion != "" {
		m.versionService = version.NewService(currentVersion, buildDate, gitCommit)
	}

	// Check for updates when the menu starts
	m.CheckForUpdatesWithEnvVars()

	// Main menu loop
	for {
		// Refresh any configuration that might have been changed
		m.refreshConfig()

		utils.ClearScreen()

		// Get security status
		securityStatus, err := security.CheckSecurityStatus(m.config, m.osInfo)

		// Create formatter for security status
		formatter := style.NewStatusFormatter([]string{
			"Host",
			"OS",
			"Uptime",
			"Risk Level",
			"SSH Root Login",
			"Firewall",
			"Users",
			"SSH Port",
			"SSH Auth",
			"AppArmor",
			"Auto Updates",
		}, 2) // 2 spaces buffer

		// Display security status if available
		if err == nil {
			m.displaySecurityStatus(securityStatus, formatter)
		} else {
			fmt.Println()
		}

		// If security update is available, wait for key press and exit
		if m.securityUpdateAvailable {
			ReadKey()
			utils.ClearScreen()
			return
		}

		// Create menu and display
		menu := m.createMainMenu()
		menu.Print()

		choice := ReadMenuInput()

		// Handle the special exit case for main menu
		if choice == "q" {
			utils.ClearScreen()
			return
		}

		// Process the menu choice
		exitRequested := m.handleMenuChoice(choice)
		if exitRequested {
			utils.ClearScreen()
			return
		}
	}
}

// createMainMenu creates the main menu with all options
func (m *MainMenu) createMainMenu() *style.Menu {
	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "User Management", Description: "Create, Configure (sudo, SSH keys)"},
		{Number: 2, Title: "SSH Login", Description: "Toggle SSH root access"},
		{Number: 3, Title: "DNS", Description: "Configure Nameservers"},
		{Number: 4, Title: "Firewall", Description: "Configure UFW rules"},
		{Number: 5, Title: "Backup", Description: "Configure Hardn backup settings"},
		{Number: 6, Title: "Dry-Run", Description: "Simulate changes"},
		{Number: 7, Title: "Run All", Description: "Execute hardening operations"},
		// {Number: 7, Title: "Linux Packages", Description: "Install specified Linux packages"},
		// {Number: 8, Title: "Package Sources", Description: "Configure package source"},
		{Number: 8, Title: "Environment", Description: "Configure environment variable"},
		{Number: 9, Title: "System Details", Description: "View system information"},
		{Number: 10, Title: "Logs", Description: "View log file"},
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
		Description: "Press 'q' to exit immediately",
	})

	return menu
}

// handleMenuChoice processes the user's menu selection and returns true if the application should exit
func (m *MainMenu) handleMenuChoice(choice string) bool {
	switch choice {
	case "1": // Sudo User
		userMenu := NewUserMenu(m.menuManager, m.config, m.osInfo)
		userMenu.Show()

	case "2": // Root SSH
		disableRootMenu := NewDisableRootMenu(m.menuManager, m.config, m.osInfo)
		disableRootMenu.Show()

	case "3": // DNS
		dnsMenu := NewDNSMenu(m.menuManager, m.config, m.osInfo)
		dnsMenu.Show()

	case "4": // Firewall
		firewallMenu := NewFirewallMenu(m.menuManager, m.config, m.osInfo)
		firewallMenu.Show()

	case "5": // Backup
		backupMenu := NewBackupMenu(m.menuManager, m.config)
		backupMenu.Show()

	case "6": // Dry-Run
		dryRunHandler := NewDryRunHandler(m.menuManager, m.config)
		dryRunHandler.Handle()

	case "7": // Run All
		runAllHandler := NewRunAllHandler(m.menuManager, m.config, m.osInfo)
		runAllHandler.Handle()

	// case "7": // Linux Packages
	// 	linuxMenu := NewLinuxPackagesMenu(m.menuManager, m.config, m.osInfo)
	// 	linuxMenu.Show()

	// case "8": // Package Sources
	// 	sourcesMenu := NewSourcesMenu(m.menuManager, m.config, m.osInfo)
	// 	sourcesMenu.Show()

	case "8": // Environment
		envMenu := NewEnvironmentSettingsMenu(m.menuManager, m.config)
		envMenu.Show()

	case "9": // Host Info
		systemDetailsMenu := NewSystemDetailsMenu(m.config, m.osInfo, m.menuManager.GetHostInfoManager())
		systemDetailsMenu.Show()

	case "10": // Logs
		logsMenu := NewLogsMenu(m.menuManager, m.config)
		logsMenu.Show()

	case "0": // Exit
		utils.ClearScreen()
		return true

	default:
		utils.PrintHeader()
		fmt.Printf("%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
	}

	return false
}
