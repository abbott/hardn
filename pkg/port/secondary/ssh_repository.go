// pkg/port/secondary/ssh_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// SSHRepository defines the interface for SSH configuration operations
type SSHRepository interface {
	// SaveSSHConfig persists the SSH configuration
	SaveSSHConfig(config model.SSHConfig) error

	// GetSSHConfig retrieves the current SSH configuration
	GetSSHConfig() (*model.SSHConfig, error)

	// DisableRootSSH disables SSH access for the root user
	DisableRootSSH() error

	// add an SSH public key to a user's authorized_keys
	AddAuthorizedKey(username string, publicKey string) error
}
