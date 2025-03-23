package service

import (
	"fmt"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockSSHRepository is a mock implementation of the SSHRepository interface for testing
type MockSSHRepository struct {
	mock.Mock
}

func (m *MockSSHRepository) SaveSSHConfig(config model.SSHConfig) error {
	args := m.Called(config)
	return args.Error(0)
}

func (m *MockSSHRepository) GetSSHConfig() (*model.SSHConfig, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	// Safely perform type assertion
	config, ok := args.Get(0).(*model.SSHConfig)
	if !ok {
		return nil, fmt.Errorf("invalid type assertion, expected *model.SSHConfig")
	}

	return config, args.Error(1)
}

func (m *MockSSHRepository) DisableRootSSH() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockSSHRepository) AddAuthorizedKey(username string, publicKey string) error {
	args := m.Called(username, publicKey)
	return args.Error(0)
}

func TestSSHServiceImpl_ConfigureSSH(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Test data
	config := model.SSHConfig{
		Port:            2222,
		ListenAddresses: []string{"0.0.0.0"},
		PermitRootLogin: false,
		AllowedUsers:    []string{"user1", "user2"},
		KeyPaths:        []string{".ssh/authorized_keys"},
		AuthMethods:     []string{"publickey"},
	}

	// Setup expectations
	mockRepo.On("SaveSSHConfig", config).Return(nil)

	// Execute
	err := service.ConfigureSSH(config)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_ConfigureSSH_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Test data
	config := model.SSHConfig{
		Port:            2222,
		ListenAddresses: []string{"0.0.0.0"},
		PermitRootLogin: false,
		AllowedUsers:    []string{"user1", "user2"},
		KeyPaths:        []string{".ssh/authorized_keys"},
		AuthMethods:     []string{"publickey"},
	}

	// Setup expectations with an error
	expectedErr := fmt.Errorf("failed to save config")
	mockRepo.On("SaveSSHConfig", config).Return(expectedErr)

	// Execute
	err := service.ConfigureSSH(config)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_DisableRootSSH(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Setup expectations
	mockRepo.On("DisableRootSSH").Return(nil)

	// Execute
	err := service.DisableRootSSH()

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_DisableRootSSH_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Setup expectations
	expectedErr := fmt.Errorf("failed to disable root access")
	mockRepo.On("DisableRootSSH").Return(expectedErr)

	// Execute
	err := service.DisableRootSSH()

	// Assert
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_AddAuthorizedKey(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Test data
	username := "testuser"
	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"

	// Setup expectations
	mockRepo.On("AddAuthorizedKey", username, publicKey).Return(nil)

	// Execute
	err := service.AddAuthorizedKey(username, publicKey)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_AddAuthorizedKey_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Test data
	username := "testuser"
	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"

	// Setup expectations
	expectedErr := fmt.Errorf("failed to add authorized key")
	mockRepo.On("AddAuthorizedKey", username, publicKey).Return(expectedErr)

	// Execute
	err := service.AddAuthorizedKey(username, publicKey)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_GetCurrentConfig(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Test data
	expectedConfig := &model.SSHConfig{
		Port:            2222,
		ListenAddresses: []string{"0.0.0.0"},
		PermitRootLogin: false,
		AllowedUsers:    []string{"user1", "user2"},
		KeyPaths:        []string{".ssh/authorized_keys"},
		AuthMethods:     []string{"publickey"},
	}

	// Setup expectations
	mockRepo.On("GetSSHConfig").Return(expectedConfig, nil)

	// Execute
	config, err := service.GetCurrentConfig()

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedConfig, config)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_GetCurrentConfig_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockSSHRepository)
	osInfo := model.OSInfo{
		Type:     "debian",
		Version:  "11",
		Codename: "bullseye",
	}
	service := NewSSHServiceImpl(mockRepo, osInfo)

	// Setup expectations
	expectedErr := fmt.Errorf("failed to get config")
	mockRepo.On("GetSSHConfig").Return(nil, expectedErr)

	// Execute
	config, err := service.GetCurrentConfig()

	// Assert
	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestSSHServiceImpl_ConfigureSSHWithDifferentOSTypes(t *testing.T) {
	// Test cases for different OS types
	testCases := []struct {
		name   string
		osInfo model.OSInfo
		config model.SSHConfig
	}{
		{
			name: "Debian",
			osInfo: model.OSInfo{
				Type:     "debian",
				Version:  "11",
				Codename: "bullseye",
			},
			config: model.SSHConfig{
				Port:            2222,
				ListenAddresses: []string{"0.0.0.0"},
				PermitRootLogin: false,
				AllowedUsers:    []string{"debianuser"},
				KeyPaths:        []string{".ssh/authorized_keys"},
				AuthMethods:     []string{"publickey"},
			},
		},
		{
			name: "Alpine",
			osInfo: model.OSInfo{
				Type:     "alpine",
				Version:  "3.15",
				Codename: "3.15",
			},
			config: model.SSHConfig{
				Port:            2222,
				ListenAddresses: []string{"0.0.0.0"},
				PermitRootLogin: false,
				AllowedUsers:    []string{"alpineuser"},
				KeyPaths:        []string{".ssh/authorized_keys"},
				AuthMethods:     []string{"publickey"},
			},
		},
		{
			name: "Ubuntu",
			osInfo: model.OSInfo{
				Type:     "ubuntu",
				Version:  "20.04",
				Codename: "focal",
			},
			config: model.SSHConfig{
				Port:            2222,
				ListenAddresses: []string{"0.0.0.0"},
				PermitRootLogin: false,
				AllowedUsers:    []string{"ubuntuuser"},
				KeyPaths:        []string{".ssh/authorized_keys"},
				AuthMethods:     []string{"publickey"},
			},
		},
		{
			name: "Proxmox",
			osInfo: model.OSInfo{
				Type:      "debian",
				Version:   "11",
				Codename:  "bullseye",
				IsProxmox: true,
			},
			config: model.SSHConfig{
				Port:            2222,
				ListenAddresses: []string{"0.0.0.0"},
				PermitRootLogin: false,
				AllowedUsers:    []string{"proxmoxuser"},
				KeyPaths:        []string{".ssh/authorized_keys"},
				AuthMethods:     []string{"publickey"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Setup
			mockRepo := new(MockSSHRepository)
			service := NewSSHServiceImpl(mockRepo, tc.osInfo)

			// Setup expectations
			mockRepo.On("SaveSSHConfig", tc.config).Return(nil)

			// Execute
			err := service.ConfigureSSH(tc.config)

			// Assert
			assert.NoError(t, err)
			mockRepo.AssertExpectations(t)
		})
	}
}
