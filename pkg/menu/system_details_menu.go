// pkg/menu/system_details_menu.go
package menu

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/system"
	"github.com/abbott/hardn/pkg/utils"
)

// SystemDetailsMenu handles displaying host information
type SystemDetailsMenu struct {
	config          *config.Config
	osInfo          *osdetect.OSInfo
	hostInfoManager *application.HostInfoManager
}

// NewSystemDetailsMenu creates a new SystemDetailsMenu
func NewSystemDetailsMenu(
	config *config.Config,
	osInfo *osdetect.OSInfo,
	hostInfoManager *application.HostInfoManager,
) *SystemDetailsMenu {
	return &SystemDetailsMenu{
		config:          config,
		osInfo:          osInfo,
		hostInfoManager: hostInfoManager,
	}
}

// Show displays the host information menu and handles user input
func (m *SystemDetailsMenu) Show() {
	utils.ClearScreen()

	// Get detailed system information using our enhanced status package
	systemInfo, err := system.GenerateSystemStatus(m.hostInfoManager)
	if err != nil {
		fmt.Printf("\n%s Error retrieving system status: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)

		// Create error display box
		boxConfig := style.BoxConfig{
			Width:          60,
			BorderColor:    style.Red,
			ShowEmptyRow:   true,
			ShowTopBorder:  true,
			ShowLeftBorder: false,
			Title:          "System Details Error",
			TitleColor:     style.BrightRed,
		}

		box := style.NewBox(boxConfig)
		box.DrawBox(func(printLine func(string)) {
			printLine(style.Colored(style.Red, "Failed to retrieve system status information."))
			printLine(style.Colored(style.Red, fmt.Sprintf("Error: %v", err)))
			printLine("")
			printLine("Please check system permissions and try again.")
		})
	} else {
		// Display the detailed system status
		system.DisplayMachineStatus(systemInfo)
	}

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Refresh Information", Description: "Reload system status from system"},
	}

	// Add export option only if we have system info
	if err == nil {
		menuOptions = append(menuOptions, style.MenuOption{
			Number: 2, Title: "Export Information", Description: "Export system status to file",
		})
	}

	// Create menu
	menu := style.NewMenu("Select an option", menuOptions)
	menu.SetExitOption(style.MenuOption{
		Number:      0,
		Title:       "Return",
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
		// Refresh information and redisplay menu
		m.Show()
		return

	case "2":
		// Export system information (only if we have system info)
		if err == nil && systemInfo != nil {
			m.exportSystemDetails(systemInfo)
		} else {
			fmt.Printf("\n%s Cannot export: No system information available\n",
				style.Colored(style.Red, style.SymCrossMark))
			fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
			ReadKey()
		}
		m.Show()
		return

	case "0":
		// Return to main menu
		return

	default:
		fmt.Printf("\n%s Invalid option. Please try again.\n",
			style.Colored(style.Red, style.SymCrossMark))

		fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
		ReadKey()
		m.Show()
	}
}

// exportSystemDetails writes system information to a file
func (m *SystemDetailsMenu) exportSystemDetails(info *system.SystemDetails) {
	fmt.Printf("\n%s Enter filename to export system status (default: system_status.txt): ", style.BulletItem)
	filename := ReadInput()

	if filename == "" {
		filename = "system_status.txt"
	}

	// Build the content
	var content strings.Builder

	// System Info
	content.WriteString("# System Details\n\n")
	content.WriteString("## operating system\n\n")

	content.WriteString(fmt.Sprintf("OS: %s %s\n", info.OSName, info.OSVersion))
	content.WriteString(fmt.Sprintf("Kernel: %s\n", info.Kernel))
	content.WriteString(fmt.Sprintf("Hostname: %s\n", info.Hostname))
	if info.Domain != "" {
		content.WriteString(fmt.Sprintf("Domain: %s\n", info.Domain))
	}
	content.WriteString(fmt.Sprintf("User: %s\n", info.CurrentUser))
	content.WriteString(fmt.Sprintf("Uptime: %s\n", info.UptimeLongFormat))

	// Network Info
	content.WriteString("\n## network\n\n")

	content.WriteString("IP Addresses:\n")
	for _, ip := range info.IPAddresses {
		content.WriteString(fmt.Sprintf("- %s\n", ip))
	}

	content.WriteString("\nClient IP: " + info.ClientIP + "\n")

	content.WriteString("\nDNS Servers:\n")
	for _, dns := range info.DNSServers {
		content.WriteString(fmt.Sprintf("- %s\n", dns))
	}

	// User Info
	if len(info.Users) > 0 {
		content.WriteString("\n## users\n\n")
		content.WriteString("Non-System Users:\n")
		for _, user := range info.Users {
			sudoStatus := ""
			if user.HasSudo {
				sudoStatus = " (sudo)"
			}
			content.WriteString(fmt.Sprintf("- %s%s\n", user.Username, sudoStatus))
		}
	}

	// CPU Info
	content.WriteString("\n## CPU\n\n")
	content.WriteString(fmt.Sprintf("Processor: %s\n", info.CPUModel))
	content.WriteString(fmt.Sprintf("Cores: %d vCPU(s) / %d Socket(s)\n", info.CPUCores, info.CPUSockets))
	content.WriteString(fmt.Sprintf("Hypervisor: %s\n", info.CPUHypervisor))
	content.WriteString(fmt.Sprintf("CPU Freq: %.2f GHz\n", info.CPUFrequency))
	content.WriteString(fmt.Sprintf("Load 1m: %.2f\n", info.LoadAvg1))
	content.WriteString(fmt.Sprintf("Load 5m: %.2f\n", info.LoadAvg5))
	content.WriteString(fmt.Sprintf("Load 15m: %.2f\n", info.LoadAvg15))

	// Memory Info
	content.WriteString("\n## memory\n\n")
	content.WriteString(fmt.Sprintf("Memory Total: %.2f GiB\n", info.MemoryTotalGB))
	content.WriteString(fmt.Sprintf("Memory Used: %.2f GiB (%.2f%%)\n", info.MemoryUsedGB, info.MemoryPercent))
	content.WriteString(fmt.Sprintf("Memory Free: %.2f GiB\n", info.MemoryFreeGB))

	// Disk Info
	content.WriteString("\n## disks\n\n")
	if info.ZFSPresent {
		content.WriteString(fmt.Sprintf("ZFS Filesystem: %s\n", info.ZFSFilesystem))
		content.WriteString(fmt.Sprintf("ZFS Health: %s\n", info.ZFSHealth))
		content.WriteString(fmt.Sprintf("ZFS Used: %.2f GB\n", info.ZFSUsedGB))
		content.WriteString(fmt.Sprintf("ZFS Available: %.2f GB\n", info.ZFSAvailableGB))
		content.WriteString(fmt.Sprintf("ZFS Usage: %.2f%%\n", info.ZFSPercent))
	} else {
		content.WriteString(fmt.Sprintf("Root Partition: %s\n", info.RootPartition))
		content.WriteString(fmt.Sprintf("Root Total: %.2f GB\n", info.RootTotalGB))
		content.WriteString(fmt.Sprintf("Root Used: %.2f GB\n", info.RootUsedGB))
		content.WriteString(fmt.Sprintf("Root Free: %.2f GB\n", info.RootFreeGB))
		content.WriteString(fmt.Sprintf("Disk Usage: %.2f%%\n", info.DiskPercent))
	}

	// Login Info
	content.WriteString("\n## login\n\n")
	content.WriteString(fmt.Sprintf("Last Login: %s\n", info.LastLoginTime))
	if info.LastLoginPresent && info.LastLoginIP != "" {
		content.WriteString(fmt.Sprintf("From: %s\n", info.LastLoginIP))
	}

	// Create file
	if m.config.DryRun {
		fmt.Printf("\n%s [DRY-RUN] Would write system status to %s\n",
			style.Colored(style.Green, style.SymInfo),
			filename)
	} else {
		// Use OS standard library to write file since we're not using the filesystem interface here
		err := utils.WriteToFile(filename, content.String())
		if err != nil {
			fmt.Printf("\n%s Failed to write file: %v\n",
				style.Colored(style.Red, style.SymCrossMark),
				err)
		} else {
			fmt.Printf("\n%s Machine status exported to %s\n",
				style.Colored(style.Green, style.SymCheckMark),
				filename)
		}
	}

	fmt.Printf("\n%s Press any key to continue...", style.Dimmed(style.SymRightCarrot))
	ReadKey()
}
