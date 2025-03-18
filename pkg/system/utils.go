// pkg/system/utils.go
package system

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

// readOSRelease parses /etc/os-release file
func readOSRelease() (map[string]string, error) {
	result := make(map[string]string)

	file, err := os.Open("/etc/os-release")
	if err != nil {
		return result, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.IndexByte(line, '='); i >= 0 {
			key := line[:i]
			val := line[i+1:]
			// Strip quotes if present
			val = strings.Trim(val, "\"")
			result[key] = val
		}
	}

	return result, scanner.Err()
}

// readDNSServers reads DNS server IPs from /etc/resolv.conf
func readDNSServers() []string {
	var servers []string

	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return servers
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "nameserver ") {
			server := strings.Fields(line)[1]
			servers = append(servers, server)
		}
	}

	return servers
}

// formatUptime formats a duration into a human-readable string
func formatUptime(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 || days > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	parts = append(parts, fmt.Sprintf("%dm", minutes))

	return strings.Join(parts, "")
}
