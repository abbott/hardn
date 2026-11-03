// pkg/domain/service/ssh_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// SSHService defines operations for SSH configuration
type SSHService interface {
	// ConfigureSSH applies SSH configuration settings
	ConfigureSSH(config model.SSHConfig) error

	// retrieve the current SSH configuration
	GetCurrentConfig() (*model.SSHConfig, error)
}

// SSHServiceImpl implements SSHService
type SSHServiceImpl struct {
	repository SSHRepository
	osInfo     model.OSInfo
}

// create a new SSHServiceImpl
func NewSSHServiceImpl(repository SSHRepository, osInfo model.OSInfo) *SSHServiceImpl {
	return &SSHServiceImpl{
		repository: repository,
		osInfo:     osInfo,
	}
}

// SSHRepository defines the repository operations needed by SSHService
type SSHRepository interface {
	SaveSSHConfig(config model.SSHConfig) error
	GetSSHConfig() (*model.SSHConfig, error)
}

// Implement SSHService methods
func (s *SSHServiceImpl) ConfigureSSH(config model.SSHConfig) error {
	return s.repository.SaveSSHConfig(config)
}

func (s *SSHServiceImpl) GetCurrentConfig() (*model.SSHConfig, error) {
	return s.repository.GetSSHConfig()
}
