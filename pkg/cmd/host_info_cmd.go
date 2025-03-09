// pkg/cmd/host_info_cmd.go
package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	exportFile    string
	outputFormat  string
	sectionFilter string
	formatAsJson  bool
	formatAsYaml  bool
)

// HostInfoCmd returns the host-info command
func HostInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host-info",
		Short: "Display host system information",
		Long:  `Gathers and displays detailed information about the host system including network, users, and storage.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHostInfo()
		},
	}

	// Add flags
	cmd.Flags().StringVarP(&exportFile, "export", "e", "", "Export host information to file")
	cmd.Flags().StringVarP(&sectionFilter, "section", "s", "", "Display only specific section (system, network, users, storage)")
	cmd.Flags().StringVarP(&outputFormat, "format", "f", "text", "Output format (text, json, yaml)")
	cmd.Flags().BoolVar(&formatAsJson, "json", false, "Output in JSON format (shorthand)")
	cmd.Flags().BoolVar(&formatAsYaml, "yaml", false, "Output in YAML format (shorthand)")

	return cmd
}

// runHostInfo executes the host-info command
func runHostInfo() error {
	// Set up provider
	provider := interfaces.NewProvider()

	// Detect OS
	osInfo, err := osdetect.DetectOS()
	if err != nil {
		return fmt.Errorf("failed to detect OS: %w", err)
	}

	// Set output format from shorthand flags if they were used
	if formatAsJson {
		outputFormat = "json"
	} else if formatAsYaml {
		outputFormat = "yaml"
	}

	// Create host info repository
	hostInfoRepo := secondary.NewOSHostInfoRepository(provider.FS, provider.Commander, osInfo.OsType)

	// Create domain service
	hostInfoService := service.NewHostInfoServiceImpl(hostInfoRepo, model.OSInfo{
		Type:      osInfo.OsType,
		Version:   osInfo.OsVersion,
		Codename:  osInfo.OsCodename,
		IsProxmox: osInfo.IsProxmox,
	})

	// Create application service
	hostInfoManager := application.NewHostInfoManager(hostInfoService)

	// Get host information
	info, err := hostInfoManager.GetHostInfo()
	if err != nil {
		return fmt.Errorf("failed to get host information: %w", err)
	}

	// If export file is specified, write to file
	if exportFile != "" {
		err := exportHostInfo(exportFile, info, hostInfoManager, outputFormat)
		if err != nil {
			return fmt.Errorf("failed to export host information: %w", err)
		}
		fmt.Printf("Host information exported to %s\n", exportFile)
		return nil
	}

	// Otherwise, print to console
	if outputFormat == "json" {
		// Output as JSON
		jsonData, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal host information to JSON: %w", err)
		}
		fmt.Println(string(jsonData))
		return nil
	} else if outputFormat == "yaml" {
		// For YAML we'd need to import a YAML library
		// For now just indicate it's not implemented
		return fmt.Errorf("YAML output format not implemented yet")
	} else {
		// Default to text output
		printHostInfo(info, hostInfoManager, sectionFilter)
	}

	return nil
}

// exportHostInfo exports host information to a file
func exportHostInfo(filePath string, info *model.HostInfo, manager *application.HostInfoManager, format string) error {
	// If JSON or YAML format, marshal and write directly
	if format == "json" {
		jsonData, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal host information to JSON: %w", err)
		}
		return utils.WriteToFile(filePath, string(jsonData))
	} else if format == "yaml" {
		// For YAML we'd need to import a YAML library
		// For now just indicate it's not implemented
		return fmt.Errorf("YAML output format not implemented yet")
	}

	// For text format, build a formatted report
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
	}

	if info.KernelInfo != "" {
		content.WriteString(fmt.Sprintf("Kernel: %s\n", info.KernelInfo))
	}

	if info.Uptime > 0 {
		content.WriteString(fmt.Sprintf("Uptime: %s\n", manager.FormatUptime(info.Uptime)))
	}

	if info.CPUInfo != "" {
		content.WriteString(fmt.Sprintf("CPU: %s\n", info.CPUInfo))
	}

	if info.MemoryTotal > 0 {
		content.WriteString(fmt.Sprintf("Memory: %s total, %s free\n",
			manager.FormatBytes(info.MemoryTotal),
			manager.FormatBytes(info.MemoryFree)))
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

	for mount, total := range info.DiskTotal {
		free := info.DiskFree[mount]
		used := total - free

		// Calculate usage percentage
		usagePercent := 0
		if total > 0 {
			usagePercent = int((used * 100) / total)
		}

		content.WriteString(fmt.Sprintf("%s:\n", mount))
		content.WriteString(fmt.Sprintf("- Total: %s\n", manager.FormatBytes(total)))
		content.WriteString(fmt.Sprintf("- Used: %s (%d%%)\n", manager.FormatBytes(used), usagePercent))
		content.WriteString(fmt.Sprintf("- Free: %s\n\n", manager.FormatBytes(free)))
	}

	// Write to file
	return utils.WriteToFile(filePath, content.String())
}

// printHostInfo prints host information to the console
func printHostInfo(info *model.HostInfo, manager *application.HostInfoManager, section string) {
	// Print header
	fmt.Println("# HOST INFORMATION REPORT")

	// Filter by section if specified
	showSystem := true
	showNetwork := true
	showUsers := true
	showStorage := true

	if section != "" {
		// Reset all sections
		showSystem = false
		showNetwork = false
		showUsers = false
		showStorage = false

		// Enable only requested section
		switch strings.ToLower(section) {
		case "system":
			showSystem = true
		case "network":
			showNetwork = true
		case "users":
			showUsers = true
		case "storage":
			showStorage = true
		default:
			logging.LogWarning("Unknown section: %s, showing all", section)
			showSystem = true
			showNetwork = true
			showUsers = true
			showStorage = true
		}
	}

	// System Information
	if showSystem {
		fmt.Println("\n## SYSTEM INFORMATION")
		fmt.Printf("Hostname: %s\n", info.Hostname)
		if info.Domain != "" {
			fmt.Printf("Domain: %s\n", info.Domain)
		}
		if info.OSName != "" {
			fmt.Printf("OS: %s %s\n", info.OSName, info.OSVersion)
		}
		if info.KernelInfo != "" {
			fmt.Printf("Kernel: %s\n", info.KernelInfo)
		}
		if info.Uptime > 0 {
			fmt.Printf("Uptime: %s\n", manager.FormatUptime(info.Uptime))
		}
		if info.CPUInfo != "" {
			fmt.Printf("CPU: %s\n", info.CPUInfo)
		}
		if info.MemoryTotal > 0 {
			fmt.Printf("Memory: %s total, %s free\n",
				manager.FormatBytes(info.MemoryTotal),
				manager.FormatBytes(info.MemoryFree))
		}
	}

	// Network Information
	if showNetwork {
		fmt.Println("\n## NETWORK INFORMATION")
		if len(info.IPAddresses) > 0 {
			fmt.Println("IP Addresses:")
			for _, ip := range info.IPAddresses {
				fmt.Printf("- %s\n", ip)
			}
		} else {
			fmt.Println("No IP addresses detected")
		}

		if len(info.DNSServers) > 0 {
			fmt.Println("\nDNS Servers:")
			for _, dns := range info.DNSServers {
				fmt.Printf("- %s\n", dns)
			}
		} else {
			fmt.Println("\nNo DNS servers detected")
		}
	}

	// User Information
	if showUsers {
		fmt.Println("\n## USER INFORMATION")
		if len(info.Users) > 0 {
			fmt.Println("Non-System Users:")
			for _, user := range info.Users {
				sudoStatus := ""
				if user.HasSudo {
					sudoStatus = " (sudo)"
				}
				fmt.Printf("- %s%s\n", user.Username, sudoStatus)
			}
		} else {
			fmt.Println("No non-system users detected")
		}

		if len(info.Groups) > 0 {
			fmt.Println("\nNon-System Groups:")
			for _, group := range info.Groups {
				fmt.Printf("- %s\n", group)
			}
		} else {
			fmt.Println("\nNo non-system groups detected")
		}
	}

	// Storage Information
	if showStorage {
		fmt.Println("\n## STORAGE INFORMATION")
		if len(info.DiskTotal) > 0 {
			for mount, total := range info.DiskTotal {
				free := info.DiskFree[mount]
				used := total - free

				// Calculate usage percentage
				usagePercent := 0
				if total > 0 {
					usagePercent = int((used * 100) / total)
				}

				fmt.Printf("%s:\n", mount)
				fmt.Printf("- Total: %s\n", manager.FormatBytes(total))
				fmt.Printf("- Used: %s (%d%%)\n", manager.FormatBytes(used), usagePercent)
				fmt.Printf("- Free: %s\n\n", manager.FormatBytes(free))
			}
		} else {
			fmt.Println("No storage information available")
		}
	}
}
