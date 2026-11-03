// pkg/port/secondary/ssh_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// SSHRepository defines the interface for SSH configuration operations
type SSHRepository interface {
	// SaveSSHConfig persists the SSH configuration
	SaveSSHConfig(config model.SSHConfig) error
}
