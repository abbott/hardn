// pkg/domain/service/ssh_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// SSHService defines operations for SSH configuration
type SSHService interface {
    // ConfigureSSH applies SSH configuration settings
    ConfigureSSH(config model.SSHConfig) error
    
    // DisableRootAccess disables SSH access for the root user
    DisableRootAccess() error
    
    // AddAuthorizedKey adds an SSH public key to a user's authorized_keys
    AddAuthorizedKey(username string, publicKey string) error
    
    // GetCurrentConfig retrieves the current SSH configuration
    GetCurrentConfig() (*model.SSHConfig, error)
}

// SSHServiceImpl implements SSHService
type SSHServiceImpl struct {
    repository SSHRepository
    osInfo     model.OSInfo
}

// NewSSHServiceImpl creates a new SSHServiceImpl
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
    DisableRootAccess() error
    AddAuthorizedKey(username string, publicKey string) error
}

// Implement SSHService methods
func (s *SSHServiceImpl) ConfigureSSH(config model.SSHConfig) error {
    return s.repository.SaveSSHConfig(config)
}

func (s *SSHServiceImpl) DisableRootAccess() error {
    return s.repository.DisableRootAccess()
}

func (s *SSHServiceImpl) AddAuthorizedKey(username string, publicKey string) error {
    return s.repository.AddAuthorizedKey(username, publicKey)
}

func (s *SSHServiceImpl) GetCurrentConfig() (*model.SSHConfig, error) {
    return s.repository.GetSSHConfig()
}