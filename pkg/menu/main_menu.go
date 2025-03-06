// pkg/menu/main_menu.go
package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/status"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// MainMenu is the main menu of the application
type MainMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
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
	}
}

// ShowMainMenu displays the main menu and handles user input
func (m *MainMenu) ShowMainMenu() {
	for {
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
			boldRiskLabel := style.Bold + "Risk Level" + style.Reset
			riskDescription = style.SymApprox + " " + riskDescription
			fmt.Println(formatter.FormatLine(style.SymDotTri, riskColor, boldRiskLabel, riskLevel, riskColor, riskDescription, "light"))
		}

		fmt.Println()

		// Display detailed security status if available
		if err == nil {
			status.DisplaySecurityStatus(m.config, securityStatus, formatter)
		}

		// Display dry-run mode if active
		fmt.Println()

		// Format the dry-run mode status like other status lines
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

		// Process the menu choice - using menuManager instead of direct calls
		switch choice {
			case "1": // Sudo User
    // Create submenu for user management
    if m.config.Username != "" {
        err := m.menuManager.CreateUser(m.config.Username, true, m.config.SudoNoPassword, m.config.SshKeys)
        if err != nil {
            fmt.Printf("\n%s Error creating user: %v\n", 
                style.Colored(style.Red, style.SymCrossMark), err)
        } else {
            fmt.Printf("\n%s User created successfully\n", 
                style.Colored(style.Green, style.SymCheckMark))
        }
    } else {
        // Use the existing implementation for now
        UserCreationMenu(m.config, m.osInfo)
    }
		// case "1": // Sudo User
		// 	// Create submenu for user management
		// 	if m.config.Username != "" {
		// 		err := m.menuManager.CreateUser(m.config.Username, true, m.config.SudoNoPassword, m.config.SshKeys)
		// 		if err != nil {
		// 			fmt.Printf("\n%s Error creating user: %v\n", 
		// 				style.Colored(style.Red, style.SymCrossMark), err)
		// 		} else {
		// 			fmt.Printf("\n%s User created successfully\n", 
		// 				style.Colored(style.Green, style.SymCheckMark))
		// 		}
		// 	} else {
		// 		userMenu := NewUserMenu(m.menuManager, m.config, m.osInfo)
		// 		userMenu.Show()
		// 	}
			
		case "2": // Root SSH
			err := m.menuManager.DisableRootSsh()
			if err != nil {
				fmt.Printf("\n%s Error disabling root SSH: %v\n", 
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s Root SSH access disabled successfully\n", 
					style.Colored(style.Green, style.SymCheckMark))
			}
			
		case "3": // DNS
			ConfigureDnsMenu(m.config, m.osInfo)
			// dnsMenu := NewDNSMenu(m.menuManager, m.config, m.osInfo)
			// dnsMenu.Show()
			
		case "4": // Firewall
			UfwMenu(m.config, m.osInfo)
			// firewallMenu := NewFirewallMenu(m.menuManager, m.config, m.osInfo)
			// firewallMenu.Show()
			
		case "5": // Run All
			hardening := model.HardeningConfig{
				CreateUser:     true,
				Username:       m.config.Username,
				SudoNoPassword: m.config.SudoNoPassword,
				SshKeys:        m.config.SshKeys,
				SshPort:        m.config.SshPort,
				SshListenAddresses: []string{m.config.SshListenAddress},
				SshAllowedUsers:    m.config.SshAllowedUsers,
				EnableFirewall:     m.config.EnableUfwSshPolicy,
				ConfigureDns:       m.config.ConfigureDns,
				Nameservers:        m.config.Nameservers,
				EnableAppArmor:     m.config.EnableAppArmor,
				EnableLynis:        m.config.EnableLynis,
				EnableUnattendedUpgrades: m.config.EnableUnattendedUpgrades,
			}
			
			err := m.menuManager.HardenSystem(&hardening)
			if err != nil {
				fmt.Printf("\n%s Error during system hardening: %v\n", 
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s System hardening completed successfully\n", 
					style.Colored(style.Green, style.SymCheckMark))
			}
			
		case "6": // Dry-Run
			// Toggle dry run mode
			m.config.DryRun = !m.config.DryRun
			
			if m.config.DryRun {
				fmt.Printf("\n%s Dry-run mode enabled. Changes will only be simulated.\n",
					style.Colored(style.Green, style.SymCheckMark))
			} else {
				fmt.Printf("\n%s Dry-run mode disabled. Changes will be applied to the system.\n", 
					style.Colored(style.Yellow, style.SymWarning))
			}
			
			// Save config changes
			err := config.SaveConfig(m.config, "hardn.yml")
			if err != nil {
				fmt.Printf("\n%s Error saving configuration: %v\n", 
					style.Colored(style.Red, style.SymCrossMark), err)
			}
			
			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
			
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
			UpdateSourcesMenu(m.config, m.osInfo)
			// sourcesMenu := NewSourcesMenu(m.menuManager, m.config, m.osInfo)
			// sourcesMenu.Show()
			
		case "10": // Backup
			BackupOptionsMenu(m.config)
			// backupMenu := NewBackupMenu(m.menuManager, m.config)
			// backupMenu.Show()
			
		case "11": // Environment
			EnvironmentSettingsMenu(m.config)
			// envMenu := NewEnvironmentMenu(m.menuManager, m.config)
			// envMenu.Show()
			
		case "12": // Logs
			// Viewing logs doesn't need to go through menuManager
			ViewLogsMenu(m.config)
			
		case "13": // Help
			// Help menu doesn't need to go through menuManager
			HelpMenu()
			
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