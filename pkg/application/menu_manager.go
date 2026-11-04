// pkg/application/menu_manager.go
package application

import (
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MenuManager orchestrates menu-related operations
type MenuManager struct {
	packageManager  *PackageManager
	hostInfoManager *HostInfoManager
}

// NewMenuManager returns a MenuManager over the package and host-info
// managers.
func NewMenuManager(
	packageManager *PackageManager,
	hostInfoManager *HostInfoManager,
) *MenuManager {
	return &MenuManager{
		packageManager:  packageManager,
		hostInfoManager: hostInfoManager,
	}
}

// install Linux packages based on the specified type
func (m *MenuManager) InstallLinuxPackages(packages []string, packageType string) error {
	return m.packageManager.InstallLinuxPackages(packages, packageType)
}

// install Python packages
func (m *MenuManager) InstallPythonPackages(systemPackages []string, pipPackages []string, useUv bool) error {
	return m.packageManager.InstallPythonPackages(systemPackages, pipPackages, useUv)
}

// update package sources configuration
func (m *MenuManager) UpdatePackageSources() error {
	return m.packageManager.UpdatePackageSources()
}

// update Proxmox-specific package sources
func (m *MenuManager) UpdateProxmoxSources() error {
	return m.packageManager.UpdateProxmoxSources()
}

// retrieve host information
func (m *MenuManager) GetHostInfo() (*model.HostInfo, error) {
	return m.hostInfoManager.GetHostInfo()
}

// retrieve system IP addresses
func (m *MenuManager) GetIPAddresses() ([]string, error) {
	return m.hostInfoManager.GetIPAddresses()
}

// retrieve configured DNS servers
func (m *MenuManager) GetDNSServers() ([]string, error) {
	return m.hostInfoManager.GetDNSServers()
}

// retrieve the system hostname and domain
func (m *MenuManager) GetHostname() (string, string, error) {
	return m.hostInfoManager.GetHostname()
}

// retrieve non-system groups
func (m *MenuManager) GetNonSystemGroups() ([]string, error) {
	return m.hostInfoManager.GetNonSystemGroups()
}

// GetUptime returns the host's uptime
func (m *MenuManager) GetUptime() (time.Duration, error) {
	return m.hostInfoManager.GetUptime()
}

// format the uptime in a human-readable format
func (m *MenuManager) FormatUptime(uptime time.Duration) string {
	return m.hostInfoManager.FormatUptime(uptime)
}

// format byte size to human readable format
func (m *MenuManager) FormatBytes(bytes int64) string {
	return m.hostInfoManager.FormatBytes(bytes)
}
