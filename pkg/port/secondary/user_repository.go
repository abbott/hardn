package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// UserRepository defines the interface for user persistence operations
type UserRepository interface {
	GetExtendedUserInfo(username string) (*model.User, error)

	// GetNonSystemUsers retrieves non-system users on the system
	GetNonSystemUsers() ([]model.User, error)

	// GetNonSystemGroups retrieves non-system groups on the system
	GetNonSystemGroups() ([]string, error)
}
