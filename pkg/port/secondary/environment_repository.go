// pkg/port/secondary/environment_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// EnvironmentRepository defines the interface for environment configuration operations
type EnvironmentRepository interface {
	// SetupSudoPreservation configures sudo to preserve the HARDN_CONFIG environment variable
	SetupSudoPreservation(username string) error
	
	// IsSudoPreservationEnabled checks if the HARDN_CONFIG environment variable is preserved in sudo
	IsSudoPreservationEnabled(username string) (bool, error)
	
	// GetEnvironmentVariables retrieves the current environment configuration
	GetEnvironmentConfig() (*model.EnvironmentConfig, error)
}