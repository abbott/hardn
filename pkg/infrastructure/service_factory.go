// pkg/infrastructure/service_factory.go
package infrastructure

import (
	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
	portsecondary "github.com/abbott/hardn/pkg/port/secondary"
)

// ServiceFactory creates and wires application components
type ServiceFactory struct {
	provider *interfaces.Provider
	osInfo   *osdetect.OSInfo
	config   *config.Config
	// Cache for repositories to avoid creating multiple instances
	userRepository portsecondary.UserRepository
}

// NewServiceFactory creates a new ServiceFactory
func NewServiceFactory(provider *interfaces.Provider, osInfo *osdetect.OSInfo) *ServiceFactory {
	return &ServiceFactory{
		provider: provider,
		osInfo:   osInfo,
	}
}

// SetConfig sets the configuration
func (f *ServiceFactory) SetConfig(config *config.Config) {
	f.config = config
}

// getUserRepository returns or creates a UserRepository
func (f *ServiceFactory) getUserRepository() portsecondary.UserRepository {
	if f.userRepository == nil {
		f.userRepository = secondary.NewOSUserRepository(f.provider.FS, f.provider.Commander, f.osInfo.OsType)
	}
	return f.userRepository
}

// CreateHostInfoManager creates a HostInfoManager
func (f *ServiceFactory) CreateHostInfoManager() *application.HostInfoManager {
	// Get the shared user repository
	userRepo := f.getUserRepository()

	// Create host info repository with user repository dependency
	hostInfoRepo := secondary.NewOSHostInfoRepository(f.provider.FS, f.provider.Commander, f.osInfo.OsType, userRepo)

	// Create domain service
	hostInfoService := service.NewHostInfoServiceImpl(hostInfoRepo, userRepo, convertOSInfo(f.osInfo))

	// Create application service
	return application.NewHostInfoManager(hostInfoService)
}

// Helper to convert osdetect.OSInfo to domain model.OSInfo
func convertOSInfo(info *osdetect.OSInfo) model.OSInfo {
	return model.OSInfo{
		Type:      info.OsType,
		Codename:  info.OsCodename,
		Version:   info.OsVersion,
		IsProxmox: info.IsProxmox,
	}
}
