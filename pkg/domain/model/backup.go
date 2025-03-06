// pkg/domain/model/backup.go
package model

import "time"

// BackupConfig represents backup configuration settings
type BackupConfig struct {
	Enabled   bool   // Whether backups are enabled
	BackupDir string // Directory to store backups
}

// BackupFile represents information about a backed up file
type BackupFile struct {
	OriginalPath string    // Path of the original file
	BackupPath   string    // Full path to the backup
	Created      time.Time // When the backup was created
	Size         int64     // Size of the backup in bytes
}