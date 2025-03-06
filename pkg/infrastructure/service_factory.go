// pkg/infrastructure/service_factory.go
package infrastructure

import (
	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/domain/model"
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

// CreateSSHManager creates an SSHManager with all required dependencies
func (f *ServiceFactory) CreateSSHManager() *application.SSHManager {
	// Create repository
	sshRepo := secondary.NewFileSSHRepository(f.provider.FS, f.provider.Commander, f.osInfo.OsType)
	
	// Create domain service
	sshService := service.NewSSHServiceImpl(sshRepo, convertOSInfo(f.osInfo))
	
	// Create application service
	return application.NewSSHManager(sshService)
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

// Update pkg/infrastructure/service_factory.go to include all managers

// CreateFirewallManager creates a FirewallManager
func (f *ServiceFactory) CreateFirewallManager() *application.FirewallManager {
	// Create repository
	firewallRepo := secondary.NewUFWFirewallRepository(f.provider.FS, f.provider.Commander)
	
	// Create domain service
	firewallService := service.NewFirewallServiceImpl(firewallRepo, convertOSInfo(f.osInfo))
	
	// Create application service
	return application.NewFirewallManager(firewallService)
}

// CreateDNSManager creates a DNSManager
func (f *ServiceFactory) CreateDNSManager() *application.DNSManager {
	// Create repository
	dnsRepo := secondary.NewFileDNSRepository(f.provider.FS, f.provider.Commander, f.osInfo.OsType)
	
	// Create domain service
	dnsService := service.NewDNSServiceImpl(dnsRepo, convertOSInfo(f.osInfo))
	
	// Create application service
	return application.NewDNSManager(dnsService)
}