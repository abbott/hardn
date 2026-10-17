package config

import (
	"testing"

	"github.com/abbott/hardn/pkg/host/backup"
)

// The keys keep their meaning and default: the default backupPath is the
// backup package's DefaultPath, with backups enabled.
func TestBackupSettings(t *testing.T) {
	want := backup.Settings{Path: backup.DefaultPath, Enabled: true}
	if got := DefaultConfig().BackupSettings(); got != want {
		t.Errorf("DefaultConfig().BackupSettings() = %+v, want %+v", got, want)
	}
	c := &Config{BackupPath: "/srv/backups/hardn", EnableBackups: false}
	if got := c.BackupSettings(); got != (backup.Settings{Path: "/srv/backups/hardn"}) {
		t.Errorf("BackupSettings() = %+v", got)
	}
}
