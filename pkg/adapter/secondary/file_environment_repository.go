// pkg/adapter/secondary/file_environment_repository.go
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

// FileEnvironmentRepository implements EnvironmentRepository using file operations
type FileEnvironmentRepository struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
}

// NewFileEnvironmentRepository creates a new FileEnvironmentRepository
func NewFileEnvironmentRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
) secondary.EnvironmentRepository {
	return &FileEnvironmentRepository{
		fs:        fs,
		commander: commander,
	}
}

// SetupSudoPreservation configures sudo to preserve the HARDN_CONFIG environment variable
func (r *FileEnvironmentRepository) SetupSudoPreservation(username string) error {
	// Check if username is empty
	if username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	// Ensure sudoers.d directory exists
	sudoersDir := "/etc/sudoers.d"
	if _, err := r.fs.Stat(sudoersDir); os.IsNotExist(err) {
		return fmt.Errorf("sudoers.d directory does not exist; your system may not support sudo drop-in configurations")
	}

	// Create/modify sudoers file for the user
	sudoersFile := filepath.Join(sudoersDir, username)

	// Check if file already exists
	var content string
	fileInfo, err := r.fs.Stat(sudoersFile)
	if err == nil && fileInfo != nil {
		// Read existing content
		data, err := r.fs.ReadFile(sudoersFile)
		if err != nil {
			return fmt.Errorf("failed to read existing sudoers file %s: %w", sudoersFile, err)
		}
		content = string(data)

		// Check if HARDN_CONFIG is already in the file
		if strings.Contains(content, "env_keep += \"HARDN_CONFIG\"") {
			return nil // Already configured
		}

		// Append to existing content
		content = strings.TrimSpace(content) + "\n"
	}

	// env_keep directive
	content += fmt.Sprintf("Defaults:%s env_keep += \"HARDN_CONFIG\"\n", username)

	// Create a temporary file for validation
	tempDir := os.TempDir()
	tempFile := filepath.Join(tempDir, "hardn_sudoers_temp")
	if err := r.fs.WriteFile(tempFile, []byte(content), 0440); err != nil {
		return fmt.Errorf("failed to create temporary sudoers file at %s: %w", tempFile, err)
	}

	// Validate the sudoers file
	_, err = r.commander.Execute("visudo", "-c", "-f", tempFile)
	if err != nil {
		// Clean up temp file
		if err := r.fs.Remove(tempFile); err != nil {
			// Log warning but don't fail the operation since this is just cleanup
			fmt.Printf("Warning: Failed to remove test file %s: %v\n", tempFile, err)
		}
		return fmt.Errorf("invalid sudoers configuration: %w", err)
	}

	// Clean up temp file
	if err := r.fs.Remove(tempFile); err != nil {
		// Log warning but don't fail the operation since this is just cleanup
		fmt.Printf("Warning: Failed to remove test file %s: %v\n", tempFile, err)
	}

	// Write the validated content to the actual sudoers file
	if err := r.fs.WriteFile(sudoersFile, []byte(content), 0440); err != nil {
		return fmt.Errorf("failed to write sudoers file %s: %w", sudoersFile, err)
	}

	return nil
}

// IsSudoPreservationEnabled checks if the HARDN_CONFIG environment variable is preserved in sudo
func (r *FileEnvironmentRepository) IsSudoPreservationEnabled(username string) (bool, error) {
	// Check if username is empty
	if username == "" {
		return false, fmt.Errorf("username cannot be empty")
	}

	// Check if sudoers file exists
	sudoersFile := filepath.Join("/etc/sudoers.d", username)
	fileInfo, err := r.fs.Stat(sudoersFile)
	if err != nil || fileInfo == nil {
		return false, nil // File doesn't exist, preservation not enabled
	}

	// Read file content
	data, err := r.fs.ReadFile(sudoersFile)
	if err != nil {
		return false, fmt.Errorf("failed to read sudoers file %s: %w", sudoersFile, err)
	}

	// Check if HARDN_CONFIG is preserved
	return strings.Contains(string(data), "env_keep += \"HARDN_CONFIG\""), nil
}

// GetEnvironmentConfig retrieves the current environment configuration
func (r *FileEnvironmentRepository) GetEnvironmentConfig() (*model.EnvironmentConfig, error) {
	config := &model.EnvironmentConfig{
		ConfigPath:   os.Getenv("HARDN_CONFIG"),
		PreserveSudo: false, // Will be determined below
	}

	// Get username
	username := os.Getenv("SUDO_USER")
	if username == "" {
		username = os.Getenv("USER")
	}
	config.Username = username

	// Check sudo preservation if username is not empty
	if username != "" {
		isEnabled, err := r.IsSudoPreservationEnabled(username)
		if err == nil {
			config.PreserveSudo = isEnabled
		}
	}

	return config, nil
}
