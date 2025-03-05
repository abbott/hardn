// pkg/adapter/secondary/os_user_repository.go
package secondary

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// OSUserRepository implements UserRepository using OS operations
type OSUserRepository struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
	osType    string  // e.g., "alpine", "debian", etc.
}

// NewOSUserRepository creates a new OSUserRepository
func NewOSUserRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	osType string,
) secondary.UserRepository {
	return &OSUserRepository{
		fs:        fs,
		commander: commander,
		osType:    osType,
	}
}

// UserExists checks if a user exists
func (r *OSUserRepository) UserExists(username string) (bool, error) {
	_, err := r.commander.Execute("id", username)
	if err != nil {
		// Command failed, user probably doesn't exist
		return false, nil
	}
	return true, nil
}

// CreateUser creates a new system user
func (r *OSUserRepository) CreateUser(user model.User) error {
	// Check if user already exists
	exists, err := r.UserExists(user.Username)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}
	if exists {
		return fmt.Errorf("user %s already exists", user.Username)
	}

	// Create the user based on OS type
	if r.osType == "alpine" {
		// Alpine user creation
		_, err := r.commander.Execute("adduser", "-D", "-g", "", user.Username)
		if err != nil {
			return fmt.Errorf("failed to create user %s on Alpine: %w", user.Username, err)
		}

		// Add to wheel group for sudo
		if user.HasSudo {
			_, err := r.commander.Execute("addgroup", user.Username, "wheel")
			if err != nil {
				return fmt.Errorf("failed to add user %s to wheel group: %w", user.Username, err)
			}
		}
	} else {
		// Debian/Ubuntu user creation
		_, err := r.commander.Execute("adduser", "--disabled-password", "--gecos", "", user.Username)
		if err != nil {
			return fmt.Errorf("failed to create user %s on Debian/Ubuntu: %w", user.Username, err)
		}

		// Add to sudo group
		if user.HasSudo {
			_, err := r.commander.Execute("usermod", "-aG", "sudo", user.Username)
			if err != nil {
				return fmt.Errorf("failed to add user %s to sudo group: %w", user.Username, err)
			}
		}
	}

	// Set up SSH keys
	for _, key := range user.SshKeys {
		if err := r.AddSSHKey(user.Username, key); err != nil {
			return err
		}
	}

	// Configure sudo if needed
	if user.HasSudo {
		if err := r.ConfigureSudo(user.Username, user.SudoNoPassword); err != nil {
			return err
		}
	}

	return nil
}

// GetUser retrieves user information
func (r *OSUserRepository) GetUser(username string) (*model.User, error) {
	// Implementation...
	return nil, nil
}

// AddSSHKey adds an SSH key for a user
func (r *OSUserRepository) AddSSHKey(username, publicKey string) error {
	// Common path for SSH keys
	var sshDir string
	var homePath string
	
	if r.osType == "alpine" {
		homePath = fmt.Sprintf("/home/%s", username)
		sshDir = filepath.Join(homePath, ".ssh")
		
		// Create .ssh directory if it doesn't exist
		if err := r.fs.MkdirAll(sshDir, 0700); err != nil {
			return fmt.Errorf("failed to create SSH directory for user %s: %w", username, err)
		}
		
		// Create authorized_keys file if it doesn't exist
		authKeysPath := filepath.Join(sshDir, "authorized_keys")
		authKeysExists := false
		_, err := r.fs.Stat(authKeysPath)
		if err == nil {
			authKeysExists = true
		}
		
		if authKeysExists {
			// Read existing keys
			existingContent, err := r.fs.ReadFile(authKeysPath)
			if err != nil {
				return fmt.Errorf("failed to read authorized_keys: %w", err)
			}
			
			// Append new key if not already present
			if !strings.Contains(string(existingContent), publicKey) {
				newContent := string(existingContent)
				if !strings.HasSuffix(newContent, "\n") {
					newContent += "\n"
				}
				newContent += publicKey + "\n"
				
				if err := r.fs.WriteFile(authKeysPath, []byte(newContent), 0600); err != nil {
					return fmt.Errorf("failed to update authorized_keys: %w", err)
				}
			}
		} else {
			// Create new file
			if err := r.fs.WriteFile(authKeysPath, []byte(publicKey+"\n"), 0600); err != nil {
				return fmt.Errorf("failed to create authorized_keys: %w", err)
			}
		}
		
		// Set correct ownership
		_, err = r.commander.Execute("chown", "-R", fmt.Sprintf("%s:%s", username, username), sshDir)
		if err != nil {
			return fmt.Errorf("failed to set ownership for SSH directory: %w", err)
		}
	} else {
		// Debian/Ubuntu - use su to run commands as the user
		_, err := r.commander.Execute("su", "-", username, "-c", "mkdir -p ~/.ssh && chmod 700 ~/.ssh")
		if err != nil {
			return fmt.Errorf("failed to create SSH directory for user %s: %w", username, err)
		}
		
		// Add the key using a here-document style input
		_, err = r.commander.ExecuteWithInput(publicKey+"\n", "su", "-", username, "-c", "cat >> ~/.ssh/authorized_keys")
		if err != nil {
			return fmt.Errorf("failed to add SSH key for user %s: %w", username, err)
		}
		
		_, err = r.commander.Execute("su", "-", username, "-c", "chmod 600 ~/.ssh/authorized_keys")
		if err != nil {
			return fmt.Errorf("failed to set permissions for authorized_keys: %w", err)
		}
	}
	
	return nil
}

// ConfigureSudo configures sudo access for a user
func (r *OSUserRepository) ConfigureSudo(username string, noPassword bool) error {
	// Create sudoers directory if needed
	sudoersDir := "/etc/sudoers.d"
	if err := r.fs.MkdirAll(sudoersDir, 0755); err != nil {
		return fmt.Errorf("failed to create sudoers directory: %w", err)
	}
	
	// Create user sudoers file
	sudoersFile := filepath.Join(sudoersDir, username)
	
	var sudoersContent string
	if noPassword {
		sudoersContent = fmt.Sprintf("%s ALL=(ALL) NOPASSWD: ALL\n", username)
	} else {
		sudoersContent = fmt.Sprintf("%s ALL=(ALL) ALL\n", username)
	}
	
	if err := r.fs.WriteFile(sudoersFile, []byte(sudoersContent), 0440); err != nil {
		return fmt.Errorf("failed to write sudoers file: %w", err)
	}
	
	return nil
}