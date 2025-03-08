package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MockDNSRepository implements DNSRepository interface for testing
type MockDNSRepository struct {
	SavedConfig     model.DNSConfig
	SaveError       error
	ReturnedConfig  *model.DNSConfig
	GetConfigError  error
	SaveCallCount   int
	GetConfigCalled bool
}

func (m *MockDNSRepository) SaveDNSConfig(config model.DNSConfig) error {
	m.SavedConfig = config
	m.SaveCallCount++
	return m.SaveError
}

func (m *MockDNSRepository) GetDNSConfig() (*model.DNSConfig, error) {
	m.GetConfigCalled = true
	return m.ReturnedConfig, m.GetConfigError
}

func TestNewDNSServiceImpl(t *testing.T) {
	repo := &MockDNSRepository{}
	osInfo := model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"}

	service := NewDNSServiceImpl(repo, osInfo)

	if service == nil {
		t.Fatal("Expected non-nil service")
	}

	if service.repository != repo {
		t.Error("Repository not properly set")
	}

	if !reflect.DeepEqual(service.osInfo, osInfo) {
		t.Error("OSInfo not properly set")
	}
}

func TestDNSServiceImpl_ConfigureDNS(t *testing.T) {
	tests := []struct {
		name           string
		config         model.DNSConfig
		mockSaveError  error
		expectError    bool
		expectSaveCall bool
	}{
		{
			name: "successful configuration",
			config: model.DNSConfig{
				Nameservers: []string{"1.1.1.1", "1.0.0.1"},
				Domain:      "example.com",
				Search:      []string{"example.com", "test.com"},
			},
			mockSaveError:  nil,
			expectError:    false,
			expectSaveCall: true,
		},
		{
			name: "empty nameservers",
			config: model.DNSConfig{
				Nameservers: []string{},
				Domain:      "example.com",
			},
			mockSaveError:  nil,
			expectError:    false,
			expectSaveCall: true,
		},
		{
			name: "repository error",
			config: model.DNSConfig{
				Nameservers: []string{"8.8.8.8"},
			},
			mockSaveError:  errors.New("mock save error"),
			expectError:    true,
			expectSaveCall: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockDNSRepository{
				SaveError: tc.mockSaveError,
			}
			osInfo := model.OSInfo{Type: "debian", Version: "11"}
			service := NewDNSServiceImpl(repo, osInfo)

			// Execute
			err := service.ConfigureDNS(tc.config)

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tc.expectSaveCall {
				if repo.SaveCallCount != 1 {
					t.Errorf("Expected SaveDNSConfig to be called once, got %d", repo.SaveCallCount)
				}

				if !reflect.DeepEqual(repo.SavedConfig, tc.config) {
					t.Errorf("Wrong config saved. Got %+v, expected %+v", repo.SavedConfig, tc.config)
				}
			} else if repo.SaveCallCount > 0 {
				t.Error("SaveDNSConfig should not have been called")
			}
		})
	}
}

func TestDNSServiceImpl_GetCurrentConfig(t *testing.T) {
	tests := []struct {
		name           string
		mockConfig     *model.DNSConfig
		mockError      error
		expectError    bool
		expectedConfig *model.DNSConfig
	}{
		{
			name: "successful retrieval",
			mockConfig: &model.DNSConfig{
				Nameservers: []string{"1.1.1.1", "1.0.0.1"},
				Domain:      "example.com",
				Search:      []string{"example.com"},
			},
			mockError:   nil,
			expectError: false,
			expectedConfig: &model.DNSConfig{
				Nameservers: []string{"1.1.1.1", "1.0.0.1"},
				Domain:      "example.com",
				Search:      []string{"example.com"},
			},
		},
		{
			name:           "repository error",
			mockConfig:     nil,
			mockError:      errors.New("mock retrieval error"),
			expectError:    true,
			expectedConfig: nil,
		},
		{
			name: "empty nameservers",
			mockConfig: &model.DNSConfig{
				Nameservers: []string{},
				Domain:      "example.com",
			},
			mockError:   nil,
			expectError: false,
			expectedConfig: &model.DNSConfig{
				Nameservers: []string{},
				Domain:      "example.com",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockDNSRepository{
				ReturnedConfig: tc.mockConfig,
				GetConfigError: tc.mockError,
			}
			osInfo := model.OSInfo{Type: "alpine", Version: "3.16"}
			service := NewDNSServiceImpl(repo, osInfo)

			// Execute
			config, err := service.GetCurrentConfig()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !repo.GetConfigCalled {
				t.Error("Expected GetDNSConfig to be called")
			}

			if tc.expectedConfig != nil {
				if config == nil {
					t.Fatal("Expected non-nil config but got nil")
				}
				if !reflect.DeepEqual(config, tc.expectedConfig) {
					t.Errorf("Wrong config returned. Got %+v, expected %+v", config, tc.expectedConfig)
				}
			} else if config != nil {
				t.Error("Expected nil config but got non-nil")
			}
		})
	}
}

func TestDNSServiceImpl_OSTypes(t *testing.T) {
	// Test with different OS types to ensure the service works consistently
	osTypes := []string{"debian", "ubuntu", "alpine", "unknown"}

	for _, osType := range osTypes {
		t.Run(osType+" OS type", func(t *testing.T) {
			// Setup
			repo := &MockDNSRepository{}
			osInfo := model.OSInfo{Type: osType, Version: "1.0"}
			service := NewDNSServiceImpl(repo, osInfo)

			// Test a simple configuration
			config := model.DNSConfig{
				Nameservers: []string{"8.8.8.8"},
			}

			// Execute
			err := service.ConfigureDNS(config)

			// Verify
			if err != nil {
				t.Errorf("Failed to configure DNS on %s: %v", osType, err)
			}

			if !reflect.DeepEqual(repo.SavedConfig, config) {
				t.Errorf("Wrong config saved for %s. Got %+v, expected %+v", osType, repo.SavedConfig, config)
			}
		})
	}
}
