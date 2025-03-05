// pkg/menu/firewall.go

package menu

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/firewall"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// UfwMenu handles UFW firewall configuration
func UfwMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("UFW Firewall Configuration", style.Blue))

	// Check current UFW status
	isInstalled, isEnabled, isConfigured, rules := checkUfwStatus()

	// Display current status
	fmt.Println()
	fmt.Println(style.Bolded("Current Firewall Status:", style.Blue))
	
	// Create formatter for status display
	formatter := style.NewStatusFormatter([]string{"UFW Installed", "UFW Status", "SSH Port"}, 2)
	
	// Installation status
	if isInstalled {
		fmt.Println(formatter.FormatSuccess("UFW Installed", "Yes", "Uncomplicated Firewall is available"))
	} else {
		fmt.Println(formatter.FormatWarning("UFW Installed", "No", "Firewall package not found"))
	}
	
	// Enabled status
	if isEnabled {
		fmt.Println(formatter.FormatSuccess("UFW Status", "Active", "Firewall is running"))
	} else {
		fmt.Println(formatter.FormatWarning("UFW Status", "Inactive", "Firewall is not running"))
	}
	
	// SSH port status
	sshPortStr := strconv.Itoa(cfg.SshPort)
	sshPortDisplay := fmt.Sprintf("Port %s/tcp", sshPortStr)
	
	if cfg.SshPort == 22 {
		fmt.Println(formatter.FormatWarning("SSH Port", sshPortDisplay, "Using default port (consider changing)"))
	} else {
		fmt.Println(formatter.FormatSuccess("SSH Port", sshPortDisplay, "Using non-standard port (good security)"))
	}
	
	// Display configuration information
	fmt.Println()
	if isConfigured && len(rules) > 0 {
		fmt.Println(style.Bolded("Current Firewall Rules:", style.Blue))
		for _, rule := range rules {
			if strings.Contains(strings.ToLower(rule), "allow") {
				fmt.Printf("%s %s\n", style.Colored(style.Green, style.SymCheckMark), rule)
			} else if strings.Contains(strings.ToLower(rule), "deny") {
				fmt.Printf("%s %s\n", style.Colored(style.Red, style.SymCrossMark), rule)
			} else {
				fmt.Printf("%s %s\n", style.BulletItem, rule)
			}
		}
	} else if isInstalled {
		fmt.Printf("%s No firewall rules configured\n", style.Colored(style.Yellow, style.SymWarning))
	}
	
	// Display app profiles if defined
	if len(cfg.UfwAppProfiles) > 0 {
		fmt.Println()
		fmt.Println(style.Bolded("Configured Application Profiles:", style.Blue))
		for _, profile := range cfg.UfwAppProfiles {
			fmt.Printf("%s %s: %s (%s)\n", 
				style.BulletItem,
				style.Bolded(profile.Name, style.Cyan),
				profile.Title,
				strings.Join(profile.Ports, ", "))
		}
	}

	// Create menu options
	var menuOptions []style.MenuOption
	
	// Install UFW if not installed
	if !isInstalled {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1, 
			Title:       "Install UFW", 
			Description: "Install Uncomplicated Firewall package",
		})
	} else {
		// Standard options when UFW is installed
		
		// Enable/disable option
		if !isEnabled {
			menuOptions = append(menuOptions, style.MenuOption{
				Number:      1, 
				Title:       "Enable firewall", 
				Description: "Start UFW and set to run at boot",
			})
		} else {
			menuOptions = append(menuOptions, style.MenuOption{
				Number:      1, 
				Title:       "Disable firewall", 
				Description: "Stop UFW (not recommended)",
			})
		}
		
		// Configure option
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2, 
			Title:       "Configure firewall", 
			Description: "Set up default policies and SSH rules",
		})
		
		// Manage application profiles
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      3, 
			Title:       "Manage application profiles", 
			Description: "Configure custom application rules",
		})
	}
	
	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to main menu",
		Description: "",
	})
	
	// Display menu
	menu.Print()
	
	choice := ReadInput()
	
	switch choice {
	case "1":
		if !isInstalled {
			// Install UFW
			fmt.Println("\nInstalling UFW...")
			
			if cfg.DryRun {
				fmt.Printf("%s [DRY-RUN] Would install UFW package\n", style.BulletItem)
			} else {
				var installCmd *exec.Cmd
				if osInfo.OsType == "alpine" {
					installCmd = exec.Command("apk", "add", "ufw")
				} else {
					installCmd = exec.Command("apt-get", "install", "-y", "ufw")
				}
				
				if err := installCmd.Run(); err != nil {
					fmt.Printf("\n%s Failed to install UFW: %v\n", 
						style.Colored(style.Red, style.SymCrossMark), err)
					logging.LogError("Failed to install UFW: %v", err)
				} else {
					fmt.Printf("\n%s UFW installed successfully\n", 
						style.Colored(style.Green, style.SymCheckMark))
				}
			}
		} else if isEnabled {
			// Disable UFW
			fmt.Printf("\n%s WARNING: Disabling the firewall will remove protection from your system.\n", 
				style.Colored(style.Red, style.SymWarning))
			fmt.Printf("%s Are you sure you want to disable UFW? (y/n): ", style.BulletItem)
			
			confirm := ReadInput()
			if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
				if cfg.DryRun {
					fmt.Printf("%s [DRY-RUN] Would disable UFW\n", style.BulletItem)
				} else {
					disableCmd := exec.Command("ufw", "disable")
					if err := disableCmd.Run(); err != nil {
						fmt.Printf("\n%s Failed to disable UFW: %v\n", 
							style.Colored(style.Red, style.SymCrossMark), err)
						logging.LogError("Failed to disable UFW: %v", err)
					} else {
						fmt.Printf("\n%s UFW disabled\n", 
							style.Colored(style.Yellow, style.SymInfo))
					}
				}
			} else {
				fmt.Println("\nOperation cancelled. UFW remains enabled.")
			}
		} else {
			// Enable UFW
			fmt.Println("\nEnabling UFW...")
			
			if cfg.DryRun {
				fmt.Printf("%s [DRY-RUN] Would enable UFW\n", style.BulletItem)
			} else {
				// First ensure there's an SSH rule to prevent lockout
				sshPort := strconv.Itoa(cfg.SshPort)
				allowCmd := exec.Command("ufw", "allow", sshPort+"/tcp", "comment", "SSH")
				if err := allowCmd.Run(); err != nil {
					fmt.Printf("%s Warning: Failed to add SSH rule before enabling UFW\n", 
						style.Colored(style.Yellow, style.SymWarning))
					logging.LogWarning("Failed to add SSH rule before enabling UFW: %v", err)
				}
				
				// Enable UFW non-interactively
				enableCmd := exec.Command("sh", "-c", "yes | ufw enable")
				if err := enableCmd.Run(); err != nil {
					fmt.Printf("\n%s Failed to enable UFW: %v\n", 
						style.Colored(style.Red, style.SymCrossMark), err)
					logging.LogError("Failed to enable UFW: %v", err)
				} else {
					fmt.Printf("\n%s UFW enabled successfully\n", 
						style.Colored(style.Green, style.SymCheckMark))
				}
			}
		}
		
		// Return to firewall menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		UfwMenu(cfg, osInfo)
		
	case "2":
		// Configure UFW
		fmt.Println("\nConfiguring UFW firewall...")
		
		if cfg.DryRun {
			fmt.Printf("%s [DRY-RUN] Would configure UFW with default policies and SSH rules\n", style.BulletItem)
			fmt.Printf("%s [DRY-RUN] SSH port: %d/tcp\n", style.BulletItem, cfg.SshPort)
		} else {
			if err := firewall.ConfigureUFW(cfg, osInfo); err != nil {
				fmt.Printf("\n%s Failed to configure UFW: %v\n", 
					style.Colored(style.Red, style.SymCrossMark), err)
				logging.LogError("Failed to configure UFW: %v", err)
			} else {
				fmt.Printf("\n%s UFW configured successfully\n", 
					style.Colored(style.Green, style.SymCheckMark))
					
				// Show important rules
				fmt.Printf("%s Default policy: deny (incoming), allow (outgoing)\n", style.BulletItem)
				fmt.Printf("%s SSH allowed on port %d/tcp\n", style.BulletItem, cfg.SshPort)
				
				// Show app profiles if configured
				if len(cfg.UfwAppProfiles) > 0 {
					fmt.Printf("%s Application profiles: %d configured\n", 
						style.BulletItem, len(cfg.UfwAppProfiles))
				}
			}
		}
		
	case "3":
		// Manage application profiles
		manageUfwAppProfilesMenu(cfg, osInfo)
		UfwMenu(cfg, osInfo)
		return
		
	case "0":
		// Return to main menu
		return
		
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", 
			style.Colored(style.Red, style.SymCrossMark))
		
		// Return to firewall menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		UfwMenu(cfg, osInfo)
		return
	}
	
	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// Helper function to manage UFW application profiles
func manageUfwAppProfilesMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Manage UFW Application Profiles", style.Blue))
	
	// Display current profiles
	fmt.Println()
	fmt.Println(style.Bolded("Configured Application Profiles:", style.Blue))
	
	if len(cfg.UfwAppProfiles) == 0 {
		fmt.Printf("%s No application profiles configured\n", style.BulletItem)
	} else {
		for i, profile := range cfg.UfwAppProfiles {
			fmt.Printf("%s %d: %s\n", style.BulletItem, i+1, style.Bolded(profile.Name, style.Cyan))
			fmt.Printf("   Title: %s\n", profile.Title)
			fmt.Printf("   Description: %s\n", profile.Description)
			fmt.Printf("   Ports: %s\n", strings.Join(profile.Ports, ", "))
			fmt.Println()
		}
	}
	
	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Add application profile", Description: "Create a new UFW application profile"},
	}
	
	// Only add remove option if profiles exist
	if len(cfg.UfwAppProfiles) > 0 {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2, 
			Title:       "Remove application profile", 
			Description: "Delete an existing UFW application profile",
		})
		
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      3, 
			Title:       "Apply profiles", 
			Description: "Enable configured application profiles in UFW",
		})
	}
	
	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to firewall menu",
		Description: "",
	})
	
	// Display menu
	menu.Print()
	
	choice := ReadInput()
	
	switch choice {
	case "1":
		// Add application profile
		addUfwAppProfile(cfg, osInfo)
		manageUfwAppProfilesMenu(cfg, osInfo)
		return
		
	case "2":
		// Remove application profile (only if profiles exist)
		if len(cfg.UfwAppProfiles) == 0 {
			fmt.Printf("\n%s No profiles to remove\n", 
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			removeUfwAppProfile(cfg, osInfo)
		}
		
		manageUfwAppProfilesMenu(cfg, osInfo)
		return
		
	case "3":
		// Apply profiles (only if profiles exist)
		if len(cfg.UfwAppProfiles) == 0 {
			fmt.Printf("\n%s No profiles to apply\n", 
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			applyUfwAppProfiles(cfg, osInfo)
		}
		
		manageUfwAppProfilesMenu(cfg, osInfo)
		return
		
	case "0":
		// Return to firewall menu
		return
		
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", 
			style.Colored(style.Red, style.SymCrossMark))
		
		// Return to app profiles menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		manageUfwAppProfilesMenu(cfg, osInfo)
		return
	}
}

// Helper function to add a UFW application profile
func addUfwAppProfile(cfg *config.Config, osInfo *osdetect.OSInfo) {
	fmt.Println()
	fmt.Println(style.Bolded("Add UFW Application Profile:", style.Blue))
	
	// Get profile details
	fmt.Printf("%s Enter profile name (e.g., 'WebServer'): ", style.BulletItem)
	name := ReadInput()
	
	if name == "" {
		fmt.Printf("\n%s Profile name cannot be empty\n", 
			style.Colored(style.Red, style.SymCrossMark))
		return
	}
	
	// Check for duplicate name
	for _, profile := range cfg.UfwAppProfiles {
		if strings.EqualFold(profile.Name, name) {
			fmt.Printf("\n%s A profile with this name already exists\n", 
				style.Colored(style.Red, style.SymCrossMark))
			return
		}
	}
	
	fmt.Printf("%s Enter profile title (e.g., 'Web Server'): ", style.BulletItem)
	title := ReadInput()
	
	fmt.Printf("%s Enter profile description: ", style.BulletItem)
	description := ReadInput()
	
	fmt.Printf("%s Enter ports (e.g., '80/tcp,443/tcp'): ", style.BulletItem)
	portsStr := ReadInput()
	
	if portsStr == "" {
		fmt.Printf("\n%s Ports cannot be empty\n", 
			style.Colored(style.Red, style.SymCrossMark))
		return
	}
	
	// Split ports by comma
	ports := strings.Split(portsStr, ",")
	
	// Validate port format
	for i, port := range ports {
		ports[i] = strings.TrimSpace(port)
		if !strings.Contains(ports[i], "/") {
			fmt.Printf("\n%s Invalid port format '%s'. Must include protocol (e.g., '80/tcp')\n", 
				style.Colored(style.Red, style.SymCrossMark), ports[i])
			return
		}
	}
	
	// Create new profile
	newProfile := config.UfwAppProfile{
		Name:        name,
		Title:       title,
		Description: description,
		Ports:       ports,
	}
	
	// Add to configuration
	cfg.UfwAppProfiles = append(cfg.UfwAppProfiles, newProfile)
	
	// Save configuration
	saveFirewallConfig(cfg)
	
	fmt.Printf("\n%s Application profile '%s' added successfully\n", 
		style.Colored(style.Green, style.SymCheckMark), name)
}

// Helper function to remove a UFW application profile
func removeUfwAppProfile(cfg *config.Config, osInfo *osdetect.OSInfo) {
	fmt.Println()
	fmt.Println(style.Bolded("Remove UFW Application Profile:", style.Blue))
	
	// Display numbered list of profiles
	for i, profile := range cfg.UfwAppProfiles {
		fmt.Printf("%s %d: %s (%s)\n", 
			style.BulletItem, i+1, profile.Name, strings.Join(profile.Ports, ", "))
	}
	
	// Get profile to remove
	fmt.Printf("\n%s Enter profile number to remove (1-%d): ", 
		style.BulletItem, len(cfg.UfwAppProfiles))
	numStr := ReadInput()
	
	// Parse number
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 1 || num > len(cfg.UfwAppProfiles) {
		fmt.Printf("\n%s Invalid profile number\n", 
			style.Colored(style.Red, style.SymCrossMark))
		return
	}
	
	// Get profile name for confirmation
	profileName := cfg.UfwAppProfiles[num-1].Name
	
	// Confirm removal
	fmt.Printf("%s Are you sure you want to remove profile '%s'? (y/n): ", 
		style.BulletItem, profileName)
	confirm := ReadInput()
	
	if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
		// Remove profile (adjust for 0-based index)
		cfg.UfwAppProfiles = append(
			cfg.UfwAppProfiles[:num-1], 
			cfg.UfwAppProfiles[num:]...
		)
		
		// Save configuration
		saveFirewallConfig(cfg)
		
		fmt.Printf("\n%s Application profile '%s' removed successfully\n", 
			style.Colored(style.Green, style.SymCheckMark), profileName)
	} else {
		fmt.Println("\nRemoval cancelled.")
	}
}

// Helper function to apply UFW application profiles
func applyUfwAppProfiles(cfg *config.Config, osInfo *osdetect.OSInfo) {
	fmt.Println()
	fmt.Println(style.Bolded("Apply UFW Application Profiles:", style.Blue))
	
	if cfg.DryRun {
		fmt.Printf("%s [DRY-RUN] Would write profiles to /etc/ufw/applications.d/hardn\n", style.BulletItem)
		for _, profile := range cfg.UfwAppProfiles {
			fmt.Printf("%s [DRY-RUN] Profile: %s (%s)\n", 
				style.BulletItem, profile.Name, strings.Join(profile.Ports, ", "))
		}
	} else {
		// Use firewall package to write and apply profiles
		if err := firewall.WriteUfwAppProfiles(cfg, osInfo); err != nil {
			fmt.Printf("\n%s Failed to apply application profiles: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
			logging.LogError("Failed to apply UFW application profiles: %v", err)
		} else {
			fmt.Printf("\n%s Application profiles applied successfully\n", 
				style.Colored(style.Green, style.SymCheckMark))
		}
	}
}

// Helper function to save firewall configuration
func saveFirewallConfig(cfg *config.Config) {
	// Save config changes
	configFile := "hardn.yml" // Default config file
	if err := config.SaveConfig(cfg, configFile); err != nil {
		logging.LogError("Failed to save configuration: %v", err)
		fmt.Printf("\n%s Failed to save configuration: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	}
}

// Helper function to check UFW status
func checkUfwStatus() (bool, bool, bool, []string) {
	// Check if UFW is installed
	_, err := exec.LookPath("ufw")
	isInstalled := (err == nil)
	
	// Default values if not installed
	isEnabled := false
	isConfigured := false
	var rules []string
	
	if isInstalled {
		// Check if UFW is enabled
		statusCmd := exec.Command("ufw", "status")
		statusOutput, err := statusCmd.CombinedOutput()
		if err == nil {
			statusText := string(statusOutput)
			isEnabled = strings.Contains(statusText, "Status: active")
			
			// Extract rules (skip header lines)
			lines := strings.Split(statusText, "\n")
			ruleSection := false
			for _, line := range lines {
				line = strings.TrimSpace(line)
				
				// Skip empty lines
				if line == "" {
					continue
				}
				
				// Skip header lines
				if strings.Contains(line, "Status:") || 
				   strings.Contains(line, "Logging:") ||
				   strings.Contains(line, "Default:") ||
				   strings.Contains(line, "New profiles:") ||
				   strings.Contains(line, "To             Action      From") {
					continue
				}
				
				// Check if we've reached the rule section
				if strings.Contains(line, "--") {
					ruleSection = true
					continue
				}
				
				// Add rule lines
				if ruleSection && line != "" {
					rules = append(rules, line)
				}
			}
			
			// Check if we have default policies configured
			isConfigured = strings.Contains(statusText, "deny (incoming)") &&
						   strings.Contains(statusText, "allow (outgoing)")
		}
	}
	
	return isInstalled, isEnabled, isConfigured, rules
}