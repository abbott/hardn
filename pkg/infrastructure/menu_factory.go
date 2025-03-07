// pkg/infrastructure/menu_factory.go
package infrastructure

import (
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/menu"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/version"
)

// MenuFactory creates menu components
type MenuFactory struct {
	serviceFactory *ServiceFactory
	config         *config.Config
	osInfo         *osdetect.OSInfo
}

func NewMenuFactory(
	serviceFactory *ServiceFactory,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *MenuFactory {
	// Set the config in the service factory
	serviceFactory.SetConfig(config)

	return &MenuFactory{
		serviceFactory: serviceFactory,
		config:         config,
		osInfo:         osInfo,
	}
}

// CreateRunAllMenu creates a RunAllMenu with all dependencies wired up
func (f *MenuFactory) CreateRunAllMenu() *menu.RunAllMenu {
	menuManager := f.serviceFactory.CreateMenuManager()
	return menu.NewRunAllMenu(menuManager, f.config, f.osInfo)
}

// CreateDryRunMenu creates a DryRunMenu with all dependencies wired up
func (f *MenuFactory) CreateDryRunMenu() *menu.DryRunMenu {
	menuManager := f.serviceFactory.CreateMenuManager()
	return menu.NewDryRunMenu(menuManager, f.config)
}

func (f *MenuFactory) CreateHelpMenu() *menu.HelpMenu {
	return menu.NewHelpMenu()
}

// CreateMainMenu creates the main menu with all dependencies wired up
func (f *MenuFactory) CreateMainMenu(versionService *version.Service) *menu.MainMenu {
	// Create required managers
	userManager := f.serviceFactory.CreateUserManager()
	sshManager := f.serviceFactory.CreateSSHManager()
	firewallManager := f.serviceFactory.CreateFirewallManager()
	dnsManager := f.serviceFactory.CreateDNSManager()
	packageManager := f.serviceFactory.CreatePackageManager()
	backupManager := f.serviceFactory.CreateBackupManager()
	environmentManager := f.serviceFactory.CreateEnvironmentManager()
	logsManager := f.serviceFactory.CreateLogsManager()
	securityManager := application.NewSecurityManager(
		userManager, sshManager, firewallManager, dnsManager)

	// Create menu manager (use := instead of = since we're not declaring it above anymore)
	menuManager := application.NewMenuManager(
		userManager,
		sshManager,
		firewallManager,
		dnsManager,
		packageManager,
		backupManager,
		securityManager,
		environmentManager,
		logsManager)

	// Create menu with all necessary fields initialized
	return menu.NewMainMenu(menuManager, f.config, f.osInfo, versionService)
}
