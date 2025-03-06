// pkg/infrastructure/service_factory.go
package infrastructure

import (
	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/config"
)

// ServiceFactory creates and wires application components
type ServiceFactory struct {
	provider *interfaces.Provider
	osInfo   *osdetect.OSInfo
	config   *config.Config
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
		DebianCorePackages:    f.config.LinuxCorePackages,
		DebianDmzPackages:     f.config.LinuxDmzPackages,
		DebianLabPackages:     f.config.LinuxLabPackages,
		AlpineCorePackages:    f.config.AlpineCorePackages,
		AlpineDmzPackages:     f.config.AlpineDmzPackages,
		AlpineLabPackages:     f.config.AlpineLabPackages,
		
		// Python packages
		DebianPythonPackages:  f.config.PythonPackages,
		NonWslPythonPackages:  f.config.NonWslPythonPackages,
		PythonPipPackages:     f.config.PythonPipPackages,
		AlpinePythonPackages:  f.config.AlpinePythonPackages,
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
	backupManager := f.CreateBackupManager()
	environmentManager := f.CreateEnvironmentManager()
	logsManager := f.CreateLogsManager()
	securityManager := application.NewSecurityManager(
			userManager, sshManager, firewallManager, dnsManager)
	
	return application.NewMenuManager(
			userManager, 
			sshManager, 
			firewallManager, 
			dnsManager, 
			packageManager, 
			backupManager, 
			securityManager,
			environmentManager,
			logsManager) 
}

// CreateBackupManager creates a BackupManager
func (f *ServiceFactory) CreateBackupManager() *application.BackupManager {
	// Create repository
	backupRepo := secondary.NewFileBackupRepository(
		f.provider.FS,
		f.provider.Commander,
		f.config.BackupPath,
		f.config.EnableBackups,
	)
	
	// Create domain service
	backupService := service.NewBackupServiceImpl(backupRepo)
	
	// Create application service
	return application.NewBackupManager(backupService)
}

// CreateEnvironmentManager creates an EnvironmentManager with all required dependencies
func (f *ServiceFactory) CreateEnvironmentManager() *application.EnvironmentManager {
	// Create repository
	environmentRepo := secondary.NewFileEnvironmentRepository(f.provider.FS, f.provider.Commander)
	
	// Create domain service
	environmentService := service.NewEnvironmentServiceImpl(environmentRepo)
	
	// Create application service
	return application.NewEnvironmentManager(environmentService)
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