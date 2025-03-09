// pkg/menu/sources_menu.go

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
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// SourcesMenu handles configuration of package sources
type SourcesMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewSourcesMenu creates a new SourcesMenu
func NewSourcesMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *SourcesMenu {
	return &SourcesMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the sources menu and handles user input
func (m *SourcesMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Package Sources Configuration", style.Blue))

	// Display current OS info
	fmt.Println()
	fmt.Println(style.Bolded("System Information:", style.Blue))

	// Create formatter for status display
	formatter := style.NewStatusFormatter([]string{"OS Type", "Version", "Codename", "Proxmox"}, 2)

	// Show OS type
	osName := cases.Title(language.English).String(m.osInfo.OsType)
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "OS Type",
		osName, style.Cyan, ""))

	// Show OS version
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Version",
		m.osInfo.OsVersion, style.Cyan, ""))

	// Show OS codename (if not Alpine)
	if m.osInfo.OsType != "alpine" {
		osCodename := cases.Title(language.English).String(m.osInfo.OsCodename)
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Codename",
			osCodename, style.Cyan, ""))
	}

	// Show Proxmox status
	proxmoxStatus := "No"
	if m.osInfo.IsProxmox {
		proxmoxStatus = "Yes"
	}
	fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Proxmox",
		proxmoxStatus, style.Cyan, ""))

	// Display current source configuration
	fmt.Println()
	fmt.Println(style.Bolded("Current Source Configuration:", style.Blue))

	if m.osInfo.OsType == "alpine" {
		// Show Alpine repository status
		m.showAlpineRepositories()
	} else {
		// Show Debian/Ubuntu repository status
		m.showDebianRepositories()
	}

	// Create menu options based on OS type
	var menuOptions []style.MenuOption

	if m.osInfo.OsType == "alpine" {
		// Alpine specific options
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1,
			Title:       "Update Alpine repositories",
			Description: "Configure main and community repositories",
		})

		// Testing repository toggle
		if m.config.AlpineTestingRepo {
			menuOptions = append(menuOptions, style.MenuOption{
				Number:      2,
				Title:       "Disable testing repository",
				Description: "Remove edge/testing repository",
			})
		} else {
			menuOptions = append(menuOptions, style.MenuOption{
				Number:      2,
				Title:       "Enable testing repository",
				Description: "Add edge/testing repository (not recommended for production)",
			})
		}
	} else {
		// Debian/Ubuntu options
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      1,
			Title:       "Update package sources",
			Description: "Configure main system repositories",
		})

		// Proxmox specific options
		if m.osInfo.IsProxmox {
			menuOptions = append(menuOptions, style.MenuOption{
				Number:      2,
				Title:       "Configure Proxmox repositories",
				Description: "Set up Proxmox-specific repositories",
			})
		}

		// Add option to edit sources
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      3,
			Title:       "Edit repositories",
			Description: "Modify repository configuration",
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
		// Update main repositories
		fmt.Println("\nUpdating package sources...")

		if m.config.DryRun {
			fmt.Printf("%s [DRY-RUN] Would update package sources for %s\n",
				style.BulletItem, m.osInfo.OsType)
		} else {
			// Use application layer to update sources
			if err := m.menuManager.UpdatePackageSources(); err != nil {
				fmt.Printf("\n%s Failed to update package sources: %v\n",
					style.Colored(style.Red, style.SymCrossMark), err)
			} else {
				fmt.Printf("\n%s Package sources updated successfully\n",
					style.Colored(style.Green, style.SymCheckMark))
			}
		}

	case "2":
		if m.osInfo.OsType == "alpine" {
			// Toggle Alpine testing repository
			m.config.AlpineTestingRepo = !m.config.AlpineTestingRepo

			if m.config.AlpineTestingRepo {
				fmt.Printf("\n%s Alpine testing repository %s\n",
					style.Colored(style.Yellow, style.SymWarning),
					style.Bolded("enabled", style.Green))
				fmt.Printf("%s WARNING: Testing repositories may contain unstable packages\n",
					style.Colored(style.Yellow, style.SymWarning))
			} else {
				fmt.Printf("\n%s Alpine testing repository %s\n",
					style.Colored(style.Green, style.SymCheckMark),
					style.Bolded("disabled", style.Green))
			}

			// Save config changes
			m.saveSourcesConfig()

			// Apply the change
			if !m.config.DryRun {
				// Use application layer to update sources
				if err := m.menuManager.UpdatePackageSources(); err != nil {
					fmt.Printf("\n%s Failed to update package sources: %v\n",
						style.Colored(style.Red, style.SymCrossMark), err)
				} else {
					fmt.Printf("%s Package sources updated successfully\n",
						style.Colored(style.Green, style.SymCheckMark))
				}
			}
		} else if m.osInfo.IsProxmox {
			// Configure Proxmox repositories
			fmt.Println("\nConfiguring Proxmox repositories...")

			if m.config.DryRun {
				fmt.Printf("%s [DRY-RUN] Would configure Proxmox repositories\n", style.BulletItem)
			} else {
				// Use application layer to update Proxmox sources
				if err := m.menuManager.UpdateProxmoxSources(); err != nil {
					fmt.Printf("\n%s Failed to configure Proxmox repositories: %v\n",
						style.Colored(style.Red, style.SymCrossMark), err)
				} else {
					fmt.Printf("\n%s Proxmox repositories configured successfully\n",
						style.Colored(style.Green, style.SymCheckMark))
					fmt.Printf("%s Created /etc/apt/sources.list.d/ceph.list\n", style.BulletItem)
					fmt.Printf("%s Created /etc/apt/sources.list.d/pve-enterprise.list\n", style.BulletItem)
				}
			}
		} else {
			fmt.Printf("\n%s Invalid option for this OS type\n",
				style.Colored(style.Red, style.SymCrossMark))
		}

	case "3":
		if m.osInfo.OsType != "alpine" {
			// Edit repositories submenu
			m.editRepositoriesMenu()
			m.Show()
			return
		} else {
			fmt.Printf("\n%s Invalid option for this OS type\n",
				style.Colored(style.Red, style.SymCrossMark))
		}

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

// Helper function to show Alpine repositories
func (m *SourcesMenu) showAlpineRepositories() {
	// Check if repositories file exists
	reposFile := "/etc/apk/repositories"
	reposContent := ""

	if data, err := os.ReadFile(reposFile); err == nil {
		reposContent = string(data)
	}

	// Display repositories
	if reposContent != "" {
		lines := strings.Split(reposContent, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			// Color differently for testing repo
			if strings.Contains(line, "edge/testing") {
				fmt.Printf("%s %s\n",
					style.Colored(style.Yellow, style.SymWarning),
					style.Colored(style.Yellow, line))
			} else {
				fmt.Printf("%s %s\n", style.BulletItem, line)
			}
		}
	} else {
		fmt.Printf("%s Could not read %s\n",
			style.Colored(style.Yellow, style.SymWarning), reposFile)
	}

	// Show testing repository flag
	fmt.Println()
	if m.config.AlpineTestingRepo {
		fmt.Printf("%s Testing repository: %s\n",
			style.BulletItem, style.Colored(style.Yellow, "Enabled"))
	} else {
		fmt.Printf("%s Testing repository: %s\n",
			style.BulletItem, style.Colored(style.Green, "Disabled"))
	}
}

// Helper function to show Debian/Ubuntu repositories
func (m *SourcesMenu) showDebianRepositories() {
	// Check if sources file exists
	sourcesFile := "/etc/apt/sources.list"
	sourcesContent := ""

	if data, err := os.ReadFile(sourcesFile); err == nil {
		sourcesContent = string(data)
	}

	// Show main sources
	if sourcesContent != "" {
		fmt.Printf("%s %s:\n", style.BulletItem, style.Bolded("Main sources", style.Cyan))
		lines := strings.Split(sourcesContent, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			fmt.Printf("   %s\n", line)
		}
	} else {
		fmt.Printf("%s Could not read %s\n",
			style.Colored(style.Yellow, style.SymWarning), sourcesFile)
	}

	// Check Proxmox repositories if relevant
	if m.osInfo.IsProxmox {
		fmt.Println()
		fmt.Printf("%s %s:\n", style.BulletItem, style.Bolded("Proxmox repositories", style.Cyan))

		// Check Ceph repo
		cephFile := "/etc/apt/sources.list.d/ceph.list"
		if data, err := os.ReadFile(cephFile); err == nil {
			cephContent := string(data)
			lines := strings.Split(cephContent, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}

				fmt.Printf("   %s\n", line)
			}
		} else {
			fmt.Printf("   %s Ceph repository not configured\n",
				style.Colored(style.Yellow, style.SymWarning))
		}

		// Check Enterprise repo
		pveFile := "/etc/apt/sources.list.d/pve-enterprise.list"
		if data, err := os.ReadFile(pveFile); err == nil {
			pveContent := string(data)
			lines := strings.Split(pveContent, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}

				fmt.Printf("   %s\n", line)
			}
		} else {
			fmt.Printf("   %s Enterprise repository not configured\n",
				style.Colored(style.Yellow, style.SymWarning))
		}
	}

	// Show configured repositories
	fmt.Println()
	fmt.Printf("%s %s:\n", style.BulletItem, style.Bolded("Configured repositories", style.Cyan))

	if len(m.config.DebianRepos) > 0 {
		for _, repo := range m.config.DebianRepos {
			// Replace CODENAME placeholder with actual codename
			displayRepo := strings.ReplaceAll(repo, "CODENAME", m.osInfo.OsCodename)
			fmt.Printf("   %s\n", displayRepo)
		}
	} else {
		fmt.Printf("   %s No repositories configured\n",
			style.Colored(style.Yellow, style.SymWarning))
	}

	// Show Proxmox configured repositories if relevant
	if m.osInfo.IsProxmox {
		fmt.Println()
		fmt.Printf("%s %s:\n",
			style.BulletItem, style.Bolded("Configured Proxmox repositories", style.Cyan))

		// Show Proxmox source repos
		if len(m.config.ProxmoxSrcRepos) > 0 {
			fmt.Printf("   %s Source repositories:\n", style.BulletItem)
			for _, repo := range m.config.ProxmoxSrcRepos {
				// Replace CODENAME placeholder with actual codename
				displayRepo := strings.ReplaceAll(repo, "CODENAME", m.osInfo.OsCodename)
				fmt.Printf("      %s\n", displayRepo)
			}
		}

		// Show Ceph repos
		if len(m.config.ProxmoxCephRepo) > 0 {
			fmt.Printf("   %s Ceph repositories:\n", style.BulletItem)
			for _, repo := range m.config.ProxmoxCephRepo {
				// Replace CODENAME placeholder with actual codename
				displayRepo := strings.ReplaceAll(repo, "CODENAME", m.osInfo.OsCodename)
				fmt.Printf("      %s\n", displayRepo)
			}
		}

		// Show Enterprise repos
		if len(m.config.ProxmoxEnterpriseRepo) > 0 {
			fmt.Printf("   %s Enterprise repositories:\n", style.BulletItem)
			for _, repo := range m.config.ProxmoxEnterpriseRepo {
				// Replace CODENAME placeholder with actual codename
				displayRepo := strings.ReplaceAll(repo, "CODENAME", m.osInfo.OsCodename)
				fmt.Printf("      %s\n", displayRepo)
			}
		}
	}
}

// Helper function to edit repositories
func (m *SourcesMenu) editRepositoriesMenu() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Edit Repositories", style.Blue))

	// Create menu options
	var menuOptions []style.MenuOption

	// Basic options for all debian-based systems
	menuOptions = append(menuOptions, style.MenuOption{
		Number:      1,
		Title:       "Add repository",
		Description: "Add a new repository to configuration",
	})

	if len(m.config.DebianRepos) > 0 {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2,
			Title:       "Remove repository",
			Description: "Remove a repository from configuration",
		})
	}

	// Proxmox specific options
	if m.osInfo.IsProxmox {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      3,
			Title:       "Edit Proxmox repositories",
			Description: "Modify Proxmox-specific repositories",
		})
	}

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to sources menu",
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
		// Add repository
		fmt.Printf("\n%s Enter repository (e.g., 'deb http://deb.debian.org/debian CODENAME main'):\n",
			style.BulletItem)
		fmt.Printf("%s Use CODENAME as placeholder for the OS codename\n", style.BulletItem)
		fmt.Printf("> ")
		newRepo := ReadInput()

		if newRepo == "" {
			fmt.Printf("\n%s Repository cannot be empty\n",
				style.Colored(style.Red, style.SymCrossMark))
		} else {
			// Check for duplicate
			isDuplicate := false
			for _, repo := range m.config.DebianRepos {
				if repo == newRepo {
					isDuplicate = true
					break
				}
			}

			if isDuplicate {
				fmt.Printf("\n%s Repository already exists in configuration\n",
					style.Colored(style.Yellow, style.SymWarning))
			} else {
				// Add new repository
				m.config.DebianRepos = append(m.config.DebianRepos, newRepo)

				// Save config
				m.saveSourcesConfig()

				fmt.Printf("\n%s Repository added to configuration\n",
					style.Colored(style.Green, style.SymCheckMark))
			}
		}

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editRepositoriesMenu()

	case "2":
		// Remove repository
		if len(m.config.DebianRepos) == 0 {
			fmt.Printf("\n%s No repositories to remove\n",
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			fmt.Println()
			for i, repo := range m.config.DebianRepos {
				// Replace CODENAME placeholder with actual codename for display
				displayRepo := strings.ReplaceAll(repo, "CODENAME", m.osInfo.OsCodename)
				fmt.Printf("%s %d: %s\n", style.BulletItem, i+1, displayRepo)
			}

			fmt.Printf("\n%s Enter repository number to remove (1-%d): ",
				style.BulletItem, len(m.config.DebianRepos))
			numStr := ReadInput()

			// Parse number
			num := 0
			n, err := fmt.Sscanf(numStr, "%d", &num)
			if err != nil || n != 1 {
				fmt.Printf("\n%s Invalid repository number: not a valid number\n",
					style.Colored(style.Red, style.SymCrossMark))
			} else if num < 1 || num > len(m.config.DebianRepos) {
				fmt.Printf("\n%s Invalid repository number: out of range\n",
					style.Colored(style.Red, style.SymCrossMark))
			} else {
				// Remove repository (adjust for 0-based index)
				removedRepo := m.config.DebianRepos[num-1]
				m.config.DebianRepos = append(m.config.DebianRepos[:num-1], m.config.DebianRepos[num:]...)

				// Save config
				m.saveSourcesConfig()

				// Replace CODENAME placeholder with actual codename for display
				displayRepo := strings.ReplaceAll(removedRepo, "CODENAME", m.osInfo.OsCodename)
				fmt.Printf("\n%s Repository removed from configuration:\n",
					style.Colored(style.Green, style.SymCheckMark))
				fmt.Printf("%s %s\n", style.BulletItem, displayRepo)
			}
		}

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editRepositoriesMenu()

	case "3":
		// Edit Proxmox repositories (only for Proxmox)
		if m.osInfo.IsProxmox {
			m.editProxmoxRepositoriesMenu()
			m.editRepositoriesMenu()
			return
		} else {
			fmt.Printf("\n%s Invalid option for this OS type\n",
				style.Colored(style.Red, style.SymCrossMark))

			fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
			ReadKey()
			m.editRepositoriesMenu()
		}

	case "0":
		// Return to sources menu
		return

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editRepositoriesMenu()
		return
	}
}

// Helper function to edit Proxmox repositories
func (m *SourcesMenu) editProxmoxRepositoriesMenu() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Edit Proxmox Repositories", style.Blue))

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Edit source repositories", Description: "Modify main Proxmox repositories"},
		{Number: 2, Title: "Edit Ceph repositories", Description: "Modify Proxmox Ceph repositories"},
		{Number: 3, Title: "Edit Enterprise repositories", Description: "Modify Proxmox Enterprise repositories"},
	}

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to edit repositories menu",
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
		// Edit source repositories
		m.editProxmoxRepoList("source",
			"Proxmox Source Repositories", &m.config.ProxmoxSrcRepos)
		m.editProxmoxRepositoriesMenu()
		return

	case "2":
		// Edit Ceph repositories
		m.editProxmoxRepoList("ceph",
			"Proxmox Ceph Repositories", &m.config.ProxmoxCephRepo)
		m.editProxmoxRepositoriesMenu()
		return

	case "3":
		// Edit Enterprise repositories
		m.editProxmoxRepoList("enterprise",
			"Proxmox Enterprise Repositories", &m.config.ProxmoxEnterpriseRepo)
		m.editProxmoxRepositoriesMenu()
		return

	case "0":
		// Return to edit repositories menu
		return

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editProxmoxRepositoriesMenu()
		return
	}
}

// Helper function to edit a Proxmox repository list
func (m *SourcesMenu) editProxmoxRepoList(
	repoType, title string, repoList *[]string) {

	utils.PrintHeader()
	fmt.Println(style.Bolded(title, style.Blue))

	// Display current repositories
	fmt.Println()
	fmt.Println(style.Bolded("Current Repositories:", style.Blue))

	if len(*repoList) == 0 {
		fmt.Printf("%s No repositories configured\n", style.BulletItem)
	} else {
		for i, repo := range *repoList {
			// Replace CODENAME placeholder with actual codename for display
			displayRepo := strings.ReplaceAll(repo, "CODENAME", m.osInfo.OsCodename)
			fmt.Printf("%s %d: %s\n", style.BulletItem, i+1, displayRepo)
		}
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Add repository", Description: "Add a new repository to configuration"},
	}

	if len(*repoList) > 0 {
		menuOptions = append(menuOptions, style.MenuOption{
			Number:      2,
			Title:       "Remove repository",
			Description: "Remove a repository from configuration",
		})
	}

	// Add options to use default repositories
	menuOptions = append(menuOptions, style.MenuOption{
		Number:      3,
		Title:       "Use default repositories",
		Description: "Reset to recommended repositories",
	})

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return to previous menu",
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
		// Add repository
		fmt.Printf("\n%s Enter repository:\n", style.BulletItem)
		fmt.Printf("%s Use CODENAME as placeholder for the OS codename\n", style.BulletItem)
		fmt.Printf("> ")
		newRepo := ReadInput()

		if newRepo == "" {
			fmt.Printf("\n%s Repository cannot be empty\n",
				style.Colored(style.Red, style.SymCrossMark))
		} else {
			// Check for duplicate
			isDuplicate := false
			for _, repo := range *repoList {
				if repo == newRepo {
					isDuplicate = true
					break
				}
			}

			if isDuplicate {
				fmt.Printf("\n%s Repository already exists in configuration\n",
					style.Colored(style.Yellow, style.SymWarning))
			} else {
				// Add new repository
				*repoList = append(*repoList, newRepo)

				// Save config
				m.saveSourcesConfig()

				fmt.Printf("\n%s Repository added to configuration\n",
					style.Colored(style.Green, style.SymCheckMark))
			}
		}

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editProxmoxRepoList(repoType, title, repoList)
		return

	case "2":
		// Remove repository
		if len(*repoList) == 0 {
			fmt.Printf("\n%s No repositories to remove\n",
				style.Colored(style.Yellow, style.SymWarning))
		} else {
			fmt.Println()
			for i, repo := range *repoList {
				// Replace CODENAME placeholder with actual codename for display
				displayRepo := strings.ReplaceAll(repo, "CODENAME", m.osInfo.OsCodename)
				fmt.Printf("%s %d: %s\n", style.BulletItem, i+1, displayRepo)
			}

			fmt.Printf("\n%s Enter repository number to remove (1-%d): ",
				style.BulletItem, len(*repoList))
			numStr := ReadInput()

			// Parse number
			num := 0
			n, err := fmt.Sscanf(numStr, "%d", &num)
			if err != nil || n != 1 {
				fmt.Printf("\n%s Invalid repository number: not a valid number\n",
					style.Colored(style.Red, style.SymCrossMark))
			} else if num < 1 || num > len(*repoList) {
				fmt.Printf("\n%s Invalid repository number: out of range\n",
					style.Colored(style.Red, style.SymCrossMark))
			} else {
				// Remove repository (adjust for 0-based index)
				removedRepo := (*repoList)[num-1]
				*repoList = append((*repoList)[:num-1], (*repoList)[num:]...)

				// Save config
				m.saveSourcesConfig()

				// Replace CODENAME placeholder with actual codename for display
				displayRepo := strings.ReplaceAll(removedRepo, "CODENAME", m.osInfo.OsCodename)
				fmt.Printf("\n%s Repository removed from configuration:\n",
					style.Colored(style.Green, style.SymCheckMark))
				fmt.Printf("%s %s\n", style.BulletItem, displayRepo)
			}
		}

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editProxmoxRepoList(repoType, title, repoList)
		return

	case "3":
		// Use default repositories
		fmt.Printf("\n%s Reset to default repositories? This will overwrite current configuration. (y/n): ",
			style.Colored(style.Yellow, style.SymWarning))
		confirm := ReadInput()

		if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
			// Set default repositories based on type
			switch repoType {
			case "source":
				*repoList = []string{
					"deb http://ftp.us.debian.org/debian CODENAME main contrib",
					"deb http://ftp.us.debian.org/debian CODENAME-updates main contrib",
					"deb http://security.debian.org CODENAME-security main contrib",
					"deb http://download.proxmox.com/debian/pve CODENAME pve-no-subscription",
				}
			case "ceph":
				*repoList = []string{
					"#deb https://enterprise.proxmox.com/debian/ceph-quincy CODENAME enterprise",
					"deb http://download.proxmox.com/debian/ceph-reef CODENAME no-subscription",
				}
			case "enterprise":
				*repoList = []string{
					"#deb https://enterprise.proxmox.com/debian/pve CODENAME pve-enterprise",
				}
			}

			// Save config
			m.saveSourcesConfig()

			fmt.Printf("\n%s Repositories reset to defaults\n",
				style.Colored(style.Green, style.SymCheckMark))
		} else {
			fmt.Println("\nOperation cancelled.")
		}

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editProxmoxRepoList(repoType, title, repoList)
		return

	case "0":
		// Return to previous menu
		return

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))

		fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
		ReadKey()
		m.editProxmoxRepoList(repoType, title, repoList)
		return
	}
}

// Helper function to save sources configuration
func (m *SourcesMenu) saveSourcesConfig() {
	// Save config changes
	configFile := "hardn.yml" // Default config file
	if err := config.SaveConfig(m.config, configFile); err != nil {
		fmt.Printf("\n%s Failed to save configuration: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)
	}
}
