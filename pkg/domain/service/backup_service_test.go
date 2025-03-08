package service

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MockBackupRepository implements BackupRepository interface for testing
type MockBackupRepository struct {
	// BackupFile tracking
	BackupFileCalled bool
	BackupFilePath   string
	BackupFileError  error

	// ListBackups tracking
	ListBackupsCalled bool
	ListBackupsPath   string
	ListBackupsResult []model.BackupFile
	ListBackupsError  error

	// RestoreBackup tracking
	RestoreBackupCalled bool
	BackupPath          string
	OriginalPath        string
	RestoreBackupError  error

	// CleanupOldBackups tracking
	CleanupCalled     bool
	CleanupBeforeTime time.Time
	CleanupError      error

	// VerifyBackupDirectory tracking
	VerifyCalled bool
	VerifyError  error

	// GetBackupConfig tracking
	GetConfigCalled bool
	BackupConfig    *model.BackupConfig
	GetConfigError  error

	// SetBackupConfig tracking
	SetConfigCalled bool
	SetConfigValue  model.BackupConfig
	SetConfigError  error
}

func (m *MockBackupRepository) BackupFile(filePath string) error {
	m.BackupFileCalled = true
	m.BackupFilePath = filePath
	return m.BackupFileError
}

func (m *MockBackupRepository) ListBackups(filePath string) ([]model.BackupFile, error) {
	m.ListBackupsCalled = true
	m.ListBackupsPath = filePath
	return m.ListBackupsResult, m.ListBackupsError
}

func (m *MockBackupRepository) RestoreBackup(backupPath, originalPath string) error {
	m.RestoreBackupCalled = true
	m.BackupPath = backupPath
	m.OriginalPath = originalPath
	return m.RestoreBackupError
}

func (m *MockBackupRepository) CleanupOldBackups(before time.Time) error {
	m.CleanupCalled = true
	m.CleanupBeforeTime = before
	return m.CleanupError
}

func (m *MockBackupRepository) VerifyBackupDirectory() error {
	m.VerifyCalled = true
	return m.VerifyError
}

func (m *MockBackupRepository) GetBackupConfig() (*model.BackupConfig, error) {
	m.GetConfigCalled = true
	return m.BackupConfig, m.GetConfigError
}

func (m *MockBackupRepository) SetBackupConfig(config model.BackupConfig) error {
	m.SetConfigCalled = true
	m.SetConfigValue = config
	return m.SetConfigError
}

func TestNewBackupServiceImpl(t *testing.T) {
	repo := &MockBackupRepository{}

	service := NewBackupServiceImpl(repo)

	if service == nil {
		t.Fatal("Expected non-nil service")
	}

	if service.repository != repo {
		t.Error("Repository not properly set")
	}
}

func TestBackupServiceImpl_BackupFile(t *testing.T) {
	tests := []struct {
		name      string
		filePath  string
		mockError error
		wantErr   bool
	}{
		{
			name:      "successful backup",
			filePath:  "/etc/hosts",
			mockError: nil,
			wantErr:   false,
		},
		{
			name:      "backup error",
			filePath:  "/etc/passwd",
			mockError: errors.New("permission denied"),
			wantErr:   true,
		},
		{
			name:      "empty path",
			filePath:  "",
			mockError: errors.New("empty path"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				BackupFileError: tt.mockError,
			}

			service := NewBackupServiceImpl(repo)

			// Execute
			err := service.BackupFile(tt.filePath)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.BackupFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.BackupFileCalled {
				t.Error("Expected BackupFile to be called")
			}

			if repo.BackupFilePath != tt.filePath {
				t.Errorf("Wrong file path passed to repository. Got %v, want %v",
					repo.BackupFilePath, tt.filePath)
			}
		})
	}
}

func TestBackupServiceImpl_ListBackups(t *testing.T) {
	mockBackups := []model.BackupFile{
		{
			OriginalPath: "/etc/hosts",
			BackupPath:   "/backup/2023-01-01/hosts.123456.bak",
			Created:      time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
			Size:         1024,
		},
	}

	tests := []struct {
		name        string
		filePath    string
		mockBackups []model.BackupFile
		mockError   error
		wantErr     bool
		wantBackups []model.BackupFile
	}{
		{
			name:        "successful list",
			filePath:    "/etc/hosts",
			mockBackups: mockBackups,
			mockError:   nil,
			wantErr:     false,
			wantBackups: mockBackups,
		},
		{
			name:        "list error",
			filePath:    "/etc/nonexistent",
			mockBackups: nil,
			mockError:   errors.New("file not found"),
			wantErr:     true,
			wantBackups: nil,
		},
		{
			name:        "empty list",
			filePath:    "/etc/fstab",
			mockBackups: []model.BackupFile{},
			mockError:   nil,
			wantErr:     false,
			wantBackups: []model.BackupFile{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				ListBackupsResult: tt.mockBackups,
				ListBackupsError:  tt.mockError,
			}

			service := NewBackupServiceImpl(repo)

			// Execute
			backups, err := service.ListBackups(tt.filePath)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.ListBackups() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.ListBackupsCalled {
				t.Error("Expected ListBackups to be called")
			}

			if repo.ListBackupsPath != tt.filePath {
				t.Errorf("Wrong file path passed to repository. Got %v, want %v",
					repo.ListBackupsPath, tt.filePath)
			}

			if !reflect.DeepEqual(backups, tt.wantBackups) {
				t.Errorf("BackupServiceImpl.ListBackups() = %v, want %v", backups, tt.wantBackups)
			}
		})
	}
}

func TestBackupServiceImpl_RestoreBackup(t *testing.T) {
	tests := []struct {
		name         string
		backupPath   string
		originalPath string
		mockError    error
		wantErr      bool
	}{
		{
			name:         "successful restore",
			backupPath:   "/backup/2023-01-01/hosts.123456.bak",
			originalPath: "/etc/hosts",
			mockError:    nil,
			wantErr:      false,
		},
		{
			name:         "restore error",
			backupPath:   "/backup/2023-01-01/hosts.123456.bak",
			originalPath: "/etc/hosts",
			mockError:    errors.New("permission denied"),
			wantErr:      true,
		},
		{
			name:         "empty backup path",
			backupPath:   "",
			originalPath: "/etc/hosts",
			mockError:    errors.New("empty backup path"),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				RestoreBackupError: tt.mockError,
			}

			service := NewBackupServiceImpl(repo)

			// Execute
			err := service.RestoreBackup(tt.backupPath, tt.originalPath)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.RestoreBackup() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.RestoreBackupCalled {
				t.Error("Expected RestoreBackup to be called")
			}

			if repo.BackupPath != tt.backupPath {
				t.Errorf("Wrong backup path passed to repository. Got %v, want %v",
					repo.BackupPath, tt.backupPath)
			}

			if repo.OriginalPath != tt.originalPath {
				t.Errorf("Wrong original path passed to repository. Got %v, want %v",
					repo.OriginalPath, tt.originalPath)
			}
		})
	}
}

func TestBackupServiceImpl_CleanupOldBackups(t *testing.T) {
	tests := []struct {
		name         string
		daysToKeep   int
		mockError    error
		wantErr      bool
		expectedDays int // roughly how many days before now the cutoff should be
	}{
		{
			name:         "successful cleanup",
			daysToKeep:   30,
			mockError:    nil,
			wantErr:      false,
			expectedDays: 30,
		},
		{
			name:         "cleanup error",
			daysToKeep:   7,
			mockError:    errors.New("permission denied"),
			wantErr:      true,
			expectedDays: 7,
		},
		{
			name:         "zero days",
			daysToKeep:   0,
			mockError:    nil,
			wantErr:      false,
			expectedDays: 0,
		},
		{
			name:         "negative days",
			daysToKeep:   -1, // should be treated as "keep everything"
			mockError:    nil,
			wantErr:      false,
			expectedDays: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				CleanupError: tt.mockError,
			}

			service := NewBackupServiceImpl(repo)

			// Get current time for comparison
			now := time.Now()

			// Execute
			err := service.CleanupOldBackups(tt.daysToKeep)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.CleanupOldBackups() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.CleanupCalled {
				t.Error("Expected CleanupOldBackups to be called")
			}

			// Verify the cutoff time is within reasonable bounds (allowing for test execution time)
			if tt.expectedDays >= 0 {
				expectedTime := now.AddDate(0, 0, -tt.expectedDays)
				timeDiff := repo.CleanupBeforeTime.Sub(expectedTime)
				if timeDiff < -2*time.Second || timeDiff > 2*time.Second {
					t.Errorf("Wrong cutoff time. Got %v, expected close to %v (diff: %v)",
						repo.CleanupBeforeTime, expectedTime, timeDiff)
				}
			}
		})
	}
}

func TestBackupServiceImpl_VerifyBackupDirectory(t *testing.T) {
	tests := []struct {
		name      string
		mockError error
		wantErr   bool
	}{
		{
			name:      "successful verification",
			mockError: nil,
			wantErr:   false,
		},
		{
			name:      "verification error",
			mockError: errors.New("directory not writable"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				VerifyError: tt.mockError,
			}

			service := NewBackupServiceImpl(repo)

			// Execute
			err := service.VerifyBackupDirectory()

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.VerifyBackupDirectory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.VerifyCalled {
				t.Error("Expected VerifyBackupDirectory to be called")
			}
		})
	}
}

func TestBackupServiceImpl_GetBackupConfig(t *testing.T) {
	mockConfig := &model.BackupConfig{
		Enabled:   true,
		BackupDir: "/var/backups/hardn",
	}

	tests := []struct {
		name       string
		mockConfig *model.BackupConfig
		mockError  error
		wantErr    bool
		wantConfig *model.BackupConfig
	}{
		{
			name:       "successful get config",
			mockConfig: mockConfig,
			mockError:  nil,
			wantErr:    false,
			wantConfig: mockConfig,
		},
		{
			name:       "get config error",
			mockConfig: nil,
			mockError:  errors.New("configuration error"),
			wantErr:    true,
			wantConfig: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				BackupConfig:   tt.mockConfig,
				GetConfigError: tt.mockError,
			}

			service := NewBackupServiceImpl(repo)

			// Execute
			config, err := service.GetBackupConfig()

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.GetBackupConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.GetConfigCalled {
				t.Error("Expected GetBackupConfig to be called")
			}

			if !reflect.DeepEqual(config, tt.wantConfig) {
				t.Errorf("BackupServiceImpl.GetBackupConfig() = %v, want %v", config, tt.wantConfig)
			}
		})
	}
}

func TestBackupServiceImpl_EnableBackups(t *testing.T) {
	mockConfig := &model.BackupConfig{
		Enabled:   false,
		BackupDir: "/var/backups/hardn",
	}

	tests := []struct {
		name            string
		enable          bool
		mockConfig      *model.BackupConfig
		getConfigError  error
		setConfigError  error
		wantErr         bool
		expectedEnabled bool
	}{
		{
			name:            "enable backups",
			enable:          true,
			mockConfig:      mockConfig,
			getConfigError:  nil,
			setConfigError:  nil,
			wantErr:         false,
			expectedEnabled: true,
		},
		{
			name:            "disable backups",
			enable:          false,
			mockConfig:      &model.BackupConfig{Enabled: true, BackupDir: "/var/backups/hardn"},
			getConfigError:  nil,
			setConfigError:  nil,
			wantErr:         false,
			expectedEnabled: false,
		},
		{
			name:           "get config error",
			enable:         true,
			mockConfig:     nil,
			getConfigError: errors.New("failed to get config"),
			wantErr:        true,
		},
		{
			name:           "set config error",
			enable:         true,
			mockConfig:     mockConfig,
			getConfigError: nil,
			setConfigError: errors.New("failed to set config"),
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				BackupConfig:   tt.mockConfig,
				GetConfigError: tt.getConfigError,
				SetConfigError: tt.setConfigError,
			}

			service := NewBackupServiceImpl(repo)

			// Execute
			err := service.EnableBackups(tt.enable)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.EnableBackups() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.GetConfigCalled {
				t.Error("Expected GetBackupConfig to be called")
			}

			// If no error getting config, check that SetBackupConfig was called
			if tt.getConfigError == nil && !tt.wantErr {
				if !repo.SetConfigCalled {
					t.Error("Expected SetBackupConfig to be called")
				}

				// Check that the enabled state was properly updated
				if repo.SetConfigValue.Enabled != tt.expectedEnabled {
					t.Errorf("Wrong enabled value. Got %v, want %v",
						repo.SetConfigValue.Enabled, tt.expectedEnabled)
				}

				// Verify backup directory unchanged
				if repo.SetConfigValue.BackupDir != tt.mockConfig.BackupDir {
					t.Errorf("Backup directory changed unexpectedly. Got %v, want %v",
						repo.SetConfigValue.BackupDir, tt.mockConfig.BackupDir)
				}
			}
		})
	}
}

func TestBackupServiceImpl_SetBackupDirectory(t *testing.T) {
	mockConfig := &model.BackupConfig{
		Enabled:   true,
		BackupDir: "/var/backups/hardn",
	}

	tests := []struct {
		name           string
		directory      string
		mockConfig     *model.BackupConfig
		getConfigError error
		setConfigError error
		wantErr        bool
		expectedDir    string
	}{
		{
			name:           "set new directory",
			directory:      "/new/backup/path",
			mockConfig:     mockConfig,
			getConfigError: nil,
			setConfigError: nil,
			wantErr:        false,
			expectedDir:    "/new/backup/path",
		},
		{
			name:           "set empty directory",
			directory:      "",
			mockConfig:     mockConfig,
			getConfigError: nil,
			setConfigError: nil,
			wantErr:        false,
			expectedDir:    "",
		},
		{
			name:           "get config error",
			directory:      "/new/backup/path",
			mockConfig:     nil,
			getConfigError: errors.New("failed to get config"),
			wantErr:        true,
		},
		{
			name:           "set config error",
			directory:      "/new/backup/path",
			mockConfig:     mockConfig,
			getConfigError: nil,
			setConfigError: errors.New("failed to set config"),
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			repo := &MockBackupRepository{
				BackupConfig:   tt.mockConfig,
				GetConfigError: tt.getConfigError,
				SetConfigError: tt.setConfigError,
			}

			service := NewBackupServiceImpl(repo)

			// Execute
			err := service.SetBackupDirectory(tt.directory)

			// Verify
			if (err != nil) != tt.wantErr {
				t.Errorf("BackupServiceImpl.SetBackupDirectory() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !repo.GetConfigCalled {
				t.Error("Expected GetBackupConfig to be called")
			}

			// If no error getting config, check that SetBackupConfig was called
			if tt.getConfigError == nil && !tt.wantErr {
				if !repo.SetConfigCalled {
					t.Error("Expected SetBackupConfig to be called")
				}

				// Check that the directory was properly updated
				if repo.SetConfigValue.BackupDir != tt.expectedDir {
					t.Errorf("Wrong directory. Got %v, want %v",
						repo.SetConfigValue.BackupDir, tt.expectedDir)
				}

				// Verify enabled state unchanged
				if repo.SetConfigValue.Enabled != tt.mockConfig.Enabled {
					t.Errorf("Enabled state changed unexpectedly. Got %v, want %v",
						repo.SetConfigValue.Enabled, tt.mockConfig.Enabled)
				}
			}
		})
	}
}
