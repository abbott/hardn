// pkg/domain/service/backup_service.go
package service

import (
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// BackupService defines operations for backup functionality
type BackupService interface {
	// BackupFile backs up a file with a timestamp
	BackupFile(filePath string) error
	
	// ListBackups returns a list of all backups for a specific file
	ListBackups(filePath string) ([]model.BackupFile, error)
	
	// RestoreBackup restores a file from backup
	RestoreBackup(backupPath, originalPath string) error
	
	// CleanupOldBackups removes backups older than specified days
	CleanupOldBackups(daysToKeep int) error
	
	// VerifyBackupDirectory ensures the backup directory exists and is writable
	VerifyBackupDirectory() error
	
	// GetBackupConfig retrieves the current backup configuration
	GetBackupConfig() (*model.BackupConfig, error)
	
	// EnableBackups enables backups
	EnableBackups(enabled bool) error
	
	// SetBackupDirectory changes the backup directory
	SetBackupDirectory(directory string) error
}

// BackupServiceImpl implements BackupService
type BackupServiceImpl struct {
	repository BackupRepository
}

// NewBackupServiceImpl creates a new BackupServiceImpl
func NewBackupServiceImpl(repository BackupRepository) *BackupServiceImpl {
	return &BackupServiceImpl{
		repository: repository,
	}
}

// BackupRepository defines the repository operations needed by BackupService
type BackupRepository interface {
	BackupFile(filePath string) error
	ListBackups(filePath string) ([]model.BackupFile, error)
	RestoreBackup(backupPath, originalPath string) error
	CleanupOldBackups(before time.Time) error
	VerifyBackupDirectory() error
	GetBackupConfig() (*model.BackupConfig, error)
	SetBackupConfig(config model.BackupConfig) error
}

// Implementation of BackupService methods
func (s *BackupServiceImpl) BackupFile(filePath string) error {
	return s.repository.BackupFile(filePath)
}

func (s *BackupServiceImpl) ListBackups(filePath string) ([]model.BackupFile, error) {
	return s.repository.ListBackups(filePath)
}

func (s *BackupServiceImpl) RestoreBackup(backupPath, originalPath string) error {
	return s.repository.RestoreBackup(backupPath, originalPath)
}

func (s *BackupServiceImpl) CleanupOldBackups(daysToKeep int) error {
	// Convert days to a specific time
	cutoffTime := time.Now().AddDate(0, 0, -daysToKeep)
	return s.repository.CleanupOldBackups(cutoffTime)
}

func (s *BackupServiceImpl) VerifyBackupDirectory() error {
	return s.repository.VerifyBackupDirectory()
}

func (s *BackupServiceImpl) GetBackupConfig() (*model.BackupConfig, error) {
	return s.repository.GetBackupConfig()
}

func (s *BackupServiceImpl) EnableBackups(enabled bool) error {
	config, err := s.repository.GetBackupConfig()
	if err != nil {
		return fmt.Errorf("failed to get backup config: %w", err)
	}
	
	config.Enabled = enabled
	return s.repository.SetBackupConfig(*config)
}

func (s *BackupServiceImpl) SetBackupDirectory(directory string) error {
	config, err := s.repository.GetBackupConfig()
	if err != nil {
		return fmt.Errorf("failed to get backup config: %w", err)
	}
	
	config.BackupDir = directory
	return s.repository.SetBackupConfig(*config)
}