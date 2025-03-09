// pkg/menu/dns_menu.go
package menu

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// DNSMenu handles DNS configuration
type DNSMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewDNSMenu creates a new DNSMenu
func NewDNSMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *DNSMenu {
	return &DNSMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the DNS configuration menu and handles user input
func (m *DNSMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("DNS Configuration", style.Blue))

	// Check current DNS status - this would ideally come from the application layer
	// but for now we'll reuse the existing code until it's refactored
	currentNameservers, dnsImplementation := getCurrentDnsSettings()

	// Display current configuration
	fmt.Println()
	fmt.Println(style.Bolded("Current DNS Configuration:", style.Blue))

	// Create formatter for status display
	formatter := style.NewStatusFormatter([]string{"DNS Implementation", "Nameservers"}, 2)

	// Show DNS implementation
	if dnsImplementation != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "DNS Implementation", dnsImplementation, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatWarning("DNS Implementation", "Unknown", "Could not detect DNS setup"))
	}

	// Show current nameservers
	if len(currentNameservers) > 0 {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Nameservers", strings.Join(currentNameservers, ", "), style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatWarning("Nameservers", "None detected", "DNS resolution may not work"))
	}

	// Show configured nameservers
	fmt.Println()
	fmt.Println(style.Bolded("Configured Nameservers:", style.Blue))

	if len(m.config.Nameservers) > 0 {
		for i, ns := range m.config.Nameservers {
			fmt.Printf("%s Nameserver %d: %s\n", style.BulletItem, i+1, style.Colored(style.Cyan, ns))
		}
	} else {
		fmt.Printf("%s No nameservers configured\n", style.Colored(style.Yellow, style.SymWarning))
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Configure DNS", Description: "Apply nameserver settings from configuration"},
		{Number: 2, Title: "Add nameserver", Description: "Add a new DNS server to configuration"},
	}

	// Add remove option if nameservers exist
	if len(m.config.Nameservers) > 0 {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      3,
			Title:       "Remove nameserver",
			Description: "Remove a DNS server from configuration",
		})
	}

	// Add popular DNS provider options
	menuOptions = append(menuOptions, style.MenuOption{
		Number:      4,
		Title:       "Use Cloudflare DNS",
		Description: "Set nameservers to 1.1.1.1, 1.0.0.1",
	})

	menuOptions = append(menuOptions, style.MenuOption{
		Number:      5,
		Title:       "Use Google DNS",
		Description: "Set nameservers to 8.8.8.8, 8.8.4.4",
	})

	menuOptions = append(menuOptions, style.MenuOption{
		Number:      6,
		Title:       "Use Quad9 DNS",
		Description: "Set nameservers to 9.9.9.9, 149.112.112.112",
	})

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
		// Configure DNS with current settings
		if len(m.config.Nameservers) == 0 {
			fmt.Printf("\n%s No nameservers configured. Please add nameservers first.\n",
				style.Colored(style.Yellow, style.SymWarning))

			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
			m.Show()
			return
		}

		fmt.Println("\nConfiguring DNS settings...")

		if m.config.DryRun {
			fmt.Printf("%s [DRY-RUN] Would configure DNS with nameservers: %s\n",
				style.BulletItem, strings.Join(m.config.Nameservers, ", "))
		} else {
			err := m.menuManager.ConfigureDNS(m.config.Nameservers, "lan")
			if err != nil {
				fmt.Printf("\n%s Failed to configure DNS: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s DNS configured successfully\n",
					style.Colored(style.Green, style.SymCheckMark))
				fmt.Printf("%s Nameservers: %s\n",
					style.BulletItem, strings.Join(m.config.Nameservers, ", "))
			}
		}

	case "2":
		// Add nameserver
		m.addNameserver()
		m.Show()
		return

	case "3":
		// Remove nameserver (only if nameservers exist)
		if len(m.config.Nameservers) == 0 {
			fmt.Printf("\n%s No nameservers to remove\n",
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			m.removeNameserver()
		}

		m.Show()
		return

	case "4":
		// Use Cloudflare DNS
		fmt.Println("\nSetting Cloudflare DNS servers...")
		m.config.Nameservers = []string{"1.1.1.1", "1.0.0.1"}

		// Save config
		if err := config.SaveConfig(m.config, "hardn.yml"); err != nil {
			fmt.Printf("\n%s Failed to save configuration: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
		}

		fmt.Printf("\n%s Nameservers set to Cloudflare DNS: 1.1.1.1, 1.0.0.1\n",
			style.Colored(style.Green, style.SymCheckMark))

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.Show()
		return

	case "5":
		// Use Google DNS
		fmt.Println("\nSetting Google DNS servers...")
		m.config.Nameservers = []string{"8.8.8.8", "8.8.4.4"}

		// Save config
		if err := config.SaveConfig(m.config, "hardn.yml"); err != nil {
			fmt.Printf("\n%s Failed to save configuration: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
		}

		fmt.Printf("\n%s Nameservers set to Google DNS: 8.8.8.8, 8.8.4.4\n",
			style.Colored(style.Green, style.SymCheckMark))

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.Show()
		return

	case "6":
		// Use Quad9 DNS
		fmt.Println("\nSetting Quad9 DNS servers...")
		m.config.Nameservers = []string{"9.9.9.9", "149.112.112.112"}

		// Save config
		if err := config.SaveConfig(m.config, "hardn.yml"); err != nil {
			fmt.Printf("\n%s Failed to save configuration: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
		}

		fmt.Printf("\n%s Nameservers set to Quad9 DNS: 9.9.9.9, 149.112.112.112\n",
			style.Colored(style.Green, style.SymCheckMark))

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.Show()
		return

	case "0":
		// Return to main menu
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

// addNameserver handles adding a new nameserver
func (m *DNSMenu) addNameserver() {
	fmt.Printf("\n%s Enter nameserver IP address: ", style.BulletItem)
	newNameserver := ReadInput()

	if newNameserver == "" {
		fmt.Printf("\n%s Nameserver cannot be empty\n",
			style.Colored(style.Red, style.SymCrossMark))
		return
	}

	// Validate IP format (basic check)
	parts := strings.Split(newNameserver, ".")
	if len(parts) != 4 {
		fmt.Printf("\n%s Invalid IP address format\n",
			style.Colored(style.Red, style.SymCrossMark))
		return
	}

	// Check for duplicate
	isDuplicate := false
	for _, ns := range m.config.Nameservers {
		if ns == newNameserver {
			isDuplicate = true
			break
		}
	}

	if isDuplicate {
		fmt.Printf("\n%s Nameserver %s is already configured\n",
			style.Colored(style.Yellow, style.SymWarning), newNameserver)
		return
	}

	// Add new nameserver
	m.config.Nameservers = append(m.config.Nameservers, newNameserver)

	// Save config
	if err := config.SaveConfig(m.config, "hardn.yml"); err != nil {
		fmt.Printf("\n%s Failed to save configuration: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)
		return
	}

	fmt.Printf("\n%s Nameserver %s added to configuration\n",
		style.Colored(style.Green, style.SymCheckMark), newNameserver)
}

// removeNameserver handles removing a nameserver
func (m *DNSMenu) removeNameserver() {
	fmt.Println()
	for i, ns := range m.config.Nameservers {
		fmt.Printf("%s %d: %s\n", style.BulletItem, i+1, ns)
	}

	fmt.Printf("\n%s Enter nameserver number to remove (1-%d): ",
		style.BulletItem, len(m.config.Nameservers))
	numStr := ReadInput()

	// Parse number
	num := 0
	n, err := fmt.Sscanf(numStr, "%d", &num)
	if err != nil || n != 1 {
		fmt.Printf("\n%s Invalid nameserver number: not a valid number\n",
			style.Colored(style.Red, style.SymCrossMark))
		return
	}

	if num < 1 || num > len(m.config.Nameservers) {
		fmt.Printf("\n%s Invalid nameserver number: out of range\n",
			style.Colored(style.Red, style.SymCrossMark))
		return
	}

	// Remove nameserver (adjust for 0-based index)
	removedNs := m.config.Nameservers[num-1]
	m.config.Nameservers = append(m.config.Nameservers[:num-1], m.config.Nameservers[num:]...)

	// Save config
	if err := config.SaveConfig(m.config, "hardn.yml"); err != nil {
		fmt.Printf("\n%s Failed to save configuration: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)
		return
	}

	fmt.Printf("\n%s Nameserver %s removed from configuration\n",
		style.Colored(style.Green, style.SymCheckMark), removedNs)
}

// getCurrentDnsSettings retrieves the current DNS settings
// This is a temporary function that will be replaced by application layer calls later
func getCurrentDnsSettings() ([]string, string) {
	var nameservers []string
	dnsImplementation := ""

	// Check if systemd-resolved is active
	systemdCmd := exec.Command("systemctl", "is-active", "systemd-resolved")
	if err := systemdCmd.Run(); err == nil {
		dnsImplementation = "systemd-resolved"

		// Get nameservers from resolved
		resolvectlCmd := exec.Command("resolvectl", "dns")
		output, err := resolvectlCmd.CombinedOutput()
		if err == nil {
			// Parse output to extract nameservers
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, ":") {
					parts := strings.Split(line, ":")
					if len(parts) >= 2 {
						ns := strings.TrimSpace(parts[1])
						if ns != "" {
							nameservers = append(nameservers, ns)
						}
					}
				}
			}
		}
	} else if _, err := exec.LookPath("resolvconf"); err == nil {
		dnsImplementation = "resolvconf"
	} else {
		dnsImplementation = "direct (/etc/resolv.conf)"
	}

	// If we couldn't get nameservers from implementation-specific means,
	// try to parse /etc/resolv.conf directly
	if len(nameservers) == 0 {
		if data, err := os.ReadFile("/etc/resolv.conf"); err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "nameserver") {
					parts := strings.Fields(line)
					if len(parts) >= 2 {
						nameservers = append(nameservers, parts[1])
					}
				}
			}
		}
	}

	return nameservers, dnsImplementation
}
