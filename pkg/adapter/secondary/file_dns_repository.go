// pkg/adapter/secondary/file_dns_repository.go
package secondary

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// FileDNSRepository implements DNSRepository using file operations
type FileDNSRepository struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
	osType    string
}

// NewFileDNSRepository creates a new FileDNSRepository
func NewFileDNSRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	osType string,
) secondary.DNSRepository {
	return &FileDNSRepository{
		fs:        fs,
		commander: commander,
		osType:    osType,
	}
}

// SaveDNSConfig persists the DNS configuration
func (r *FileDNSRepository) SaveDNSConfig(config model.DNSConfig) error {
	// Check if systemd-resolved is active
	systemdActive := false
	if _, err := r.commander.Execute("systemctl", "is-active", "systemd-resolved"); err == nil {
		systemdActive = true
	}

	// Check if resolvconf is installed
	resolvconfInstalled := false
	if _, err := r.commander.Execute("which", "resolvconf"); err == nil {
		resolvconfInstalled = true
	}

	if systemdActive {
		return r.configureSystemdResolved(config)
	} else if resolvconfInstalled {
		return r.configureResolvconf(config)
	} else {
		return r.configureDirectResolv(config)
	}
}

// GetDNSConfig retrieves the current DNS configuration
func (r *FileDNSRepository) GetDNSConfig() (*model.DNSConfig, error) {
	// Read /etc/resolv.conf to get current configuration
	data, err := r.fs.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil, fmt.Errorf("failed to read resolv.conf: %w", err)
	}

	config := model.DNSConfig{}

	// Parse file
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		directive := fields[0]
		value := fields[1]

		switch directive {
		case "nameserver":
			config.Nameservers = append(config.Nameservers, value)
		case "domain":
			config.Domain = value
		case "search":
			config.Search = fields[1:]
		}
	}

	return &config, nil
}

// configureSystemdResolved configures DNS using systemd-resolved
func (r *FileDNSRepository) configureSystemdResolved(config model.DNSConfig) error {
	// Create resolved.conf content
	var content strings.Builder

	content.WriteString("[Resolve]\n")
	content.WriteString(fmt.Sprintf("DNS=%s\n", strings.Join(config.Nameservers, " ")))

	if config.Domain != "" {
		content.WriteString(fmt.Sprintf("Domains=%s\n", config.Domain))
	}

	// Write resolved.conf
	if err := r.fs.WriteFile("/etc/systemd/resolved.conf", []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write systemd-resolved config: %w", err)
	}

	// Restart systemd-resolved
	if _, err := r.commander.Execute("systemctl", "restart", "systemd-resolved"); err != nil {
		return fmt.Errorf("failed to restart systemd-resolved: %w", err)
	}

	return nil
}

// configures DNS using resolvconf
func (r *FileDNSRepository) configureResolvconf(config model.DNSConfig) error {
	var content strings.Builder

	// Add domain if specified
	if config.Domain != "" {
		content.WriteString(fmt.Sprintf("domain %s\n", config.Domain))
	}

	// Add search domains
	if len(config.Search) > 0 {
		content.WriteString(fmt.Sprintf("search %s\n", strings.Join(config.Search, " ")))
	} else if config.Domain != "" {
		content.WriteString(fmt.Sprintf("search %s\n", config.Domain))
	}

	// Add nameservers
	for _, nameserver := range config.Nameservers {
		content.WriteString(fmt.Sprintf("nameserver %s\n", nameserver))
	}

	// Create resolvconf directory if it doesn't exist
	resolvconfDir := "/etc/resolvconf/resolv.conf.d"
	if err := r.fs.MkdirAll(resolvconfDir, 0755); err != nil {
		return fmt.Errorf("failed to create resolvconf directory: %w", err)
	}

	// Write head file
	headPath := filepath.Join(resolvconfDir, "head")
	if err := r.fs.WriteFile(headPath, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write resolvconf head file: %w", err)
	}

	// Update resolvconf
	if _, err := r.commander.Execute("resolvconf", "-u"); err != nil {
		return fmt.Errorf("failed to update resolvconf: %w", err)
	}

	return nil
}

// configureDirectResolv configures DNS by directly writing to resolv.conf
func (r *FileDNSRepository) configureDirectResolv(config model.DNSConfig) error {
	var content strings.Builder

	// Add domain if specified
	if config.Domain != "" {
		content.WriteString(fmt.Sprintf("domain %s\n", config.Domain))
	}

	// Add search domains
	if len(config.Search) > 0 {
		content.WriteString(fmt.Sprintf("search %s\n", strings.Join(config.Search, " ")))
	} else if config.Domain != "" {
		content.WriteString(fmt.Sprintf("search %s\n", config.Domain))
	}

	// Add nameservers
	for _, nameserver := range config.Nameservers {
		content.WriteString(fmt.Sprintf("nameserver %s\n", nameserver))
	}

	// Write resolv.conf
	if err := r.fs.WriteFile("/etc/resolv.conf", []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write resolv.conf: %w", err)
	}

	return nil
}
