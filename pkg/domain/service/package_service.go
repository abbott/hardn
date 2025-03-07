// pkg/domain/service/package_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// PackageService defines operations for package management
type PackageService interface {
	// InstallPackages installs the specified packages
	InstallPackages(request model.PackageInstallRequest) error

	// UpdatePackageSources updates package repository sources
	UpdatePackageSources() error

	// UpdateProxmoxSources updates Proxmox-specific package sources
	UpdateProxmoxSources() error

	// IsPackageInstalled checks if a package is installed
	IsPackageInstalled(packageName string) (bool, error)
}

// PackageServiceImpl implements PackageService
type PackageServiceImpl struct {
	repository PackageRepository
	osInfo     model.OSInfo
}

// NewPackageServiceImpl creates a new PackageServiceImpl
func NewPackageServiceImpl(repository PackageRepository, osInfo model.OSInfo) *PackageServiceImpl {
	return &PackageServiceImpl{
		repository: repository,
		osInfo:     osInfo,
	}
}

// PackageRepository defines the repository operations needed by PackageService
type PackageRepository interface {
	InstallPackages(request model.PackageInstallRequest) error
	UpdatePackageSources(sources model.PackageSources) error
	UpdateProxmoxSources(sources model.PackageSources) error
	IsPackageInstalled(packageName string) (bool, error)
	GetPackageSources() (*model.PackageSources, error)
}

// Implementation of PackageService methods
func (s *PackageServiceImpl) InstallPackages(request model.PackageInstallRequest) error {
	return s.repository.InstallPackages(request)
}

func (s *PackageServiceImpl) UpdatePackageSources() error {
	sources, err := s.repository.GetPackageSources()
	if err != nil {
		return err
	}

	return s.repository.UpdatePackageSources(*sources)
}

func (s *PackageServiceImpl) UpdateProxmoxSources() error {
	sources, err := s.repository.GetPackageSources()
	if err != nil {
		return err
	}

	return s.repository.UpdateProxmoxSources(*sources)
}

func (s *PackageServiceImpl) IsPackageInstalled(packageName string) (bool, error) {
	return s.repository.IsPackageInstalled(packageName)
}
