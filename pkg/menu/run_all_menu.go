// pkg/menu/run_all_menu.go
package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// RunAllMenu handles the "Run All Hardening" functionality through the new architecture
type RunAllMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewRunAllMenu creates a new RunAllMenu
func NewRunAllMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *RunAllMenu {
	return &RunAllMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the Run All menu and handles user input
func (m *RunAllMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Run All Hardening Steps", style.Blue))

	// Create a formatter for status
	formatter := style.NewStatusFormatter([]string{"Dry-Run Mode", "Username", "SSH Port"}, 2)

	// Display current configuration status
	fmt.Println()
	fmt.Println(style.Bolded("Current Configuration:", style.Blue))

	// Show dry-run status
	if m.config.DryRun {
		fmt.Println(formatter.FormatSuccess("Dry-Run Mode", "Enabled", "No actual changes will be made"))
	} else {
		fmt.Println(formatter.FormatWarning("Dry-Run Mode", "Disabled", "System will be modified!"))
	}

	// Show username (or warn if not set)
	if m.config.Username != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Username", m.config.Username, style.Cyan, "", "light"))
	} else {
		fmt.Println(formatter.FormatWarning("Username", "Not set", "User creation will be skipped"))
	}

	// Show SSH port
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "SSH Port", fmt.Sprintf("%d", m.config.SshPort),
		style.Cyan, "", "light"))

	// Show enabled features
	fmt.Println()
	fmt.Println(style.Bolded("Enabled Features:", style.Blue))

	featuresTable := []struct {
		name    string
		enabled bool
		desc    string
	}{
		{"AppArmor", m.config.EnableAppArmor, "Application control system"},
		{"Lynis", m.config.EnableLynis, "Security audit tool"},
		{"Unattended Upgrades", m.config.EnableUnattendedUpgrades, "Automatic security updates"},
		{"UFW SSH Policy", m.config.EnableUfwSshPolicy, "Firewall rules for SSH"},
		{"DNS Configuration", m.config.ConfigureDns, "DNS settings"},
		{"Root SSH Disable", m.config.DisableRoot, "Disable root SSH access"},
	}

	featuresFormatter := style.NewStatusFormatter([]string{"Feature"}, 2)
	for _, feature := range featuresTable {
		if feature.enabled {
			fmt.Println(featuresFormatter.FormatSuccess("Feature: "+feature.name, "Enabled", feature.desc))
		} else {
			fmt.Println(featuresFormatter.FormatLine(style.SymInfo, style.Yellow, "Feature: "+feature.name,
				"Disabled", style.Yellow, feature.desc, "light"))
		}
	}

	// Security warning
	fmt.Println()
	fmt.Println(style.Bolded("SECURITY WARNING:", style.Red))
	fmt.Println(style.Colored(style.Yellow, "This will run ALL hardening steps on your system."))
	if !m.config.DryRun {
		fmt.Println(style.Colored(style.Red, "Your system will be modified. This cannot be undone."))
	} else {
		fmt.Println(style.Colored(style.Green, "Dry-run mode is enabled. Changes will only be simulated."))
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Run all hardening steps", Description: "Execute all configured hardening measures"},
	}

	// Add dry-run toggle option
	if m.config.DryRun {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2,
			Title:       "Disable dry-run mode and run",
			Description: "Apply real changes to the system",
		})
	} else {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2,
			Title:       "Enable dry-run mode and run",
			Description: "Simulate changes without applying them",
		})
	}

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to main menu",
		Description: "Cancel operation",
	})

	// Display menu
	menu.Print()

	choice := ReadInput()

	switch choice {
	case "1":
		// Run with current settings
		m.runAllHardening()
	case "2":
		// Toggle dry-run mode and run
		m.config.DryRun = !m.config.DryRun
		if m.config.DryRun {
			fmt.Printf("\n%s Dry-run mode has been %s\n",
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("enabled", style.Green))
		} else {
			fmt.Printf("\n%s Dry-run mode has been %s\n",
				style.Colored(style.Yellow, style.SymWarning),
				style.Bolded("disabled", style.Yellow))
			fmt.Println(style.Bolded("CAUTION: ", style.Red) +
				style.Bolded("Your system will be modified!", style.Yellow))
		}

		// Confirm before proceeding with actual changes
		if !m.config.DryRun {
			fmt.Print("\nType 'yes' to confirm you want to apply real changes: ")
			confirm := ReadInput()
			if strings.ToLower(confirm) != "yes" {
				fmt.Printf("\n%s Operation cancelled. No changes were made.\n",
					style.Colored(style.Yellow, style.SymInfo))
				fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
				ReadKey()
				return
			}
		}

		// Save config changes
		configFile := "hardn.yml" // Default config file
		if err := config.SaveConfig(m.config, configFile); err != nil {
			fmt.Printf("\n%s Failed to save configuration: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
		}

		// Run with new dry-run setting
		m.runAllHardening()
	case "0":
		fmt.Println("\nOperation cancelled. No changes were made.")
		return
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.Show()
		return
	}

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

	// runAllHardening uses the MenuManager to execute all hardening steps
func (m *RunAllMenu) runAllHardening() {
	utils.PrintLogo()
	fmt.Println(style.Bolded("Executing All Hardening Steps", style.Blue))

	// Build a comprehensive HardeningConfig from current configuration
	hardening := model.HardeningConfig{
		CreateUser:              	m.config.Username != "",
		Username:                	m.config.Username,
		SudoNoPassword:          	m.config.SudoNoPassword,
		SshKeys:                 	m.config.SshKeys,
		SshPort:                 	m.config.SshPort,
		SshListenAddresses:      	[]string{m.config.SshListenAddress},
		SshAllowedUsers:         	m.config.SshAllowedUsers,
		EnableFirewall:          	m.config.EnableUfwSshPolicy,
		AllowedPorts:            	m.config.UfwAllowedPorts,
		ConfigureDns:            	m.config.ConfigureDns,
		Nameservers:             	m.config.Nameservers,
		EnableAppArmor:          	m.config.EnableAppArmor,
		EnableLynis:             	m.config.EnableLynis,
		EnableUnattendedUpgrades: m.config.EnableUnattendedUpgrades,
	}

	// Track progress with step counting
	totalSteps := calculateTotalSteps(&hardening)
	currentStep := 0

	// Function to show progress
	showProgress := func(stepName string) {
		currentStep++
		fmt.Printf("\n%s [%d/%d] %s\n",
			style.Colored(style.Cyan, style.SymArrowRight),
			currentStep,
			totalSteps,
			style.Bolded(stepName, style.Cyan))
	}

	// Begin hardening steps
	showProgress("Preparing system hardening")
	
	if m.config.DryRun {
		// In dry-run mode, show what would happen
			// updateRepositories := true
	// installPackages := true
		useUvPackageManager := m.config.UseUvPackageManager
    dryRunHardening(&hardening, showProgress, m.osInfo.IsProxmox, useUvPackageManager)
} else {
		// Execute the hardening through the MenuManager
		err := m.menuManager.HardenSystem(&hardening)
		
		if err != nil {
			fmt.Printf("\n%s System hardening failed: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
			return
		}
		
		// Show steps completed when not in dry-run mode
		if hardening.CreateUser {
			showProgress("User account configured")
		}
		
		showProgress("SSH configuration completed")
		
		if hardening.EnableFirewall {
			showProgress("Firewall configured")
		}
		
		if hardening.ConfigureDns {
			showProgress("DNS settings applied")
		}
		
		if hardening.EnableAppArmor {
			showProgress("AppArmor configured")
		}
		
		if hardening.EnableLynis {
			showProgress("Lynis security audit completed")
		}
		
		if hardening.EnableUnattendedUpgrades {
			showProgress("Automatic updates configured")
		}
	}

	// Final status
	fmt.Println()
	if m.config.DryRun {
		fmt.Printf("%s System hardening %s (DRY-RUN)\n",
			style.Colored(style.Green, style.SymCheckMark),
			style.Bolded("simulation completed", style.Green))
		fmt.Println(style.Dimmed("No actual changes were made to your system."))
	} else {
		fmt.Printf("%s System hardening %s\n",
			style.Colored(style.Green, style.SymCheckMark),
			style.Bolded("completed successfully", style.Green))
	}

	fmt.Printf("\n%s Check the log file at %s for details\n",
		style.Colored(style.Cyan, style.SymInfo),
		style.Colored(style.Cyan, m.config.LogFile))
}

// calculateTotalSteps determines the total number of hardening steps
func calculateTotalSteps(config *model.HardeningConfig) int {
	// Start with base steps (always performed)
	totalSteps := 7 // Preparation, repositories, packages, Python packages, SSH config, completion
	
	// Add optional steps
	if config.CreateUser {
			totalSteps++
	}
	
	if config.EnableFirewall {
			totalSteps++
	}
	
	if config.ConfigureDns {
			totalSteps++
	}
	
	if config.EnableAppArmor {
			totalSteps++
	}
	
	if config.EnableLynis {
			totalSteps++
	}
	
	if config.EnableUnattendedUpgrades {
			totalSteps++
	}
	
	return totalSteps
}

// dryRunHardening simulates the hardening process without making changes
func dryRunHardening(config *model.HardeningConfig, showProgress func(string), isProxmox bool, useUvPackageManager bool) {
	// Simulate user creation
	if config.CreateUser {
			showProgress("Simulating user account creation")
			fmt.Printf("%s Would create user '%s' with sudo %s\n", 
					style.BulletItem, 
					config.Username,
					map[bool]string{true: "without password", false: "with password"}[config.SudoNoPassword])
			
			if len(config.SshKeys) > 0 {
					fmt.Printf("%s Would configure %d SSH keys\n", 
							style.BulletItem, 
							len(config.SshKeys))
			}
	}
	
	// Simulate package repository update
	showProgress("Simulating package repository update")
	fmt.Printf("%s Would update package sources for system\n", style.BulletItem)
	
	if isProxmox {
			fmt.Printf("%s Would configure Proxmox-specific repositories\n", style.BulletItem)
	}
	
	// Simulate package installation
	showProgress("Simulating package installation")
	fmt.Printf("%s Would install core system packages\n", style.BulletItem)
	
	// Check if DMZ subnet is detected (this is a simulation)
	fmt.Printf("%s Would determine network environment (DMZ vs. Lab)\n", style.BulletItem)
	fmt.Printf("%s Would install appropriate packages for environment\n", style.BulletItem)
	
	// Simulate Python package installation
	showProgress("Simulating Python package installation")
	packageManager := "pip"
	if useUvPackageManager {
			packageManager = "UV"
	}
	fmt.Printf("%s Would install Python packages with %s\n",
			style.BulletItem,
			packageManager)
	
	// Simulate SSH configuration
	showProgress("Simulating SSH configuration")
	fmt.Printf("%s Would configure SSH on port %d\n", 
			style.BulletItem, 
			config.SshPort)
	
	// Simulate firewall configuration
	if config.EnableFirewall {
			showProgress("Simulating firewall configuration")
			fmt.Printf("%s Would configure firewall to allow SSH on port %d\n", 
					style.BulletItem, 
					config.SshPort)
	}
	
	// Simulate DNS configuration
	if config.ConfigureDns {
			showProgress("Simulating DNS configuration")
			if len(config.Nameservers) > 0 {
					fmt.Printf("%s Would configure nameservers: %s\n", 
							style.BulletItem, 
							strings.Join(config.Nameservers, ", "))
			}
	}
	
	// Simulate AppArmor setup
	if config.EnableAppArmor {
			showProgress("Simulating AppArmor configuration")
			fmt.Printf("%s Would install and activate AppArmor\n", style.BulletItem)
	}
	
	// Simulate Lynis installation
	if config.EnableLynis {
			showProgress("Simulating Lynis security audit")
			fmt.Printf("%s Would install and run Lynis security audit\n", style.BulletItem)
	}
	
	// Simulate unattended upgrades setup
	if config.EnableUnattendedUpgrades {
			showProgress("Simulating automatic updates configuration")
			fmt.Printf("%s Would configure unattended security updates\n", style.BulletItem)
	}
}
