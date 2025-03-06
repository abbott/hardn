package menu

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/status"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// Shared reader for all menus
var reader = bufio.NewReader(os.Stdin)

// ReadInput reads a line of input from the user
func ReadInput() string {
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// ReadKey reads a single key pressed by the user
func ReadKey() string {
	// Configure terminal for raw input
	exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	defer exec.Command("stty", "-F", "/dev/tty", "-cbreak").Run()

	// Read the first byte
	var firstByte = make([]byte, 1)
	os.Stdin.Read(firstByte)

	// If it's an escape character (27), read and discard the sequence
	if firstByte[0] == 27 {
		// Read and discard the next two bytes (common for arrow keys)
		var discardBytes = make([]byte, 2)
		os.Stdin.Read(discardBytes)

		// Return empty to indicate a special key was pressed
		return ""
	}

	return string(firstByte)
}


func RiskStatus(symbol string, color string, label string, status string, description string) string {
	padding := strings.Repeat(" ", 6) // "Risk" is short, so hardcode reasonable padding

	return style.Colored(color, symbol) + " " + label +
		padding + style.Bolded(status, color) + " " + style.Dimmed(description)
}

// ShowMainMenu displays the main menu and handles user input
func ShowMainMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	for {
		utils.PrintLogo()

		// Define separator line
		separator := "~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~"
		sepWidth := len(separator)

		securityStatus, err := status.CheckSecurityStatus(cfg, osInfo)
		var riskLevel, riskDescription, riskColor string
		if err == nil {
			riskLevel, riskDescription, riskColor = status.GetSecurityRiskLevel(securityStatus)
		}

		// Prepare OS display information
		var osDisplay string
		if osInfo != nil {
			titleCaser := cases.Title(language.English) // Title case formatter

			// If Proxmox is detected, treat it as the OS
			if osInfo.IsProxmox {
				osDisplay = " Proxmox "
			} else {
				// Convert OS type and codename to title case
				osName := titleCaser.String(osInfo.OsType)
				osCodename := titleCaser.String(osInfo.OsCodename)

				if osInfo.OsType == "alpine" {
					osDisplay = fmt.Sprintf(" %s Linux %s ", osName, osInfo.OsVersion)
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

		fmt.Println()

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
		}, 2) // 2 spaces buffer

		// Format and print risk status using the same formatter, with bold label
		if riskLevel != "" {
			// Use bold for "Risk" label
			boldRiskLabel := style.Bold + "Risk Level" + style.Reset

			riskDescription = style.SymApprox + " " + riskDescription

			// Display risk status with appropriate formatting
			fmt.Println(formatter.FormatLine(style.SymDotTri, riskColor, boldRiskLabel, riskLevel, riskColor, riskDescription, "light"))
		}

		fmt.Println()

		// Display detailed security status if available
		if err == nil {
			// Pass the formatter to the security status display to ensure consistent formatting
			status.DisplaySecurityStatus(cfg, securityStatus, formatter)
		}

		// Display dry-run mode if active
		fmt.Println()

		// Format the dry-run mode status like other status lines
		if cfg.DryRun {
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

		// First check if q is pressed immediately without Enter
		firstKey := ReadKey()
		if firstKey == "q" || firstKey == "Q" {
			fmt.Println("q")
			utils.PrintHeader()
			fmt.Println("Hardn has exited.")
			fmt.Println()
			return
		}

		// If firstKey is empty (like from an arrow key), try reading again
		if firstKey == "" {
			firstKey = ReadKey()
			if firstKey == "" || firstKey == "q" || firstKey == "Q" {
				fmt.Println("q")
				utils.PrintHeader()
				fmt.Println("Hardn has exited.")
				fmt.Println()
				return
			}
		}

		// Read the rest of the line with standard input
		restKey := ReadInput()

		// Combine the inputs for the complete choice
		choice := firstKey + restKey

		// Process the menu choice
		switch choice {
		case "1":
			UserCreationMenu(cfg, osInfo)
		case "2":
			DisableRootMenu(cfg, osInfo)
		case "3":
			ConfigureDnsMenu(cfg, osInfo)
		case "4":
			UfwMenu(cfg, osInfo)
		case "5":
			RunAllHardeningMenu(cfg, osInfo)
		case "6":
			ToggleDryRunMenu(cfg)
		case "7":
			// LinuxPackagesMenu(cfg, osInfo)
		case "8":
			// PythonPackagesMenu(cfg, osInfo)
		case "9":
			UpdateSourcesMenu(cfg, osInfo)
		case "10":
			BackupOptionsMenu(cfg)
		case "11":
			EnvironmentSettingsMenu(cfg)
		case "12":
			ViewLogsMenu(cfg)
		case "13":
			HelpMenu()
		case "0":
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
