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
    securityManager := application.NewSecurityManager(
        userManager, sshManager, firewallManager, dnsManager)
    
    // Create menu manager
    menuManager := application.NewMenuManager(
        userManager, sshManager, firewallManager, dnsManager, securityManager)
    
    // Create menu
    return menu.NewMainMenu(menuManager, f.config, f.osInfo)
}

// Example for UserMenu
type UserMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

func NewUserMenu(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *UserMenu {
	return &UserMenu{
			menuManager: menuManager,
			config:      config,
			osInfo:      osInfo,
	}
}

func (m *UserMenu) Show() {
	// Implementation adapted from UserCreationMenu function
}