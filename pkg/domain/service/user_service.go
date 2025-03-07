// pkg/domain/service/user_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// UserService defines operations for user management
type UserService interface {
	CreateUser(user model.User) error
	GetUser(username string) (*model.User, error)
	AddSSHKey(username, publicKey string) error
	ConfigureSudo(username string, noPassword bool) error
}

// UserServiceImpl implements UserService
type UserServiceImpl struct {
	repository UserRepository
}

// NewUserServiceImpl creates a new UserServiceImpl
func NewUserServiceImpl(repository UserRepository) *UserServiceImpl {
	return &UserServiceImpl{
		repository: repository,
	}
}

// UserRepository defines user data operations needed by UserService
type UserRepository interface {
	CreateUser(user model.User) error
	GetUser(username string) (*model.User, error)
	AddSSHKey(username, publicKey string) error
	ConfigureSudo(username string, noPassword bool) error
}

// Implement UserService methods...
func (s *UserServiceImpl) CreateUser(user model.User) error {
	return s.repository.CreateUser(user)
}

func (s *UserServiceImpl) GetUser(username string) (*model.User, error) {
	return s.repository.GetUser(username)
}

func (s *UserServiceImpl) AddSSHKey(username, publicKey string) error {
	return s.repository.AddSSHKey(username, publicKey)
}

func (s *UserServiceImpl) ConfigureSudo(username string, noPassword bool) error {
	return s.repository.ConfigureSudo(username, noPassword)
}
