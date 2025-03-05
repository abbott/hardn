// pkg/menu/linux.go

package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/packages"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// LinuxPackagesMenu handles installation of Linux packages
func LinuxPackagesMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Linux Packages Installation", style.Blue))

	// Display current packages
	fmt.Println()
	fmt.Println(style.Bolded("Configured Packages:", style.Blue))

	if osInfo.OsType == "alpine" {
		// Alpine packages
		if len(cfg.AlpineCorePackages) > 0 {
			fmt.Printf("%s Core packages: %s\n", style.BulletItem, 
				style.Colored(style.Cyan, strings.Join(cfg.AlpineCorePackages, ", ")))
		}
		
		if len(cfg.AlpineDmzPackages) > 0 {
			fmt.Printf("%s DMZ packages: %s\n", style.BulletItem, 
				style.Colored(style.Cyan, strings.Join(cfg.AlpineDmzPackages, ", ")))
		}
		
		if len(cfg.AlpineLabPackages) > 0 {
			fmt.Printf("%s Lab packages: %s\n", style.BulletItem, 
				style.Colored(style.Cyan, strings.Join(cfg.AlpineLabPackages, ", ")))
		}
	} else {
		// Debian/Ubuntu packages
		if len(cfg.LinuxCorePackages) > 0 {
			fmt.Printf("%s Core packages: %s\n", style.BulletItem, 
				style.Colored(style.Cyan, strings.Join(cfg.LinuxCorePackages, ", ")))
		}
		
		if len(cfg.LinuxDmzPackages) > 0 {
			fmt.Printf("%s DMZ packages: %s\n", style.BulletItem, 
				style.Colored(style.Cyan, strings.Join(cfg.LinuxDmzPackages, ", ")))
		}
		
		if len(cfg.LinuxLabPackages) > 0 {
			fmt.Printf("%s Lab packages: %s\n", style.BulletItem, 
				style.Colored(style.Cyan, strings.Join(cfg.LinuxLabPackages, ", ")))
		}
	}

	// Check subnet status for package selection
	isDmz, _ := utils.CheckSubnet(cfg.DmzSubnet)
	if isDmz {
		fmt.Printf("\n%s DMZ subnet detected: %s\n", 
			style.Colored(style.Yellow, style.SymInfo), 
			style.Colored(style.Yellow, cfg.DmzSubnet))
		fmt.Printf("%s Only DMZ packages will be installed\n", style.BulletItem)
	} else {
		fmt.Printf("\n%s Not in DMZ subnet\n", 
			style.Colored(style.Green, style.SymInfo))
		fmt.Printf("%s Both DMZ and Lab packages will be installed\n", style.BulletItem)
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Install Core Packages", Description: "Install essential system packages"},
		{Number: 2, Title: "Install DMZ Packages", Description: "Install packages for DMZ environments"},
		{Number: 3, Title: "Install Lab Packages", Description: "Install packages for development/lab environments"},
		{Number: 4, Title: "Install All Packages", Description: "Install all configured Linux packages"},
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
		// Install core packages
		fmt.Println("\nInstalling Core Linux packages...")
		
		if osInfo.OsType == "alpine" {
			if len(cfg.AlpineCorePackages) > 0 {
				installPackages(cfg.AlpineCorePackages, "Core", osInfo, cfg)
			} else {
				fmt.Printf("\n%s No Alpine Core packages configured\n", 
					style.Colored(style.Yellow, style.SymWarning))
			}
		} else {
			if len(cfg.LinuxCorePackages) > 0 {
				installPackages(cfg.LinuxCorePackages, "Core", osInfo, cfg)
			} else {
				fmt.Printf("\n%s No Linux Core packages configured\n", 
					style.Colored(style.Yellow, style.SymWarning))
			}
		}
		
	case "2":
		// Install DMZ packages
		fmt.Println("\nInstalling DMZ Linux packages...")
		
		if osInfo.OsType == "alpine" {
			if len(cfg.AlpineDmzPackages) > 0 {
				installPackages(cfg.AlpineDmzPackages, "DMZ", osInfo, cfg)
			} else {
				fmt.Printf("\n%s No Alpine DMZ packages configured\n", 
					style.Colored(style.Yellow, style.SymWarning))
			}
		} else {
			if len(cfg.LinuxDmzPackages) > 0 {
				installPackages(cfg.LinuxDmzPackages, "DMZ", osInfo, cfg)
			} else {
				fmt.Printf("\n%s No Linux DMZ packages configured\n", 
					style.Colored(style.Yellow, style.SymWarning))
			}
		}
		
	case "3":
		// Install Lab packages
		fmt.Println("\nInstalling Lab Linux packages...")
		
		if osInfo.OsType == "alpine" {
			if len(cfg.AlpineLabPackages) > 0 {
				installPackages(cfg.AlpineLabPackages, "Lab", osInfo, cfg)
			} else {
				fmt.Printf("\n%s No Alpine Lab packages configured\n", 
					style.Colored(style.Yellow, style.SymWarning))
			}
		} else {
			if len(cfg.LinuxLabPackages) > 0 {
				installPackages(cfg.LinuxLabPackages, "Lab", osInfo, cfg)
			} else {
				fmt.Printf("\n%s No Linux Lab packages configured\n", 
					style.Colored(style.Yellow, style.SymWarning))
			}
		}
		
	case "4":
		// Install all packages
		fmt.Println("\nInstalling All Linux packages...")
		fmt.Println(style.Dimmed("This may take some time. Please wait..."))
		
		if osInfo.OsType == "alpine" {
			// Install Alpine packages
			if len(cfg.AlpineCorePackages) > 0 {
				installPackages(cfg.AlpineCorePackages, "Core", osInfo, cfg)
			}
			
			if isDmz {
				if len(cfg.AlpineDmzPackages) > 0 {
					installPackages(cfg.AlpineDmzPackages, "DMZ", osInfo, cfg)
				}
			} else {
				if len(cfg.AlpineDmzPackages) > 0 {
					installPackages(cfg.AlpineDmzPackages, "DMZ", osInfo, cfg)
				}
				
				if len(cfg.AlpineLabPackages) > 0 {
					installPackages(cfg.AlpineLabPackages, "Lab", osInfo, cfg)
				}
			}
		} else {
			// Install Debian/Ubuntu packages
			if len(cfg.LinuxCorePackages) > 0 {
				installPackages(cfg.LinuxCorePackages, "Core", osInfo, cfg)
			}
			
			if isDmz {
				if len(cfg.LinuxDmzPackages) > 0 {
					installPackages(cfg.LinuxDmzPackages, "DMZ", osInfo, cfg)
				}
			} else {
				if len(cfg.LinuxDmzPackages) > 0 {
					installPackages(cfg.LinuxDmzPackages, "DMZ", osInfo, cfg)
				}
				
				if len(cfg.LinuxLabPackages) > 0 {
					installPackages(cfg.LinuxLabPackages, "Lab", osInfo, cfg)
				}
			}
		}
		
		fmt.Printf("\n%s All Linux packages installed successfully!\n", 
			style.Colored(style.Green, style.SymCheckMark))
		
	case "0":
		return
		
	default:
		fmt.Printf("\n%s Invalid option. No changes were made.\n", 
			style.Colored(style.Yellow, style.SymWarning))
	}
	
	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}

// Helper function to install packages with nice formatting
func installPackages(pkgs []string, pkgType string, osInfo *osdetect.OSInfo, cfg *config.Config) {
	if len(pkgs) == 0 {
		return
	}
	
	fmt.Printf("\n%s Installing %s packages: %s\n", 
		style.BulletItem,
		pkgType, 
		style.Dimmed(strings.Join(pkgs, ", ")))
		
	if err := packages.InstallPackages(pkgs, osInfo, cfg); err != nil {
		fmt.Printf("\n%s Failed to install %s packages: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), 
			pkgType,
			err)
		logging.LogError("Failed to install %s packages: %v", pkgType, err)
	} else {
		fmt.Printf("\n%s %s packages installed successfully!\n", 
			style.Colored(style.Green, style.SymCheckMark),
			pkgType)
	}
}