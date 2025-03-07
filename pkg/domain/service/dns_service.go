// pkg/domain/service/dns_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// DNSService defines operations for DNS configuration
type DNSService interface {
	// ConfigureDNS applies DNS configuration settings
	ConfigureDNS(config model.DNSConfig) error

	// GetCurrentConfig retrieves the current DNS configuration
	GetCurrentConfig() (*model.DNSConfig, error)
}

// DNSServiceImpl implements DNSService
type DNSServiceImpl struct {
	repository DNSRepository
	osInfo     model.OSInfo
}

// NewDNSServiceImpl creates a new DNSServiceImpl
func NewDNSServiceImpl(repository DNSRepository, osInfo model.OSInfo) *DNSServiceImpl {
	return &DNSServiceImpl{
		repository: repository,
		osInfo:     osInfo,
	}
}

// DNSRepository defines the repository operations needed by DNSService
type DNSRepository interface {
	SaveDNSConfig(config model.DNSConfig) error
	GetDNSConfig() (*model.DNSConfig, error)
}

// Implementation of DNSService methods
func (s *DNSServiceImpl) ConfigureDNS(config model.DNSConfig) error {
	return s.repository.SaveDNSConfig(config)
}

func (s *DNSServiceImpl) GetCurrentConfig() (*model.DNSConfig, error) {
	return s.repository.GetDNSConfig()
}
