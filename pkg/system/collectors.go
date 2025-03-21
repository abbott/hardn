// pkg/system/collectors.go
package system

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/application"
	"github.com/shirou/gopsutil/v3/cpu"  // Still needed for detailed CPU info
	"github.com/shirou/gopsutil/v3/disk" // Still needed for ZFS support
	"github.com/shirou/gopsutil/v3/load" // Still needed for load averages
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// collectOSInfo gathers operating system details
func (m *SystemDetails) collectOSInfo(hostInfoManager *application.HostInfoManager) error {
	// Get OS information using os/release files
	osInfo, err := readOSRelease()
	if err == nil {
		m.OSName = cases.Title(language.Und).String(osInfo["ID"])
		m.OSVersion = osInfo["VERSION_ID"]
		if codename, ok := osInfo["VERSION_CODENAME"]; ok {
			m.OSVersion += " " + cases.Title(language.Und).String(codename)
		}
	}

	// Get kernel information from Host Info Service
	hostInfo, err := hostInfoManager.GetHostInfo()
	if err == nil && hostInfo.KernelInfo != "" {
		m.Kernel = "Linux " + hostInfo.KernelInfo
	} else {
		// Fallback to direct command
		kernelInfo, err := exec.Command("uname", "-r").Output()
		if err == nil {
			m.Kernel = "Linux " + strings.TrimSpace(string(kernelInfo))
		}
	}

	// Get hostname from Host Info Service
	hostname, domain, err := hostInfoManager.GetHostname()
	if err == nil {
		m.Hostname = hostname
		m.Domain = domain
	} else {
		// Fallback to os.Hostname
		hostname, err := os.Hostname()
		if err == nil {
			m.Hostname = hostname
			// Try to extract domain
			parts := strings.Split(hostname, ".")
			if len(parts) > 1 {
				m.Domain = strings.Join(parts[1:], ".")
			}
		}
	}

	// Get current user
	currentUser, err := user.Current()
	if err == nil {
		m.CurrentUser = currentUser.Username
	}

	// Get uptime from Host Info Service
	uptime, err := hostInfoManager.GetUptime()
	if err == nil {
		m.Uptime = uptime
		m.UptimeFormatted = formatUptime(m.Uptime)
		m.UptimeLongFormat = hostInfoManager.FormatUptime(m.Uptime) // Use the manager's formatter
	} else {
		// Fallback to old implementation if necessary
		hostInfoCmd := exec.Command("uptime")
		hostInfoOutput, err := hostInfoCmd.Output()
		if err == nil {
			// Parse uptime output - this is just a fallback, so simple parsing
			uptimeStr := strings.TrimSpace(string(hostInfoOutput))
			m.UptimeFormatted = uptimeStr
			m.UptimeLongFormat = uptimeStr
		}
	}

	return nil
}

// collectNetworkInfo gathers network interface details
func (m *SystemDetails) collectNetworkInfo(hostInfoManager *application.HostInfoManager) error {
	// Get IP addresses from Host Info Service
	ipAddresses, err := hostInfoManager.GetIPAddresses()
	if err != nil {
		// Fallback to older implementation
		ipAddresses, err = getIPAddresses()
		if err != nil {
			return fmt.Errorf("failed to get IP addresses: %w", err)
		}
	}

	// Store all found IP addresses
	m.IPAddresses = ipAddresses

	// Set primary system IP (first one found)
	if len(ipAddresses) > 0 {
		m.MachineIP = ipAddresses[0]
	} else {
		m.MachineIP = "Not available"
	}

	// Try to get client IP from SSH connection
	if sshConn := os.Getenv("SSH_CLIENT"); sshConn != "" {
		parts := strings.Fields(sshConn)
		if len(parts) > 0 {
			m.ClientIP = parts[0]
		}
	}

	if m.ClientIP == "" {
		m.ClientIP = "Not connected"
	}

	// Get DNS servers from Host Info Service
	dnsServers, err := hostInfoManager.GetDNSServers()
	if err == nil && len(dnsServers) > 0 {
		m.DNSServers = dnsServers
	} else {
		// Fallback to older implementation
		m.DNSServers = readDNSServers()
	}

	return nil
}

// getIPAddresses retrieves all IPv4 addresses on the system
func getIPAddresses() ([]string, error) {
	var addresses []string

	// Use standard library's net package for interfaces
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	// Get IP addresses for each interface
	for _, iface := range interfaces {
		// Skip loopback interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Skip down interfaces
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		// Skip docker and other container interfaces
		if strings.Contains(iface.Name, "docker") || strings.Contains(iface.Name, "veth") {
			continue
		}

		// Get addresses for this interface
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			// Get IP from address
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Skip loopback, link-local, and IPv6 addresses
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}

			// We only want IPv4 addresses for now
			if ip4 := ip.To4(); ip4 != nil {
				addresses = append(addresses, ip4.String())
			}
		}
	}

	return addresses, nil
}

// collectCPUInfo gathers CPU information
func (m *SystemDetails) collectCPUInfo(hostInfoManager *application.HostInfoManager) error {
	// Try to get CPU info from Host Info service first
	hostInfo, err := hostInfoManager.GetHostInfo()
	if err == nil && hostInfo.CPUInfo != "" {
		// Use the CPU model from Host Info service
		m.CPUModel = hostInfo.CPUInfo

		// Still need to get other CPU details that aren't in the Host Info model
		// We'll get these details using direct commands or gopsutil
	} else {
		// Fallback to gopsutil for CPU model
		cpuInfo, err := cpu.Info()
		if err != nil {
			// Further fallback to direct command
			cmd := exec.Command("cat", "/proc/cpuinfo")
			output, err := cmd.Output()
			if err != nil {
				// Just set a placeholder if all methods fail
				m.CPUModel = "Unknown CPU"
			} else {
				// Parse CPU model name
				scanner := bufio.NewScanner(strings.NewReader(string(output)))
				for scanner.Scan() {
					line := scanner.Text()
					if strings.HasPrefix(line, "model name") {
						parts := strings.SplitN(line, ":", 2)
						if len(parts) >= 2 {
							m.CPUModel = strings.TrimSpace(parts[1])
							break
						}
					}
				}
			}
		} else if len(cpuInfo) > 0 {
			m.CPUModel = cpuInfo[0].ModelName
			m.CPUFrequency = float64(cpuInfo[0].Mhz) / 1000.0 // Convert to GHz
		}
	}

	// Get CPU cores count
	cmd := exec.Command("nproc")
	output, err := cmd.Output()
	if err == nil {
		cores, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err == nil {
			m.CPUCores = cores
		}
	}

	// Fallback to gopsutil for core count if command failed
	if m.CPUCores == 0 {
		cpuInfo, err := cpu.Info()
		if err == nil {
			m.CPUCores = len(cpuInfo)
			if len(cpuInfo) > 0 && m.CPUFrequency == 0 {
				m.CPUFrequency = float64(cpuInfo[0].Mhz) / 1000.0
			}
		}
	}

	// Check for hypervisor using lscpu
	hypervisorCmd := exec.Command("lscpu")
	hypervisorOutput, err := hypervisorCmd.Output()
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(hypervisorOutput)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Hypervisor vendor") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					m.CPUHypervisor = strings.TrimSpace(parts[1])
				}
				break
			}
		}
	}

	if m.CPUHypervisor == "" {
		m.CPUHypervisor = "Bare Metal"
	}

	// Get load averages
	loadAvg, err := load.Avg()
	if err == nil {
		m.LoadAvg1 = loadAvg.Load1
		m.LoadAvg5 = loadAvg.Load5
		m.LoadAvg15 = loadAvg.Load15
	} else {
		// Fallback to reading /proc/loadavg
		data, err := os.ReadFile("/proc/loadavg")
		if err == nil {
			fields := strings.Fields(string(data))
			if len(fields) >= 3 {
				m.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
				m.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
				m.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
			}
		}
	}

	// Try to determine sockets from lscpu output
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(hypervisorOutput)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "Socket(s)") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					if sockets, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil {
						m.CPUSockets = sockets
					}
				}
				break
			}
		}
	}

	// Default to 1 socket if we couldn't determine it
	if m.CPUSockets == 0 {
		m.CPUSockets = 1
	}

	return nil
}

// collectMemoryInfo gathers memory usage statistics
func (m *SystemDetails) collectMemoryInfo(hostInfoManager *application.HostInfoManager) error {
	// Get host info with memory details
	hostInfo, err := hostInfoManager.GetHostInfo()
	if err == nil && hostInfo.MemoryTotal > 0 {
		// Use memory info from Host Info service
		m.MemoryTotal = uint64(hostInfo.MemoryTotal)
		m.MemoryFree = uint64(hostInfo.MemoryFree)
		m.MemoryUsed = m.MemoryTotal - m.MemoryFree

		// Calculate memory usage percentage
		if m.MemoryTotal > 0 {
			m.MemoryPercent = float64(m.MemoryUsed) / float64(m.MemoryTotal) * 100
		}
	} else {
		// Fallback to direct command if Host Info service fails
		cmd := exec.Command("free", "-b")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to get memory info: %w", err)
		}

		// Parse free command output
		scanner := bufio.NewScanner(strings.NewReader(string(output)))
		// Skip header line
		scanner.Scan()
		// Get memory info line
		if scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 3 {
				total, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					m.MemoryTotal = total
				}

				free, err := strconv.ParseUint(fields[3], 10, 64)
				if err == nil {
					m.MemoryFree = free
				}

				m.MemoryUsed = m.MemoryTotal - m.MemoryFree
				if m.MemoryTotal > 0 {
					m.MemoryPercent = float64(m.MemoryUsed) / float64(m.MemoryTotal) * 100
				}
			}
		}
	}

	// Convert to GB for display
	m.MemoryTotalGB = float64(m.MemoryTotal) / (1024 * 1024 * 1024)
	m.MemoryUsedGB = float64(m.MemoryUsed) / (1024 * 1024 * 1024)
	m.MemoryFreeGB = float64(m.MemoryFree) / (1024 * 1024 * 1024)

	return nil
}

// collectDiskInfo gathers disk usage statistics with ZFS support
func (m *SystemDetails) collectDiskInfo(hostInfoManager *application.HostInfoManager) error {
	// Try to get disk info from Host Info service first
	hostInfo, err := hostInfoManager.GetHostInfo()

	// First check if ZFS is present regardless of Host Info
	if _, err := exec.LookPath("zfs"); err == nil {
		// Try to get ZFS information
		if out, err := exec.Command("zpool", "status", "-x").Output(); err == nil {
			if strings.Contains(string(out), "is healthy") {
				m.ZFSPresent = true
				m.ZFSHealth = "HEALTH O.K."

				// Get ZFS filesystem usage
				cmd := exec.Command("zfs", "get", "-Hp", "available", m.ZFSFilesystem)
				out, err := cmd.Output()
				if err == nil {
					fields := strings.Fields(string(out))
					if len(fields) >= 3 {
						if available, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
							m.ZFSAvailableGB = float64(available) / (1024 * 1024 * 1024)
						}
					}
				}

				cmd = exec.Command("zfs", "get", "-Hp", "used", m.ZFSFilesystem)
				out, err = cmd.Output()
				if err == nil {
					fields := strings.Fields(string(out))
					if len(fields) >= 3 {
						if used, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
							m.ZFSUsedGB = float64(used) / (1024 * 1024 * 1024)
							// Calculate percentage
							total := m.ZFSUsedGB + m.ZFSAvailableGB
							if total > 0 {
								m.ZFSPercent = (m.ZFSUsedGB / total) * 100
								m.DiskPercent = m.ZFSPercent
							}
						}
					}
				}

				// ZFS is present, so we don't need to get standard disk info
				return nil
			}
		}
	}

	// If not using ZFS, try to get disk info from Host Info service
	if !m.ZFSPresent && err == nil && len(hostInfo.DiskTotal) > 0 && len(hostInfo.DiskFree) > 0 {
		// Look for root partition data
		if total, ok := hostInfo.DiskTotal["/"]; ok {
			if free, ok := hostInfo.DiskFree["/"]; ok {
				m.RootTotalGB = float64(total) / (1024 * 1024 * 1024)
				m.RootFreeGB = float64(free) / (1024 * 1024 * 1024)
				m.RootUsedGB = m.RootTotalGB - m.RootFreeGB

				// Calculate disk usage percentage
				if m.RootTotalGB > 0 {
					m.DiskPercent = (m.RootUsedGB / m.RootTotalGB) * 100
				}
				return nil
			}
		}
	}

	// If Host Info service didn't provide disk info or it wasn't for root, use gopsutil
	if !m.ZFSPresent {
		usage, err := disk.Usage(m.RootPartition)
		if err != nil {
			// Last resort: use df command
			cmd := exec.Command("df", "-k", m.RootPartition)
			output, cmdErr := cmd.Output()
			if cmdErr != nil {
				return fmt.Errorf("failed to get disk info: %w", err)
			}

			scanner := bufio.NewScanner(strings.NewReader(string(output)))
			// Skip header
			scanner.Scan()
			// Get disk info line
			if scanner.Scan() {
				fields := strings.Fields(scanner.Text())
				if len(fields) >= 5 {
					total, err := strconv.ParseUint(fields[1], 10, 64)
					if err == nil {
						m.RootTotalGB = float64(total) * 1024 / (1024 * 1024 * 1024)
					}

					used, err := strconv.ParseUint(fields[2], 10, 64)
					if err == nil {
						m.RootUsedGB = float64(used) * 1024 / (1024 * 1024 * 1024)
					}

					free, err := strconv.ParseUint(fields[3], 10, 64)
					if err == nil {
						m.RootFreeGB = float64(free) * 1024 / (1024 * 1024 * 1024)
					}

					percentStr := strings.TrimSuffix(fields[4], "%")
					percent, err := strconv.ParseFloat(percentStr, 64)
					if err == nil {
						m.DiskPercent = percent
					} else if m.RootTotalGB > 0 {
						m.DiskPercent = (m.RootUsedGB / m.RootTotalGB) * 100
					}
				}
			}
		} else {
			m.RootTotalGB = float64(usage.Total) / (1024 * 1024 * 1024)
			m.RootUsedGB = float64(usage.Used) / (1024 * 1024 * 1024)
			m.RootFreeGB = float64(usage.Free) / (1024 * 1024 * 1024)
			m.DiskPercent = usage.UsedPercent
		}
	}

	return nil
}

// collectLoginInfo gathers information about the last login
func (m *SystemDetails) collectLoginInfo() error {
	currentUser, err := user.Current()
	if err != nil {
		return err
	}

	// Use the UserLoginPort to get login information
	lastLoginTime, ipAddress, err := m.userLoginPort.GetLastLoginInfo(currentUser.Username)
	if err != nil {
		return nil // Not critical, continue without last login info
	}

	// Check if we got a valid login time
	if !lastLoginTime.IsZero() {
		m.LastLoginPresent = true
		// Convert to local timezone first
		localTime := lastLoginTime.Local()
		m.LastLoginTime = localTime.Format("Jan 2 15:04:05 -0700")

		// Set IP address if available
		if ipAddress != "" {
			m.LastLoginIP = ipAddress
		} else {
			m.LastLoginIP = "Unknown"
		}
	} else {
		m.LastLoginTime = "Never logged in"
		m.LastLoginIP = ""
		m.LastLoginPresent = false
	}

	return nil
}
