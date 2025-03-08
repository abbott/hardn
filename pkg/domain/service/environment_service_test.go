package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MockEnvironmentRepository implements EnvironmentRepository interface for testing
type MockEnvironmentRepository struct {
	// SetupSudoPreservation tracking
	PreservedUsername string
	SetupError        error
	SetupCallCount    int

	// IsSudoPreservationEnabled tracking
	CheckedUsername     string
	PreservationEnabled bool
	CheckError          error
	CheckCallCount      int

	// GetEnvironmentConfig tracking
	ReturnedConfig     *model.EnvironmentConfig
	GetConfigError     error
	GetConfigCallCount int
}

func (m *MockEnvironmentRepository) SetupSudoPreservation(username string) error {
	m.PreservedUsername = username
	m.SetupCallCount++
	return m.SetupError
}

func (m *MockEnvironmentRepository) IsSudoPreservationEnabled(username string) (bool, error) {
	m.CheckedUsername = username
	m.CheckCallCount++
	return m.PreservationEnabled, m.CheckError
}

func (m *MockEnvironmentRepository) GetEnvironmentConfig() (*model.EnvironmentConfig, error) {
	m.GetConfigCallCount++
	return m.ReturnedConfig, m.GetConfigError
}

func TestNewEnvironmentServiceImpl(t *testing.T) {
	repo := &MockEnvironmentRepository{}

	service := NewEnvironmentServiceImpl(repo)

	if service == nil {
		t.Fatal("Expected non-nil service")
	}

	if service.repository != repo {
		t.Error("Repository not properly set")
	}
}

func TestEnvironmentServiceImpl_SetupSudoPreservation(t *testing.T) {
	tests := []struct {
		name              string
		configUsername    string
		getConfigError    error
		setupError        error
		expectError       bool
		expectSetupCalled bool
	}{
		{
			name:              "successful setup",
			configUsername:    "testuser",
			getConfigError:    nil,
			setupError:        nil,
			expectError:       false,
			expectSetupCalled: true,
		},
		{
			name:              "empty username",
			configUsername:    "",
			getConfigError:    nil,
			setupError:        nil,
			expectError:       false,
			expectSetupCalled: false, // Should not call setup if username is empty
		},
		{
			name:              "get config error",
			configUsername:    "testuser",
			getConfigError:    errors.New("mock get config error"),
			setupError:        nil,
			expectError:       true,
			expectSetupCalled: false,
		},
		{
			name:              "setup error",
			configUsername:    "testuser",
			getConfigError:    nil,
			setupError:        errors.New("mock setup error"),
			expectError:       true,
			expectSetupCalled: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockEnvironmentRepository{
				ReturnedConfig: &model.EnvironmentConfig{
					Username: tc.configUsername,
				},
				GetConfigError: tc.getConfigError,
				SetupError:     tc.setupError,
			}

			service := NewEnvironmentServiceImpl(repo)

			// Execute
			err := service.SetupSudoPreservation()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tc.expectSetupCalled {
				if repo.SetupCallCount != 1 {
					t.Errorf("Expected SetupSudoPreservation to be called once, got %d", repo.SetupCallCount)
				}

				if repo.PreservedUsername != tc.configUsername {
					t.Errorf("Wrong username passed. Got %s, expected %s", repo.PreservedUsername, tc.configUsername)
				}
			} else {
				if repo.SetupCallCount > 0 {
					t.Errorf("Expected SetupSudoPreservation not to be called, but was called %d times", repo.SetupCallCount)
				}
			}

			// GetEnvironmentConfig should always be called
			if repo.GetConfigCallCount != 1 {
				t.Errorf("Expected GetEnvironmentConfig to be called once, got %d", repo.GetConfigCallCount)
			}
		})
	}
}

func TestEnvironmentServiceImpl_IsSudoPreservationEnabled(t *testing.T) {
	tests := []struct {
		name                string
		configUsername      string
		getConfigError      error
		preservationEnabled bool
		checkError          error
		expectError         bool
		expectEnabled       bool
		expectCheckCalled   bool
	}{
		{
			name:                "preservation enabled",
			configUsername:      "testuser",
			getConfigError:      nil,
			preservationEnabled: true,
			checkError:          nil,
			expectError:         false,
			expectEnabled:       true,
			expectCheckCalled:   true,
		},
		{
			name:                "preservation disabled",
			configUsername:      "testuser",
			getConfigError:      nil,
			preservationEnabled: false,
			checkError:          nil,
			expectError:         false,
			expectEnabled:       false,
			expectCheckCalled:   true,
		},
		{
			name:                "empty username",
			configUsername:      "",
			getConfigError:      nil,
			preservationEnabled: false,
			checkError:          nil,
			expectError:         false,
			expectEnabled:       false,
			expectCheckCalled:   false, // Should not call check if username is empty
		},
		{
			name:                "get config error",
			configUsername:      "testuser",
			getConfigError:      errors.New("mock get config error"),
			preservationEnabled: false,
			checkError:          nil,
			expectError:         true,
			expectEnabled:       false,
			expectCheckCalled:   false,
		},
		{
			name:                "check error",
			configUsername:      "testuser",
			getConfigError:      nil,
			preservationEnabled: false,
			checkError:          errors.New("mock check error"),
			expectError:         true,
			expectEnabled:       false,
			expectCheckCalled:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockEnvironmentRepository{
				ReturnedConfig: &model.EnvironmentConfig{
					Username: tc.configUsername,
				},
				GetConfigError:      tc.getConfigError,
				PreservationEnabled: tc.preservationEnabled,
				CheckError:          tc.checkError,
			}

			service := NewEnvironmentServiceImpl(repo)

			// Execute
			enabled, err := service.IsSudoPreservationEnabled()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if enabled != tc.expectEnabled {
				t.Errorf("Wrong enabled status. Got %v, expected %v", enabled, tc.expectEnabled)
			}

			if tc.expectCheckCalled {
				if repo.CheckCallCount != 1 {
					t.Errorf("Expected IsSudoPreservationEnabled to be called once, got %d", repo.CheckCallCount)
				}

				if repo.CheckedUsername != tc.configUsername {
					t.Errorf("Wrong username checked. Got %s, expected %s", repo.CheckedUsername, tc.configUsername)
				}
			} else {
				if repo.CheckCallCount > 0 {
					t.Errorf("Expected IsSudoPreservationEnabled not to be called, but was called %d times", repo.CheckCallCount)
				}
			}

			// GetEnvironmentConfig should always be called
			if repo.GetConfigCallCount != 1 {
				t.Errorf("Expected GetEnvironmentConfig to be called once, got %d", repo.GetConfigCallCount)
			}
		})
	}
}

func TestEnvironmentServiceImpl_GetEnvironmentConfig(t *testing.T) {
	tests := []struct {
		name           string
		returnedConfig *model.EnvironmentConfig
		getConfigError error
		expectError    bool
	}{
		{
			name: "successful config retrieval",
			returnedConfig: &model.EnvironmentConfig{
				ConfigPath:   "/path/to/config",
				PreserveSudo: true,
				Username:     "testuser",
			},
			getConfigError: nil,
			expectError:    false,
		},
		{
			name:           "get config error",
			returnedConfig: nil,
			getConfigError: errors.New("mock get config error"),
			expectError:    true,
		},
		{
			name: "empty config",
			returnedConfig: &model.EnvironmentConfig{
				ConfigPath:   "",
				PreserveSudo: false,
				Username:     "",
			},
			getConfigError: nil,
			expectError:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockEnvironmentRepository{
				ReturnedConfig: tc.returnedConfig,
				GetConfigError: tc.getConfigError,
			}

			service := NewEnvironmentServiceImpl(repo)

			// Execute
			config, err := service.GetEnvironmentConfig()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tc.returnedConfig != nil {
				if config == nil {
					t.Fatal("Expected non-nil config but got nil")
				}
				if !reflect.DeepEqual(config, tc.returnedConfig) {
					t.Errorf("Wrong config returned. Got %+v, expected %+v", config, tc.returnedConfig)
				}
			} else if config != nil {
				t.Error("Expected nil config but got non-nil")
			}

			// GetEnvironmentConfig should be called
			if repo.GetConfigCallCount != 1 {
				t.Errorf("Expected GetEnvironmentConfig to be called once, got %d", repo.GetConfigCallCount)
			}
		})
	}
}
