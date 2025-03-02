package user

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
)

// CreateUser creates a new system user with SSH keys and sudo access
func CreateUser(username string, cfg *config.Config, osInfo *osdetect.OSInfo) error {
	// Check if user already exists
	_, err := user.Lookup(username)
	if err == nil {
		utils.LogInfo("User %s already exists. Skipping user creation.", username)
		return nil
	}

	utils.LogInfo("Creating user %s...", username)

	if cfg.DryRun {
		utils.LogInfo("[DRY-RUN] Create user: %s", username)
		utils.LogInfo("[DRY-RUN] Add user to sudo/wheel group")
		utils.LogInfo("[DRY-RUN] Configure sudo with NOPASSWD: %t", cfg.SudoNoPassword)
		utils.LogInfo("[DRY-RUN] Set up SSH keys in: %s", cfg.SshKeyPath)
		return nil
	}

	// Check if sudo is installed, install it if necessary
	_, err = exec.LookPath("sudo")
	if err != nil {
		if osInfo.OsType == "alpine" {
			cmd := exec.Command("apk", "add", "sudo")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to install sudo on Alpine: %w", err)
			}
			utils.LogInstall("sudo")
		} else {
			cmd := exec.Command("apt-get", "update")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to update package indexes: %w", err)
			}
			cmd = exec.Command("apt-get", "install", "-y", "sudo")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to install sudo on Debian/Ubuntu: %w", err)
			}
			utils.LogInstall("sudo")
		}
	}

	if osInfo.OsType == "alpine" {
		// Alpine user creation
		cmd := exec.Command("adduser", "-D", "-g", "", username)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create user %s on Alpine: %w", username, err)
		}

		// Add to wheel group (sudo group for Alpine)
		addGroupCmd := exec.Command("addgroup", username, "wheel")
		if err := addGroupCmd.Run(); err != nil {
			utils.LogError("Failed to add %s to wheel group: %v", username, err)
		} else {
			utils.LogInfo("Added %s to wheel group", username)
		}

		// Configure sudo
		sudoersDir := "/etc/sudoers.d"
		if err := os.MkdirAll(sudoersDir, 0755); err != nil {
			return fmt.Errorf("failed to create sudoers.d directory: %w", err)
		}

		sudoersFile := filepath.Join(sudoersDir, username)
		utils.BackupFile(sudoersFile, cfg)

		var sudoersContent string
		if cfg.SudoNoPassword {
			sudoersContent = fmt.Sprintf("%s ALL=(ALL) NOPASSWD: ALL\n", username)
		} else {
			sudoersContent = fmt.Sprintf("%s ALL=(ALL) ALL\n", username)
		}

		if err := os.WriteFile(sudoersFile, []byte(sudoersContent), 0440); err != nil {
			return fmt.Errorf("failed to write sudoers file: %w", err)
		}

		// Extract the actual directory name from the SSH_KEY_PATH pattern
		sshDir := strings.ReplaceAll(cfg.SshKeyPath, "%u", username)
		userHomeDir := fmt.Sprintf("/home/%s", username)
		sshDirPath := filepath.Join(userHomeDir, sshDir)

		// Create SSH key directory
		if err := os.MkdirAll(sshDirPath, 0700); err != nil {
			return fmt.Errorf("failed to create SSH directory %s: %w", sshDirPath, err)
		}

		// Add SSH keys
		authorizedKeysPath := filepath.Join(sshDirPath, "authorized_keys")
		authorizedKeysContent := strings.Join(cfg.SshKeys, "\n") + "\n"
		if err := os.WriteFile(authorizedKeysPath, []byte(authorizedKeysContent), 0600); err != nil {
			return fmt.Errorf("failed to write authorized_keys: %w", err)
		}

		// Set permissions
		chownCmd := exec.Command("chown", "-R", fmt.Sprintf("%s:%s", username, username), sshDirPath)
		if err := chownCmd.Run(); err != nil {
			utils.LogError("Failed to set ownership for SSH directory: %v", err)
		}

		// Add .hushlogin
		hushLoginPath := filepath.Join(userHomeDir, ".hushlogin")
		hushLoginFile, err := os.Create(hushLoginPath)
		if err != nil {
			utils.LogError("Failed to create .hushlogin file: %v", err)
		} else {
			hushLoginFile.Close()
			chownHushCmd := exec.Command("chown", fmt.Sprintf("%s:%s", username, username), hushLoginPath)
			if err := chownHushCmd.Run(); err != nil {
				utils.LogError("Failed to set ownership for .hushlogin: %v", err)
			}
		}
	} else {
		// Debian/Ubuntu user creation
		cmd := exec.Command("adduser", "--disabled-password", "--gecos", "", username)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to create user %s on Debian/Ubuntu: %w", username, err)
		}

		// Add to sudo group
		addGroupCmd := exec.Command("usermod", "-aG", "sudo", username)
		if err := addGroupCmd.Run(); err != nil {
			utils.LogError("Failed to add %s to sudo group: %v", username, err)
		} else {
			utils.LogInfo("Added %s to sudo group", username)
		}

		// Configure sudo
		sudoersDir := "/etc/sudoers.d"
		if err := os.MkdirAll(sudoersDir, 0755); err != nil {
			return fmt.Errorf("failed to create sudoers.d directory: %w", err)
		}

		sudoersFile := filepath.Join(sudoersDir, username)
		utils.BackupFile(sudoersFile, cfg)

		var sudoersContent string
		if cfg.SudoNoPassword {
			sudoersContent = fmt.Sprintf("%s ALL=(ALL) NOPASSWD: ALL\n", username)
		} else {
			sudoersContent = fmt.Sprintf("%s ALL=(ALL) ALL\n", username)
		}

		if err := os.WriteFile(sudoersFile, []byte(sudoersContent), 0440); err != nil {
			return fmt.Errorf("failed to write sudoers file: %w", err)
		}

		// Extract the actual directory name from the SSH_KEY_PATH pattern
		sshDir := strings.ReplaceAll(cfg.SshKeyPath, "%u", username)

		// Run commands as the new user to set up SSH
		suCmd := exec.Command("su", "-", username, "-c", fmt.Sprintf("mkdir -p ~/%s && chmod 700 ~/%s", sshDir, sshDir))
		if err := suCmd.Run(); err != nil {
			utils.LogError("Failed to create SSH directory for user: %v", err)
		}

		// Add SSH keys
		for _, key := range cfg.SshKeys {
			suKeyCmd := exec.Command("su", "-", username, "-c", fmt.Sprintf("echo '%s' >> ~/%s/authorized_keys", key, sshDir))
			if err := suKeyCmd.Run(); err != nil {
				utils.LogError("Failed to add SSH key for user: %v", err)
			}
		}

		// Set permissions for authorized_keys
		suPermCmd := exec.Command("su", "-", username, "-c", fmt.Sprintf("chmod 600 ~/%s/authorized_keys", sshDir))
		if err := suPermCmd.Run(); err != nil {
			utils.LogError("Failed to set permissions for authorized_keys: %v", err)
		}

		// Add .hushlogin
		suHushCmd := exec.Command("su", "-", username, "-c", "touch ~/.hushlogin")
		if err := suHushCmd.Run(); err != nil {
			utils.LogError("Failed to create .hushlogin file: %v", err)
		}
	}

	utils.LogSuccess("User %s created successfully", username)
	return nil
}

// DeleteUser deletes a user and their home directory
func DeleteUser(username string, osInfo *osdetect.OSInfo) error {
	// Check if user exists
	_, err := user.Lookup(username)
	if err != nil {
		return fmt.Errorf("user %s does not exist", username)
	}

	utils.LogInfo("Deleting user %s...", username)

	var cmd *exec.Cmd
	if osInfo.OsType == "alpine" {
		cmd = exec.Command("deluser", "--remove-home", username)
	} else {
		cmd = exec.Command("deluser", "--remove-home", username)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete user %s: %w", username, err)
	}

	// Remove sudo configuration
	sudoersFile := filepath.Join("/etc/sudoers.d", username)
	if _, err := os.Stat(sudoersFile); err == nil {
		if err := os.Remove(sudoersFile); err != nil {
			utils.LogError("Failed to remove sudoers file for %s: %v", username, err)
		}
	}

	utils.LogSuccess("User %s deleted successfully", username)
	return nil
}

// ListUsers lists all non-system users
func ListUsers() ([]string, error) {
	var users []string

	// Get all users from /etc/passwd
	passwdFile, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil, fmt.Errorf("failed to read /etc/passwd: %w", err)
	}

	// Parse passwd file
	lines := strings.Split(string(passwdFile), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		fields := strings.Split(line, ":")
		if len(fields) < 7 {
			continue
		}

		username := fields[0]
		uid := fields[2]
		shell := fields[6]

		// Skip system users (UID < 1000) and users with nologin shell
		uidInt := 0
		fmt.Sscanf(uid, "%d", &uidInt)
		if uidInt >= 1000 && !strings.Contains(shell, "nologin") && !strings.Contains(shell, "false") {
			users = append(users, username)
		}
	}

	return users, nil
}