// pkg/menu/linux_packages_menu.go
package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// LinuxPackagesMenu handles Linux packages installation
type LinuxPackagesMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewLinuxPackagesMenu creates a new LinuxPackagesMenu
func NewLinuxPackagesMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *LinuxPackagesMenu {
	return &LinuxPackagesMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the Linux packages menu and handles user input
func (m *LinuxPackagesMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Linux Packages Installation", style.Blue))

	// Display current packages
	fmt.Println()
	fmt.Println(style.Bolded("Configured Packages:", style.Blue))

	if m.osInfo.OsType == "alpine" {
		// Alpine packages
		if len(m.config.AlpineCorePackages) > 0 {
			fmt.Printf("%s Core packages: %s\n", style.BulletItem,
				style.Colored(style.Cyan, strings.Join(m.config.AlpineCorePackages, ", ")))
		}

		if len(m.config.AlpineDmzPackages) > 0 {
			fmt.Printf("%s DMZ packages: %s\n", style.BulletItem,
				style.Colored(style.Cyan, strings.Join(m.config.AlpineDmzPackages, ", ")))
		}

		if len(m.config.AlpineLabPackages) > 0 {
			fmt.Printf("%s Lab packages: %s\n", style.BulletItem,
				style.Colored(style.Cyan, strings.Join(m.config.AlpineLabPackages, ", ")))
		}
	} else {
		// Debian/Ubuntu packages
		if len(m.config.LinuxCorePackages) > 0 {
			fmt.Printf("%s Core packages: %s\n", style.BulletItem,
				style.Colored(style.Cyan, strings.Join(m.config.LinuxCorePackages, ", ")))
		}

		if len(m.config.LinuxDmzPackages) > 0 {
			fmt.Printf("%s DMZ packages: %s\n", style.BulletItem,
				style.Colored(style.Cyan, strings.Join(m.config.LinuxDmzPackages, ", ")))
		}

		if len(m.config.LinuxLabPackages) > 0 {
			fmt.Printf("%s Lab packages: %s\n", style.BulletItem,
				style.Colored(style.Cyan, strings.Join(m.config.LinuxLabPackages, ", ")))
		}
	}

	// Check subnet status for package selection

	provider := interfaces.NewProvider()
	isDmz, _ := utils.CheckSubnet(m.config.DmzSubnet, provider.Network)
	if isDmz {
		fmt.Printf("\n%s DMZ subnet detected: %s\n",
			style.Colored(style.Yellow, style.SymInfo),
			style.Colored(style.Yellow, m.config.DmzSubnet))
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

	choice := ReadMenuInput()

	// Handle 'q' as a special exit case
	if choice == "q" {
		return
	}

	switch choice {
	case "1":
		// Install core packages
		fmt.Println("\nInstalling Core Linux packages...")

		if m.osInfo.OsType == "alpine" {
			if len(m.config.AlpineCorePackages) > 0 {
				m.installPackages(m.config.AlpineCorePackages, "Core")
			} else {
				fmt.Printf("\n%s No Alpine Core packages configured\n",
					style.Colored(style.Yellow, style.SymWarning))
			}
		} else {
			if len(m.config.LinuxCorePackages) > 0 {
				m.installPackages(m.config.LinuxCorePackages, "Core")
			} else {
				fmt.Printf("\n%s No Linux Core packages configured\n",
					style.Colored(style.Yellow, style.SymWarning))
			}
		}

	case "2":
		// Install DMZ packages
		fmt.Println("\nInstalling DMZ Linux packages...")

		if m.osInfo.OsType == "alpine" {
			if len(m.config.AlpineDmzPackages) > 0 {
				m.installPackages(m.config.AlpineDmzPackages, "DMZ")
			} else {
				fmt.Printf("\n%s No Alpine DMZ packages configured\n",
					style.Colored(style.Yellow, style.SymWarning))
			}
		} else {
			if len(m.config.LinuxDmzPackages) > 0 {
				m.installPackages(m.config.LinuxDmzPackages, "DMZ")
			} else {
				fmt.Printf("\n%s No Linux DMZ packages configured\n",
					style.Colored(style.Yellow, style.SymWarning))
			}
		}

	case "3":
		// Install Lab packages
		fmt.Println("\nInstalling Lab Linux packages...")

		if m.osInfo.OsType == "alpine" {
			if len(m.config.AlpineLabPackages) > 0 {
				m.installPackages(m.config.AlpineLabPackages, "Lab")
			} else {
				fmt.Printf("\n%s No Alpine Lab packages configured\n",
					style.Colored(style.Yellow, style.SymWarning))
			}
		} else {
			if len(m.config.LinuxLabPackages) > 0 {
				m.installPackages(m.config.LinuxLabPackages, "Lab")
			} else {
				fmt.Printf("\n%s No Linux Lab packages configured\n",
					style.Colored(style.Yellow, style.SymWarning))
			}
		}

	case "4":
		// Install all packages
		fmt.Println("\nInstalling All Linux packages...")
		fmt.Println(style.Dimmed("This may take some time. Please wait..."))

		if m.osInfo.OsType == "alpine" {
			// Install Alpine packages
			if len(m.config.AlpineCorePackages) > 0 {
				m.installPackages(m.config.AlpineCorePackages, "Core")
			}

			if isDmz {
				if len(m.config.AlpineDmzPackages) > 0 {
					m.installPackages(m.config.AlpineDmzPackages, "DMZ")
				}
			} else {
				if len(m.config.AlpineDmzPackages) > 0 {
					m.installPackages(m.config.AlpineDmzPackages, "DMZ")
				}

				if len(m.config.AlpineLabPackages) > 0 {
					m.installPackages(m.config.AlpineLabPackages, "Lab")
				}
			}
		} else {
			// Install Debian/Ubuntu packages
			if len(m.config.LinuxCorePackages) > 0 {
				m.installPackages(m.config.LinuxCorePackages, "Core")
			}

			if isDmz {
				if len(m.config.LinuxDmzPackages) > 0 {
					m.installPackages(m.config.LinuxDmzPackages, "DMZ")
				}
			} else {
				if len(m.config.LinuxDmzPackages) > 0 {
					m.installPackages(m.config.LinuxDmzPackages, "DMZ")
				}

				if len(m.config.LinuxLabPackages) > 0 {
					m.installPackages(m.config.LinuxLabPackages, "Lab")
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

// installPackages handles installing packages with nice formatting
func (m *LinuxPackagesMenu) installPackages(pkgs []string, pkgType string) {
	if len(pkgs) == 0 {
		return
	}

	fmt.Printf("\n%s Installing %s packages: %s\n",
		style.BulletItem,
		pkgType,
		style.Dimmed(strings.Join(pkgs, ", ")))

	if m.config.DryRun {
		fmt.Printf("\n%s [DRY-RUN] Would install %s packages: %s\n",
			style.Colored(style.Green, style.SymInfo),
			pkgType,
			strings.Join(pkgs, ", "))
		return
	}

	// Use the application layer through menuManager
	err := m.menuManager.InstallLinuxPackages(pkgs, pkgType)
	if err != nil {
		fmt.Printf("\n%s Failed to install %s packages: %v\n",
			style.Colored(style.Red, style.SymCrossMark),
			pkgType,
			err)
	} else {
		fmt.Printf("\n%s %s packages installed successfully\n",
			style.Colored(style.Green, style.SymCheckMark),
			pkgType)
	}
}
