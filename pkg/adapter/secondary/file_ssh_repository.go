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
}

// NewFileSSHRepository creates a new FileSSHRepository
func NewFileSSHRepository(
    fs interfaces.FileSystem,
    commander interfaces.Commander,
    osType string,
) secondary.SSHRepository {
    return &FileSSHRepository{
        fs:        fs,
        commander: commander,
        osType:    osType,
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
    
    // Root login setting
    rootLoginValue := "no"
    if config.PermitRootLogin {
        rootLoginValue = "yes"
    }
    content.WriteString(fmt.Sprintf("PermitRootLogin %s\n", rootLoginValue))
    
    // Allowed users
    if len(config.AllowedUsers) > 0 {
        content.WriteString(fmt.Sprintf("AllowUsers %s\n", strings.Join(config.AllowedUsers, " ")))
    }
    content.WriteString("\n")
    
    // Password authentication
    content.WriteString("PasswordAuthentication no\n")
    content.WriteString("PermitEmptyPasswords no\n\n")
    
    // Authorized keys
    if len(config.KeyPaths) > 0 {
        for _, path := range config.KeyPaths {
            content.WriteString(fmt.Sprintf("AuthorizedKeysFile %s\n", path))
        }
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

// GetSSHConfig reads the current SSH configuration
func (r *FileSSHRepository) GetSSHConfig() (*model.SSHConfig, error) {
    // Implementation to parse SSH config file and return configuration
    // ... (implementation details omitted for brevity)
    return &model.SSHConfig{Port: 22}, nil
}

// DisableRootAccess disables SSH access for the root user
func (r *FileSSHRepository) DisableRootAccess() error {
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

// AddAuthorizedKey adds an SSH public key to a user's authorized_keys
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