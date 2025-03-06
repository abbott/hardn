// pkg/menu/dns.go

package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/dns"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// ConfigureDnsMenu handles DNS configuration options
func ConfigureDnsMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("DNS Configuration", style.Blue))

	// Check current DNS status
	currentNameservers, dnsImplementation := getCurrentDnsSettings()

	// Display current configuration
	fmt.Println()
	fmt.Println(style.Bolded("Current DNS Configuration:", style.Blue))
	
	// Create formatter for status display
	formatter := style.NewStatusFormatter([]string{"DNS Implementation", "Nameservers"}, 2)
	
	// Show DNS implementation
	if dnsImplementation != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "DNS Implementation", 
			dnsImplementation, style.Cyan, "", "light"))
	} else {
		fmt.Println(formatter.FormatWarning("DNS Implementation", "Unknown", "Could not detect DNS setup"))
	}
	
	// Show current nameservers
	if len(currentNameservers) > 0 {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Nameservers", 
			strings.Join(currentNameservers, ", "), style.Cyan, "", "light"))
	} else {
		fmt.Println(formatter.FormatWarning("Nameservers", "None detected", "DNS resolution may not work"))
	}
	
	// Show configured nameservers
	fmt.Println()
	fmt.Println(style.Bolded("Configured Nameservers:", style.Blue))
	
	if len(cfg.Nameservers) > 0 {
		for i, ns := range cfg.Nameservers {
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
	if len(cfg.Nameservers) > 0 {
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
	
	choice := ReadInput()
	
	switch choice {
	case "1":
		// Configure DNS with current settings
		if len(cfg.Nameservers) == 0 {
			fmt.Printf("\n%s No nameservers configured. Please add nameservers first.\n", 
				style.Colored(style.Yellow, style.SymWarning))
				
			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
			ConfigureDnsMenu(cfg, osInfo)
			return
		}
		
		fmt.Println("\nConfiguring DNS settings...")
		
		if cfg.DryRun {
			fmt.Printf("%s [DRY-RUN] Would configure DNS with nameservers: %s\n", 
				style.BulletItem, strings.Join(cfg.Nameservers, ", "))
		} else {
			if err := dns.ConfigureDNS(cfg, osInfo); err != nil {
				fmt.Printf("\n%s Failed to configure DNS: %v\n", 
					style.Colored(style.Red, style.SymCrossMark), err)
				logging.LogError("Failed to configure DNS: %v", err)
			} else {
				fmt.Printf("\n%s DNS configured successfully\n", 
					style.Colored(style.Green, style.SymCheckMark))
				fmt.Printf("%s Nameservers: %s\n", 
					style.BulletItem, strings.Join(cfg.Nameservers, ", "))
			}
		}
		
	case "2":
		// Add nameserver
		fmt.Printf("\n%s Enter nameserver IP address: ", style.BulletItem)
		newNameserver := ReadInput()
		
		if newNameserver == "" {
			fmt.Printf("\n%s Nameserver cannot be empty\n", 
				style.Colored(style.Red, style.SymCrossMark))
		} else {
			// Validate IP format (basic check)
			parts := strings.Split(newNameserver, ".")
			if len(parts) != 4 {
				fmt.Printf("\n%s Invalid IP address format\n", 
					style.Colored(style.Red, style.SymCrossMark))
			} else {
				// Check for duplicate
				isDuplicate := false
				for _, ns := range cfg.Nameservers {
					if ns == newNameserver {
						isDuplicate = true
						break
					}
				}
				
				if isDuplicate {
					fmt.Printf("\n%s Nameserver %s is already configured\n", 
						style.Colored(style.Yellow, style.SymWarning), newNameserver)
				} else {
					// Add new nameserver
					cfg.Nameservers = append(cfg.Nameservers, newNameserver)
					
					// Save config
					saveDnsConfig(cfg)
					
					fmt.Printf("\n%s Nameserver %s added to configuration\n", 
						style.Colored(style.Green, style.SymCheckMark), newNameserver)
				}
			}
		}
		
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		ConfigureDnsMenu(cfg, osInfo)
		
	case "3":
		// Remove nameserver
		if len(cfg.Nameservers) == 0 {
			fmt.Printf("\n%s No nameservers to remove\n", 
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			fmt.Println()
			for i, ns := range cfg.Nameservers {
				fmt.Printf("%s %d: %s\n", style.BulletItem, i+1, ns)
			}
			
			fmt.Printf("\n%s Enter nameserver number to remove (1-%d): ", 
				style.BulletItem, len(cfg.Nameservers))
			numStr := ReadInput()
			
			// Parse number
			num := 0
			fmt.Sscanf(numStr, "%d", &num)
			
			if num < 1 || num > len(cfg.Nameservers) {
				fmt.Printf("\n%s Invalid nameserver number\n", 
					style.Colored(style.Red, style.SymCrossMark))
			} else {
				// Remove nameserver (adjust for 0-based index)
				removedNs := cfg.Nameservers[num-1]
				cfg.Nameservers = append(cfg.Nameservers[:num-1], cfg.Nameservers[num:]...)
				
				// Save config
				saveDnsConfig(cfg)
				
				fmt.Printf("\n%s Nameserver %s removed from configuration\n", 
					style.Colored(style.Green, style.SymCheckMark), removedNs)
			}
		}
		
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		ConfigureDnsMenu(cfg, osInfo)
		
	case "4":
		// Use Cloudflare DNS
		fmt.Println("\nSetting Cloudflare DNS servers...")
		cfg.Nameservers = []string{"1.1.1.1", "1.0.0.1"}
		
		// Save config
		saveDnsConfig(cfg)
		
		fmt.Printf("\n%s Nameservers set to Cloudflare DNS: 1.1.1.1, 1.0.0.1\n", 
			style.Colored(style.Green, style.SymCheckMark))
			
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		ConfigureDnsMenu(cfg, osInfo)
		
	case "5":
		// Use Google DNS
		fmt.Println("\nSetting Google DNS servers...")
		cfg.Nameservers = []string{"8.8.8.8", "8.8.4.4"}
		
		// Save config
		saveDnsConfig(cfg)
		
		fmt.Printf("\n%s Nameservers set to Google DNS: 8.8.8.8, 8.8.4.4\n", 
			style.Colored(style.Green, style.SymCheckMark))
			
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		ConfigureDnsMenu(cfg, osInfo)
		
	case "6":
		// Use Quad9 DNS
		fmt.Println("\nSetting Quad9 DNS servers...")
		cfg.Nameservers = []string{"9.9.9.9", "149.112.112.112"}
		
		// Save config
		saveDnsConfig(cfg)
		
		fmt.Printf("\n%s Nameservers set to Quad9 DNS: 9.9.9.9, 149.112.112.112\n", 
			style.Colored(style.Green, style.SymCheckMark))
			
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		ConfigureDnsMenu(cfg, osInfo)
		
	case "0":
		// Return to main menu
		return
		
	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n", 
			style.Colored(style.Red, style.SymCrossMark))
		
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		ConfigureDnsMenu(cfg, osInfo)
		return
	}
	
	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// Helper function to save DNS configuration
func saveDnsConfig(cfg *config.Config) {
	// Save config changes
	configFile := "hardn.yml" // Default config file
	if err := config.SaveConfig(cfg, configFile); err != nil {
		logging.LogError("Failed to save configuration: %v", err)
		fmt.Printf("\n%s Failed to save configuration: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	}
}
