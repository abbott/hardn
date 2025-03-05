// pkg/infrastructure/service_factory.go
package infrastructure

import (
	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
)

// ServiceFactory creates and wires application components
type ServiceFactory struct {
	provider *interfaces.Provider
	osInfo   *osdetect.OSInfo
}

// NewServiceFactory creates a new ServiceFactory
func NewServiceFactory(provider *interfaces.Provider, osInfo *osdetect.OSInfo) *ServiceFactory {
	return &ServiceFactory{
		provider: provider,
		osInfo:   osInfo,
	}
}

// CreateUserManager creates a UserManager with all required dependencies
func (f *ServiceFactory) CreateUserManager() *application.UserManager {
	// Create repository
	userRepo := secondary.NewOSUserRepository(f.provider.FS, f.provider.Commander, f.osInfo.OsType)
	
	// Create domain service
	userService := service.NewUserServiceImpl(userRepo)
	
	// Create application service
	return application.NewUserManager(userService)
}