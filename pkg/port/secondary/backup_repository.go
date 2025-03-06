package secondary

import (
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// BackupRepository defines the interface for backup operations
type BackupRepository interface {
	// BackupFile backs up a file with a timestamp
	BackupFile(filePath string) error
	
	// ListBackups returns a list of all backups for a specific file
	ListBackups(filePath string) ([]model.BackupFile, error)
	
	// RestoreBackup restores a file from backup
	RestoreBackup(backupPath, originalPath string) error
	
	// CleanupOldBackups removes backups older than specified date
	CleanupOldBackups(before time.Time) error
	
	// VerifyBackupDirectory ensures the backup directory exists and is writable
	VerifyBackupDirectory() error
	
	// GetBackupConfig retrieves the current backup configuration
	GetBackupConfig() (*model.BackupConfig, error)
	
	// SetBackupConfig updates the backup configuration
	SetBackupConfig(config model.BackupConfig) error
}