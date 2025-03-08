// pkg/domain/model/user.go
package model

// User represents a system user
type User struct {
	Username       string
	HasSudo        bool
	SshKeys        []string
	SudoNoPassword bool
}
