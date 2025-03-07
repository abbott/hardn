// pkg/adapter/secondary/file_backup_repository.go
package secondary

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// FileBackupRepository implements BackupRepository using file operations
type FileBackupRepository struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
	config    *model.BackupConfig
}

// NewFileBackupRepository creates a new FileBackupRepository
func NewFileBackupRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	backupDir string,
	enabled bool,
) secondary.BackupRepository {
	return &FileBackupRepository{
		fs:        fs,
		commander: commander,
		config: &model.BackupConfig{
			Enabled:   enabled,
			BackupDir: backupDir,
		},
	}
}

// BackupFile backs up a file with a timestamp
func (r *FileBackupRepository) BackupFile(filePath string) error {
	if !r.config.Enabled {
		return nil // Backups disabled, silently succeed
	}

	// Create backup directory for today
	backupDir := filepath.Join(r.config.BackupDir, time.Now().Format("2006-01-02"))
	if err := r.fs.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory %s: %w", backupDir, err)
	}

	// Get filename without path
	fileName := filepath.Base(filePath)

	// Check if file exists
	_, err := r.fs.Stat(filePath)
	if os.IsNotExist(err) {
		return nil // File doesn't exist, nothing to backup
	}

	// Create backup with timestamp
	backupFile := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", fileName, time.Now().Format("150405")))

	// Read original file
	data, err := r.fs.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s for backup: %w", filePath, err)
	}

	// Write backup file
	if err := r.fs.WriteFile(backupFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup file %s: %w", backupFile, err)
	}

	return nil
}

// ListBackups returns a list of all backups for a specific file
func (r *FileBackupRepository) ListBackups(filePath string) ([]model.BackupFile, error) {
	var backups []model.BackupFile

	// Get filename without path
	fileName := filepath.Base(filePath)

	// Walk through backup directory
	if err := filepath.Walk(r.config.BackupDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("error accessing path %s: %w", path, err)
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if this is a backup of our file
		if matched, err := filepath.Match(fmt.Sprintf("%s.*.bak", fileName), info.Name()); err != nil {
			return fmt.Errorf("error matching pattern for file %s: %w", info.Name(), err)
		} else if matched {
			backup := model.BackupFile{
				OriginalPath: filePath,
				BackupPath:   path,
				Created:      info.ModTime(),
				Size:         info.Size(),
			}
			backups = append(backups, backup)
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to list backups for %s: %w", filePath, err)
	}

	return backups, nil
}

// RestoreBackup restores a file from backup
func (r *FileBackupRepository) RestoreBackup(backupPath, originalPath string) error {
	// Check if backup exists
	fileInfo, err := r.fs.Stat(backupPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("backup file %s does not exist", backupPath)
	}
	if err != nil {
		return fmt.Errorf("failed to access backup file %s: %w", backupPath, err)
	}

	// Make sure it's not a directory
	if fileInfo.IsDir() {
		return fmt.Errorf("backup path %s is a directory, not a file", backupPath)
	}

	// Read backup file
	data, err := r.fs.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file %s: %w", backupPath, err)
	}

	// Create directory for restored file if needed
	targetDir := filepath.Dir(originalPath)
	if err := r.fs.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s for restored file: %w", targetDir, err)
	}

	// Write restored file
	if err := r.fs.WriteFile(originalPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write restored file %s: %w", originalPath, err)
	}

	return nil
}

// CleanupOldBackups removes backups older than specified date
func (r *FileBackupRepository) CleanupOldBackups(before time.Time) error {
	// Check if backup directory exists
	backupDirInfo, err := r.fs.Stat(r.config.BackupDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Backup path doesn't exist yet - nothing to clean
			return nil
		}
		return fmt.Errorf("failed to access backup directory %s: %w", r.config.BackupDir, err)
	}

	// Make sure it's a directory
	if !backupDirInfo.IsDir() {
		return fmt.Errorf("backup path %s is not a directory", r.config.BackupDir)
	}

	// Since we don't have ReadDir in our interface, we'll use a different approach
	// We'll have the repository implementation check known date directories directly

	// Get current and past dates to check (e.g., past 90 days)
	var datesToCheck []string
	for i := 0; i < 90; i++ {
		date := time.Now().AddDate(0, 0, -i)
		dateStr := date.Format("2006-01-02")
		datesToCheck = append(datesToCheck, dateStr)
	}

	// Check each possible date directory
	for _, dateStr := range datesToCheck {
		dirPath := filepath.Join(r.config.BackupDir, dateStr)

		// Check if directory exists
		dirInfo, err := r.fs.Stat(dirPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Directory doesn't exist, skip
				continue
			}
			// Other error, log and continue
			fmt.Printf("Warning: Error checking directory %s: %v\n", dirPath, err)
			continue
		}

		// Skip if not a directory
		if !dirInfo.IsDir() {
			continue
		}

		// Parse date from directory name
		dirDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			// Should never happen since we're generating these dates
			continue
		}

		// If directory is older than cutoff, remove it
		if dirDate.Before(before) {
			if err := r.fs.RemoveAll(dirPath); err != nil {
				return fmt.Errorf("failed to remove old backup directory %s: %w", dirPath, err)
			}
		}
	}

	return nil
}

// VerifyBackupDirectory ensures the backup directory exists and is writable
func (r *FileBackupRepository) VerifyBackupDirectory() error {
	// Create backup directory if it doesn't exist
	if err := r.fs.MkdirAll(r.config.BackupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory %s: %w", r.config.BackupDir, err)
	}

	// Check if directory is writable by writing a test file
	testFile := filepath.Join(r.config.BackupDir, ".write_test")
	if err := r.fs.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("backup directory %s is not writable: %w", r.config.BackupDir, err)
	}

	// Clean up test file
	if err := r.fs.Remove(testFile); err != nil {
		// Log warning but don't fail the operation since this is just cleanup
		fmt.Printf("Warning: Failed to remove test file %s: %v\n", testFile, err)
	}
	return nil
}

// GetBackupConfig retrieves the current backup configuration
func (r *FileBackupRepository) GetBackupConfig() (*model.BackupConfig, error) {
	// Return a copy to prevent direct modification
	config := *r.config
	return &config, nil
}

// SetBackupConfig updates the backup configuration
func (r *FileBackupRepository) SetBackupConfig(config model.BackupConfig) error {
	// Update the configuration
	r.config.Enabled = config.Enabled
	r.config.BackupDir = config.BackupDir

	// If enabling backups, verify the directory exists and is writable
	if r.config.Enabled {
		return r.VerifyBackupDirectory()
	}

	return nil
}
