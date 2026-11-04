// pkg/adapter/secondary/file_ssh_repository.go
package secondary

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// FileSSHRepository implements SSHRepository using file operations
type FileSSHRepository struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
	osType    string
	pve       *ProxmoxHost
}

// NewFileSSHRepository creates a new FileSSHRepository. pve says whether
// the host is Proxmox VE; nil makes the repository check for itself.
func NewFileSSHRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	osType string,
	pve *ProxmoxHost,
) secondary.SSHRepository {
	return &FileSSHRepository{
		fs:        fs,
		commander: commander,
		osType:    osType,
		pve:       proxmoxOrProbe(pve, fs, commander, osType),
	}
}

// SaveSSHConfig writes the SSH configuration to the appropriate file
func (r *FileSSHRepository) SaveSSHConfig(config model.SSHConfig) error {
	// Determine config file path based on OS type
	configFile := config.ConfigFilePath
	if configFile == "" {
		if r.osType == "alpine" {
			configFile = "/etc/ssh/sshd_config"
		} else {
			configFile = "/etc/ssh/sshd_config.d/hardn.conf"
		}
	}

	// Format SSH configuration content
	var content strings.Builder

	content.WriteString("# SSH configuration managed by Hardn\n\n")
	content.WriteString("Protocol 2\n")
	content.WriteString("StrictModes yes\n\n")

	// Port configuration
	content.WriteString(fmt.Sprintf("Port %d\n", config.Port))

	// Listen addresses
	for _, addr := range config.ListenAddresses {
		content.WriteString(fmt.Sprintf("ListenAddress %s\n", addr))
	}
	content.WriteString("\n")

	// Authentication methods
	if len(config.AuthMethods) > 0 {
		content.WriteString(fmt.Sprintf("AuthenticationMethods %s\n", strings.Join(config.AuthMethods, ",")))
	} else {
		content.WriteString("AuthenticationMethods publickey\n")
	}
	content.WriteString("PubkeyAuthentication yes\n\n")

	// Root login setting: key-only rather than no on Proxmox (proxmoxTreatedAsDebian)
	rootSSHValue := "no"
	if config.PermitRootLogin {
		rootSSHValue = "yes"
	} else if r.pve.IsProxmox() {
		rootSSHValue = pveRootLogin
	}
	content.WriteString(fmt.Sprintf("PermitRootLogin %s\n", rootSSHValue))

	// Allowed users
	if len(config.AllowedUsers) > 0 {
		content.WriteString(fmt.Sprintf("AllowUsers %s\n", strings.Join(config.AllowedUsers, " ")))
	}
	content.WriteString("\n")

	// Password authentication
	content.WriteString("PasswordAuthentication no\n")
	content.WriteString("PermitEmptyPasswords no\n\n")

	// Authorized keys: one line, since sshd uses the first AuthorizedKeysFile
	// it reads and a line lists every file (sshd_config(5)); origin/main's
	// line per path is kept behind authorizedKeysFileLinePerPath.
	if len(config.KeyPaths) > 0 && authorizedKeysFileLinePerPath {
		for _, path := range config.KeyPaths {
			content.WriteString(fmt.Sprintf("AuthorizedKeysFile %s\n", path))
		}
	} else if len(config.KeyPaths) > 0 {
		content.WriteString(fmt.Sprintf("AuthorizedKeysFile %s\n", strings.Join(config.KeyPaths, " ")))
	} else {
		content.WriteString("AuthorizedKeysFile .ssh/authorized_keys\n")
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(configFile)
	if err := r.fs.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for SSH config: %w", err)
	}

	// Write the configuration file
	if err := r.fs.WriteFile(configFile, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write SSH config file: %w", err)
	}

	// Restart SSH service based on OS type
	var cmd string
	var args []string

	if r.osType == "alpine" {
		cmd = "rc-service"
		args = []string{"sshd", "restart"}
	} else {
		cmd = "systemctl"
		args = []string{"restart", "ssh"}
	}

	if _, err := r.commander.Execute(cmd, args...); err != nil {
		return fmt.Errorf("failed to restart SSH service: %w", err)
	}

	return nil
}

// authorizedKeysFileLinePerPath: DISABLED 2026-10-17. Flip to true to restore.
//
// With it true, SaveSSHConfig writes one "AuthorizedKeysFile <path>" line
// per key path, as origin/main did. sshd uses the first AuthorizedKeysFile
// it reads (sshd_config(5)), so of two paths only the first counts: v0.3.2's
// pair, which hardn -c passed until refactor plan slice 5, unit (d) put -c
// on the User control (no sshd configured), would lose
// ~/.ssh_<user>/authorized_keys again (V24). No origin/main caller passed
// more than one path (-c passed one; HardenSystem's
// SshKeyPaths is never set), and no caller passes more than one now, so
// the files are the same either way (gate 4, V24).
//
// With it false, every path goes on one AuthorizedKeysFile line.
const authorizedKeysFileLinePerPath = false
