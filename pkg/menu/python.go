// pkg/menu/python.go

package menu

import (
	"fmt"
	"os"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/packages"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// PythonPackagesMenu handles Python package installation and configuration
func PythonPackagesMenu(cfg *config.Config, osInfo *osdetect.OSInfo) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Python Packages Installation", style.Blue))

	// Get OS-specific package information
	var packageDisplay string
	if osInfo.OsType == "alpine" {
		packageDisplay = fmt.Sprintf("Alpine Python packages: %s", 
			style.Colored(style.Cyan, strings.Join(cfg.AlpinePythonPackages, ", ")))
	} else {
		// For Debian/Ubuntu
		allPackages := append([]string{}, cfg.PythonPackages...)
		if os.Getenv("WSL") == "" {
			allPackages = append(allPackages, cfg.NonWslPythonPackages...)
		}
		
		packageDisplay = fmt.Sprintf("System Python packages: %s", 
			style.Colored(style.Cyan, strings.Join(allPackages, ", ")))
	}
	
	// Display pip packages if available
	pipPackageDisplay := ""
	if len(cfg.PythonPipPackages) > 0 {
		pipPackageDisplay = fmt.Sprintf("\n%s Pip packages: %s", 
			style.BulletItem, 
			style.Colored(style.Cyan, strings.Join(cfg.PythonPipPackages, ", ")))
	}

	// Display current Python package management settings
	fmt.Println()
	fmt.Println(style.Bolded("Current Package Management Settings:", style.Blue))
	
	// Create a formatter with the label we need
	formatter := style.NewStatusFormatter([]string{"UV Package Manager"}, 2)
	
	// Show UV package manager status
	if cfg.UseUvPackageManager {
		fmt.Println(formatter.FormatSuccess("UV Package Manager", "Enabled", "Modern, fast package manager"))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "UV Package Manager", "Disabled", 
			style.Yellow, "Using standard pip", "light"))
	}
	
	// Show package information
	fmt.Printf("\n%s %s", style.BulletItem, packageDisplay)
	if pipPackageDisplay != "" {
		fmt.Print(pipPackageDisplay)
	}

	// Create menu options
	var menuOptions []style.MenuOption
	
	// Toggle UV option
	if cfg.UseUvPackageManager {
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
	
	choice := ReadInput()
	
	switch choice {
	case "1":
		// Toggle UV package manager
		if cfg.UseUvPackageManager {
			cfg.UseUvPackageManager = false
			fmt.Printf("\n%s UV package manager has been %s. Will use standard pip.\n", 
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("disabled", style.Yellow))
		} else {
			cfg.UseUvPackageManager = true
			fmt.Printf("\n%s UV package manager has been %s. Will use UV for Python packages.\n", 
				style.Colored(style.Green, style.SymCheckMark),
				style.Bolded("enabled", style.Green))
		}

		// Save config changes
		configFile := "hardn.yml" // Default config file
		if err := config.SaveConfig(cfg, configFile); err != nil {
			logging.LogError("Failed to save configuration: %v", err)
		}
		
		// Return to this menu after toggling
		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		PythonPackagesMenu(cfg, osInfo)

	case "2":
		// Install packages
		fmt.Println("\nInstalling Python packages...")
		fmt.Println(style.Dimmed("This may take some time. Please wait..."))
		
		if err := packages.InstallPythonPackages(cfg, osInfo); err != nil {
			fmt.Printf("\n%s Failed to install Python packages: %v\n", 
				style.Colored(style.Red, style.SymCrossMark), err)
		} else {
			fmt.Printf("\n%s Python packages installed successfully!\n", 
				style.Colored(style.Green, style.SymCheckMark))
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