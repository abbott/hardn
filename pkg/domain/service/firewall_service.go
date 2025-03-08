// pkg/domain/service/firewall_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// FirewallService defines operations for firewall configuration
type FirewallService interface {

	// GetFirewallStatus retrieves the current status of the firewall
	GetFirewallStatus() (isInstalled bool, isEnabled bool, isConfigured bool, rules []string, err error)

	// ConfigureFirewall applies the firewall configuration
	ConfigureFirewall(config model.FirewallConfig) error

	// AddRule adds a firewall rule
	AddRule(rule model.FirewallRule) error

	// RemoveRule removes a firewall rule
	RemoveRule(rule model.FirewallRule) error

	// AddProfile adds a firewall application profile
	AddProfile(profile model.FirewallProfile) error

	// GetCurrentConfig retrieves the current firewall configuration
	GetCurrentConfig() (*model.FirewallConfig, error)

	// EnableFirewall enables the firewall
	EnableFirewall() error

	// DisableFirewall disables the firewall
	DisableFirewall() error
}

// FirewallServiceImpl implements FirewallService
type FirewallServiceImpl struct {
	repository FirewallRepository
	osInfo     model.OSInfo
}

// NewFirewallServiceImpl creates a new FirewallServiceImpl
func NewFirewallServiceImpl(repository FirewallRepository, osInfo model.OSInfo) *FirewallServiceImpl {
	return &FirewallServiceImpl{
		repository: repository,
		osInfo:     osInfo,
	}
}

// FirewallRepository defines the repository operations needed by FirewallService
type FirewallRepository interface {
	GetFirewallStatus() (bool, bool, bool, []string, error)
	SaveFirewallConfig(config model.FirewallConfig) error
	GetFirewallConfig() (*model.FirewallConfig, error)
	AddRule(rule model.FirewallRule) error
	RemoveRule(rule model.FirewallRule) error
	AddProfile(profile model.FirewallProfile) error
	EnableFirewall() error
	DisableFirewall() error
}

// GetFirewallStatus retrieves the current status of the firewall
func (s *FirewallServiceImpl) GetFirewallStatus() (bool, bool, bool, []string, error) {
	return s.repository.GetFirewallStatus()
}

// Implementation of FirewallService methods
func (s *FirewallServiceImpl) ConfigureFirewall(config model.FirewallConfig) error {
	return s.repository.SaveFirewallConfig(config)
}

func (s *FirewallServiceImpl) AddRule(rule model.FirewallRule) error {
	return s.repository.AddRule(rule)
}

func (s *FirewallServiceImpl) RemoveRule(rule model.FirewallRule) error {
	return s.repository.RemoveRule(rule)
}

func (s *FirewallServiceImpl) AddProfile(profile model.FirewallProfile) error {
	return s.repository.AddProfile(profile)
}

func (s *FirewallServiceImpl) GetCurrentConfig() (*model.FirewallConfig, error) {
	return s.repository.GetFirewallConfig()
}

func (s *FirewallServiceImpl) EnableFirewall() error {
	return s.repository.EnableFirewall()
}

func (s *FirewallServiceImpl) DisableFirewall() error {
	return s.repository.DisableFirewall()
}
