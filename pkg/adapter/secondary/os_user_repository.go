// pkg/adapter/secondary/os_user_repository.go
package secondary

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/ports/secondary" // New import for the port
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	portsecondary "github.com/abbott/hardn/pkg/port/secondary"
)

// Helper function to get the configuration for SSH key path
func getConfigForSSHKeyPath() *config.Config {
	// Set silent mode for the logger to prevent info messages
	logging.SetSilentMode(true)

	// Load the configuration
	cfg, err := config.LoadConfig("")
	if err != nil {
		// If we can't load config, return the default
		return config.DefaultConfig()
	}

	// Restore silent mode to its previous state if needed
	// logging.SetSilentMode(false)

	return cfg
}

type OSUserRepository struct {
	fs            interfaces.FileSystem
	commander     interfaces.Commander
	osType        string
	userLoginPort secondary.UserLoginPort
}

// create a new OSUserRepository
func NewOSUserRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	osType string,
) portsecondary.UserRepository {
	return &OSUserRepository{
		fs:            fs,
		commander:     commander,
		osType:        osType,
		userLoginPort: NewLastCommandAdapter(commander),
	}
}

// check if a user exists
func (r *OSUserRepository) UserExists(username string) (bool, error) {
	_, err := r.commander.Execute("id", username)
	if err != nil {
		// Command failed, user probably doesn't exist
		return false, nil
	}
	return true, nil
}

// Create a new system user
func (r *OSUserRepository) CreateUser(user model.User) error {
	// Check if user already exists
	exists, err := r.UserExists(user.Username)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}

	// If user exists, we'll just update their sudo and SSH settings
	if exists {
		// Configure sudo if needed
		if user.HasSudo {
			if err := r.ConfigureSudo(user.Username, user.SudoNoPassword); err != nil {
				return err
			}
		}

		// Set up SSH keys
		for _, key := range user.SshKeys {
			if err := r.AddSSHKey(user.Username, key); err != nil {
				return err
			}
		}

		return nil
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

// GetUser retrieves basic user information
func (r *OSUserRepository) GetUser(username string) (*model.User, error) {
	exists, err := r.UserExists(username)
	if err != nil || !exists {
		return nil, fmt.Errorf("user %s does not exist or error checking: %w", username, err)
	}

	// Just return basic user for now
	return &model.User{
		Username: username,
	}, nil
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

// Configure sudo access for a user
func (r *OSUserRepository) ConfigureSudo(username string, noPassword bool) error {
	// First check if the user exists
	exists, err := r.UserExists(username)
	if err != nil {
		return fmt.Errorf("error checking user existence: %w", err)
	}
	if !exists {
		return fmt.Errorf("user %s does not exist", username)
	}

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

// GetNonSystemUsers retrieves non-system users on the system
func (r *OSUserRepository) GetNonSystemUsers() ([]model.User, error) {
	var users []model.User

	// Try to read /etc/passwd
	data, err := r.fs.ReadFile("/etc/passwd")
	if err != nil {
		// Try with command if file can't be read
		output, cmdErr := r.commander.Execute("cat", "/etc/passwd")
		if cmdErr != nil {
			return nil, fmt.Errorf("failed to read user information: %w", err)
		}
		data = output
	}

	// Parse user entries
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ":")
		if len(fields) >= 7 {
			username := fields[0]
			uid, err := strconv.Atoi(fields[2])
			if err != nil {
				continue
			}

			// Skip system users (UID < 1000 on most systems)
			if uid < 1000 {
				continue
			}

			// Skip system service users
			if strings.HasSuffix(fields[6], "/nologin") ||
				strings.HasSuffix(fields[6], "/false") ||
				strings.HasSuffix(fields[6], "/null") {
				continue
			}

			user := model.User{
				Username: username,
			}

			// Check if user has sudo access
			hasSudo, err := r.checkUserSudo(username)
			if err == nil {
				user.HasSudo = hasSudo
			}

			users = append(users, user)
		}
	}

	return users, nil
}

// checkUserSudo checks if a user has sudo access
func (r *OSUserRepository) checkUserSudo(username string) (bool, error) {
	// Check if user is in sudo or wheel group
	groupData, err := r.fs.ReadFile("/etc/group")
	if err != nil {
		// Try command if file can't be read
		output, cmdErr := r.commander.Execute("cat", "/etc/group")
		if cmdErr != nil {
			return false, fmt.Errorf("failed to read group information: %w", err)
		}
		groupData = output
	}

	scanner := bufio.NewScanner(strings.NewReader(string(groupData)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "sudo:") || strings.HasPrefix(line, "wheel:") {
			fields := strings.Split(line, ":")
			if len(fields) >= 4 {
				users := strings.Split(fields[3], ",")
				for _, user := range users {
					if user == username {
						return true, nil
					}
				}
			}
		}
	}

	// Check sudoers file
	sudoersFile := filepath.Join("/etc/sudoers.d", username)
	_, err = r.fs.Stat(sudoersFile)
	if err == nil {
		return true, nil
	}

	// Check main sudoers file
	output, err := r.commander.Execute("grep", username, "/etc/sudoers")
	if err == nil && len(output) > 0 {
		return true, nil
	}

	return false, nil
}

// GetNonSystemGroups retrieves non-system groups on the system
func (r *OSUserRepository) GetNonSystemGroups() ([]string, error) {
	var groups []string

	// Try to read /etc/group
	data, err := r.fs.ReadFile("/etc/group")
	if err != nil {
		// Try command if file can't be read
		output, cmdErr := r.commander.Execute("cat", "/etc/group")
		if cmdErr != nil {
			return nil, fmt.Errorf("failed to read group information: %w", err)
		}
		data = output
	}

	// Parse group entries
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ":")
		if len(fields) >= 4 {
			groupName := fields[0]
			gid, err := strconv.Atoi(fields[2])
			if err != nil {
				continue
			}

			// Skip system groups (GID < 1000 on most systems)
			if gid < 1000 {
				continue
			}

			// Skip empty groups if they don't have members
			if fields[3] == "" {
				// Only add groups if they're relevant (either not empty or known user groups)
				if !r.isUserGroup(groupName) {
					continue
				}
			}

			groups = append(groups, groupName)
		}
	}

	return groups, nil
}

// isUserGroup returns true if the group is a typical user group
func (r *OSUserRepository) isUserGroup(name string) bool {
	userGroups := []string{
		"users", "staff", "wheel", "sudo", "admin", "adm", "netdev",
		"lpadmin", "sambashare", "docker", "plugdev", "libvirt",
	}

	for _, group := range userGroups {
		if name == group {
			return true
		}
	}

	return false
}

// GetExtendedUserInfo retrieves detailed information about a user including UID, GID, home directory, and last login
func (r *OSUserRepository) GetExtendedUserInfo(username string) (*model.User, error) {
	// Get configuration from config package
	cfg := getConfigForSSHKeyPath()

	// First check if the user exists
	exists, err := r.UserExists(username)
	if err != nil {
		return nil, fmt.Errorf("error checking user existence: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("user %s does not exist", username)
	}

	// Get basic user first
	user, err := r.GetUser(username)
	if err != nil {
		return nil, err
	}

	// Get UID and GID (get numeric values directly)
	uidOutput, err := r.commander.Execute("id", "-u", username)
	if err == nil {
		user.UID = strings.TrimSpace(string(uidOutput))
	}

	gidOutput, err := r.commander.Execute("id", "-g", username)
	if err == nil {
		user.GID = strings.TrimSpace(string(gidOutput))
	}

	// If we still don't have UID/GID, try to get from /etc/passwd
	if user.UID == "" || user.GID == "" {
		passwdOutput, err := r.commander.Execute("getent", "passwd", username)
		if err == nil {
			passwdParts := strings.Split(string(passwdOutput), ":")
			if len(passwdParts) >= 4 {
				if user.UID == "" {
					user.UID = strings.TrimSpace(passwdParts[2])
				}
				if user.GID == "" {
					user.GID = strings.TrimSpace(passwdParts[3])
				}
			}
		}
	}

	// If we still don't have values, fall back to defaults
	if user.UID == "" {
		user.UID = "1000"
	}
	if user.GID == "" {
		user.GID = "1000"
	}

	// Get home directory
	homeOutput, err := r.commander.Execute("getent", "passwd", username)
	if err != nil {
		// Set a default home directory
		user.HomeDirectory = fmt.Sprintf("/home/%s", username)
	}

	// Parse home directory from passwd entry (format: name:x:uid:gid:gecos:home:shell)
	passwdParts := strings.Split(string(homeOutput), ":")
	if len(passwdParts) >= 6 {
		user.HomeDirectory = passwdParts[5]
	}

	// Get last login time and IP using the port
	lastLoginTime, ipAddress, err := r.userLoginPort.GetLastLoginInfo(username)
	if err != nil || lastLoginTime.IsZero() {
		// No login found or error occurred
		user.LastLogin = "Never logged in"
		user.LastLoginIP = ""
	} else {
		// Format the time as a string with timezone instead of year (no day of week)
		// Convert to local timezone first
		localTime := lastLoginTime.Local()
		user.LastLogin = localTime.Format("Jan 2 15:04:05 -0700")
		user.LastLoginIP = ipAddress
	}

	// Check if user has sudo
	user.HasSudo = false
	user.SudoNoPassword = false

	// Check sudo group membership
	sudoGroup := "sudo"
	if r.osType == "alpine" {
		sudoGroup = "wheel"
	}

	groupOutput, err := r.commander.Execute("groups", username)
	if err == nil && strings.Contains(string(groupOutput), sudoGroup) {
		user.HasSudo = true
	}

	// Check for NOPASSWD sudo config
	sudoersFile := filepath.Join("/etc/sudoers.d", username)
	sudoersContent, err := r.fs.ReadFile(sudoersFile)
	if err == nil && strings.Contains(string(sudoersContent), "NOPASSWD:") {
		user.SudoNoPassword = true
	}

	// Get SSH keys
	user.SshKeys = []string{}

	// Use the configured sshKeyPath pattern, replacing %u with username
	sshKeyPath := cfg.SshKeyPath
	if sshKeyPath == "" {
		sshKeyPath = ".ssh_%u" // Default pattern
	}

	// Replace %u with username
	sshKeyPath = strings.ReplaceAll(sshKeyPath, "%u", username)

	// If path doesn't start with /, assume it's relative to home directory
	if !strings.HasPrefix(sshKeyPath, "/") {
		sshKeyPath = filepath.Join(user.HomeDirectory, sshKeyPath)
	}

	// If path doesn't end with authorized_keys, assume it's a directory
	if !strings.HasSuffix(sshKeyPath, "authorized_keys") {
		// Check if path already has .ssh in it
		if !strings.Contains(sshKeyPath, ".ssh") {
			// Add .ssh directory
			sshKeyPath = filepath.Join(sshKeyPath, ".ssh")
		}
		sshKeyPath = filepath.Join(sshKeyPath, "authorized_keys")
	}

	// Try to read the keys file
	authKeysContent, err := r.fs.ReadFile(sshKeyPath)
	if err != nil {
		// Try alternative method using command
		keyOutput, cmdErr := r.commander.Execute("sudo", "cat", sshKeyPath)
		if cmdErr == nil && len(keyOutput) > 0 {
			// Successfully read keys with command
			keys := strings.Split(strings.TrimSpace(string(keyOutput)), "\n")
			for _, key := range keys {
				if key != "" {
					user.SshKeys = append(user.SshKeys, key)
				}
			}
		} else {
			// Try the traditional location as a fallback
			fallbackPath := filepath.Join(user.HomeDirectory, ".ssh", "authorized_keys")
			if sshKeyPath != fallbackPath {
				fallbackContent, fallbackErr := r.fs.ReadFile(fallbackPath)
				if fallbackErr == nil {
					// Found keys in the fallback location
					keys := strings.Split(strings.TrimSpace(string(fallbackContent)), "\n")
					for _, key := range keys {
						if key != "" {
							user.SshKeys = append(user.SshKeys, key)
						}
					}
				}
			}
		}
	} else {
		// Successfully read keys directly
		keys := strings.Split(strings.TrimSpace(string(authKeysContent)), "\n")
		for _, key := range keys {
			if key != "" {
				user.SshKeys = append(user.SshKeys, key)
			}
		}
	}

	return user, nil
}
