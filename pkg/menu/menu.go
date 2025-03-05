package menu

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/dns"
	"github.com/abbott/hardn/pkg/firewall"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/packages"
	"github.com/abbott/hardn/pkg/security"
	"github.com/abbott/hardn/pkg/ssh"
	"github.com/abbott/hardn/pkg/status"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/updates"
	"github.com/abbott/hardn/pkg/user"
	"github.com/abbott/hardn/pkg/utils"
)

var reader = bufio.NewReader(os.Stdin)

// readInput reads a line of input from the user
func readInput() string {
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

// Improved readKey function - ignores escape sequences
func readKey() string {
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
		// This will cause the code to just ignore the keypress
		return ""
	}

	return string(firstByte)
}

func RiskStatus(symbol string, color string, label string, status string, description string) string {
	// Use the same spacing style as the other status lines
	// For just one item, we can use a simpler approach
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

		fmt.Println()
		fmt.Println(style.SubHeader("Select an option"))

		fmt.Println(style.Bolded("1) ") + " Sudo User            " + style.Dimmed("Create non-root user with sudo access"))
		fmt.Println(style.Bolded("2) ") + " Root SSH             " + style.Dimmed("Disable SSH access for root user"))
		fmt.Println(style.Bolded("3) ") + " DNS                  " + style.Dimmed("Configure DNS settings"))
		fmt.Println(style.Bolded("4) ") + " Firewall             " + style.Dimmed("Configure UFW rules"))
		fmt.Println(style.Bolded("5) ") + " Run All              " + style.Dimmed("Run all hardening operations"))
		fmt.Println(style.Bolded("6) ") + " Dry-Run              " + style.Dimmed("Preview changes without applying them"))
		fmt.Println(style.Bolded("7) ") + " Linux Packages       " + style.Dimmed("Install specified Linux packages"))
		fmt.Println(style.Bolded("8) ") + " Python Packages      " + style.Dimmed("Install specified Python packages"))
		fmt.Println(style.Bolded("9) ") + " Package Sources      " + style.Dimmed("Configure package source"))
		fmt.Println(style.Bolded("10)") + " Backup               " + style.Dimmed("Configure backup settings"))
		fmt.Println(style.Bolded("11)") + " Environment          " + style.Dimmed("Configure environment variable support"))
		fmt.Println(style.Bolded("12)") + " Logs                 " + style.Dimmed("View log file"))
		fmt.Println(style.Bolded("13)") + " Help                 " + style.Dimmed("View usage information"))

		// Exit option with color
		fmt.Println("\n" + style.Bolded("0) ") + " Exit                 " + style.Dimmed("Tip: Press 'q' to exit immediately"))

		fmt.Print("\n" + style.BulletItem + "Enter your choice [0-12 or q]: ")

		// First check if q is pressed immediately without Enter
		firstKey := readKey()
		if firstKey == "q" || firstKey == "Q" {
			fmt.Println("q")
			utils.PrintHeader()
			// fmt.Println("\033[1;32m#\033[0m Hardn has exited.")
			fmt.Println("Hardn has exited.")
			fmt.Println()
			return
		}

		// If firstKey is empty (like from an arrow key), try reading again
		if firstKey == "" {
			firstKey = readKey()
			if firstKey == "" || firstKey == "q" || firstKey == "Q" {
				fmt.Println("q")
				utils.PrintHeader()
				// fmt.Println("\033[1;32m#\033[0m Hardn has exited.")
				fmt.Println("Hardn has exited.")
				fmt.Println()
				return
			}
		}

		// Read the rest of the line with standard input
		restKey := readInput()

		// Combine the inputs for the complete choice
		choice := firstKey + restKey

		// Process the menu choice
		switch choice {
		case "1":
			userCreationMenu(cfg, osInfo)
		case "2":
			disableRootMenu(cfg, osInfo)
		case "3":
			configureDnsMenu(cfg, osInfo)
		case "4":
			ufwMenu(cfg, osInfo)
		case "5":
			runAllHardeningMenu(cfg, osInfo)
		case "6":
			toggleDryRunMenu(cfg)
		case "7":
			linuxPackagesMenu(cfg, osInfo)
		case "8":
			pythonPackagesMenu(cfg, osInfo)
		case "9":
			updateSourcesMenu(cfg, osInfo)
		case "10":
			backupOptionsMenu(cfg)
		case "11":
			environmentSettingsMenu(cfg)
		case "12":
			viewLogsMenu(cfg)
		case "13":
			helpMenu()
		case "0":
			utils.PrintHeader()
			fmt.Println("\033[1;32m#\033[0m Hardn has exited.")
			fmt.Println()
			return
		default:
			utils.PrintHeader()
			fmt.Println("\033[1;31m#\033[0m Invalid option. Please try again.")
			fmt.Println()
			fmt.Println("# Press any key to continue...")
			readKey()
		}
	}
}

// Toggle dry-run mode menu
func toggleDryRunMenu(cfg *config.Config) {
	utils.PrintHeader()
	fmt.Println()
	// Create a formatter with just the label we need
	formatter := style.NewStatusFormatter([]string{"Dry-run Mode"}, 2)

	fmt.Println(style.SubHeader("Select an option"))

	if cfg.DryRun {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Enabled", style.Green, "", "bold"))
		fmt.Println("\n" + style.SymDash + " In this mode, the script will preview changes without applying them.")
		fmt.Print("\n" + style.SymArrowRight + " Would you like to disable dry-run mode? (y/n): ")
		choice := readInput()

		if choice == "y" || choice == "Y" {
			cfg.DryRun = false
			fmt.Println("\n" + formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Disabled", style.Yellow, "", "bold"))
			// fmt.Println("\n" + "Dry-run mode has been " + style.Bolded("Disabled", style.Yellow) + " - proceed with caution.")
		} else {
			fmt.Println("\n" + "Dry-run mode remains " + style.Bolded("Enabled", style.Green) + " - have fun!")
		}
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Disabled", style.Yellow, "", "bold"))
		// fmt.Println("\n" + style.Bolded("CAUTION", style.Yellow) + " - changes will be applied to the system.")
		fmt.Println(style.Dimmed("  Enable dry-run mode to preview changes without applying them."))
		fmt.Print("\n" + style.SymArrowRight + " Would you like to enable dry-run mode? (y/n): ")
		choice := readInput()

		if choice == "y" || choice == "Y" {
			cfg.DryRun = true
			// fmt.Println("\n" + style.SymInfo + "Dry-run Mode" + style.Bolded("Enabled", style.Green) + " - have fun!")
			fmt.Println("\n" + formatter.FormatLine(style.SymInfo, style.BrightCyan, "Dry-run Mode", "Enabled", style.Green, "", "bold"))
			fmt.Println(style.Dimmed("  You can now simulate enabling security measures."))
		} else {
			fmt.Println("\n" + "Dry-run remains " + style.Bolded("Disabled", style.Yellow) + " - proceed with caution.")
		}
	}

	// Save config changes
	configFile := "hardn.yml" // Default config file
	if err := config.SaveConfig(cfg, configFile); err != nil {
		logging.LogError("Failed to save configuration: %v", err)
	}

	fmt.Print("\n" + style.SymArrowRight + " Press any key to return to the main menu...")
	readKey()
}

// Function to display menu for user creation
func userCreationMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m User Creation Menu")

	// Prompt for username if not provided
	username := cfg.Username
	if username == "" {
		fmt.Print("\n\033[39m#\033[0m Enter username to create: ")
		username = readInput()
	}

	if username == "" {
		fmt.Println("\n\033[1;31m#\033[0m No username provided. Returning to main menu...")
		fmt.Print("\n\033[39m#\033[0m Press any key to continue...")
		readKey()
		return
	}

	fmt.Printf("\n\033[39m#\033[0m Creating user: %s\n", username)

	// Create user
	if err := user.CreateUser(username, cfg, osInfo); err != nil {
		logging.LogError("Failed to create user: %v", err)
	}

	// Configure SSH
	if err := ssh.WriteSSHConfig(cfg, osInfo); err != nil {
		logging.LogError("Failed to configure SSH: %v", err)
	}

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to display menu for disabling root SSH access
func disableRootMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Disable Root SSH Access")

	fmt.Println("\n\033[1;33m#\033[0m WARNING: This will disable SSH access for the root user!")
	fmt.Println("\033[1;33m#\033[0m Make sure you have another user with sudo privileges.")
	fmt.Print("\n\033[39m#\033[0m Do you want to continue? (y/n): ")
	choice := readInput()

	if choice == "y" || choice == "Y" {
		err := ssh.DisableRootSSHAccess(cfg, osInfo)
		if err == nil {
			fmt.Println("\n\033[1;32m#\033[0m Root SSH access has been disabled.")

			// Restart SSH service
			if osInfo.OsType == "alpine" {
				exec.Command("rc-service", "sshd", "restart").Run()
			} else {
				exec.Command("systemctl", "restart", "ssh").Run()
			}
		} else {
			fmt.Printf("\n\033[1;31m#\033[0m Failed to disable root SSH access: %v\n", err)
		}
	} else {
		fmt.Println("\n\033[39m#\033[0m Operation cancelled.")
	}

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to display menu for installing Linux packages
func linuxPackagesMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Linux Packages Installation")
	fmt.Println()

	if osInfo.OsType == "alpine" {
		fmt.Println("\n\033[39m#\033[0m Installing Alpine Linux packages...")

		// Install core Alpine packages first
		if len(cfg.AlpineCorePackages) > 0 {
			logging.LogInfo("Installing Alpine core packages...")
			packages.InstallPackages(cfg.AlpineCorePackages, osInfo, cfg)
		}

		// Check subnet to determine which package sets to install
		isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
		if isDmz {
			if len(cfg.AlpineDmzPackages) > 0 {
				logging.LogInfo("Installing Alpine DMZ packages...")
				packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
			}
		} else {
			// Install both
			if len(cfg.AlpineDmzPackages) > 0 {
				logging.LogInfo("Installing Alpine DMZ packages...")
				packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
			}
			if len(cfg.AlpineLabPackages) > 0 {
				logging.LogInfo("Installing Alpine LAB packages...")
				packages.InstallPackages(cfg.AlpineLabPackages, osInfo, cfg)
			}
		}
	} else {
		// Install core Linux packages first
		if len(cfg.LinuxCorePackages) > 0 {
			logging.LogInfo("Installing Linux core packages...")
			packages.InstallPackages(cfg.LinuxCorePackages, osInfo, cfg)
		}

		// Check subnet to determine which package sets to install
		isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
		if isDmz {
			if len(cfg.LinuxDmzPackages) > 0 {
				logging.LogInfo("Installing Debian DMZ packages...")
				packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
			}
		} else {
			// Install both
			if len(cfg.LinuxDmzPackages) > 0 {
				logging.LogInfo("Installing Debian DMZ packages...")
				packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
			}
			if len(cfg.LinuxLabPackages) > 0 {
				logging.LogInfo("Installing Debian Lab packages...")
				packages.InstallPackages(cfg.LinuxLabPackages, osInfo, cfg)
			}
		}
	}

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to display menu for installing Python packages
func pythonPackagesMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Python Packages Installation")
	fmt.Println()

	// Display current Python package management settings
	fmt.Println("\n\033[39m#\033[0m Current Python Package Management Settings:")
	if cfg.UseUvPackageManager {
		fmt.Println("\033[39m#\033[0m Using UV Package Manager: \033[1;32mEnabled\033[0m")
	} else {
		fmt.Println("\033[39m#\033[0m Using UV Package Manager: \033[1;31mDisabled\033[0m (using standard pip)")
	}

	// Allow toggling UV package manager setting
	fmt.Print("\n\033[39m#\033[0m Would you like to toggle UV package manager? (y/n): ")
	choice := readInput()

	if choice == "y" || choice == "Y" {
		if cfg.UseUvPackageManager {
			cfg.UseUvPackageManager = false
			fmt.Println("\n\033[1;32m#\033[0m UV package manager has been disabled. Will use standard pip.")
		} else {
			cfg.UseUvPackageManager = true
			fmt.Println("\n\033[1;32m#\033[0m UV package manager has been enabled. Will use UV for Python packages.")
		}

		// Save config changes
		configFile := "hardn.yml" // Default config file
		if err := config.SaveConfig(cfg, configFile); err != nil {
			logging.LogError("Failed to save configuration: %v", err)
		}
	}

	// Install packages
	if err := packages.InstallPythonPackages(cfg, osInfo); err != nil {
		logging.LogError("Failed to install Python packages: %v", err)
	} else {
		fmt.Println("\n\033[1;32m#\033[0m Python packages installed successfully!")
	}

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to display menu for configuring UFW
func ufwMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m UFW Firewall Configuration")

	fmt.Println("\n\033[1;33m#\033[0m WARNING: This will configure and enable the UFW firewall!")
	fmt.Printf("\033[1;33m#\033[0m SSH access will be allowed on port %d.\n", cfg.SshPort)
	fmt.Print("\n\033[39m#\033[0m Do you want to continue? (y/n): ")
	choice := readInput()

	if choice == "y" || choice == "Y" {
		if err := firewall.ConfigureUFW(cfg, osInfo); err != nil {
			logging.LogError("Failed to configure UFW: %v", err)
		} else {
			fmt.Println("\n\033[1;32m#\033[0m UFW configured and enabled successfully!")
		}
	} else {
		fmt.Println("\n\033[39m#\033[0m Operation cancelled.")
	}

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to display menu for configuring DNS
func configureDnsMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Configure DNS Settings")

	fmt.Println("\n\033[1;33m#\033[0m WARNING: This will configure DNS settings!")
	fmt.Print("\n\033[39m#\033[0m Do you want to continue? (y/n): ")
	choice := readInput()

	if choice == "y" || choice == "Y" {
		fmt.Println("")
		if err := dns.ConfigureDNS(cfg, osInfo); err != nil {
			logging.LogError("Failed to configure DNS: %v", err)
			fmt.Println("\n\033[1;31m#\033[0m Failed to configure DNS settings.")
		} else {
			fmt.Println("\n\033[1;32m#\033[0m DNS settings configured successfully.")
		}
	} else {
		fmt.Println("\n\033[39m#\033[0m Operation cancelled.")
	}

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to update package sources
func updateSourcesMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Update Package Sources")

	if err := packages.WriteSources(cfg, osInfo); err != nil {
		logging.LogError("Failed to write package sources: %v", err)
	}

	if osInfo.OsType != "alpine" && osInfo.IsProxmox {
		if err := packages.WriteProxmoxRepos(cfg, osInfo); err != nil {
			logging.LogError("Failed to write Proxmox repositories: %v", err)
		}
	}

	fmt.Println("\n\033[1;32m#\033[0m Package sources updated successfully!")

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to run all hardening steps
func runAllHardeningMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Run All Hardening Steps")

	fmt.Println("\n\033[1;33m#\033[0m WARNING: This will run all hardening steps!")
	if cfg.DryRun {
		fmt.Println("\033[1;32m#\033[0m Dry-run mode is enabled. No actual changes will be made.")
	} else {
		fmt.Println("\033[1;31m#\033[0m Dry-run mode is disabled. System will be modified!")
	}
	fmt.Print("\n\033[39m#\033[0m Do you want to continue? (y/n): ")
	choice := readInput()

	if choice == "y" || choice == "Y" {
		utils.PrintLogo()
		logging.LogInfo("Running complete system hardening...")

		// Setup basic configuration
		utils.SetupHushlogin(cfg)

		// Update package repositories
		packages.WriteSources(cfg, osInfo)
		if osInfo.OsType != "alpine" && osInfo.IsProxmox {
			packages.WriteProxmoxRepos(cfg, osInfo)
		}

		// Install packages
		if osInfo.OsType == "alpine" {
			// Install Alpine packages
			if len(cfg.AlpineCorePackages) > 0 {
				logging.LogInfo("Installing Alpine core packages...")
				packages.InstallPackages(cfg.AlpineCorePackages, osInfo, cfg)
			}

			// Check subnet to determine which package sets to install
			isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
			if isDmz {
				if len(cfg.AlpineDmzPackages) > 0 {
					logging.LogInfo("Installing Alpine DMZ packages...")
					packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
				}
			} else {
				if len(cfg.AlpineDmzPackages) > 0 {
					logging.LogInfo("Installing Alpine DMZ packages...")
					packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
				}

				if len(cfg.AlpineLabPackages) > 0 {
					logging.LogInfo("Installing Alpine LAB packages...")
					packages.InstallPackages(cfg.AlpineLabPackages, osInfo, cfg)
				}
			}

			// Install Python packages if defined
			if len(cfg.AlpinePythonPackages) > 0 {
				logging.LogInfo("Installing Alpine Python packages...")
				packages.InstallPackages(cfg.AlpinePythonPackages, osInfo, cfg)
			}
		} else {
			// Install core Linux packages first
			if len(cfg.LinuxCorePackages) > 0 {
				logging.LogInfo("Installing Linux core packages...")
				packages.InstallPackages(cfg.LinuxCorePackages, osInfo, cfg)
			}

			// Check subnet to determine which package sets to install
			isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
			if isDmz {
				if len(cfg.LinuxDmzPackages) > 0 {
					logging.LogInfo("Installing Debian DMZ packages...")
					packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
				}
			} else {
				// Install both
				if len(cfg.LinuxDmzPackages) > 0 {
					logging.LogInfo("Installing Debian DMZ packages...")
					packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
				}
				if len(cfg.LinuxLabPackages) > 0 {
					logging.LogInfo("Installing Debian Lab packages...")
					packages.InstallPackages(cfg.LinuxLabPackages, osInfo, cfg)
				}
			}
		}

		// Create non-root user with sudo access if USERNAME is set
		if cfg.Username != "" {
			user.CreateUser(cfg.Username, cfg, osInfo)
		}

		// Configure SSH
		ssh.WriteSSHConfig(cfg, osInfo)

		// Disable root SSH access if requested
		if cfg.DisableRoot {
			ssh.DisableRootSSHAccess(cfg, osInfo)
		}

		// Configure UFW
		if cfg.EnableUfwSshPolicy {
			firewall.ConfigureUFW(cfg, osInfo)
		}

		// Configure DNS
		if cfg.ConfigureDns {
			dns.ConfigureDNS(cfg, osInfo)
		}

		// Setup AppArmor if enabled
		if cfg.EnableAppArmor {
			security.SetupAppArmor(cfg, osInfo)
		}

		// Setup Lynis if enabled
		if cfg.EnableLynis {
			security.SetupLynis(cfg, osInfo)
		}

		// Setup unattended upgrades if enabled
		if cfg.EnableUnattendedUpgrades {
			updates.SetupUnattendedUpgrades(cfg, osInfo)
		}

		fmt.Println("\n\033[1;32m#\033[0m System hardening completed successfully!")
		fmt.Printf("\033[1;34m#\033[0m Check the log file at %s for details.\n", cfg.LogFile)
	} else {
		fmt.Println("\n\033[39m#\033[0m Operation cancelled.")
	}

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

func environmentSettingsMenu(cfg *config.Config) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Environment Variable Settings")

	// Check if HARDN_CONFIG is set
	configEnv := os.Getenv("HARDN_CONFIG")
	if configEnv != "" {
		fmt.Printf("\n\033[39m#\033[0m Current HARDN_CONFIG: \033[1;32m%s\033[0m\n", configEnv)
	} else {
		fmt.Println("\n\033[39m#\033[0m HARDN_CONFIG environment variable is not set")
	}

	// Check sudo preservation status
	sudoPreservation := checkSudoEnvPreservation()
	if sudoPreservation {
		fmt.Println("\033[39m#\033[0m Sudo preservation: \033[1;32mEnabled\033[0m")
	} else {
		fmt.Println("\033[39m#\033[0m Sudo preservation: \033[1;31mDisabled\033[0m")
	}

	fmt.Println("\n\033[39m#\033[0m Select an option:")
	fmt.Println("\033[1;36m#\033[0m 1) Setup sudo environment preservation")
	fmt.Println("\033[1;36m#\033[0m 2) Show environment variables guide")
	fmt.Println("\033[1;36m#\033[0m 0) Return to main menu")

	fmt.Print("\n\033[39m#\033[0m Enter your choice [0-2]: ")
	choice := readInput()

	switch choice {
	case "1":
		// Run sudo env setup
		fmt.Println("\n\033[39m#\033[0m Setting up sudo environment preservation...")

		// Check if running as root
		if os.Geteuid() != 0 {
			fmt.Println("\n\033[1;31m#\033[0m This operation requires sudo privileges.")
			fmt.Println("\033[39m#\033[0m Please run: sudo hardn setup-sudo-env")
		} else {
			err := utils.SetupSudoEnvPreservation()
			if err != nil {
				fmt.Printf("\n\033[1;31m#\033[0m Failed to configure sudo: %v\n", err)
			}
		}

		fmt.Print("\n\033[39m#\033[0m Press any key to continue...")
		readKey()
		environmentSettingsMenu(cfg)

	case "2":
		// Show environment guide
		utils.PrintHeader()
		fmt.Println("\033[1;34m#\033[0m Environment Variables Guide")
		fmt.Println("\n\033[39m#\033[0m HARDN_CONFIG Environment Variable")
		fmt.Println("\033[39m#\033[0m ------------------------------------")
		fmt.Println("\033[39m#\033[0m Set this variable to specify a custom config file location:")
		fmt.Println("\033[39m#\033[0m   export HARDN_CONFIG=/path/to/your/config.yml")

		fmt.Println("\n\033[39m#\033[0m Using with sudo")
		fmt.Println("\033[39m#\033[0m ------------------------------------")
		fmt.Println("\033[39m#\033[0m To preserve the variable when using sudo, run:")
		fmt.Println("\033[39m#\033[0m   sudo hardn setup-sudo-env")

		fmt.Println("\n\033[39m#\033[0m For persistent configuration:")
		fmt.Println("\033[39m#\033[0m   echo 'export HARDN_CONFIG=/path/to/config.yml' >> ~/.bashrc")

		fmt.Print("\n\033[39m#\033[0m Press any key to continue...")
		readKey()
		environmentSettingsMenu(cfg)

	case "0":
		return

	default:
		fmt.Println("\n\033[1;31m#\033[0m Invalid option. Please try again.")
		fmt.Print("\n\033[39m#\033[0m Press any key to continue...")
		readKey()
		environmentSettingsMenu(cfg)
	}
}

// Helper function to check if sudo preservation is enabled
// Helper function to check if sudo preservation is enabled
func checkSudoEnvPreservation() bool {
	// First check for SUDO_USER which is the original user when using sudo
	username := os.Getenv("SUDO_USER")

	// If that's empty, fall back to USER
	if username == "" {
		username = os.Getenv("USER")

		// If that's still empty, try to get username another way
		if username == "" {
			currentUser, err := osuser.Current()
			if err != nil {
				return false
			}
			username = currentUser.Username
		}
	}

	// Check if sudoers file exists
	sudoersFile := filepath.Join("/etc/sudoers.d", username)
	if _, err := os.Stat(sudoersFile); os.IsNotExist(err) {
		return false
	}

	// Check file content
	data, err := os.ReadFile(sudoersFile)
	if err != nil {
		return false
	}

	return strings.Contains(string(data), "env_keep += \"HARDN_CONFIG\"")
}

// Function to view logs
func viewLogsMenu(cfg *config.Config) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m View Logs")

	logging.PrintLogs(cfg.LogFile)

	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}

// Function to display backup options menu
func backupOptionsMenu(cfg *config.Config) {
	utils.PrintHeader()
	fmt.Println("\033[1;34m#\033[0m Backup Settings")

	fmt.Println("\n\033[39m#\033[0m Current backup settings:")
	fmt.Printf("\033[39m#\033[0m Backups enabled: %t\n", cfg.EnableBackups)
	fmt.Printf("\033[39m#\033[0m Backup path: %s\n", cfg.BackupPath)

	fmt.Println("\n\033[39m#\033[0m Select an option:")
	fmt.Println("\033[1;36m#\033[0m 1) Toggle backups (currently: ", cfg.EnableBackups, ")")
	fmt.Println("\033[1;36m#\033[0m 2) Change backup path (currently: ", cfg.BackupPath, ")")
	fmt.Println("\033[1;36m#\033[0m 0) Return to main menu")

	fmt.Print("\n\033[39m#\033[0m Enter your choice [0-2]: ")
	choiceStr := readInput()
	choice, _ := strconv.Atoi(choiceStr)

	switch choice {
	case 1:
		if cfg.EnableBackups {
			cfg.EnableBackups = false
			fmt.Println("\n\033[1;32m#\033[0m Backups have been disabled.")
		} else {
			cfg.EnableBackups = true
			fmt.Println("\n\033[1;32m#\033[0m Backups have been enabled.")
		}
		// Save config changes
		configFile := "hardn.yml" // Default config file
		if err := config.SaveConfig(cfg, configFile); err != nil {
			logging.LogError("Failed to save configuration: %v", err)
		}
		fmt.Print("\n\033[39m#\033[0m Press any key to continue...")
		readKey()
		backupOptionsMenu(cfg)
	case 2:
		fmt.Printf("\n\033[39m#\033[0m Enter new backup path [%s]: ", cfg.BackupPath)
		newPath := readInput()

		if newPath != "" {
			cfg.BackupPath = newPath
			fmt.Printf("\n\033[1;32m#\033[0m Backup path updated to: %s\n", cfg.BackupPath)
			// Save config changes
			configFile := "hardn.yml" // Default config file
			if err := config.SaveConfig(cfg, configFile); err != nil {
				logging.LogError("Failed to save configuration: %v", err)
			}
		} else {
			fmt.Println("\n\033[39m#\033[0m Backup path unchanged.")
		}
		fmt.Print("\n\033[39m#\033[0m Press any key to continue...")
		readKey()
		backupOptionsMenu(cfg)
	case 0:
		return
	default:
		fmt.Println("\n\033[1;31m#\033[0m Invalid option. Please try again.")
		fmt.Print("\n\033[39m#\033[0m Press any key to continue...")
		readKey()
		backupOptionsMenu(cfg)
	}
}

// Help menu
func helpMenu() {
	utils.PrintLogo()
	fmt.Println(style.Bolded("~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~", style.BrightGreen))
	fmt.Print(`
  Tool usage:
  hardn [options]

  Command line options:
    -f, --config-file string  Configuration file path
    -u, --username string     Specify username to create
    -c, --create-user         Create user
    -d, --disable-root        Disable root SSH access
    -g, --configure-dns       Configure DNS resolvers
    -w, --configure-ufw       Configure UFW
    -r, --run-all             Run all hardening operations
    -n, --dry-run             Preview changes without applying them
    -l, --install-linux       Install specified Linux packages
    -i, --install-python      Install specifiedPython packages
    -a, --install-all         Install all specified packages
    -s, --configure-sources   Configure package sources
    -p, --print-logs          View logs
    -h, --help                View usage information

`)

	fmt.Println()
	fmt.Print("\n\033[39m#\033[0m Press any key to return to the main menu...")
	readKey()
}
