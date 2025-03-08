// pkg/application/ssh_manager.go
package application

import (
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// SSHManager is an application service for SSH configuration
type SSHManager struct {
	sshService service.SSHService
}

// NewSSHManager creates a new SSHManager
func NewSSHManager(sshService service.SSHService) *SSHManager {
	return &SSHManager{
		sshService: sshService,
	}
}

// ConfigureSSH applies SSH configuration with the specified settings
func (m *SSHManager) ConfigureSSH(
	port int,
	listenAddresses []string,
	permitRootLogin bool,
	allowedUsers []string,
	keyPaths []string,
) error {
	// Create SSH config object
	config := model.SSHConfig{
		Port:            port,
		ListenAddresses: listenAddresses,
		PermitRootLogin: permitRootLogin,
		AllowedUsers:    allowedUsers,
		KeyPaths:        keyPaths,
		AuthMethods:     []string{"publickey"},
	}

	// Call domain service
	return m.sshService.ConfigureSSH(config)
}

// SecureSSH applies recommended security settings to SSH
func (m *SSHManager) SecureSSH(port int, allowedUsers []string) error {
	// Create SSH config with secure defaults
	config := model.SSHConfig{
		Port:            port,
		ListenAddresses: []string{"0.0.0.0"},
		PermitRootLogin: true,
		AllowedUsers:    allowedUsers,
		AuthMethods:     []string{"publickey"},
		KeyPaths:        []string{".ssh/authorized_keys"},
	}

	// Apply the configuration
	return m.sshService.ConfigureSSH(config)
}

// DisableRootAccess disables SSH access for the root user
func (m *SSHManager) DisableRootAccess() error {
	return m.sshService.DisableRootAccess()
}

// AddSSHKey adds an SSH public key for a user
func (m *SSHManager) AddSSHKey(username string, publicKey string) error {
	return m.sshService.AddAuthorizedKey(username, publicKey)
}
