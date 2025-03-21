package service

import (
	"fmt"
	"testing"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockUserRepository is a mock implementation of the UserRepository interface for testing
type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) CreateUser(user model.User) error {
	args := m.Called(user)
	return args.Error(0)
}

func (m *MockUserRepository) GetUser(username string) (*model.User, error) {
	args := m.Called(username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	// Safely perform type assertion
	user, ok := args.Get(0).(*model.User)
	if !ok {
		return nil, fmt.Errorf("invalid type assertion, expected *model.User")
	}

	return user, args.Error(1)
}

func (m *MockUserRepository) AddSSHKey(username, publicKey string) error {
	args := m.Called(username, publicKey)
	return args.Error(0)
}

func (m *MockUserRepository) ConfigureSudo(username string, noPassword bool) error {
	args := m.Called(username, noPassword)
	return args.Error(0)
}

func (m *MockUserRepository) UserExists(username string) (bool, error) {
	args := m.Called(username)
	return args.Bool(0), args.Error(1)
}

func (m *MockUserRepository) GetNonSystemUsers() ([]model.User, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	// Safely perform type assertion
	users, ok := args.Get(0).([]model.User)
	if !ok {
		return nil, fmt.Errorf("invalid type assertion, expected []model.User")
	}

	return users, args.Error(1)
}

func (m *MockUserRepository) GetNonSystemGroups() ([]string, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	// Safely perform type assertion
	groups, ok := args.Get(0).([]string)
	if !ok {
		return nil, fmt.Errorf("invalid type assertion, expected []string")
	}

	return groups, args.Error(1)
}

func (m *MockUserRepository) GetExtendedUserInfo(username string) (*model.User, error) {
	args := m.Called(username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}

	// Safely perform type assertion
	user, ok := args.Get(0).(*model.User)
	if !ok {
		return nil, fmt.Errorf("invalid type assertion, expected *model.User")
	}

	return user, args.Error(1)
}

func TestUserServiceImpl_CreateUser(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	user := model.User{
		Username:       "testuser",
		HasSudo:        true,
		SshKeys:        []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"},
		SudoNoPassword: true,
	}

	// Setup expectations
	mockRepo.On("CreateUser", user).Return(nil)

	// Execute
	err := service.CreateUser(user)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_CreateUser_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	user := model.User{
		Username:       "testuser",
		HasSudo:        true,
		SshKeys:        []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"},
		SudoNoPassword: true,
	}

	// Setup expectations
	expectedErr := fmt.Errorf("failed to create user")
	mockRepo.On("CreateUser", user).Return(expectedErr)

	// Execute
	err := service.CreateUser(user)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_GetUser(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "testuser"
	expectedUser := &model.User{
		Username:       username,
		HasSudo:        true,
		SshKeys:        []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"},
		SudoNoPassword: true,
	}

	// Setup expectations
	mockRepo.On("GetUser", username).Return(expectedUser, nil)

	// Execute
	user, err := service.GetUser(username)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedUser, user)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_GetUser_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "nonexistentuser"

	// Setup expectations
	expectedErr := fmt.Errorf("user not found")
	mockRepo.On("GetUser", username).Return(nil, expectedErr)

	// Execute
	user, err := service.GetUser(username)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_AddSSHKey(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "testuser"
	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"

	// Setup expectations
	mockRepo.On("AddSSHKey", username, publicKey).Return(nil)

	// Execute
	err := service.AddSSHKey(username, publicKey)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_AddSSHKey_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "testuser"
	publicKey := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"

	// Setup expectations
	expectedErr := fmt.Errorf("failed to add SSH key")
	mockRepo.On("AddSSHKey", username, publicKey).Return(expectedErr)

	// Execute
	err := service.AddSSHKey(username, publicKey)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_ConfigureSudo(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "testuser"
	noPassword := true

	// Setup expectations
	mockRepo.On("ConfigureSudo", username, noPassword).Return(nil)

	// Execute
	err := service.ConfigureSudo(username, noPassword)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_ConfigureSudo_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "testuser"
	noPassword := true

	// Setup expectations
	expectedErr := fmt.Errorf("failed to configure sudo")
	mockRepo.On("ConfigureSudo", username, noPassword).Return(expectedErr)

	// Execute
	err := service.ConfigureSudo(username, noPassword)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_WithSpecialCharacters(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data with special characters to check for escaping issues
	userWithSpecialChars := model.User{
		Username:       "user-with.special_chars",
		HasSudo:        true,
		SshKeys:        []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@example.com"},
		SudoNoPassword: true,
	}

	// Setup expectations
	mockRepo.On("CreateUser", userWithSpecialChars).Return(nil)

	// Execute
	err := service.CreateUser(userWithSpecialChars)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_UserWithEmptyValues(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data with empty values
	userWithEmptyValues := model.User{
		Username:       "minimal-user",
		HasSudo:        false,
		SshKeys:        []string{},
		SudoNoPassword: false,
	}

	// Setup expectations
	mockRepo.On("CreateUser", userWithEmptyValues).Return(nil)

	// Execute
	err := service.CreateUser(userWithEmptyValues)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_UserWithMultipleSSHKeys(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data with multiple SSH keys
	userWithMultipleKeys := model.User{
		Username: "user-with-keys",
		HasSudo:  true,
		SshKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... key1@example.com",
			"ssh-rsa AAAAB3NzaC1yc2EAAAADA... key2@example.com",
			"ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY... key3@example.com",
		},
		SudoNoPassword: true,
	}

	// Setup expectations
	mockRepo.On("CreateUser", userWithMultipleKeys).Return(nil)

	// Execute
	err := service.CreateUser(userWithMultipleKeys)

	// Assert
	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_UserWithExistingUsername(t *testing.T) {
	// This would be an integration test or would require more complex repository mocking
	// that would check if the user exists first and then return an appropriate error

	// In a real implementation, the repository would check if the user exists before creating
	// For this unit test example, we're just simulating the error returned when a duplicate user
	// creation is attempted

	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	existingUser := model.User{
		Username:       "existing-user",
		HasSudo:        true,
		SshKeys:        []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... user@example.com"},
		SudoNoPassword: true,
	}

	// Setup expectations
	existsErr := fmt.Errorf("user 'existing-user' already exists")
	mockRepo.On("CreateUser", existingUser).Return(existsErr)

	// Execute
	err := service.CreateUser(existingUser)

	// Assert
	assert.Error(t, err)
	assert.Equal(t, existsErr, err)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_GetExtendedUserInfo(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "testuser"
	expectedUser := &model.User{
		Username:       username,
		HasSudo:        true,
		SshKeys:        []string{"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAI... testuser@example.com"},
		SudoNoPassword: true,
		UID:            "1000",
		GID:            "1000",
		HomeDirectory:  "/home/testuser",
		LastLogin:      "Mon Mar 18 10:30:45 2025",
	}

	// Setup expectations
	mockRepo.On("GetExtendedUserInfo", username).Return(expectedUser, nil)

	// Execute
	user, err := service.GetExtendedUserInfo(username)

	// Assert
	assert.NoError(t, err)
	assert.Equal(t, expectedUser, user)
	mockRepo.AssertExpectations(t)
}

func TestUserServiceImpl_GetExtendedUserInfo_Error(t *testing.T) {
	// Setup
	mockRepo := new(MockUserRepository)
	service := NewUserServiceImpl(mockRepo)

	// Test data
	username := "nonexistentuser"

	// Setup expectations
	expectedErr := fmt.Errorf("user not found")
	mockRepo.On("GetExtendedUserInfo", username).Return(nil, expectedErr)

	// Execute
	user, err := service.GetExtendedUserInfo(username)

	// Assert
	assert.Error(t, err)
	assert.Nil(t, user)
	assert.Equal(t, expectedErr, err)
	mockRepo.AssertExpectations(t)
}
