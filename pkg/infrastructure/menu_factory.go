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

func (f *MenuFactory) CreateHelpMenu() *menu.HelpMenu {
	return menu.NewHelpMenu()
}

// CreateMainMenu creates the main menu with all dependencies wired up
func (f *MenuFactory) CreateMainMenu(versionService *version.Service) *menu.MainMenu {
	packageManager := f.serviceFactory.CreatePackageManager()
	hostInfoManager := f.serviceFactory.CreateHostInfoManager()
	menuManager := application.NewMenuManager(packageManager, hostInfoManager)

	// Create menu with all necessary fields initialized
	return menu.NewMainMenu(menuManager, f.config, f.osInfo, versionService)
}
