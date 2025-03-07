// pkg/application/user_manager.go
package application

import (
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// UserManager is an application service for user management
type UserManager struct {
	userService service.UserService
}

// NewUserManager creates a new UserManager
func NewUserManager(userService service.UserService) *UserManager {
	return &UserManager{
		userService: userService,
	}
}

// CreateUser creates a new system user with the specified settings
func (m *UserManager) CreateUser(username string, hasSudo bool, sudoNoPassword bool, sshKeys []string) error {
	user := model.User{
		Username:       username,
		HasSudo:        hasSudo,
		SudoNoPassword: sudoNoPassword,
		SshKeys:        sshKeys,
	}

	return m.userService.CreateUser(user)
}

// AddSSHKey adds an SSH key to an existing user
func (m *UserManager) AddSSHKey(username string, publicKey string) error {
	return m.userService.AddSSHKey(username, publicKey)
}
