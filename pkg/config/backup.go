package config

import "github.com/abbott/hardn/pkg/host/backup"

// BackupSettings returns the backup store the backupPath and enableBackups
// keys describe (refactor plan §3.16): each run's backups go to
// <backupPath>/<run-id>/, and with enableBackups false the backups a
// rollback needs are still taken.
func (c *Config) BackupSettings() backup.Settings {
	return backup.Settings{Path: c.BackupPath, Enabled: c.EnableBackups}
}
