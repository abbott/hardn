package ssh

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
)

// WriteSSHConfig writes the SSH server configuration based on OS type
func WriteSSHConfig(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Configure SSH server with the following settings:")
		logging.LogInfo("[DRY-RUN] - Protocol: 2")
		logging.LogInfo("[DRY-RUN] - Port: %d", cfg.SshPort)
		logging.LogInfo("[DRY-RUN] - Listen Address: %s", cfg.SshListenAddress)
		logging.LogInfo("[DRY-RUN] - Authentication Method: publickey")
		logging.LogInfo("[DRY-RUN] - PermitRootLogin: %t", cfg.PermitRootLogin)
		logging.LogInfo("[DRY-RUN] - Allowed Users: %s", strings.Join(cfg.SshAllowedUsers, ", "))
		logging.LogInfo("[DRY-RUN] - Password Authentication: no")
		logging.LogInfo("[DRY-RUN] - AuthorizedKeysFile: .ssh/authorized_keys %s/authorized_keys", cfg.SshKeyPath)

		if osInfo.OsType == "alpine" {
			logging.LogInfo("[DRY-RUN] - Write config to /etc/ssh/sshd_config")
			logging.LogInfo("[DRY-RUN] - Restart sshd service using OpenRC")
		} else {
			logging.LogInfo("[DRY-RUN] - Configure systemd socket at /etc/systemd/system/ssh.socket.d/listen.conf")
			logging.LogInfo("[DRY-RUN] - Write config to %s", cfg.SshConfigFile)
			logging.LogInfo("[DRY-RUN] - Restart ssh service using systemd")
		}
		return nil
	}

	logging.LogInfo("Configuring SSH...")

	// Format SSH listen address and port
	sshListenAddress := cfg.SshListenAddress
	if !strings.Contains(sshListenAddress, ":") {
		sshListenAddress = fmt.Sprintf("%s:%d", sshListenAddress, cfg.SshPort)
	}

	if osInfo.OsType == "alpine" {
		// Alpine uses /etc/ssh/sshd_config directly
		// Backup original config
		utils.BackupFile("/etc/ssh/sshd_config", cfg)

		// Determine root login setting
		permitRootLogin := "no"
		if cfg.PermitRootLogin {
			permitRootLogin = "yes"
		}

		// Create new config
		configContent := fmt.Sprintf(`Protocol 2
StrictModes yes

Port %d
ListenAddress %s

AuthenticationMethods publickey
PubkeyAuthentication yes

HostbasedAcceptedKeyTypes ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519

PermitRootLogin %s
AllowUsers %s

PasswordAuthentication no
PermitEmptyPasswords no

AuthorizedKeysFile    .ssh/authorized_keys    %s/authorized_keys
`, cfg.SshPort, sshListenAddress, permitRootLogin, strings.Join(cfg.SshAllowedUsers, " "), cfg.SshKeyPath)

		// Write the file
		if err := os.WriteFile("/etc/ssh/sshd_config", []byte(configContent), 0644); err != nil {
			return fmt.Errorf("failed to write Alpine SSH config for port %d: %w", cfg.SshPort, err)
		}

		// Restart SSH using OpenRC
		cmd := exec.Command("rc-service", "sshd", "restart")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restart Alpine SSH service for port %d: %w", cfg.SshPort, err)
		}

		logging.LogSuccess("SSH configured for Alpine Linux")
	} else {
		// Debian/Ubuntu with systemd
		utils.BackupFile("/etc/systemd/system/ssh.socket.d/listen.conf", cfg)

		// Create socket config directory
		if err := os.MkdirAll("/etc/systemd/system/ssh.socket.d", 0755); err != nil {
			return fmt.Errorf("failed to create SSH socket directory: %w", err)
		}

		// Write socket config
		socketConfig := fmt.Sprintf(`[Socket]
ListenStream=
ListenStream=%d
`, cfg.SshPort)

		if err := os.WriteFile("/etc/systemd/system/ssh.socket.d/listen.conf", []byte(socketConfig), 0644); err != nil {
			logging.LogError("Failed to set ssh port listener for port %d.", cfg.SshPort)
			return fmt.Errorf("failed to write SSH socket config for port %d: %w", cfg.SshPort, err)
		}

		// Ensure config directory exists
		if err := os.MkdirAll(filepath.Dir(cfg.SshConfigFile), 0755); err != nil {
			return fmt.Errorf("failed to create SSH config directory %s: %w", filepath.Dir(cfg.SshConfigFile), err)
		}

		// Determine root login setting
		permitRootLogin := "no"
		if cfg.PermitRootLogin {
			permitRootLogin = "yes"
		}

		// Set SSH config
		utils.BackupFile(cfg.SshConfigFile, cfg)

		configContent := fmt.Sprintf(`### Reference
### https://cryptsus.com/blog/how-to-secure-your-ssh-server-with-public-key-elliptic-curve-ed25519-crypto.html

Protocol 2
StrictModes yes

ListenAddress %s

AuthenticationMethods publickey
PubkeyAuthentication yes

HostbasedAcceptedKeyTypes ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,ssh-ed25519
#PubkeyAcceptedKeyTypes sk-ecdsa-sha2-nistp256@openssh.com,ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,sk-ssh-ed25519@openssh.com

PermitRootLogin %s
AllowUsers %s

# To disable tunneled clear text passwords, change to no here!
PasswordAuthentication no
PermitEmptyPasswords no

#AuthorizedKeysFile /etc/ssh/authorized_keys
# mkdir custom SSH path (e.g., /home/$USERNAME/$SSH_KEY_PATH)
AuthorizedKeysFile    .ssh/authorized_keys    %s/authorized_keys

### PVE ONLY: DO NOT DISABLE
#X11Forwarding yes
#AuthorizedKeysFile /etc/pve/priv/authorized_keys
`, sshListenAddress, permitRootLogin, strings.Join(cfg.SshAllowedUsers, " "), cfg.SshKeyPath)

		if err := os.WriteFile(cfg.SshConfigFile, []byte(configContent), 0644); err != nil {
			logging.LogError("Failed to create %s", cfg.SshConfigFile)
			return fmt.Errorf("failed to write SSH config to %s: %w", cfg.SshConfigFile, err)
		}

		// Restart SSH
		cmd := exec.Command("systemctl", "restart", "ssh")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restart SSH service on port %d: %w", cfg.SshPort, err)
		}

		logging.LogSuccess("SSH configured for Debian/Ubuntu")
	}

	return nil
}

// DisableRootSSHAccess disables root SSH access
func DisableRootSSHAccess(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Disable root SSH access with the following changes:")
		if osInfo.OsType == "alpine" {
			logging.LogInfo("[DRY-RUN] - Modify /etc/ssh/sshd_config to set 'PermitRootLogin no'")
			logging.LogInfo("[DRY-RUN] - Remove 'root' from AllowUsers directive")
			logging.LogInfo("[DRY-RUN] - Restart sshd service using OpenRC")
		} else {
			logging.LogInfo("[DRY-RUN] - Modify %s to set 'PermitRootLogin no'", cfg.SshConfigFile)
			logging.LogInfo("[DRY-RUN] - Remove 'root' from AllowUsers directive")
			logging.LogInfo("[DRY-RUN] - Restart ssh service using systemd")
		}
		return nil
	}

	if osInfo.OsType == "alpine" {
		// For Alpine, modify the main sshd_config file
		configFile := "/etc/ssh/sshd_config"
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			return fmt.Errorf("/etc/ssh/sshd_config not found: %w", err)
		}

		utils.BackupFile(configFile, cfg)

		// Read the file
		content, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read Alpine SSH config: %w", err)
		}

		// Modify the content
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			// Change PermitRootLogin
			if strings.HasPrefix(line, "PermitRootLogin yes") {
				lines[i] = "PermitRootLogin no"
			}

			// Remove 'root' from AllowUsers
			if strings.HasPrefix(line, "AllowUsers") {
				// Get the users
				fields := strings.Fields(line)
				if len(fields) > 1 {
					// Remove 'root'
					var newUsers []string
					for _, user := range fields[1:] {
						if user != "root" {
							newUsers = append(newUsers, user)
						}
					}

					// Put back together
					lines[i] = "AllowUsers " + strings.Join(newUsers, " ")
				}
			}
		}

		// Write back the file
		if err := os.WriteFile(configFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return fmt.Errorf("failed to write updated Alpine SSH config: %w", err)
		}

		// Restart SSH
		cmd := exec.Command("rc-service", "sshd", "restart")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restart Alpine SSH service after disabling root login: %w", err)
		}

		logging.LogSuccess("Root SSH access disabled in Alpine Linux")
	} else {
		// For Debian/Ubuntu
		configFile := cfg.SshConfigFile
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			return fmt.Errorf("SSH config file %s not found: %w", configFile, err)
		}

		utils.BackupFile(configFile, cfg)

		// Read the file
		content, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read SSH config %s: %w", configFile, err)
		}

		// Modify the content
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			// Change PermitRootLogin
			if strings.HasPrefix(line, "PermitRootLogin yes") {
				lines[i] = "PermitRootLogin no"
			}

			// Remove 'root' from AllowUsers
			if strings.HasPrefix(line, "AllowUsers") {
				// Get the users
				fields := strings.Fields(line)
				if len(fields) > 1 {
					// Remove 'root'
					var newUsers []string
					for _, user := range fields[1:] {
						if user != "root" {
							newUsers = append(newUsers, user)
						}
					}

					// Put back together
					lines[i] = "AllowUsers " + strings.Join(newUsers, " ")
				}
			}
		}

		// Write back the file
		if err := os.WriteFile(configFile, []byte(strings.Join(lines, "\n")), 0644); err != nil {
			return fmt.Errorf("failed to write updated SSH config to %s: %w", configFile, err)
		}

		// Restart SSH
		cmd := exec.Command("systemctl", "restart", "ssh")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to restart SSH service after disabling root login: %w", err)
		}

		logging.LogSuccess("Root SSH access disabled in Debian/Ubuntu")
	}

	return nil
}