// pkg/menu/run_all.go

package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/dns"
	"github.com/abbott/hardn/pkg/firewall"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/packages"
	"github.com/abbott/hardn/pkg/security"
	"github.com/abbott/hardn/pkg/ssh"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/updates"
	"github.com/abbott/hardn/pkg/user"
	"github.com/abbott/hardn/pkg/utils"
)

// RunAllHardeningMenu handles running all hardening steps
func RunAllHardeningMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Run All Hardening Steps", style.Blue))

	// Create a formatter for status
	formatter := style.NewStatusFormatter([]string{"Dry-Run Mode", "Username", "SSH Port"}, 2)

	// Display current configuration status
	fmt.Println()
	fmt.Println(style.Bolded("Current Configuration:", style.Blue))

	// Show dry-run status
	if cfg.DryRun {
		fmt.Println(formatter.FormatSuccess("Dry-Run Mode", "Enabled", "No actual changes will be made"))
	} else {
		fmt.Println(formatter.FormatWarning("Dry-Run Mode", "Disabled", "System will be modified!"))
	}

	// Show username (or warn if not set)
	if cfg.Username != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Username", cfg.Username, style.Cyan, "", "light"))
	} else {
		fmt.Println(formatter.FormatWarning("Username", "Not set", "User creation will be skipped"))
	}

	// Show SSH port
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "SSH Port", fmt.Sprintf("%d", cfg.SshPort), 
		style.Cyan, "", "light"))

	// Show enabled features
	fmt.Println()
	fmt.Println(style.Bolded("Enabled Features:", style.Blue))

	featuresTable := []struct {
		name    string
		enabled bool
		desc    string
	}{
		{"AppArmor", cfg.EnableAppArmor, "Application control system"},
		{"Lynis", cfg.EnableLynis, "Security audit tool"},
		{"Unattended Upgrades", cfg.EnableUnattendedUpgrades, "Automatic security updates"},
		{"UFW SSH Policy", cfg.EnableUfwSshPolicy, "Firewall rules for SSH"},
		{"DNS Configuration", cfg.ConfigureDns, "DNS settings"},
		{"Root SSH Disable", cfg.DisableRoot, "Disable root SSH access"},
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
	if !cfg.DryRun {
		fmt.Println(style.Colored(style.Red, "Your system will be modified. This cannot be undone."))
	} else {
		fmt.Println(style.Colored(style.Green, "Dry-run mode is enabled. Changes will only be simulated."))
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Run all hardening steps", Description: "Execute all configured hardening measures"},
	}

	// Add dry-run toggle option
	if cfg.DryRun {
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
		runAllHardening(cfg, osInfo)
	case "2":
		// Toggle dry-run mode and run
		cfg.DryRun = !cfg.DryRun
		if cfg.DryRun {
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
		if !cfg.DryRun {
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
		if err := config.SaveConfig(cfg, configFile); err != nil {
			logging.LogError("Failed to save configuration: %v", err)
		}

		// Run with new dry-run setting
		runAllHardening(cfg, osInfo)
	case "0":
		fmt.Println("\nOperation cancelled. No changes were made.")
		return
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		RunAllHardeningMenu(cfg, osInfo)
		return
	}

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// runAllHardening executes all hardening steps
func runAllHardening(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintLogo()
	fmt.Println(style.Bolded("Executing All Hardening Steps", style.Blue))
	logging.LogInfo("Running complete system hardening...")

	// Track progress with step counting
	totalSteps := 8 // Base steps, may increase based on enabled features
	if cfg.EnableAppArmor {
		totalSteps++
	}
	if cfg.EnableLynis {
		totalSteps++
	}
	if cfg.EnableUnattendedUpgrades {
		totalSteps++
	}
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

	// 1. Setup hushlogin
	showProgress("Setup basic configuration")
	if err := utils.SetupHushlogin(cfg); err != nil {
		fmt.Printf("%s Failed to setup hushlogin: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	} else if !cfg.DryRun {
		fmt.Printf("%s Hushlogin configured\n", 
			style.Colored(style.Green, style.SymCheckMark))
	}

	// 2. Update package repositories
	showProgress("Update package repositories")
	if err := packages.WriteSources(cfg, osInfo); err != nil {
		fmt.Printf("%s Failed to configure package sources: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	} else if !cfg.DryRun {
		fmt.Printf("%s Package sources updated\n", 
			style.Colored(style.Green, style.SymCheckMark))
	}

	// 3. Handle Proxmox repositories if needed
	if osInfo.OsType != "alpine" && osInfo.IsProxmox {
		if err := packages.WriteProxmoxRepos(cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to configure Proxmox repositories: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s Proxmox repositories configured\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	}

	// 4. Install packages
	showProgress("Install system packages")
	installSystemPackages(cfg, osInfo)

	// 5. Create user
	showProgress("Configure user account")
	if cfg.Username != "" {
		if err := user.CreateUser(cfg.Username, cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to create user: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s User '%s' configured\n", 
				style.Colored(style.Green, style.SymCheckMark),
				cfg.Username)
		}
	} else {
		fmt.Printf("%s No username specified, skipping user creation\n", 
			style.Colored(style.Yellow, style.SymWarning))
	}

	// 6. Configure SSH
	showProgress("Configure SSH")
	if err := ssh.WriteSSHConfig(cfg, osInfo); err != nil {
		fmt.Printf("%s Failed to configure SSH: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	} else if !cfg.DryRun {
		fmt.Printf("%s SSH configured\n", 
			style.Colored(style.Green, style.SymCheckMark))
	}

	// 7. Disable root SSH access if requested
	showProgress("Configure root SSH access")
	if cfg.DisableRoot {
		if err := ssh.DisableRootSSHAccess(cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to disable root SSH access: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s Root SSH access disabled\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	} else {
		fmt.Printf("%s Root SSH access will remain enabled (not configured to disable)\n", 
			style.Colored(style.Yellow, style.SymInfo))
	}

	// 8. Configure UFW
	showProgress("Configure firewall")
	if cfg.EnableUfwSshPolicy {
		if err := firewall.ConfigureUFW(cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to configure firewall: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s Firewall configured\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	} else {
		fmt.Printf("%s Firewall configuration skipped (not enabled in config)\n", 
			style.Colored(style.Yellow, style.SymInfo))
	}

	// 9. Configure DNS
	showProgress("Configure DNS")
	if cfg.ConfigureDns {
		if err := dns.ConfigureDNS(cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to configure DNS: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s DNS configured\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	} else {
		fmt.Printf("%s DNS configuration skipped (not enabled in config)\n", 
			style.Colored(style.Yellow, style.SymInfo))
	}

	// 10. Setup AppArmor if enabled
	if cfg.EnableAppArmor {
		showProgress("Configure AppArmor")
		if err := security.SetupAppArmor(cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to configure AppArmor: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s AppArmor configured\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	}

	// 11. Setup Lynis if enabled
	if cfg.EnableLynis {
		showProgress("Install Lynis security audit")
		if err := security.SetupLynis(cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to configure Lynis: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s Lynis installed and audit completed\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	}

	// 12. Setup unattended upgrades if enabled
	if cfg.EnableUnattendedUpgrades {
		showProgress("Configure automatic updates")
		if err := updates.SetupUnattendedUpgrades(cfg, osInfo); err != nil {
			fmt.Printf("%s Failed to configure unattended upgrades: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else if !cfg.DryRun {
			fmt.Printf("%s Automatic updates configured\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	}

	// Final status
	fmt.Println()
	if cfg.DryRun {
		fmt.Printf("%s System hardening %s (DRY-RUN)\n", 
			style.Colored(style.Green, style.SymCheckMark),
			style.Bolded("simulation completed", style.Green))
		fmt.Println(style.Dimmed("No actual changes were made to your system."))
	} else {
		fmt.Printf("%s System hardening %s\n", 
			style.Colored(style.Green, style.SymCheckMark),
			style.Bolded("completed successfully", style.Green))
	}

	logging.LogSuccess("System hardening completed")
	fmt.Printf("\n%s Check the log file at %s for details\n", 
		style.Colored(style.Cyan, style.SymInfo),
		style.Colored(style.Cyan, cfg.LogFile))
}

// Helper function to install system packages
func installSystemPackages(cfg *config.Config, osInfo *osdetect.OSInfo) {
	if osInfo.OsType == "alpine" {
		// Install Alpine packages
		if len(cfg.AlpineCorePackages) > 0 {
			fmt.Printf("%s Installing Alpine core packages...\n", style.BulletItem)
			if !cfg.DryRun {
				packages.InstallPackages(cfg.AlpineCorePackages, osInfo, cfg)
			}
		}

		// Check subnet to determine which package sets to install
		isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet, provider.Network)
		// isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
		if isDmz {
			if len(cfg.AlpineDmzPackages) > 0 {
				fmt.Printf("%s Installing Alpine DMZ packages...\n", style.BulletItem)
				if !cfg.DryRun {
					packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
				}
			}
		} else {
			if len(cfg.AlpineDmzPackages) > 0 {
				fmt.Printf("%s Installing Alpine DMZ packages...\n", style.BulletItem)
				if !cfg.DryRun {
					packages.InstallPackages(cfg.AlpineDmzPackages, osInfo, cfg)
				}
			}

			if len(cfg.AlpineLabPackages) > 0 {
				fmt.Printf("%s Installing Alpine LAB packages...\n", style.BulletItem)
				if !cfg.DryRun {
					packages.InstallPackages(cfg.AlpineLabPackages, osInfo, cfg)
				}
			}
		}

		// Install Python packages if defined
		if len(cfg.AlpinePythonPackages) > 0 {
			fmt.Printf("%s Installing Alpine Python packages...\n", style.BulletItem)
			if !cfg.DryRun {
				packages.InstallPackages(cfg.AlpinePythonPackages, osInfo, cfg)
			}
		}
	} else {
		// Install core Linux packages first
		if len(cfg.LinuxCorePackages) > 0 {
			fmt.Printf("%s Installing Linux core packages...\n", style.BulletItem)
			if !cfg.DryRun {
				packages.InstallPackages(cfg.LinuxCorePackages, osInfo, cfg)
			}
		}

		// Check subnet to determine which package sets to install
		isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet, provider.Network)
		// isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
		if isDmz {
			if len(cfg.LinuxDmzPackages) > 0 {
				fmt.Printf("%s Installing Debian DMZ packages...\n", style.BulletItem)
				if !cfg.DryRun {
					packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
				}
			}
		} else {
			// Install both
			if len(cfg.LinuxDmzPackages) > 0 {
				fmt.Printf("%s Installing Debian DMZ packages...\n", style.BulletItem)
				if !cfg.DryRun {
					packages.InstallPackages(cfg.LinuxDmzPackages, osInfo, cfg)
				}
			}
			if len(cfg.LinuxLabPackages) > 0 {
				fmt.Printf("%s Installing Debian Lab packages...\n", style.BulletItem)
				if !cfg.DryRun {
					packages.InstallPackages(cfg.LinuxLabPackages, osInfo, cfg)
				}
			}
		}
	}
}