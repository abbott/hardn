// pkg/adapter/secondary/os_host_info_repository.go
package secondary

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// OSHostInfoRepository implements HostInfoRepository using OS operations
type OSHostInfoRepository struct {
	fs             interfaces.FileSystem
	commander      interfaces.Commander
	osType         string
	userRepository secondary.UserRepository
}

// NewOSHostInfoRepository creates a new OSHostInfoRepository
func NewOSHostInfoRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	osType string,
	userRepository secondary.UserRepository,
) secondary.HostInfoRepository {
	return &OSHostInfoRepository{
		fs:             fs,
		commander:      commander,
		osType:         osType,
		userRepository: userRepository,
	}
}

// GetHostInfo retrieves comprehensive host information
func (r *OSHostInfoRepository) GetHostInfo() (*model.HostInfo, error) {
	info := &model.HostInfo{
		DiskTotal: make(map[string]int64),
		DiskFree:  make(map[string]int64),
	}

	// Get network information
	ipAddresses, err := r.GetIPAddresses()
	if err == nil {
		info.IPAddresses = ipAddresses
	}

	dnsServers, err := r.GetDNSServers()
	if err == nil {
		info.DNSServers = dnsServers
	}

	hostname, domain, err := r.GetHostname()
	if err == nil {
		info.Hostname = hostname
		info.Domain = domain
	}

	// Get user information
	users, err := r.userRepository.GetNonSystemUsers()
	if err == nil {
		info.Users = users
	}

	groups, err := r.userRepository.GetNonSystemGroups()
	if err == nil {
		info.Groups = groups
	}

	// Get system information
	osName, osVersion, err := r.getOSInfo()
	if err == nil {
		info.OSName = osName
		info.OSVersion = osVersion
	}

	uptime, err := r.GetUptime()
	if err == nil {
		info.Uptime = uptime
	}

	kernel, err := r.getKernelInfo()
	if err == nil {
		info.KernelInfo = kernel
	}

	// Get additional information
	cpuInfo, err := r.getCPUInfo()
	if err == nil {
		info.CPUInfo = cpuInfo
	}

	memTotal, memFree, err := r.getMemoryInfo()
	if err == nil {
		info.MemoryTotal = memTotal
		info.MemoryFree = memFree
	}

	diskInfo, err := r.getDiskInfo()
	if err == nil {
		if diskInfo["total"] != nil {
			info.DiskTotal = diskInfo["total"]
		}
		if diskInfo["free"] != nil {
			info.DiskFree = diskInfo["free"]
		}
	}

	return info, nil
}

// GetIPAddresses retrieves the IP addresses of the system
func (r *OSHostInfoRepository) GetIPAddresses() ([]string, error) {
	var addresses []string

	// Get network interfaces
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

// GetDNSServers retrieves the configured DNS servers
func (r *OSHostInfoRepository) GetDNSServers() ([]string, error) {
	var servers []string

	// Try to read resolv.conf
	data, err := r.fs.ReadFile("/etc/resolv.conf")
	if err != nil {
		// Try alternate method with command if file can't be read
		output, cmdErr := r.commander.Execute("cat", "/etc/resolv.conf")
		if cmdErr != nil {
			return nil, fmt.Errorf("failed to read DNS configuration: %w", err)
		}
		data = output
	}

	// Parse nameserver entries
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				servers = append(servers, fields[1])
			}
		}
	}

	return servers, nil
}

// GetHostname retrieves the system hostname and domain
func (r *OSHostInfoRepository) GetHostname() (string, string, error) {
	// Try OS function first
	hostname, err := os.Hostname()
	if err != nil {
		// Fall back to command
		output, cmdErr := r.commander.Execute("hostname", "-f")
		if cmdErr != nil {
			return "", "", fmt.Errorf("failed to get hostname: %w", err)
		}
		hostname = strings.TrimSpace(string(output))
	}

	// Split hostname into host and domain parts
	parts := strings.Split(hostname, ".")
	host := parts[0]
	domain := ""

	if len(parts) > 1 {
		domain = strings.Join(parts[1:], ".")
	} else {
		// Try to get domain from domainname command if no domain in hostname
		output, err := r.commander.Execute("domainname")
		if err == nil {
			domain = strings.TrimSpace(string(output))
			// Filter out "none" or "(none)" responses
			if domain == "none" || domain == "(none)" {
				domain = ""
			}
		}
	}

	return host, domain, nil
}

// GetUptime retrieves the system uptime
func (r *OSHostInfoRepository) GetUptime() (time.Duration, error) {
	// Try reading from /proc/uptime first
	data, err := r.fs.ReadFile("/proc/uptime")
	if err == nil {
		// Parse uptime value
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			// Convert to seconds
			uptimeSeconds, err := strconv.ParseFloat(fields[0], 64)
			if err == nil {
				return time.Duration(uptimeSeconds * float64(time.Second)), nil
			}
		}
	}

	// Fall back to uptime command
	output, err := r.commander.Execute("uptime")
	if err != nil {
		return 0, fmt.Errorf("failed to get uptime: %w", err)
	}

	// Parse output - this is harder because format varies by OS
	// Example: "14:30:34 up 16 days, 2:14, 5 users, load average: 0.29, 0.36, 0.37"
	uptimeString := strings.TrimSpace(string(output))

	// Just try to extract days and hours
	var days, hours, minutes int

	if strings.Contains(uptimeString, "day") {
		// Has days
		daysIdx := strings.Index(uptimeString, " day")
		if daysIdx > 0 {
			// Find where the number starts
			spaceIdx := strings.LastIndex(uptimeString[:daysIdx], " ")
			if spaceIdx >= 0 && spaceIdx < daysIdx {
				dayStr := strings.TrimSpace(uptimeString[spaceIdx:daysIdx])
				days, _ = strconv.Atoi(dayStr)
			}
		}
	}

	// Try to find hours and minutes
	if strings.Contains(uptimeString, ":") {
		// Time format with colon
		colonIdx := strings.LastIndex(uptimeString, ":")
		if colonIdx > 0 {
			// Find the start of the time
			spaceIdx := strings.LastIndex(uptimeString[:colonIdx], " ")
			if spaceIdx >= 0 && spaceIdx < colonIdx {
				// Extract hours
				hoursStr := strings.TrimSpace(uptimeString[spaceIdx:colonIdx])
				hours, _ = strconv.Atoi(hoursStr)

				// Extract minutes
				if len(uptimeString) > colonIdx+1 {
					commaIdx := strings.Index(uptimeString[colonIdx:], ",")
					var minutesStr string
					if commaIdx > 0 {
						minutesStr = strings.TrimSpace(uptimeString[colonIdx+1 : colonIdx+commaIdx])
					} else {
						minutesStr = strings.TrimSpace(uptimeString[colonIdx+1:])
					}
					minutes, _ = strconv.Atoi(minutesStr)
				}
			}
		}
	}

	// Convert to duration
	duration := time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute

	return duration, nil
}

// getOSInfo retrieves OS name and version
func (r *OSHostInfoRepository) getOSInfo() (string, string, error) {
	// Try to read /etc/os-release
	data, err := r.fs.ReadFile("/etc/os-release")
	if err != nil {
		// Try with command
		output, cmdErr := r.commander.Execute("cat", "/etc/os-release")
		if cmdErr != nil {
			return "", "", fmt.Errorf("failed to read OS information: %w", err)
		}
		data = output
	}

	var osName, osVersion string

	// Parse os-release
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "NAME=") {
			osName = strings.Trim(strings.TrimPrefix(line, "NAME="), "\"")
		} else if strings.HasPrefix(line, "VERSION=") {
			osVersion = strings.Trim(strings.TrimPrefix(line, "VERSION="), "\"")
		}
	}

	return osName, osVersion, nil
}

// getKernelInfo retrieves kernel information
func (r *OSHostInfoRepository) getKernelInfo() (string, error) {
	output, err := r.commander.Execute("uname", "-r")
	if err != nil {
		return "", fmt.Errorf("failed to get kernel info: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// getCPUInfo retrieves CPU information
func (r *OSHostInfoRepository) getCPUInfo() (string, error) {
	// Try to read /proc/cpuinfo
	data, err := r.fs.ReadFile("/proc/cpuinfo")
	if err != nil {
		// Try with command
		output, cmdErr := r.commander.Execute("cat", "/proc/cpuinfo")
		if cmdErr != nil {
			return "", fmt.Errorf("failed to read CPU information: %w", err)
		}
		data = output
	}

	// Parse CPU model name
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	// Try alternate method for ARM systems
	scanner = bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Hardware") || strings.HasPrefix(line, "Processor") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1]), nil
			}
		}
	}

	return "", fmt.Errorf("could not parse CPU info")
}

// getMemoryInfo retrieves memory information
func (r *OSHostInfoRepository) getMemoryInfo() (int64, int64, error) {
	// Try to read /proc/meminfo
	data, err := r.fs.ReadFile("/proc/meminfo")
	if err != nil {
		// Try with command
		output, cmdErr := r.commander.Execute("cat", "/proc/meminfo")
		if cmdErr != nil {
			return 0, 0, fmt.Errorf("failed to read memory information: %w", err)
		}
		data = output
	}

	var memTotal, memFree int64

	// Parse memory values
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					memTotal = val * 1024 // Convert KB to bytes
				}
			}
		} else if strings.HasPrefix(line, "MemFree:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					memFree = val * 1024 // Convert KB to bytes
				}
			}
		}
	}

	return memTotal, memFree, nil
}

// getDiskInfo retrieves disk space information
func (r *OSHostInfoRepository) getDiskInfo() (map[string]map[string]int64, error) {
	result := map[string]map[string]int64{
		"total": make(map[string]int64),
		"free":  make(map[string]int64),
	}

	// Execute df command
	output, err := r.commander.Execute("df", "-k")
	if err != nil {
		return result, fmt.Errorf("failed to get disk info: %w", err)
	}

	// Parse output
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	// Skip header
	scanner.Scan()

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 6 {
			mountPoint := fields[5]

			// Skip certain mount points
			if strings.HasPrefix(mountPoint, "/dev") ||
				strings.HasPrefix(mountPoint, "/sys") ||
				strings.HasPrefix(mountPoint, "/proc") ||
				strings.HasPrefix(mountPoint, "/run") {
				continue
			}

			// Parse values
			total, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				continue
			}

			free, err := strconv.ParseInt(fields[3], 10, 64)
			if err != nil {
				continue
			}

			// Convert KB to bytes
			result["total"][mountPoint] = total * 1024
			result["free"][mountPoint] = free * 1024
		}
	}

	return result, nil
}
