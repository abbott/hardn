// pkg/domain/ports/secondary/user_login_port.go
package secondary

import (
	"time"
)

// UserLoginPort defines the interface for retrieving user login information.
// This is a secondary port (driven side) in hexagonal architecture.
type UserLoginPort interface {
	// GetLastLoginTime retrieves the timestamp of a user's last login
	GetLastLoginTime(username string) (time.Time, error)

	// GetLastLoginInfo retrieves both the timestamp and additional information like IP
	GetLastLoginInfo(username string) (time.Time, string, error)
}
