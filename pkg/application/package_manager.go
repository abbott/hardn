// pkg/application/package_manager.go
package application

import (
	"os"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/interfaces"
)

// PackageManager is an application service for package management
// PackageManager is an application service for package management
type PackageManager struct {
	packageService service.PackageService
	config         *model.PackageSources
	osInfo         *model.OSInfo
	networkOps     interfaces.NetworkOperations
	dmzSubnet      string
}

// NewPackageManager creates a new PackageManager
func NewPackageManager(
	packageService service.PackageService,
	config *model.PackageSources,
	osInfo *model.OSInfo,
	networkOps interfaces.NetworkOperations,
	dmzSubnet string,
) *PackageManager {
	return &PackageManager{
		packageService: packageService,
		config:         config,
		osInfo:         osInfo,
		networkOps:     networkOps,
		dmzSubnet:      dmzSubnet,
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
		Packages:    systemPackages,
		PipPackages: pipPackages,
		UseUv:       useUv,
		IsPython:    true,
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

// InstallAllLinuxPackages installs all appropriate packages based on OS type and environment
func (m *PackageManager) InstallAllLinuxPackages() error {
	// Check if we're in a DMZ subnet
	isDMZ, _ := m.networkOps.CheckSubnet(m.dmzSubnet)

	var corePackages, dmzPackages, labPackages []string

	// Determine packages based on OS type
	if m.osInfo.Type == "alpine" {
		// Get Alpine packages from the configuration
		if m.config != nil {
			corePackages = m.config.AlpineCorePackages
			dmzPackages = m.config.AlpineDmzPackages
			labPackages = m.config.AlpineLabPackages
		}
	} else {
		// Get Debian/Ubuntu packages from the configuration
		if m.config != nil {
			corePackages = m.config.DebianCorePackages
			dmzPackages = m.config.DebianDmzPackages
			labPackages = m.config.DebianLabPackages
		}
	}

	// Install core packages
	if len(corePackages) > 0 {
		if err := m.InstallLinuxPackages(corePackages, "core"); err != nil {
			return err
		}
	}

	// Install DMZ packages
	if len(dmzPackages) > 0 {
		if err := m.InstallLinuxPackages(dmzPackages, "dmz"); err != nil {
			return err
		}
	}

	// Install lab packages if not in DMZ
	if !isDMZ && len(labPackages) > 0 {
		if err := m.InstallLinuxPackages(labPackages, "lab"); err != nil {
			return err
		}
	}

	return nil
}

// InstallAllPythonPackages installs all appropriate Python packages based on OS type
func (m *PackageManager) InstallAllPythonPackages(useUv bool) error {
	var systemPackages []string
	var pipPackages []string

	if m.osInfo.Type == "alpine" {
		// Get Alpine Python packages
		if m.config != nil {
			systemPackages = m.config.AlpinePythonPackages
		}
	} else {
		// Get Debian/Ubuntu Python packages
		if m.config != nil {
			systemPackages = m.config.DebianPythonPackages

			// Add non-WSL packages if not in WSL
			if os.Getenv("WSL") == "" && len(m.config.NonWslPythonPackages) > 0 {
				systemPackages = append(systemPackages, m.config.NonWslPythonPackages...)
			}

			pipPackages = m.config.PythonPipPackages
		}
	}

	// Install Python packages
	if len(systemPackages) > 0 || len(pipPackages) > 0 {
		return m.InstallPythonPackages(systemPackages, pipPackages, useUv)
	}

	return nil
}
