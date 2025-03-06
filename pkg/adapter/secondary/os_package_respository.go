// pkg/adapter/secondary/os_package_repository.go
package secondary

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// OSPackageRepository implements PackageRepository using OS operations
type OSPackageRepository struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
	osType    string
	osVersion string
	osCodename string
	isProxmox bool
	config    *model.PackageSources
}

// NewOSPackageRepository creates a new OSPackageRepository
func NewOSPackageRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	osType string,
	osVersion string,
	osCodename string,
	isProxmox bool,
	config *model.PackageSources,
) secondary.PackageRepository {
	return &OSPackageRepository{
		fs:        fs,
		commander: commander,
		osType:    osType,
		osVersion: osVersion,
		osCodename: osCodename,
		isProxmox: isProxmox,
		config:    config,
	}
}

// InstallPackages installs packages based on the request
func (r *OSPackageRepository) InstallPackages(request model.PackageInstallRequest) error {
	if len(request.Packages) == 0 && len(request.PipPackages) == 0 {
		return nil
	}

	if request.IsPython {
		return r.installPythonPackages(request)
	}
	
	// Standard Linux packages installation
	var args []string
	
	if r.osType == "alpine" {
		args = append([]string{"add", "--no-cache"}, request.Packages...)
		_, err := r.commander.Execute("apk", args...)
		if err != nil {
			return fmt.Errorf("failed to install Alpine packages: %w", err)
		}
	} else {
		// Hold Proxmox packages if necessary
		if r.isProxmox {
			if err := r.holdProxmoxPackages(); err != nil {
				return err
			}
		}

		// Update package lists
		_, err := r.commander.Execute("apt-get", "update")
		if err != nil {
			return fmt.Errorf("failed to update package lists: %w", err)
		}

		// Install packages
		args = append([]string{"install", "--yes"}, request.Packages...)
		_, err = r.commander.Execute("apt-get", args...)
		if err != nil {
			return fmt.Errorf("failed to install Debian/Ubuntu packages: %w", err)
		}

		// Clean up
		r.commander.Execute("apt-get", "autoremove", "--yes")
		r.commander.Execute("apt-get", "clean")
		r.commander.Execute("rm", "-rf", "/var/lib/apt/lists/*")

		// Unhold Proxmox packages
		if r.isProxmox {
			r.unholdProxmoxPackages()
		}
	}

	return nil
}

// installPythonPackages handles Python package installation
func (r *OSPackageRepository) installPythonPackages(request model.PackageInstallRequest) error {
	if r.osType == "alpine" {
		// Use Alpine's package manager for Python packages
		if len(request.Packages) > 0 {
			args := append([]string{"add", "--no-cache"}, request.Packages...)
			_, err := r.commander.Execute("apk", args...)
			if err != nil {
				return fmt.Errorf("failed to install Alpine Python packages: %w", err)
			}
		}
	} else {
		// For Debian/Ubuntu systems
		if len(request.Packages) > 0 {
			// Install system packages first
			_, err := r.commander.Execute("apt-get", "update")
			if err != nil {
				return fmt.Errorf("failed to update package lists for Python installation: %w", err)
			}

			args := append([]string{"install", "--yes"}, request.Packages...)
			_, err = r.commander.Execute("apt-get", args...)
			if err != nil {
				return fmt.Errorf("failed to install Python system packages: %w", err)
			}
		}
	}

	// Handle pip/UV packages
	if len(request.PipPackages) > 0 {
		if request.UseUv {
			// Check if UV is installed
			_, err := r.commander.Execute("which", "uv")
			if err != nil {
				// Install UV
				_, err = r.commander.Execute("pip3", "install", "uv")
				if err != nil {
					return fmt.Errorf("failed to install UV package manager: %w", err)
				}
			}

			// Install packages using UV
			args := append([]string{"pip", "install"}, request.PipPackages...)
			_, err = r.commander.Execute("uv", args...)
			if err != nil {
				return fmt.Errorf("failed to install Python pip packages with UV: %w", err)
			}
		} else {
			// Use standard pip
			args := append([]string{"install"}, request.PipPackages...)
			_, err := r.commander.Execute("pip3", args...)
			if err != nil {
				return fmt.Errorf("failed to install Python pip packages: %w", err)
			}
		}
	}

	return nil
}

// UpdatePackageSources updates package sources configuration
func (r *OSPackageRepository) UpdatePackageSources(sources model.PackageSources) error {
	if r.osType == "alpine" {
		return r.updateAlpineSources(sources)
	}
	
	// Debian/Ubuntu
	return r.updateDebianSources(sources)
}

// updateAlpineSources updates Alpine Linux repository configuration
func (r *OSPackageRepository) updateAlpineSources(sources model.PackageSources) error {
	// Format Alpine version for repositories
	versionPrefix := r.osVersion
	if idx := strings.LastIndex(versionPrefix, "."); idx != -1 {
		versionPrefix = versionPrefix[:idx]
	}

	// Create Alpine repository file content
	content := fmt.Sprintf(`# Main repositories
https://dl-cdn.alpinelinux.org/alpine/v%s/main
https://dl-cdn.alpinelinux.org/alpine/v%s/community

# Security updates
https://dl-cdn.alpinelinux.org/alpine/v%s/main
https://dl-cdn.alpinelinux.org/alpine/v%s/community
`, versionPrefix, versionPrefix, versionPrefix, versionPrefix)

	// testing repo if enabled
	if sources.AlpineTestingRepo {
		content += `
# Testing repository (use with caution)
https://dl-cdn.alpinelinux.org/alpine/edge/testing
`
	}

	// Write the file
	if err := r.fs.WriteFile("/etc/apk/repositories", []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write Alpine repositories: %w", err)
	}

	// Update package index
	_, err := r.commander.Execute("apk", "update")
	if err != nil {
		return fmt.Errorf("failed to update Alpine package index: %w", err)
	}

	return nil
}

// updateDebianSources updates Debian/Ubuntu repository configuration
func (r *OSPackageRepository) updateDebianSources(sources model.PackageSources) error {
	// Prepare content by replacing CODENAME placeholder
	var content strings.Builder
	for _, repo := range sources.DebianRepos {
		content.WriteString(strings.ReplaceAll(repo, "CODENAME", r.osCodename))
		content.WriteString("\n")
	}

	// Backup original file
	backupFile := "/etc/apt/sources.list.bak"
	originalData, err := r.fs.ReadFile("/etc/apt/sources.list")
	if err == nil {
		r.fs.WriteFile(backupFile, originalData, 0644)
	}

	// Write the file
	if err := r.fs.WriteFile("/etc/apt/sources.list", []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Debian/Ubuntu sources list: %w", err)
	}

	return nil
}

// UpdateProxmoxSources updates Proxmox-specific sources
func (r *OSPackageRepository) UpdateProxmoxSources(sources model.PackageSources) error {
	if !r.isProxmox {
		return nil
	}

	// Create directory if it doesn't exist
	if err := r.fs.MkdirAll("/etc/apt/sources.list.d", 0755); err != nil {
		return fmt.Errorf("failed to create sources.list.d directory: %w", err)
	}

	// Write Ceph repository
	var cephContent strings.Builder
	for _, repo := range sources.ProxmoxCephRepo {
		cephContent.WriteString(strings.ReplaceAll(repo, "CODENAME", r.osCodename))
		cephContent.WriteString("\n")
	}
	
	if err := r.fs.WriteFile("/etc/apt/sources.list.d/ceph.list", []byte(cephContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Proxmox Ceph repository: %w", err)
	}

	// Write Enterprise repository
	var enterpriseContent strings.Builder
	for _, repo := range sources.ProxmoxEnterpriseRepo {
		enterpriseContent.WriteString(strings.ReplaceAll(repo, "CODENAME", r.osCodename))
		enterpriseContent.WriteString("\n")
	}
	
	if err := r.fs.WriteFile("/etc/apt/sources.list.d/pve-enterprise.list", []byte(enterpriseContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Proxmox Enterprise repository: %w", err)
	}

	return nil
}

// IsPackageInstalled checks if a package is installed
func (r *OSPackageRepository) IsPackageInstalled(packageName string) (bool, error) {
	if r.osType == "alpine" {
		// Alpine method
		_, err := r.commander.Execute("apk", "info", "-e", packageName)
		if err != nil {
			return false, nil // Package not installed
		}
		return true, nil
	} else {
		// Debian/Ubuntu method
		_, err := r.commander.Execute("dpkg", "-l", packageName)
		if err != nil {
			return false, nil // Package not installed
		}
		return true, nil
	}
}

// GetPackageSources retrieves the current package sources configuration
func (r *OSPackageRepository) GetPackageSources() (*model.PackageSources, error) {
	// Return the injected configuration
	return r.config, nil
}

// holdProxmoxPackages holds Proxmox packages to prevent accidental removal
func (r *OSPackageRepository) holdProxmoxPackages() error {
	packages := []string{"proxmox-archive-keyring", "proxmox-backup-client", "proxmox-ve", "pve-kernel"}
	
	for _, pkg := range packages {
		_, err := r.commander.Execute("apt-mark", "hold", pkg)
		if err != nil {
			// Non-fatal, just log and continue
			fmt.Printf("Warning: Failed to hold package %s: %v\n", pkg, err)
		}
	}
	
	return nil
}

// unholdProxmoxPackages releases held Proxmox packages
func (r *OSPackageRepository) unholdProxmoxPackages() error {
	packages := []string{"proxmox-archive-keyring", "proxmox-backup-client", "proxmox-ve", "pve-kernel"}
	
	for _, pkg := range packages {
		_, err := r.commander.Execute("apt-mark", "unhold", pkg)
		if err != nil {
			// Non-fatal, just log and continue
			fmt.Printf("Warning: Failed to unhold package %s: %v\n", pkg, err)
		}
	}
	
	return nil
}