// pkg/system/display.go
package system

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/style"
)

// generateGraphs creates visual bar graphs for load and usage
func (m *SystemDetails) generateGraphs() {
	// Generate load average graphs
	m.LoadAvg1Graph = createBarGraph(m.LoadAvg1, float64(m.CPUCores), 20)
	m.LoadAvg5Graph = createBarGraph(m.LoadAvg5, float64(m.CPUCores), 20)
	m.LoadAvg15Graph = createBarGraph(m.LoadAvg15, float64(m.CPUCores), 20)

	// Generate memory usage graph
	m.MemoryGraphUsed = createBarGraph(float64(m.MemoryUsed), float64(m.MemoryTotal), 20)

	// Generate disk usage graph
	if m.ZFSPresent {
		m.DiskGraphUsed = createBarGraph(m.ZFSUsedGB, m.ZFSUsedGB+m.ZFSAvailableGB, 20)
	} else {
		m.DiskGraphUsed = createBarGraph(m.RootUsedGB, m.RootTotalGB, 20)
	}
}

// DisplayMachineStatus shows the system status report using a styled box
func DisplayMachineStatus(info *SystemDetails) {
	boxConfig := style.BoxConfig{
		Width:          50,
		BorderColor:    style.Cyan,
		ShowEmptyRow:   false,
		ShowTopBorder:  true,
		ShowLeftBorder: true,
		Title:          "System Details",
		TitleColor:     style.BrightCyan,
	}

	box := style.NewBox(boxConfig)

	box.DrawBox(func(printLine func(string)) {
		// operating system
		printLine("")
		printLine(fmt.Sprintf("OS: %s %s", info.OSName, info.OSVersion))
		printLine(fmt.Sprintf("Kernel: %s", info.Kernel))
		printLine("")

		// network
		printLine("")
		printLine(fmt.Sprintf("Hostname: %s", info.Hostname))
		if info.Domain != "" {
			printLine(fmt.Sprintf("Domain: %s", info.Domain))
		}

		// IP addresses
		printLine("IP Addresses:")
		for _, ip := range info.IPAddresses {
			printLine(fmt.Sprintf("- %s", ip))
		}

		printLine(fmt.Sprintf("Client IP: %s", info.ClientIP))

		// Print DNS servers if available
		for i, dns := range info.DNSServers {
			printLine(fmt.Sprintf("DNS IP %d: %s", i+1, dns))
		}

		printLine(fmt.Sprintf("User: %s", info.CurrentUser))
		printLine("")

		// users
		if len(info.Users) > 0 {
			printLine("")
			printLine("Non-System Users:")
			for _, user := range info.Users {
				sudoStatus := ""
				if user.HasSudo {
					sudoStatus = " (sudo)"
				}
				printLine(fmt.Sprintf("- %s%s", user.Username, sudoStatus))
			}
			printLine("")
		}

		// CPU
		printLine("")
		printLine(fmt.Sprintf("Processor: %s", info.CPUModel))
		printLine(fmt.Sprintf("Cores: %d vCPU(s) / %d Socket(s)", info.CPUCores, info.CPUSockets))
		printLine(fmt.Sprintf("Hypervisor: %s", info.CPUHypervisor))
		printLine(fmt.Sprintf("CPU Freq: %.2f GHz", info.CPUFrequency))
		printLine(fmt.Sprintf("Load 1m:  %s (%.2f)", info.LoadAvg1Graph, info.LoadAvg1))
		printLine(fmt.Sprintf("Load 5m:  %s (%.2f)", info.LoadAvg5Graph, info.LoadAvg5))
		printLine(fmt.Sprintf("Load 15m: %s (%.2f)", info.LoadAvg15Graph, info.LoadAvg15))
		printLine("")

		// disks
		printLine("")
		if info.ZFSPresent {
			printLine(fmt.Sprintf("Volume: %.2f/%.2f GB [%.2f%%]",
				info.ZFSUsedGB, info.ZFSAvailableGB, info.ZFSPercent))
			printLine(fmt.Sprintf("Disk Usage: %s", info.DiskGraphUsed))
			printLine(fmt.Sprintf("ZFS Health: %s", info.ZFSHealth))
		} else {
			printLine(fmt.Sprintf("Volume: %.2f/%.2f GB [%.2f%%]",
				info.RootUsedGB, info.RootTotalGB, info.DiskPercent))
			printLine(fmt.Sprintf("Disk Usage: %s", info.DiskGraphUsed))
		}
		printLine("")

		// memory
		printLine("")
		printLine(fmt.Sprintf("Memory: %.2f/%.2f GiB [%.2f%%]",
			info.MemoryUsedGB, info.MemoryTotalGB, info.MemoryPercent))
		printLine(fmt.Sprintf("Usage: %s", info.MemoryGraphUsed))
		printLine("")

		// login
		printLine("")
		printLine(fmt.Sprintf("Last Login: %s", info.LastLoginTime))
		if info.LastLoginPresent && info.LastLoginIP != "" {
			printLine(fmt.Sprintf("From: %s", info.LastLoginIP))
		}
		// Use the long format uptime
		printLine(fmt.Sprintf("Uptime: %s", info.UptimeLongFormat))
	})
}

// createBarGraph generates a visual bar graph
func createBarGraph(used float64, total float64, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}

	percentage := used / total
	if percentage > 1.0 {
		percentage = 1.0
	}

	filledWidth := int(percentage * float64(width))
	emptyWidth := width - filledWidth

	return strings.Repeat("█", filledWidth) + strings.Repeat("░", emptyWidth)
}
