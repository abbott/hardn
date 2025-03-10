// pkg/application/menu_manager.go
package application

import (
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MenuManager orchestrates menu-related operations
type MenuManager struct {
	userManager        *UserManager
	sshManager         *SSHManager
	firewallManager    *FirewallManager
	dnsManager         *DNSManager
	packageManager     *PackageManager
	backupManager      *BackupManager
	securityManager    *SecurityManager
	environmentManager *EnvironmentManager
	logsManager        *LogsManager
	hostInfoManager    *HostInfoManager
}

// In the struct definition:
func NewMenuManager(
	userManager *UserManager,
	sshManager *SSHManager,
	firewallManager *FirewallManager,
	dnsManager *DNSManager,
	packageManager *PackageManager,
	backupManager *BackupManager,
	securityManager *SecurityManager,
	environmentManager *EnvironmentManager,
	logsManager *LogsManager,
	hostInfoManager *HostInfoManager,
) *MenuManager {
	return &MenuManager{
		userManager:        userManager,
		sshManager:         sshManager,
		firewallManager:    firewallManager,
		dnsManager:         dnsManager,
		packageManager:     packageManager,
		backupManager:      backupManager,
		securityManager:    securityManager,
		environmentManager: environmentManager,
		logsManager:        logsManager,
		hostInfoManager:    hostInfoManager, // Add this field
	}
}

// Methods for handling menu operations
// func (m *MenuManager) CreateUser(username string, hasSudo bool, sshKeys []string) error {
//     return m.userManager.CreateUser(username, hasSudo, true, sshKeys)
// }

// CreateUser creates a user with the specified settings
func (m *MenuManager) CreateUser(username string, hasSudo bool, sudoNoPassword bool, sshKeys []string) error {
	// Create the user
	err := m.userManager.CreateUser(username, hasSudo, sudoNoPassword, sshKeys)
	if err != nil {
		return err
	}

	// If SSH keys are provided, ensure they're added
	for _, key := range sshKeys {
		if err := m.sshManager.AddSSHKey(username, key); err != nil {
			return fmt.Errorf("error adding SSH key: %w", err)
		}
	}

	return nil
}

// AddSSHKey adds an SSH key for the specified user
func (m *MenuManager) AddSSHKey(username, publicKey string) error {
	return m.sshManager.AddSSHKey(username, publicKey)
}

// DisableRootSsh disables SSH access for the root user
func (m *MenuManager) DisableRootSsh() error {
	return m.sshManager.DisableRootAccess()
}

// HardenSystem applies comprehensive system hardening
func (m *MenuManager) HardenSystem(config *model.HardeningConfig) error {
	return m.securityManager.HardenSystem(config)
}

// ConfigureDNS configures DNS with the specified nameservers
func (m *MenuManager) ConfigureDNS(nameservers []string, domain string) error {
	return m.dnsManager.ConfigureDNS(nameservers, domain)
}

// ConfigureSecureFirewall configures the firewall with secure settings
func (m *MenuManager) ConfigureSecureFirewall(sshPort int, allowedPorts []int, profiles []model.FirewallProfile) error {
	return m.firewallManager.ConfigureSecureFirewall(sshPort, allowedPorts, profiles)
}

// InstallLinuxPackages installs Linux packages based on the specified type
func (m *MenuManager) InstallLinuxPackages(packages []string, packageType string) error {
	return m.packageManager.InstallLinuxPackages(packages, packageType)
}

// InstallPythonPackages installs Python packages
func (m *MenuManager) InstallPythonPackages(systemPackages []string, pipPackages []string, useUv bool) error {
	return m.packageManager.InstallPythonPackages(systemPackages, pipPackages, useUv)
}

// UpdatePackageSources updates package sources configuration
func (m *MenuManager) UpdatePackageSources() error {
	return m.packageManager.UpdatePackageSources()
}

// UpdateProxmoxSources updates Proxmox-specific package sources
func (m *MenuManager) UpdateProxmoxSources() error {
	return m.packageManager.UpdateProxmoxSources()
}

// GetFirewallStatus retrieves the current status of the firewall
func (m *MenuManager) GetFirewallStatus() (bool, bool, bool, []string, error) {
	return m.firewallManager.GetFirewallStatus()
}

// GetBackupStatus returns the backup status and directory
func (m *MenuManager) GetBackupStatus() (bool, string, error) {
	return m.backupManager.GetBackupStatus()
}

// VerifyBackupPath checks if the backup path exists and is writable
func (m *MenuManager) VerifyBackupPath() (bool, error) {
	return m.backupManager.VerifyBackupPath()
}

// ToggleBackups enables or disables backups
func (m *MenuManager) ToggleBackups() error {
	return m.backupManager.ToggleBackups()
}

// SetBackupDirectory changes the backup directory
func (m *MenuManager) SetBackupDirectory(directory string) error {
	return m.backupManager.SetBackupDirectory(directory)
}

// VerifyBackupDirectory ensures the backup directory exists and is writable
func (m *MenuManager) VerifyBackupDirectory() error {
	return m.backupManager.VerifyBackupDirectory()
}

// Add these methods to pkg/application/menu_manager.go

// Add these fields and methods to MenuManager

// Replace the existing methods with these:

// SetupSudoPreservation configures sudo to preserve the HARDN_CONFIG environment variable
func (m *MenuManager) SetupSudoPreservation() error {
	return m.environmentManager.SetupSudoPreservation()
}

// IsSudoPreservationEnabled checks if sudo is configured to preserve the HARDN_CONFIG environment variable
func (m *MenuManager) IsSudoPreservationEnabled() (bool, error) {
	return m.environmentManager.IsSudoPreservationEnabled()
}

// GetEnvironmentConfig retrieves the current environment configuration
func (m *MenuManager) GetEnvironmentConfig() (*model.EnvironmentConfig, error) {
	return m.environmentManager.GetEnvironmentConfig()
}

// PrintLogs prints the log file content to the console
func (m *MenuManager) PrintLogs() error {
	return m.logsManager.PrintLogs()
}

// GetLogConfig retrieves the current log configuration
func (m *MenuManager) GetLogConfig() (*model.LogsConfig, error) {
	return m.logsManager.GetLogConfig()
}

// GetHostInfo retrieves host information
func (m *MenuManager) GetHostInfo() (*model.HostInfo, error) {
	return m.hostInfoManager.GetHostInfo()
}

// GetIPAddresses retrieves system IP addresses
func (m *MenuManager) GetIPAddresses() ([]string, error) {
	return m.hostInfoManager.GetIPAddresses()
}

// GetDNSServers retrieves configured DNS servers
func (m *MenuManager) GetDNSServers() ([]string, error) {
	return m.hostInfoManager.GetDNSServers()
}

// GetHostname retrieves the system hostname and domain
func (m *MenuManager) GetHostname() (string, string, error) {
	return m.hostInfoManager.GetHostname()
}

// GetNonSystemUsers retrieves non-system users
func (m *MenuManager) GetNonSystemUsers() ([]model.User, error) {
	return m.hostInfoManager.GetNonSystemUsers()
}

// GetNonSystemGroups retrieves non-system groups
func (m *MenuManager) GetNonSystemGroups() ([]string, error) {
	return m.hostInfoManager.GetNonSystemGroups()
}

// GetUptime retrieves the system uptime
func (m *MenuManager) GetUptime() (time.Duration, error) {
	return m.hostInfoManager.GetUptime()
}

// FormatUptime formats the uptime in a human-readable format
func (m *MenuManager) FormatUptime(uptime time.Duration) string {
	return m.hostInfoManager.FormatUptime(uptime)
}

// FormatBytes formats byte size to human readable format
func (m *MenuManager) FormatBytes(bytes int64) string {
	return m.hostInfoManager.FormatBytes(bytes)
}
