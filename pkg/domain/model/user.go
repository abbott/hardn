// pkg/domain/model/user.go
package model

// User represents a system user
type User struct {
	Username       string
	HasSudo        bool
	SshKeys        []string
	SudoNoPassword bool
	// Extended information
	UID           string
	GID           string
	HomeDirectory string
	LastLogin     string
	LastLoginIP   string // Added field for last login IP address
}
