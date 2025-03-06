// pkg/port/secondary/package_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// PackageRepository defines the interface for package management operations
type PackageRepository interface {
	// InstallPackages installs packages based on the request
	InstallPackages(request model.PackageInstallRequest) error
	
	// UpdatePackageSources updates package repository sources
	UpdatePackageSources(sources model.PackageSources) error
	
	// UpdateProxmoxSources updates Proxmox-specific package sources
	UpdateProxmoxSources(sources model.PackageSources) error
	
	// IsPackageInstalled checks if a package is installed
	IsPackageInstalled(packageName string) (bool, error)
	
	// GetPackageSources retrieves the current package sources configuration
	GetPackageSources() (*model.PackageSources, error)
}