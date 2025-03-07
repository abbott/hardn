// pkg/menu/firewall_menu.go
package menu

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// FirewallMenu handles UFW firewall configuration
type FirewallMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewFirewallMenu creates a new FirewallMenu
func NewFirewallMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *FirewallMenu {
	return &FirewallMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the firewall menu and handles user input
func (m *FirewallMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("UFW Firewall Configuration", style.Blue))

	// Check current UFW status - this would ideally come from the application layer
	isInstalled, isEnabled, isConfigured, rules, err := m.menuManager.GetFirewallStatus()
	if err != nil {
		fmt.Printf("\n%s Error getting firewall status: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)
		isInstalled = false
		isEnabled = false
		isConfigured = false
		rules = []string{}
	}

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
	sshPortStr := strconv.Itoa(m.config.SshPort)
	sshPortDisplay := fmt.Sprintf("Port %s/tcp", sshPortStr)

	if m.config.SshPort == 22 {
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
	if len(m.config.UfwAppProfiles) > 0 {
		fmt.Println()
		fmt.Println(style.Bolded("Configured Application Profiles:", style.Blue))
		for _, profile := range m.config.UfwAppProfiles {
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

	choice := ReadMenuInput()

	// Handle 'q' as a special exit case
	if choice == "q" {
		return
	}

	switch choice {
	case "1":
		if !isInstalled {
			// Install UFW - this should call through to an application service
			fmt.Println("\nInstalling UFW...")

			if m.config.DryRun {
				fmt.Printf("%s [DRY-RUN] Would install UFW package\n", style.BulletItem)
			} else {
				// TODO: This should go through the application layer
				// For now, we'll just provide a message
				fmt.Printf("%s This operation isn't yet implemented in the new architecture\n",
					style.Colored(style.Yellow, style.SymWarning))
			}
		} else if isEnabled {
			// Disable firewall through application layer
			fmt.Printf("\n%s WARNING: Disabling the firewall will remove protection from your system.\n",
				style.Colored(style.Red, style.SymWarning))
			fmt.Printf("%s Are you sure you want to disable UFW? (y/n): ", style.BulletItem)

			confirm := ReadInput()
			if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
				if m.config.DryRun {
					fmt.Printf("%s [DRY-RUN] Would disable UFW\n", style.BulletItem)
				} else {
					// Call to application layer to disable firewall
					// TODO: Implement this in MenuManager and FirewallManager
					fmt.Printf("%s This operation isn't yet implemented in the new architecture\n",
						style.Colored(style.Yellow, style.SymWarning))
				}
			} else {
				fmt.Println("\nOperation cancelled. UFW remains enabled.")
			}
		} else {
			// Enable firewall through application layer
			fmt.Println("\nEnabling UFW...")

			if m.config.DryRun {
				fmt.Printf("%s [DRY-RUN] Would enable UFW\n", style.BulletItem)
			} else {
				// Configure secure firewall with SSH port
				err := m.menuManager.ConfigureFirewall(m.config.SshPort, []int{})
				if err != nil {
					fmt.Printf("\n%s Failed to enable and configure firewall: %v\n",
						style.Colored(style.Red, style.SymCrossMark), err)
				} else {
					fmt.Printf("\n%s Firewall enabled and configured successfully\n",
						style.Colored(style.Green, style.SymCheckMark))
				}
			}
		}

		// Return to firewall menu
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.Show()

	case "2":
		// Configure UFW
		fmt.Println("\nConfiguring UFW firewall...")

		if m.config.DryRun {
			fmt.Printf("%s [DRY-RUN] Would configure UFW with default policies and SSH rules\n", style.BulletItem)
			fmt.Printf("%s [DRY-RUN] SSH port: %d/tcp\n", style.BulletItem, m.config.SshPort)
		} else {
			// Convert app profiles to domain model format
			var profiles []model.FirewallProfile
			for _, profile := range m.config.UfwAppProfiles {
				profiles = append(profiles, model.FirewallProfile{
					Name:        profile.Name,
					Title:       profile.Title,
					Description: profile.Description,
					Ports:       profile.Ports,
				})
			}

			// Call application layer to configure firewall
			err := m.menuManager.ConfigureFirewall(m.config.SshPort, []int{})
			if err != nil {
				fmt.Printf("\n%s Failed to configure firewall: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s Firewall configured successfully\n",
					style.Colored(style.Green, style.SymCheckMark))

				// Show important rules
				fmt.Printf("%s Default policy: deny (incoming), allow (outgoing)\n", style.BulletItem)
				fmt.Printf("%s SSH allowed on port %d/tcp\n", style.BulletItem, m.config.SshPort)

				// Show app profiles if configured
				if len(m.config.UfwAppProfiles) > 0 {
					fmt.Printf("%s Application profiles: %d configured\n",
						style.BulletItem, len(m.config.UfwAppProfiles))
				}
			}
		}

	case "3":
		// Manage application profiles
		m.manageAppProfiles()
		m.Show()
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
		m.Show()
		return
	}

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// manageAppProfiles handles the application profiles management submenu
func (m *FirewallMenu) manageAppProfiles() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Manage UFW Application Profiles", style.Blue))

	// Display current profiles
	fmt.Println()
	fmt.Println(style.Bolded("Configured Application Profiles:", style.Blue))

	if len(m.config.UfwAppProfiles) == 0 {
		fmt.Printf("%s No application profiles configured\n", style.BulletItem)
	} else {
		for i, profile := range m.config.UfwAppProfiles {
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
	if len(m.config.UfwAppProfiles) > 0 {
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

	choice := ReadMenuInput()

	// Handle 'q' as a special exit case
	if choice == "q" {
		return
	}

	switch choice {
	case "1":
		// Add application profile
		m.addAppProfile()
		m.manageAppProfiles()
		return

	case "2":
		// Remove application profile (only if profiles exist)
		if len(m.config.UfwAppProfiles) == 0 {
			fmt.Printf("\n%s No profiles to remove\n",
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			m.removeAppProfile()
		}

		m.manageAppProfiles()
		return

	case "3":
		// Apply profiles (only if profiles exist)
		if len(m.config.UfwAppProfiles) == 0 {
			fmt.Printf("\n%s No profiles to apply\n",
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			m.applyAppProfiles()
		}

		m.manageAppProfiles()
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
		m.manageAppProfiles()
		return
	}
}

// addAppProfile handles adding a new application profile
func (m *FirewallMenu) addAppProfile() {
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
	for _, profile := range m.config.UfwAppProfiles {
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
	m.config.UfwAppProfiles = append(m.config.UfwAppProfiles, newProfile)

	// Save configuration
	if err := config.SaveConfig(m.config, "hardn.yml"); err != nil {
		fmt.Printf("\n%s Failed to save configuration: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)
		return
	}

	fmt.Printf("\n%s Application profile '%s' added successfully\n",
		style.Colored(style.Green, style.SymCheckMark), name)
}

// removeAppProfile handles removing an application profile
func (m *FirewallMenu) removeAppProfile() {
	fmt.Println()
	fmt.Println(style.Bolded("Remove UFW Application Profile:", style.Blue))

	// Display numbered list of profiles
	for i, profile := range m.config.UfwAppProfiles {
		fmt.Printf("%s %d: %s (%s)\n",
			style.BulletItem, i+1, profile.Name, strings.Join(profile.Ports, ", "))
	}

	// Get profile to remove
	fmt.Printf("\n%s Enter profile number to remove (1-%d): ",
		style.BulletItem, len(m.config.UfwAppProfiles))
	numStr := ReadInput()

	// Parse number
	num, err := strconv.Atoi(numStr)
	if err != nil || num < 1 || num > len(m.config.UfwAppProfiles) {
		fmt.Printf("\n%s Invalid profile number\n",
			style.Colored(style.Red, style.SymCrossMark))
		return
	}

	// Get profile name for confirmation
	profileName := m.config.UfwAppProfiles[num-1].Name

	// Confirm removal
	fmt.Printf("%s Are you sure you want to remove profile '%s'? (y/n): ",
		style.BulletItem, profileName)
	confirm := ReadInput()

	if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
		// Remove profile (adjust for 0-based index)
		m.config.UfwAppProfiles = append(
			m.config.UfwAppProfiles[:num-1],
			m.config.UfwAppProfiles[num:]...,
		)

		// Save configuration
		if err := config.SaveConfig(m.config, "hardn.yml"); err != nil {
			fmt.Printf("\n%s Failed to save configuration: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
			return
		}

		fmt.Printf("\n%s Application profile '%s' removed successfully\n",
			style.Colored(style.Green, style.SymCheckMark), profileName)
	} else {
		fmt.Println("\nRemoval cancelled.")
	}
}

// applyAppProfiles handles applying application profiles
func (m *FirewallMenu) applyAppProfiles() {
	fmt.Println()
	fmt.Println(style.Bolded("Apply UFW Application Profiles:", style.Blue))

	if m.config.DryRun {
		fmt.Printf("%s [DRY-RUN] Would write profiles to /etc/ufw/applications.d/hardn\n", style.BulletItem)
		for _, profile := range m.config.UfwAppProfiles {
			fmt.Printf("%s [DRY-RUN] Profile: %s (%s)\n",
				style.BulletItem, profile.Name, strings.Join(profile.Ports, ", "))
		}
	} else {
		// This should call the application layer, but for now we'll just provide a message
		fmt.Printf("\n%s This operation isn't yet implemented in the new architecture\n",
			style.Colored(style.Yellow, style.SymWarning))
	}
}
