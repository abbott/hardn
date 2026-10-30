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
	// One Proxmox VE check, shared by the SSH and UFW repositories
	proxmoxHost *secondary.ProxmoxHost
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

// getProxmoxHost returns or creates the host's Proxmox VE check
func (f *ServiceFactory) getProxmoxHost() *secondary.ProxmoxHost {
	if f.proxmoxHost == nil {
		f.proxmoxHost = secondary.NewProxmoxHost(f.provider.FS, f.provider.Commander, f.osInfo.OsType, f.osInfo.IsProxmox)
	}
	return f.proxmoxHost
}

// IsProxmoxHost reports whether the host is Proxmox VE by the check the
// SSH and UFW repositories share, so a caller can say what they did there:
// root login set to key-only rather than off, and UFW refused. It is false
// while pkg/adapter/secondary's proxmoxTreatedAsDebian is set.
func (f *ServiceFactory) IsProxmoxHost() bool {
	return f.getProxmoxHost().IsProxmox()
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

// CreateUserManager creates a UserManager with all required dependencies
func (f *ServiceFactory) CreateUserManager() *application.UserManager {
	// Get the shared user repository
	userRepo := f.getUserRepository()

	// Create domain service
	userService := service.NewUserServiceImpl(userRepo)

	// Create application service
	return application.NewUserManager(userService)
}

// CreateSSHManager creates an SSHManager with all required dependencies
func (f *ServiceFactory) CreateSSHManager() *application.SSHManager {
	// Create repository
	sshRepo := secondary.NewFileSSHRepository(f.provider.FS, f.provider.Commander, f.osInfo.OsType, f.getProxmoxHost())

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

// CreateFirewallManager creates a FirewallManager
func (f *ServiceFactory) CreateFirewallManager() *application.FirewallManager {
	// Create repository
	firewallRepo := secondary.NewUFWFirewallRepository(f.provider.FS, f.provider.Commander, f.getProxmoxHost())

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

// CreatePackageManager creates a PackageManager
func (f *ServiceFactory) CreatePackageManager() *application.PackageManager {
	// Convert config to PackageSources model
	sources := &model.PackageSources{
		// Standard repositories
		DebianRepos:           f.config.DebianRepos,
		ProxmoxSrcRepos:       f.config.ProxmoxSrcRepos,
		ProxmoxCephRepo:       f.config.ProxmoxCephRepo,
		ProxmoxEnterpriseRepo: f.config.ProxmoxEnterpriseRepo,
		AlpineTestingRepo:     f.config.AlpineTestingRepo,

		// Package lists
		DebianCorePackages: f.config.LinuxCorePackages,
		DebianDmzPackages:  f.config.LinuxDmzPackages,
		DebianLabPackages:  f.config.LinuxLabPackages,
		AlpineCorePackages: f.config.AlpineCorePackages,
		AlpineDmzPackages:  f.config.AlpineDmzPackages,
		AlpineLabPackages:  f.config.AlpineLabPackages,

		// Python packages
		DebianPythonPackages: f.config.PythonPackages,
		NonWslPythonPackages: f.config.NonWslPythonPackages,
		PythonPipPackages:    f.config.PythonPipPackages,
		AlpinePythonPackages: f.config.AlpinePythonPackages,
	}

	// Create repository
	packageRepo := secondary.NewOSPackageRepository(
		f.provider.FS,
		f.provider.Commander,
		f.osInfo.OsType,
		f.osInfo.OsVersion,
		f.osInfo.OsCodename,
		f.osInfo.IsProxmox,
		sources,
	)

	// Create domain service
	packageService := service.NewPackageServiceImpl(packageRepo, convertOSInfo(f.osInfo))

	// Create application service with all required dependencies
	return application.NewPackageManager(
		packageService,
		sources,
		&model.OSInfo{
			Type:      f.osInfo.OsType,
			Version:   f.osInfo.OsVersion,
			Codename:  f.osInfo.OsCodename,
			IsProxmox: f.osInfo.IsProxmox,
		},
		f.provider.Network,
		f.config.DmzSubnet,
	)
}

// CreateMenuManager creates a MenuManager with all required dependencies
func (f *ServiceFactory) CreateMenuManager() *application.MenuManager {
	userManager := f.CreateUserManager()
	sshManager := f.CreateSSHManager()
	firewallManager := f.CreateFirewallManager()
	dnsManager := f.CreateDNSManager()
	packageManager := f.CreatePackageManager()
	logsManager := f.CreateLogsManager()
	hostInfoManager := f.CreateHostInfoManager()
	securityManager := application.NewSecurityManager(
		userManager, sshManager, firewallManager, dnsManager)

	menuManager := application.NewMenuManager(
		userManager,
		sshManager,
		firewallManager,
		dnsManager,
		packageManager,
		securityManager,
		logsManager,
		hostInfoManager)
	menuManager.SetProxmoxCheck(f.IsProxmoxHost)
	return menuManager
}

// CreateLogsManager creates a LogsManager
func (f *ServiceFactory) CreateLogsManager() *application.LogsManager {
	// Create repository
	logsRepo := secondary.NewFileLogsRepository(
		f.provider.FS,
		f.config.LogFile,
	)

	// Create domain service
	logsService := service.NewLogsServiceImpl(logsRepo)

	// Create application service
	return application.NewLogsManager(logsService)
}
