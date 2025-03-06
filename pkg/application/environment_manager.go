// pkg/application/environment_manager.go
package application

import (
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// EnvironmentManager is an application service for environment variable management
type EnvironmentManager struct {
	environmentService service.EnvironmentService
}

// NewEnvironmentManager creates a new EnvironmentManager
func NewEnvironmentManager(
	environmentService service.EnvironmentService,
) *EnvironmentManager {
	return &EnvironmentManager{
		environmentService: environmentService,
	}
}

// SetupSudoPreservation configures sudo to preserve environment variables
func (m *EnvironmentManager) SetupSudoPreservation() error {
	return m.environmentService.SetupSudoPreservation()
}

// IsSudoPreservationEnabled checks if sudo preservation is enabled
func (m *EnvironmentManager) IsSudoPreservationEnabled() (bool, error) {
	return m.environmentService.IsSudoPreservationEnabled()
}

// GetEnvironmentConfig retrieves the current environment configuration
func (m *EnvironmentManager) GetEnvironmentConfig() (*model.EnvironmentConfig, error) {
	return m.environmentService.GetEnvironmentConfig()
}

// GetConfigPath returns the path to the configuration file
func (m *EnvironmentManager) GetConfigPath() (string, error) {
	config, err := m.environmentService.GetEnvironmentConfig()
	if err != nil {
		return "", err
	}
	
	return config.ConfigPath, nil
}

// IsEnvironmentVariableSet checks if a specific environment variable is set
func (m *EnvironmentManager) IsEnvironmentVariableSet(name string) (bool, string) {
	value, exists := "", false
	
	// Currently only HARDN_CONFIG is supported
	if name == "HARDN_CONFIG" {
		config, err := m.environmentService.GetEnvironmentConfig()
		if err == nil && config.ConfigPath != "" {
			exists = true
			value = config.ConfigPath
		}
	}
	
	return exists, value
}