// pkg/menu/python_packages_menu.go
package menu

import (
	"fmt"
	"os"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// PythonPackagesMenu handles Python packages installation
type PythonPackagesMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewPythonPackagesMenu creates a new PythonPackagesMenu
func NewPythonPackagesMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *PythonPackagesMenu {
	return &PythonPackagesMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the Python packages menu and handles user input
func (m *PythonPackagesMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Python Packages Installation", style.Blue))

	// Get OS-specific package information
	var packageDisplay string
	if m.osInfo.OsType == "alpine" {
		packageDisplay = fmt.Sprintf("Alpine Python packages: %s",
			style.Colored(style.Cyan, strings.Join(m.config.AlpinePythonPackages, ", ")))
	} else {
		// For Debian/Ubuntu
		allPackages := append([]string{}, m.config.PythonPackages...)
		if os.Getenv("WSL") == "" {
			allPackages = append(allPackages, m.config.NonWslPythonPackages...)
		}

		packageDisplay = fmt.Sprintf("System Python packages: %s",
			style.Colored(style.Cyan, strings.Join(allPackages, ", ")))
	}

	// Display pip packages if available
	pipPackageDisplay := ""
	if len(m.config.PythonPipPackages) > 0 {
		pipPackageDisplay = fmt.Sprintf("\n%s Pip packages: %s",
			style.BulletItem,
			style.Colored(style.Cyan, strings.Join(m.config.PythonPipPackages, ", ")))
	}

	// Display current Python package management settings
	fmt.Println()
	fmt.Println(style.Bolded("Current Package Management Settings:", style.Blue))

	// Create a formatter with the label we need
	formatter := style.NewStatusFormatter([]string{"UV Package Manager"}, 2)

	// Show UV package manager status
	if m.config.UseUvPackageManager {
		fmt.Println(formatter.FormatSuccess("UV Package Manager", "Enabled", "Modern, fast package manager"))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "UV Package Manager", "Disabled",
			style.Yellow, "Using standard pip"))
	}

	// Show package information
	fmt.Printf("\n%s %s", style.BulletItem, packageDisplay)
	if pipPackageDisplay != "" {
		fmt.Print(pipPackageDisplay)
	}

	// Create menu options
	var menuOptions []style.MenuOption

	// Toggle UV option
	if m.config.UseUvPackageManager {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1,
			Title:       "Disable UV Package Manager",
			Description: "Revert to standard pip for Python packages",
		})
	} else {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1,
			Title:       "Enable UV Package Manager",
			Description: "Use UV for faster Python package installation",
		})
	}

	// Install packages option
	menuOptions = append(menuOptions, style.MenuOption{
		Number:      2,
		Title:       "Install Python Packages",
		Description: "Install all configured Python packages",
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
		// Toggle UV package manager
		m.config.UseUvPackageManager = !m.config.UseUvPackageManager

		if m.config.UseUvPackageManager {
			fmt.Printf("\n%s UV package manager has been %s. Will use UV for Python packages.\n",
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("enabled", style.Green))
		} else {
			fmt.Printf("\n%s UV package manager has been %s. Will use standard pip.\n",
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("disabled", style.Yellow))
		}

		// Save config changes
		configFile := "hardn.yml" // Default config file
		if err := config.SaveConfig(m.config, configFile); err != nil {
			fmt.Printf("\n%s Failed to save configuration: %v\n",
				style.Colored(style.Red, style.SymCrossMark), err)
		}

		// Return to this menu after toggling
		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()

	case "2":
		// Install packages
		fmt.Println("\nInstalling Python packages...")
		fmt.Println(style.Dimmed("This may take some time. Please wait..."))

		if m.config.DryRun {
			if m.osInfo.OsType == "alpine" {
				fmt.Printf("\n%s [DRY-RUN] Would install Alpine Python packages: %s\n",
					style.Colored(style.Green, style.SymInfo),
					strings.Join(m.config.AlpinePythonPackages, ", "))
			} else {
				allPackages := append([]string{}, m.config.PythonPackages...)
				if os.Getenv("WSL") == "" {
					allPackages = append(allPackages, m.config.NonWslPythonPackages...)
				}

				fmt.Printf("\n%s [DRY-RUN] Would install Python packages: %s\n",
					style.Colored(style.Green, style.SymInfo),
					strings.Join(allPackages, ", "))

				if len(m.config.PythonPipPackages) > 0 {
					packageManager := "pip"
					if m.config.UseUvPackageManager {
						packageManager = "UV"
					}
					fmt.Printf("\n%s [DRY-RUN] Would install Pip packages using %s: %s\n",
						style.Colored(style.Green, style.SymInfo),
						packageManager,
						strings.Join(m.config.PythonPipPackages, ", "))
				}
			}
		} else {
			// Use the application layer through menuManager
			var systemPackages []string
			if m.osInfo.OsType == "alpine" {
				systemPackages = m.config.AlpinePythonPackages
			} else {
				systemPackages = m.config.PythonPackages
				if os.Getenv("WSL") == "" {
					systemPackages = append(systemPackages, m.config.NonWslPythonPackages...)
				}
			}

			err := m.menuManager.InstallPythonPackages(
				systemPackages,
				m.config.PythonPipPackages,
				m.config.UseUvPackageManager)

			if err != nil {
				fmt.Printf("\n%s Failed to install Python packages: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s Python packages installed successfully\n",
					style.Colored(style.Green, style.SymCheckMark))
			}
		}
	case "0":
		return

	default:
		fmt.Printf("\n%s Invalid option. No changes were made.\n",
			style.Colored(style.Yellow, style.SymWarning))
	}

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}
