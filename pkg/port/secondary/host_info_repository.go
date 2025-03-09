// pkg/port/secondary/host_info_repository.go
package secondary

import (
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// HostInfoRepository defines the interface for retrieving host information
type HostInfoRepository interface {
	// GetHostInfo retrieves the system information about the host
	GetHostInfo() (*model.HostInfo, error)

	// GetIPAddresses retrieves the IP addresses of the system
	GetIPAddresses() ([]string, error)

	// GetDNSServers retrieves the configured DNS servers
	GetDNSServers() ([]string, error)

	// GetHostname retrieves the system hostname and domain
	// Returns hostname, domain, error
	GetHostname() (string, string, error)

	// GetNonSystemUsers retrieves non-system users on the system
	GetNonSystemUsers() ([]model.User, error)

	// GetNonSystemGroups retrieves non-system groups on the system
	GetNonSystemGroups() ([]string, error)

	// GetUptime retrieves the system uptime
	GetUptime() (time.Duration, error)
}
