// pkg/domain/service/environment_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// EnvironmentService defines operations for environment variable management
type EnvironmentService interface {
	// SetupSudoPreservation configures sudo to preserve the HARDN_CONFIG environment variable
	SetupSudoPreservation() error
	
	// IsSudoPreservationEnabled checks if the HARDN_CONFIG environment variable is preserved in sudo
	IsSudoPreservationEnabled() (bool, error)
	
	// GetEnvironmentConfig retrieves the current environment configuration
	GetEnvironmentConfig() (*model.EnvironmentConfig, error)
}

// EnvironmentServiceImpl implements EnvironmentService
type EnvironmentServiceImpl struct {
	repository EnvironmentRepository
}

// NewEnvironmentServiceImpl creates a new EnvironmentServiceImpl
func NewEnvironmentServiceImpl(repository EnvironmentRepository) *EnvironmentServiceImpl {
	return &EnvironmentServiceImpl{
		repository: repository,
	}
}

// EnvironmentRepository defines the repository operations needed by EnvironmentService
type EnvironmentRepository interface {
	SetupSudoPreservation(username string) error
	IsSudoPreservationEnabled(username string) (bool, error)
	GetEnvironmentConfig() (*model.EnvironmentConfig, error)
}

// SetupSudoPreservation configures sudo to preserve the HARDN_CONFIG environment variable
func (s *EnvironmentServiceImpl) SetupSudoPreservation() error {
	// Get current config to obtain username
	config, err := s.repository.GetEnvironmentConfig()
	if err != nil {
		return err
	}
	
	if config.Username == "" {
		return nil // No username, nothing to do
	}
	
	return s.repository.SetupSudoPreservation(config.Username)
}

// IsSudoPreservationEnabled checks if the HARDN_CONFIG environment variable is preserved in sudo
func (s *EnvironmentServiceImpl) IsSudoPreservationEnabled() (bool, error) {
	// Get current config to obtain username
	config, err := s.repository.GetEnvironmentConfig()
	if err != nil {
		return false, err
	}
	
	if config.Username == "" {
		return false, nil // No username, no preservation
	}
	
	return s.repository.IsSudoPreservationEnabled(config.Username)
}

// GetEnvironmentConfig retrieves the current environment configuration
func (s *EnvironmentServiceImpl) GetEnvironmentConfig() (*model.EnvironmentConfig, error) {
	return s.repository.GetEnvironmentConfig()
}