package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MockFirewallRepository implements FirewallRepository interface for testing
type MockFirewallRepository struct {
	// Status information
	Installed       bool
	Enabled         bool
	Configured      bool
	Rules           []string
	StatusError     error
	StatusCallCount int

	// Configuration information
	SavedConfig         model.FirewallConfig
	SaveConfigError     error
	SaveConfigCallCount int

	ReturnedConfig     *model.FirewallConfig
	GetConfigError     error
	GetConfigCallCount int

	// Rule management
	AddedRule        model.FirewallRule
	AddRuleError     error
	AddRuleCallCount int

	RemovedRule         model.FirewallRule
	RemoveRuleError     error
	RemoveRuleCallCount int

	// Profile management
	AddedProfile        model.FirewallProfile
	AddProfileError     error
	AddProfileCallCount int

	// Firewall state
	EnableError     error
	EnableCallCount int

	DisableError     error
	DisableCallCount int
}

func (m *MockFirewallRepository) GetFirewallStatus() (bool, bool, bool, []string, error) {
	m.StatusCallCount++
	return m.Installed, m.Enabled, m.Configured, m.Rules, m.StatusError
}

func (m *MockFirewallRepository) SaveFirewallConfig(config model.FirewallConfig) error {
	m.SavedConfig = config
	m.SaveConfigCallCount++
	return m.SaveConfigError
}

func (m *MockFirewallRepository) GetFirewallConfig() (*model.FirewallConfig, error) {
	m.GetConfigCallCount++
	return m.ReturnedConfig, m.GetConfigError
}

func (m *MockFirewallRepository) AddRule(rule model.FirewallRule) error {
	m.AddedRule = rule
	m.AddRuleCallCount++
	return m.AddRuleError
}

func (m *MockFirewallRepository) RemoveRule(rule model.FirewallRule) error {
	m.RemovedRule = rule
	m.RemoveRuleCallCount++
	return m.RemoveRuleError
}

func (m *MockFirewallRepository) AddProfile(profile model.FirewallProfile) error {
	m.AddedProfile = profile
	m.AddProfileCallCount++
	return m.AddProfileError
}

func (m *MockFirewallRepository) EnableFirewall() error {
	m.EnableCallCount++
	return m.EnableError
}

func (m *MockFirewallRepository) DisableFirewall() error {
	m.DisableCallCount++
	return m.DisableError
}

func TestNewFirewallServiceImpl(t *testing.T) {
	repo := &MockFirewallRepository{}
	osInfo := model.OSInfo{Type: "debian", Version: "11", Codename: "bullseye"}

	service := NewFirewallServiceImpl(repo, osInfo)

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

func TestFirewallServiceImpl_GetFirewallStatus(t *testing.T) {
	tests := []struct {
		name        string
		installed   bool
		enabled     bool
		configured  bool
		rules       []string
		statusError error
		expectError bool
	}{
		{
			name:        "successful status retrieval",
			installed:   true,
			enabled:     true,
			configured:  true,
			rules:       []string{"22/tcp ALLOW", "80/tcp DENY"},
			statusError: nil,
			expectError: false,
		},
		{
			name:        "firewall not installed",
			installed:   false,
			enabled:     false,
			configured:  false,
			rules:       []string{},
			statusError: nil,
			expectError: false,
		},
		{
			name:        "repository error",
			installed:   false,
			enabled:     false,
			configured:  false,
			rules:       []string{},
			statusError: errors.New("mock status error"),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				Installed:   tc.installed,
				Enabled:     tc.enabled,
				Configured:  tc.configured,
				Rules:       tc.rules,
				StatusError: tc.statusError,
			}

			osInfo := model.OSInfo{Type: "debian", Version: "11"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			installed, enabled, configured, rules, err := service.GetFirewallStatus()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.StatusCallCount != 1 {
				t.Errorf("Expected GetFirewallStatus to be called once, got %d", repo.StatusCallCount)
			}

			if installed != tc.installed {
				t.Errorf("Incorrect installed status. Got %v, expected %v", installed, tc.installed)
			}

			if enabled != tc.enabled {
				t.Errorf("Incorrect enabled status. Got %v, expected %v", enabled, tc.enabled)
			}

			if configured != tc.configured {
				t.Errorf("Incorrect configured status. Got %v, expected %v", configured, tc.configured)
			}

			if !reflect.DeepEqual(rules, tc.rules) {
				t.Errorf("Incorrect rules. Got %v, expected %v", rules, tc.rules)
			}
		})
	}
}

func TestFirewallServiceImpl_ConfigureFirewall(t *testing.T) {
	tests := []struct {
		name            string
		config          model.FirewallConfig
		saveConfigError error
		expectError     bool
	}{
		{
			name: "successful configuration",
			config: model.FirewallConfig{
				Enabled:         true,
				DefaultIncoming: "deny",
				DefaultOutgoing: "allow",
				Rules: []model.FirewallRule{
					{Action: "allow", Protocol: "tcp", Port: 22},
				},
				ApplicationProfiles: []model.FirewallProfile{
					{Name: "OpenSSH", Title: "Secure Shell", Ports: []string{"22/tcp"}},
				},
			},
			saveConfigError: nil,
			expectError:     false,
		},
		{
			name: "empty rules",
			config: model.FirewallConfig{
				Enabled:             true,
				DefaultIncoming:     "deny",
				DefaultOutgoing:     "allow",
				Rules:               []model.FirewallRule{},
				ApplicationProfiles: []model.FirewallProfile{},
			},
			saveConfigError: nil,
			expectError:     false,
		},
		{
			name: "repository error",
			config: model.FirewallConfig{
				Enabled:         true,
				DefaultIncoming: "deny",
				DefaultOutgoing: "allow",
			},
			saveConfigError: errors.New("mock save config error"),
			expectError:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				SaveConfigError: tc.saveConfigError,
			}

			osInfo := model.OSInfo{Type: "debian", Version: "11"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			err := service.ConfigureFirewall(tc.config)

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.SaveConfigCallCount != 1 {
				t.Errorf("Expected SaveFirewallConfig to be called once, got %d", repo.SaveConfigCallCount)
			}

			if !reflect.DeepEqual(repo.SavedConfig, tc.config) {
				t.Errorf("Wrong config saved. Got %+v, expected %+v", repo.SavedConfig, tc.config)
			}
		})
	}
}

func TestFirewallServiceImpl_AddRule(t *testing.T) {
	tests := []struct {
		name         string
		rule         model.FirewallRule
		addRuleError error
		expectError  bool
	}{
		{
			name: "successful rule addition",
			rule: model.FirewallRule{
				Action:      "allow",
				Protocol:    "tcp",
				Port:        22,
				SourceIP:    "",
				Description: "SSH",
			},
			addRuleError: nil,
			expectError:  false,
		},
		{
			name: "rule with source IP",
			rule: model.FirewallRule{
				Action:      "allow",
				Protocol:    "tcp",
				Port:        80,
				SourceIP:    "192.168.1.0/24",
				Description: "Web from LAN",
			},
			addRuleError: nil,
			expectError:  false,
		},
		{
			name: "repository error",
			rule: model.FirewallRule{
				Action:   "allow",
				Protocol: "tcp",
				Port:     443,
			},
			addRuleError: errors.New("mock add rule error"),
			expectError:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				AddRuleError: tc.addRuleError,
			}

			osInfo := model.OSInfo{Type: "ubuntu", Version: "20.04"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			err := service.AddRule(tc.rule)

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.AddRuleCallCount != 1 {
				t.Errorf("Expected AddRule to be called once, got %d", repo.AddRuleCallCount)
			}

			if !reflect.DeepEqual(repo.AddedRule, tc.rule) {
				t.Errorf("Wrong rule added. Got %+v, expected %+v", repo.AddedRule, tc.rule)
			}
		})
	}
}

func TestFirewallServiceImpl_RemoveRule(t *testing.T) {
	tests := []struct {
		name            string
		rule            model.FirewallRule
		removeRuleError error
		expectError     bool
	}{
		{
			name: "successful rule removal",
			rule: model.FirewallRule{
				Action:      "allow",
				Protocol:    "tcp",
				Port:        22,
				Description: "SSH",
			},
			removeRuleError: nil,
			expectError:     false,
		},
		{
			name: "repository error",
			rule: model.FirewallRule{
				Action:   "allow",
				Protocol: "tcp",
				Port:     80,
			},
			removeRuleError: errors.New("mock remove rule error"),
			expectError:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				RemoveRuleError: tc.removeRuleError,
			}

			osInfo := model.OSInfo{Type: "ubuntu", Version: "20.04"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			err := service.RemoveRule(tc.rule)

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.RemoveRuleCallCount != 1 {
				t.Errorf("Expected RemoveRule to be called once, got %d", repo.RemoveRuleCallCount)
			}

			if !reflect.DeepEqual(repo.RemovedRule, tc.rule) {
				t.Errorf("Wrong rule removed. Got %+v, expected %+v", repo.RemovedRule, tc.rule)
			}
		})
	}
}

func TestFirewallServiceImpl_AddProfile(t *testing.T) {
	tests := []struct {
		name            string
		profile         model.FirewallProfile
		addProfileError error
		expectError     bool
	}{
		{
			name: "successful profile addition",
			profile: model.FirewallProfile{
				Name:        "OpenSSH",
				Title:       "Secure Shell",
				Description: "SSH server",
				Ports:       []string{"22/tcp"},
			},
			addProfileError: nil,
			expectError:     false,
		},
		{
			name: "multiple ports",
			profile: model.FirewallProfile{
				Name:        "NGINX",
				Title:       "Web Server",
				Description: "NGINX web server",
				Ports:       []string{"80/tcp", "443/tcp"},
			},
			addProfileError: nil,
			expectError:     false,
		},
		{
			name: "repository error",
			profile: model.FirewallProfile{
				Name:  "Invalid",
				Ports: []string{},
			},
			addProfileError: errors.New("mock add profile error"),
			expectError:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				AddProfileError: tc.addProfileError,
			}

			osInfo := model.OSInfo{Type: "debian", Version: "11"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			err := service.AddProfile(tc.profile)

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.AddProfileCallCount != 1 {
				t.Errorf("Expected AddProfile to be called once, got %d", repo.AddProfileCallCount)
			}

			if !reflect.DeepEqual(repo.AddedProfile, tc.profile) {
				t.Errorf("Wrong profile added. Got %+v, expected %+v", repo.AddedProfile, tc.profile)
			}
		})
	}
}

func TestFirewallServiceImpl_GetCurrentConfig(t *testing.T) {
	tests := []struct {
		name           string
		mockConfig     *model.FirewallConfig
		mockError      error
		expectError    bool
		expectedConfig *model.FirewallConfig
	}{
		{
			name: "successful retrieval",
			mockConfig: &model.FirewallConfig{
				Enabled:         true,
				DefaultIncoming: "deny",
				DefaultOutgoing: "allow",
				Rules: []model.FirewallRule{
					{Action: "allow", Protocol: "tcp", Port: 22},
				},
			},
			mockError:   nil,
			expectError: false,
			expectedConfig: &model.FirewallConfig{
				Enabled:         true,
				DefaultIncoming: "deny",
				DefaultOutgoing: "allow",
				Rules: []model.FirewallRule{
					{Action: "allow", Protocol: "tcp", Port: 22},
				},
			},
		},
		{
			name:           "repository error",
			mockConfig:     nil,
			mockError:      errors.New("mock get config error"),
			expectError:    true,
			expectedConfig: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				ReturnedConfig: tc.mockConfig,
				GetConfigError: tc.mockError,
			}

			osInfo := model.OSInfo{Type: "alpine", Version: "3.16"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			config, err := service.GetCurrentConfig()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.GetConfigCallCount != 1 {
				t.Errorf("Expected GetFirewallConfig to be called once, got %d", repo.GetConfigCallCount)
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

func TestFirewallServiceImpl_EnableFirewall(t *testing.T) {
	tests := []struct {
		name        string
		enableError error
		expectError bool
	}{
		{
			name:        "successful enable",
			enableError: nil,
			expectError: false,
		},
		{
			name:        "repository error",
			enableError: errors.New("mock enable error"),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				EnableError: tc.enableError,
			}

			osInfo := model.OSInfo{Type: "debian", Version: "11"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			err := service.EnableFirewall()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.EnableCallCount != 1 {
				t.Errorf("Expected EnableFirewall to be called once, got %d", repo.EnableCallCount)
			}
		})
	}
}

func TestFirewallServiceImpl_DisableFirewall(t *testing.T) {
	tests := []struct {
		name         string
		disableError error
		expectError  bool
	}{
		{
			name:         "successful disable",
			disableError: nil,
			expectError:  false,
		},
		{
			name:         "repository error",
			disableError: errors.New("mock disable error"),
			expectError:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{
				DisableError: tc.disableError,
			}

			osInfo := model.OSInfo{Type: "debian", Version: "11"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Execute
			err := service.DisableFirewall()

			// Verify
			if tc.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if repo.DisableCallCount != 1 {
				t.Errorf("Expected DisableFirewall to be called once, got %d", repo.DisableCallCount)
			}
		})
	}
}

func TestFirewallServiceImpl_OSTypes(t *testing.T) {
	// Test with different OS types to ensure the service works consistently
	osTypes := []string{"debian", "ubuntu", "alpine", "proxmox", "unknown"}

	for _, osType := range osTypes {
		t.Run(osType+" OS type", func(t *testing.T) {
			// Setup
			repo := &MockFirewallRepository{}
			osInfo := model.OSInfo{Type: osType, Version: "1.0"}
			service := NewFirewallServiceImpl(repo, osInfo)

			// Test a simple rule addition
			rule := model.FirewallRule{
				Action:   "allow",
				Protocol: "tcp",
				Port:     22,
			}

			// Execute
			err := service.AddRule(rule)

			// Verify
			if err != nil {
				t.Errorf("Failed to add rule on %s: %v", osType, err)
			}

			if !reflect.DeepEqual(repo.AddedRule, rule) {
				t.Errorf("Wrong rule added for %s. Got %+v, expected %+v", osType, repo.AddedRule, rule)
			}
		})
	}
}
