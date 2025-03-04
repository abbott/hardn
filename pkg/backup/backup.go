package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
)

// BackupFile backs up a file with a timestamp
func BackupFile(filePath string, cfg *config.Config) error {
	if !cfg.EnableBackups {
		logging.LogInfo("Backups disabled. Skipping backup of %s", filePath)
		return nil
	}

	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Backup %s to %s", filePath, cfg.BackupPath)
		return nil
	}

	// Create backup directory with date
	backupDir := filepath.Join(cfg.BackupPath, time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory %s: %w", backupDir, err)
	}

	// Get filename without path
	fileName := filepath.Base(filePath)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logging.LogInfo("File %s does not exist, no backup needed", filePath)
		return nil
	}

	// Create backup with timestamp
	backupFile := filepath.Join(backupDir, fmt.Sprintf("%s.%s.bak", fileName, time.Now().Format("150405")))

	// Read original file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s for backup: %w", filePath, err)
	}

	// Write backup file
	if err := os.WriteFile(backupFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write backup file %s: %w", backupFile, err)
	}

	logging.LogInfo("Backed up %s to %s", filePath, backupFile)
	return nil
}

// ListBackups returns a list of all backups for a specific file
func ListBackups(filePath string, cfg *config.Config) ([]string, error) {
	// Get filename without path
	fileName := filepath.Base(filePath)
	var backups []string

	// Walk through backup directories
	err := filepath.Walk(cfg.BackupPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if this is a backup of our file
		if matched, _ := filepath.Match(fmt.Sprintf("%s.*.bak", fileName), info.Name()); matched {
			backups = append(backups, path)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	return backups, nil
}

// RestoreBackup restores a file from backup
func RestoreBackup(backupPath, originalPath string, cfg *config.Config) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Restore backup %s to %s", backupPath, originalPath)
		return nil
	}

	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file %s does not exist", backupPath)
	}

	// Read backup file
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file %s: %w", backupPath, err)
	}

	// Make sure target directory exists
	targetDir := filepath.Dir(originalPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for restored file: %w", err)
	}

	// Write restored file
	if err := os.WriteFile(originalPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write restored file %s: %w", originalPath, err)
	}

	logging.LogSuccess("Restored %s from backup %s", originalPath, backupPath)
	return nil
}

// CleanupOldBackups removes backups older than specified days
func CleanupOldBackups(cfg *config.Config, daysToKeep int) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Clean up backups older than %d days in %s", daysToKeep, cfg.BackupPath)
		return nil
	}

	// Calculate cutoff time
	cutoff := time.Now().AddDate(0, 0, -daysToKeep)

	// Get all date-based directories in backup path
	entries, err := os.ReadDir(cfg.BackupPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Backup path doesn't exist yet - nothing to clean
			return nil
		}
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Check each directory
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a date format
		dirDate, err := time.Parse("2006-01-02", entry.Name())
		if err != nil {
			// Not a date directory, skip
			continue
		}

		// If directory is older than cutoff, remove it
		if dirDate.Before(cutoff) {
			dirPath := filepath.Join(cfg.BackupPath, entry.Name())
			if err := os.RemoveAll(dirPath); err != nil {
				logging.LogError("Failed to remove old backup directory %s: %v", dirPath, err)
			} else {
				logging.LogInfo("Removed old backup directory %s", dirPath)
			}
		}
	}

	logging.LogSuccess("Backup cleanup completed")
	return nil
}

// VerifyBackupDirectory ensures the backup directory exists and is writable
func VerifyBackupDirectory(cfg *config.Config) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Verify backup directory %s exists and is writable", cfg.BackupPath)
		return nil
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(cfg.BackupPath, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Check if directory is writable by writing a test file
	testFile := filepath.Join(cfg.BackupPath, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		return fmt.Errorf("backup directory is not writable: %w", err)
	}

	// Clean up test file
	os.Remove(testFile)

	logging.LogInfo("Backup directory %s verified", cfg.BackupPath)
	return nil
}
