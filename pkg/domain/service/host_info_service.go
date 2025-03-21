// pkg/domain/service/host_info_service.go
package service

import (
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// HostInfoService defines operations for retrieving host information
type HostInfoService interface {
	// GetHostInfo retrieves the system information about the host
	GetHostInfo() (*model.HostInfo, error)

	// GetIPAddresses retrieves the IP addresses of the system
	GetIPAddresses() ([]string, error)

	// GetDNSServers retrieves the configured DNS servers
	GetDNSServers() ([]string, error)

	// GetHostname retrieves the system hostname and domain
	GetHostname() (string, string, error)

	// GetNonSystemUsers retrieves non-system users on the system
	GetNonSystemUsers() ([]model.User, error)

	// GetNonSystemGroups retrieves non-system groups on the system
	GetNonSystemGroups() ([]string, error)

	// GetUptime retrieves the system uptime
	GetUptime() (time.Duration, error)
}

// HostInfoServiceImpl implements HostInfoService
type HostInfoServiceImpl struct {
	hostInfoRepo HostInfoRepository
	userRepo     UserRepository
	osInfo       model.OSInfo
}

// NewHostInfoServiceImpl creates a new HostInfoServiceImpl
func NewHostInfoServiceImpl(hostInfoRepo HostInfoRepository, userRepo UserRepository, osInfo model.OSInfo) *HostInfoServiceImpl {
	return &HostInfoServiceImpl{
		hostInfoRepo: hostInfoRepo,
		userRepo:     userRepo,
		osInfo:       osInfo,
	}
}

// HostInfoRepository defines the repository operations needed by HostInfoService
type HostInfoRepository interface {
	GetHostInfo() (*model.HostInfo, error)
	GetIPAddresses() ([]string, error)
	GetDNSServers() ([]string, error)
	GetHostname() (string, string, error)
	GetUptime() (time.Duration, error)
}

// GetHostInfo retrieves comprehensive host information
func (s *HostInfoServiceImpl) GetHostInfo() (*model.HostInfo, error) {
	return s.hostInfoRepo.GetHostInfo()
}

// GetIPAddresses retrieves the IP addresses of the system
func (s *HostInfoServiceImpl) GetIPAddresses() ([]string, error) {
	return s.hostInfoRepo.GetIPAddresses()
}

// GetDNSServers retrieves the configured DNS servers
func (s *HostInfoServiceImpl) GetDNSServers() ([]string, error) {
	return s.hostInfoRepo.GetDNSServers()
}

// GetHostname retrieves the system hostname and domain
func (s *HostInfoServiceImpl) GetHostname() (string, string, error) {
	return s.hostInfoRepo.GetHostname()
}

// GetNonSystemUsers retrieves non-system users on the system
func (s *HostInfoServiceImpl) GetNonSystemUsers() ([]model.User, error) {
	return s.userRepo.GetNonSystemUsers()
}

// GetNonSystemGroups retrieves non-system groups on the system
func (s *HostInfoServiceImpl) GetNonSystemGroups() ([]string, error) {
	return s.userRepo.GetNonSystemGroups()
}

// GetUptime retrieves the system uptime
func (s *HostInfoServiceImpl) GetUptime() (time.Duration, error) {
	return s.hostInfoRepo.GetUptime()
}
