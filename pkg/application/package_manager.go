// pkg/application/package_manager.go
package application

import (
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// PackageManager is an application service for package management
type PackageManager struct {
	packageService service.PackageService
}

// NewPackageManager creates a new PackageManager
func NewPackageManager(packageService service.PackageService) *PackageManager {
	return &PackageManager{
		packageService: packageService,
	}
}

// InstallLinuxPackages installs system packages based on the specified type
func (m *PackageManager) InstallLinuxPackages(packages []string, packageType string) error {
	// Create a package installation request
	request := model.PackageInstallRequest{
		Packages:    packages,
		PackageType: packageType,
		IsPython:    false,
	}
	
	// Call the domain service
	return m.packageService.InstallPackages(request)
}

// InstallPythonPackages installs Python packages
func (m *PackageManager) InstallPythonPackages(
	systemPackages []string,
	pipPackages []string,
	useUv bool,
) error {
	// Create a Python package installation request
	request := model.PackageInstallRequest{
		Packages:      systemPackages,
		PipPackages:   pipPackages,
		UseUv:         useUv,
		IsPython:      true,
	}
	
	// Call the domain service
	return m.packageService.InstallPackages(request)
}

// UpdatePackageSources updates package sources configuration
func (m *PackageManager) UpdatePackageSources() error {
	return m.packageService.UpdatePackageSources()
}

// UpdateProxmoxSources updates Proxmox-specific package sources configuration
func (m *PackageManager) UpdateProxmoxSources() error {
	return m.packageService.UpdateProxmoxSources()
}