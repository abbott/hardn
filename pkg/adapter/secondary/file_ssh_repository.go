// pkg/adapter/secondary/file_ssh_repository.go
package secondary

import (
	"fmt"
	"os"
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
// pair that hardn -c now passes (pkg/cmd authorizedKeysSSHUOnly) would lose
// ~/.ssh_<user>/authorized_keys again (V24). No origin/main caller passed
// more than one path (-c and SecureSSH pass one; HardenSystem's SshKeyPaths
// is never set), so origin/main's files are the same either way: flip it
// with authorizedKeysSSHUOnly to restore origin/main's code as well as its
// output (gate 4, V5 and V24).
//
// With it false, every path goes on one AuthorizedKeysFile line.
const authorizedKeysFileLinePerPath = false

// GetSSHConfig reads the current SSH configuration
func (r *FileSSHRepository) GetSSHConfig() (*model.SSHConfig, error) {
	// Implementation to parse SSH config file and return configuration
	// ... (implementation details omitted for brevity)
	return &model.SSHConfig{Port: 22}, nil
}

// stubDisableRoot: DISABLED 2026-10-17. Flip to true to restore.
//
// With it true, DisableRootSSH rebuilds the SSH config from GetSSHConfig's
// stub (&model.SSHConfig{Port: 22}) and writes it whole through
// SaveSSHConfig (disableRootSSHFromStub): Port 22, no ListenAddress and no
// AllowUsers, AuthenticationMethods publickey and PasswordAuthentication no,
// whatever the host had. A host on another port, whose firewall allows only
// that port, gets sshd moved back to 22, and on Alpine the whole
// /etc/ssh/sshd_config is replaced, Subsystem sftp and Include lines with it
// (V4, V15; Phase 0 side-effects §1, harness A2).
//
// With it false, DisableRootSSH reads the file it manages through the
// FileSystem seam and edits it in place, as v0.3.2's pkg/ssh did
// (setRootLogin): PermitRootLogin is set to no and root is taken out of
// AllowUsers, and every other line is kept. Neither path validates the
// result with sshd -t before restarting sshd; validation and reload come
// with Phase 2 (next/p2-sshd-validate, next/p2-reload).
const stubDisableRoot = false

// DisableRootSSH disables SSH access for the root user
func (r *FileSSHRepository) DisableRootSSH() error {
	if stubDisableRoot {
		return r.disableRootSSHFromStub()
	}
	return r.disableRootSSHInPlace()
}

// sshdConfigFile is the file DisableRootSSH edits: the whole sshd_config
// on Alpine, where SaveSSHConfig writes it, and elsewhere hardn's drop-in,
// which SaveSSHConfig writes and Debian's and Ubuntu's sshd_config
// includes first.
func (r *FileSSHRepository) sshdConfigFile() string {
	if r.osType == "alpine" {
		return "/etc/ssh/sshd_config"
	}
	return "/etc/ssh/sshd_config.d/hardn.conf"
}

// disableRootSSHInPlace sets PermitRootLogin no and removes root from
// AllowUsers in sshdConfigFile, keeping every other line (setRootLogin).
// On Proxmox VE it sets PermitRootLogin prohibit-password and keeps root in
// AllowUsers, so root stays able to log in with a key, as clustering needs
// (proxmoxTreatedAsDebian).
// A missing drop-in is created holding only the directive; a missing
// Alpine sshd_config is an error, as in v0.3.2, since writing one would
// drop the distribution's defaults. When the file already says what it
// should, nothing is written and sshd is not restarted.
func (r *FileSSHRepository) disableRootSSHInPlace() error {
	configFile := r.sshdConfigFile()
	permitRootLogin, removeRoot := "no", true
	if r.pve.IsProxmox() {
		permitRootLogin, removeRoot = pveRootLogin, false
	}

	var current []byte
	_, err := r.fs.Stat(configFile)
	switch {
	case err == nil:
		if current, err = r.fs.ReadFile(configFile); err != nil {
			return fmt.Errorf("failed to read SSH config %s: %w", configFile, err)
		}
	case os.IsNotExist(err) && r.osType != "alpine":
		// hardn's drop-in does not exist yet: it starts empty.
	case os.IsNotExist(err):
		return fmt.Errorf("SSH config file %s not found", configFile)
	default:
		return fmt.Errorf("failed to check SSH config %s: %w", configFile, err)
	}

	edited, err := setRootLogin(string(current), permitRootLogin, removeRoot)
	if err != nil {
		return fmt.Errorf("refusing to disable root SSH login in %s: %w", configFile, err)
	}
	if edited == string(current) {
		return nil
	}

	if current == nil {
		if err := r.fs.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
			return fmt.Errorf("failed to create directory for SSH config: %w", err)
		}
	}
	if err := r.fs.WriteFile(configFile, []byte(edited), 0644); err != nil {
		return fmt.Errorf("failed to write SSH config file: %w", err)
	}
	return r.restartSSHService()
}

// restartSSHService restarts sshd as SaveSSHConfig does.
func (r *FileSSHRepository) restartSSHService() error {
	cmd, args := "systemctl", []string{"restart", "ssh"}
	if r.osType == "alpine" {
		cmd, args = "rc-service", []string{"sshd", "restart"}
	}
	if _, err := r.commander.Execute(cmd, args...); err != nil {
		return fmt.Errorf("failed to restart SSH service: %w", err)
	}
	return nil
}

// disableRootSSHFromStub is origin/main's DisableRootSSH, kept behind
// stubDisableRoot (V4).
func (r *FileSSHRepository) disableRootSSHFromStub() error {
	// Get current config
	config, err := r.GetSSHConfig()
	if err != nil {
		return err
	}

	// Disable root login
	config.PermitRootLogin = false

	// Remove 'root' from AllowedUsers
	var newAllowedUsers []string
	for _, user := range config.AllowedUsers {
		if user != "root" {
			newAllowedUsers = append(newAllowedUsers, user)
		}
	}
	config.AllowedUsers = newAllowedUsers

	// Save the modified configuration
	return r.SaveSSHConfig(*config)
}

// add an SSH public key to a user's authorized_keys
func (r *FileSSHRepository) AddAuthorizedKey(username string, publicKey string) error {
	var homeDir string
	var sshDir string
	var authKeysFile string

	// Determine paths based on user
	if username == "root" {
		homeDir = "/root"
	} else {
		homeDir = fmt.Sprintf("/home/%s", username)
	}

	sshDir = filepath.Join(homeDir, ".ssh")
	authKeysFile = filepath.Join(sshDir, "authorized_keys")

	// Create .ssh directory if it doesn't exist
	if err := r.fs.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("failed to create SSH directory for user %s: %w", username, err)
	}

	// Check if authorized_keys file exists
	fileInfo, err := r.fs.Stat(authKeysFile)
	var content string

	if err == nil && fileInfo != nil {
		// File exists, read content and append
		data, err := r.fs.ReadFile(authKeysFile)
		if err != nil {
			return fmt.Errorf("failed to read authorized_keys file: %w", err)
		}

		content = string(data)
		// Check if key already exists
		if strings.Contains(content, publicKey) {
			return nil // Key already exists
		}

		// Ensure file ends with newline
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += publicKey + "\n"
	} else {
		// File doesn't exist, create new
		content = publicKey + "\n"
	}

	// Write the file
	if err := r.fs.WriteFile(authKeysFile, []byte(content), 0600); err != nil {
		return fmt.Errorf("failed to write authorized_keys file: %w", err)
	}

	// Set correct ownership
	chownCmd := fmt.Sprintf("chown -R %s:%s %s", username, username, sshDir)
	if _, err := r.commander.Execute("sh", "-c", chownCmd); err != nil {
		return fmt.Errorf("failed to set ownership on SSH directory: %w", err)
	}

	return nil
}
