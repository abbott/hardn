// pkg/domain/service/user_service.go
package service

// UserRepository is the user data HostInfoService reads.
type UserRepository interface {
	GetNonSystemGroups() ([]string, error)
}
