// pkg/menu/host_info_menu.go
package menu

import (
	"fmt"
	"sort"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// HostInfoMenu handles displaying host information
type HostInfoMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewHostInfoMenu creates a new HostInfoMenu
func NewHostInfoMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *HostInfoMenu {
	return &HostInfoMenu{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Show displays the host information menu and handles user input
func (m *HostInfoMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("Host Information", style.Blue))

	// Get host information
	hostInfo, err := m.menuManager.GetHostInfo()
	if err != nil {
		fmt.Printf("\n%s Error retrieving host information: %v\n",
			style.Colored(style.Red, style.SymCrossMark), err)

		// Create basic host info if we failed to get it
		hostInfo = &model.HostInfo{}
	}

	// Display host information sections
	m.displaySystemInfo(hostInfo)
	fmt.Println()
	m.displayNetworkInfo(hostInfo)
	fmt.Println()
	m.displayUserInfo(hostInfo)
	fmt.Println()
	m.displayStorageInfo(hostInfo)

	// Create menu options
	menuOptions := []style.MenuOption{
		{Number: 1, Title: "Refresh Information", Description: "Reload host information from system"},
		{Number: 2, Title: "Export Information", Description: "Export host information to file"},
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
		// Refresh information and redisplay menu
		m.Show()
		return

	case "2":
		// Export host information
		m.exportHostInfo(hostInfo)
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
	}
}

// displaySystemInfo displays system information
func (m *HostInfoMenu) displaySystemInfo(info *model.HostInfo) {
	fmt.Println(style.Bolded("\nSystem Information:", style.Blue))

	// Create formatter for clean output alignment
	formatter := style.NewStatusFormatter([]string{
		"Hostname",
		"Domain",
		"OS Name",
		"OS Version",
		"Kernel",
		"Uptime",
		"CPU Info",
		"Memory",
	}, 2)

	// Hostname
	if info.Hostname != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Hostname",
			info.Hostname, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "Hostname",
			"Unknown", style.Yellow, ""))
	}

	// Domain
	if info.Domain != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Domain",
			info.Domain, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "Domain",
			"Not set", style.Yellow, ""))
	}

	// OS Name
	if info.OSName != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "OS Name",
			info.OSName, style.Cyan, ""))
	} else if m.osInfo != nil {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "OS Name",
			m.osInfo.OsType, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "OS Name",
			"Unknown", style.Yellow, ""))
	}

	// OS Version
	if info.OSVersion != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "OS Version",
			info.OSVersion, style.Cyan, ""))
	} else if m.osInfo != nil {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "OS Version",
			m.osInfo.OsVersion, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "OS Version",
			"Unknown", style.Yellow, ""))
	}

	// Kernel
	if info.KernelInfo != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Kernel",
			info.KernelInfo, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "Kernel",
			"Unknown", style.Yellow, ""))
	}

	// Uptime
	if info.Uptime > 0 {
		uptimeStr := m.menuManager.FormatUptime(info.Uptime)
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Uptime",
			uptimeStr, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "Uptime",
			"Unknown", style.Yellow, ""))
	}

	// CPU Info
	if info.CPUInfo != "" {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "CPU Info",
			info.CPUInfo, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "CPU Info",
			"Unknown", style.Yellow, ""))
	}

	// Memory
	if info.MemoryTotal > 0 {
		memTotal := m.menuManager.FormatBytes(info.MemoryTotal)
		memFree := m.menuManager.FormatBytes(info.MemoryFree)
		memUsed := m.menuManager.FormatBytes(info.MemoryTotal - info.MemoryFree)
		memDisplay := fmt.Sprintf("%s total, %s used, %s free", memTotal, memUsed, memFree)

		fmt.Println(formatter.FormatLine(style.SymInfo, style.Cyan, "Memory",
			memDisplay, style.Cyan, ""))
	} else {
		fmt.Println(formatter.FormatLine(style.SymInfo, style.Yellow, "Memory",
			"Unknown", style.Yellow, ""))
	}
}

// displayNetworkInfo displays network information
func (m *HostInfoMenu) displayNetworkInfo(info *model.HostInfo) {
	fmt.Println(style.Bolded("\nNetwork Information:", style.Blue))

	// IP Addresses
	fmt.Printf("%s IP Addresses:\n", style.Bolded("", style.Cyan))
	if len(info.IPAddresses) > 0 {
		for _, ip := range info.IPAddresses {
			fmt.Printf("   %s %s\n", style.BulletItem, ip)
		}
	} else {
		fmt.Printf("   %s None detected\n", style.Colored(style.Yellow, style.SymWarning))
	}

	// DNS Servers
	fmt.Printf("\n%s DNS Servers:\n", style.Bolded("", style.Cyan))
	if len(info.DNSServers) > 0 {
		for _, dns := range info.DNSServers {
			fmt.Printf("   %s %s\n", style.BulletItem, dns)
		}
	} else {
		fmt.Printf("   %s None detected\n", style.Colored(style.Yellow, style.SymWarning))
	}
}

// displayUserInfo displays user information
func (m *HostInfoMenu) displayUserInfo(info *model.HostInfo) {
	fmt.Println(style.Bolded("\nUser Information:", style.Blue))

	// Users
	fmt.Printf("%s Non-System Users:\n", style.Bolded("", style.Cyan))
	if len(info.Users) > 0 {
		for _, user := range info.Users {
			sudoStatus := ""
			if user.HasSudo {
				sudoStatus = " " + style.Colored(style.Green, "(sudo)")
			}
			fmt.Printf("   %s %s%s\n", style.BulletItem, user.Username, sudoStatus)
		}
	} else {
		fmt.Printf("   %s None detected\n", style.Colored(style.Yellow, style.SymWarning))
	}

	// Groups
	fmt.Printf("\n%s Non-System Groups:\n", style.Bolded("", style.Cyan))
	if len(info.Groups) > 0 {
		for _, group := range info.Groups {
			fmt.Printf("   %s %s\n", style.BulletItem, group)
		}
	} else {
		fmt.Printf("   %s None detected\n", style.Colored(style.Yellow, style.SymWarning))
	}
}

// displayStorageInfo displays storage information
func (m *HostInfoMenu) displayStorageInfo(info *model.HostInfo) {
	fmt.Println(style.Bolded("\nStorage Information:", style.Blue))

	if len(info.DiskTotal) > 0 {
		// Get mount points and sort them
		var mountPoints []string
		for mount := range info.DiskTotal {
			mountPoints = append(mountPoints, mount)
		}
		sort.Strings(mountPoints)

		// Print storage information for each mount point
		for _, mount := range mountPoints {
			total := info.DiskTotal[mount]
			free, hasFree := info.DiskFree[mount]

			if !hasFree {
				free = 0
			}

			used := total - free

			// Format values
			totalStr := m.menuManager.FormatBytes(total)
			usedStr := m.menuManager.FormatBytes(used)
			freeStr := m.menuManager.FormatBytes(free)

			// Calculate usage percentage
			usagePercent := 0
			if total > 0 {
				usagePercent = int((used * 100) / total)
			}

			// Display usage with color based on percentage
			var usageColor string
			if usagePercent >= 90 {
				usageColor = style.Red
			} else if usagePercent >= 70 {
				usageColor = style.Yellow
			} else {
				usageColor = style.Green
			}

			usageStr := fmt.Sprintf("%d%%", usagePercent)

			fmt.Printf("%s %s:\n", style.BulletItem, style.Bolded(mount, style.Cyan))
			fmt.Printf("   %s Total: %s\n", style.BulletItem, totalStr)
			fmt.Printf("   %s Used: %s (%s)\n", style.BulletItem, usedStr, style.Colored(usageColor, usageStr))
			fmt.Printf("   %s Free: %s\n", style.BulletItem, freeStr)
			fmt.Println()
		}
	} else {
		fmt.Printf("%s No storage information available\n",
			style.Colored(style.Yellow, style.SymWarning))
	}
}

// exportHostInfo writes host information to a file
func (m *HostInfoMenu) exportHostInfo(info *model.HostInfo) {
	fmt.Printf("\n%s Enter filename to export host information (default: host_info.txt): ", style.BulletItem)
	filename := ReadInput()

	if filename == "" {
		filename = "host_info.txt"
	}

	// Build the content
	var content strings.Builder

	// System Info
	content.WriteString("# HOST INFORMATION REPORT\n\n")
	content.WriteString("## SYSTEM INFORMATION\n\n")

	content.WriteString(fmt.Sprintf("Hostname: %s\n", info.Hostname))
	if info.Domain != "" {
		content.WriteString(fmt.Sprintf("Domain: %s\n", info.Domain))
	}

	if info.OSName != "" {
		content.WriteString(fmt.Sprintf("OS: %s %s\n", info.OSName, info.OSVersion))
	} else if m.osInfo != nil {
		content.WriteString(fmt.Sprintf("OS: %s %s\n", m.osInfo.OsType, m.osInfo.OsVersion))
	}

	if info.KernelInfo != "" {
		content.WriteString(fmt.Sprintf("Kernel: %s\n", info.KernelInfo))
	}

	if info.Uptime > 0 {
		content.WriteString(fmt.Sprintf("Uptime: %s\n", m.menuManager.FormatUptime(info.Uptime)))
	}

	if info.CPUInfo != "" {
		content.WriteString(fmt.Sprintf("CPU: %s\n", info.CPUInfo))
	}

	if info.MemoryTotal > 0 {
		content.WriteString(fmt.Sprintf("Memory: %s total, %s free\n",
			m.menuManager.FormatBytes(info.MemoryTotal),
			m.menuManager.FormatBytes(info.MemoryFree)))
	}

	// Network Info
	content.WriteString("\n## NETWORK INFORMATION\n\n")

	if len(info.IPAddresses) > 0 {
		content.WriteString("IP Addresses:\n")
		for _, ip := range info.IPAddresses {
			content.WriteString(fmt.Sprintf("- %s\n", ip))
		}
	}

	if len(info.DNSServers) > 0 {
		content.WriteString("\nDNS Servers:\n")
		for _, dns := range info.DNSServers {
			content.WriteString(fmt.Sprintf("- %s\n", dns))
		}
	}

	// User Info
	content.WriteString("\n## USER INFORMATION\n\n")

	if len(info.Users) > 0 {
		content.WriteString("Non-System Users:\n")
		for _, user := range info.Users {
			sudoStatus := ""
			if user.HasSudo {
				sudoStatus = " (sudo)"
			}
			content.WriteString(fmt.Sprintf("- %s%s\n", user.Username, sudoStatus))
		}
	}

	if len(info.Groups) > 0 {
		content.WriteString("\nNon-System Groups:\n")
		for _, group := range info.Groups {
			content.WriteString(fmt.Sprintf("- %s\n", group))
		}
	}

	// Storage Info
	content.WriteString("\n## STORAGE INFORMATION\n\n")

	if len(info.DiskTotal) > 0 {
		// Get mount points and sort them
		var mountPoints []string
		for mount := range info.DiskTotal {
			mountPoints = append(mountPoints, mount)
		}
		sort.Strings(mountPoints)

		for _, mount := range mountPoints {
			total := info.DiskTotal[mount]
			free, hasFree := info.DiskFree[mount]

			if !hasFree {
				free = 0
			}

			used := total - free

			// Calculate usage percentage
			usagePercent := 0
			if total > 0 {
				usagePercent = int((used * 100) / total)
			}

			content.WriteString(fmt.Sprintf("%s:\n", mount))
			content.WriteString(fmt.Sprintf("- Total: %s\n", m.menuManager.FormatBytes(total)))
			content.WriteString(fmt.Sprintf("- Used: %s (%d%%)\n", m.menuManager.FormatBytes(used), usagePercent))
			content.WriteString(fmt.Sprintf("- Free: %s\n\n", m.menuManager.FormatBytes(free)))
		}
	}

	// Create file
	if m.config.DryRun {
		fmt.Printf("\n%s [DRY-RUN] Would write host information to %s\n",
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
			fmt.Printf("\n%s Host information exported to %s\n",
				style.Colored(style.Green, style.SymCheckMark),
				filename)
		}
	}

	fmt.Printf("\n%s Press any key to continue...", style.BulletItem)
	ReadKey()
}
