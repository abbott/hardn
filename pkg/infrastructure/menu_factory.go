// pkg/infrastructure/menu_factory.go
package infrastructure

import (
    "github.com/abbott/hardn/pkg/application"
    "github.com/abbott/hardn/pkg/config"
    "github.com/abbott/hardn/pkg/menu"
    "github.com/abbott/hardn/pkg/osdetect"
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

// CreateMainMenu creates the main menu with all dependencies wired up
func (f *MenuFactory) CreateMainMenu() *menu.MainMenu {
	// Create required managers
	userManager := f.serviceFactory.CreateUserManager()
	sshManager := f.serviceFactory.CreateSSHManager()
	firewallManager := f.serviceFactory.CreateFirewallManager()
	dnsManager := f.serviceFactory.CreateDNSManager()
	packageManager := f.serviceFactory.CreatePackageManager()
	securityManager := application.NewSecurityManager(
		userManager, sshManager, firewallManager, dnsManager)
	
	// Create menu manager
	menuManager := application.NewMenuManager(
		userManager, sshManager, firewallManager, dnsManager, packageManager, securityManager)
    
    // Create menu
    return menu.NewMainMenu(menuManager, f.config, f.osInfo)
}
