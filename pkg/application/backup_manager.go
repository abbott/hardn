// pkg/application/backup_manager.go
package application

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// BackupManager is an application service for backup operations
type BackupManager struct {
	backupService service.BackupService
}

// NewBackupManager creates a new BackupManager
func NewBackupManager(backupService service.BackupService) *BackupManager {
	return &BackupManager{
		backupService: backupService,
	}
}

// BackupFile creates a backup of the specified file
func (m *BackupManager) BackupFile(filePath string) error {
	return m.backupService.BackupFile(filePath)
}

// GetBackupConfig retrieves the current backup configuration
func (m *BackupManager) GetBackupConfig() (*model.BackupConfig, error) {
	return m.backupService.GetBackupConfig()
}

// ToggleBackups enables or disables backups
func (m *BackupManager) ToggleBackups() error {
	config, err := m.backupService.GetBackupConfig()
	if err != nil {
		return fmt.Errorf("failed to get backup config: %w", err)
	}
	
	return m.backupService.EnableBackups(!config.Enabled)
}

// SetBackupDirectory changes the backup directory
func (m *BackupManager) SetBackupDirectory(directory string) error {
	// Expand path if it starts with ~
	if len(directory) > 0 && directory[:1] == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			directory = filepath.Join(home, directory[1:])
		}
	}
	
	return m.backupService.SetBackupDirectory(directory)
}

// VerifyBackupDirectory ensures the backup directory exists and is writable
func (m *BackupManager) VerifyBackupDirectory() error {
	return m.backupService.VerifyBackupDirectory()
}

// CleanupOldBackups removes backups older than the specified number of days
func (m *BackupManager) CleanupOldBackups(days int) error {
	return m.backupService.CleanupOldBackups(days)
}

// GetBackupStatus returns a simple status indicating if backups are enabled
// and the current backup directory
func (m *BackupManager) GetBackupStatus() (bool, string, error) {
	config, err := m.backupService.GetBackupConfig()
	if err != nil {
		return false, "", fmt.Errorf("failed to get backup status: %w", err)
	}
	
	return config.Enabled, config.BackupDir, nil
}

// VerifyBackupPath checks if the backup path exists and is writable
func (m *BackupManager) VerifyBackupPath() (bool, error) {
	config, err := m.backupService.GetBackupConfig()
	if err != nil {
		return false, fmt.Errorf("failed to get backup config: %w", err)
	}
	
	// Check if directory exists
	if _, err := os.Stat(config.BackupDir); os.IsNotExist(err) {
		return false, nil
	}
	
	// Check if directory is writable by trying to create a test file
	testFile := filepath.Join(config.BackupDir, ".write_test")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	if err != nil {
		return false, nil
	}
	
	// Clean up test file
	os.Remove(testFile)
	
	return true, nil
}