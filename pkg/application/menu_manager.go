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
		hostInfoManager:    hostInfoManager,
	}
}

// create a user with the specified settings
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

// add an SSH key for the specified user
func (m *MenuManager) AddSSHKey(username, publicKey string) error {
	return m.sshManager.AddSSHKey(username, publicKey)
}

// disable SSH access for the root user
func (m *MenuManager) DisableRootSsh() error {
	return m.sshManager.DisableRootAccess()
}

// apply comprehensive system hardening
func (m *MenuManager) HardenSystem(config *model.HardeningConfig) error {
	return m.securityManager.HardenSystem(config)
}

// configure DNS with the specified nameservers
func (m *MenuManager) ConfigureDNS(nameservers []string, domain string) error {
	return m.dnsManager.ConfigureDNS(nameservers, domain)
}

// configure the firewall with secure settings
func (m *MenuManager) ConfigureSecureFirewall(sshPort int, allowedPorts []int, profiles []model.FirewallProfile) error {
	return m.firewallManager.ConfigureSecureFirewall(sshPort, allowedPorts, profiles)
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

// retrieve the current status of the firewall
func (m *MenuManager) GetFirewallStatus() (bool, bool, bool, []string, error) {
	return m.firewallManager.GetFirewallStatus()
}

// return the backup status and directory
func (m *MenuManager) GetBackupStatus() (bool, string, error) {
	return m.backupManager.GetBackupStatus()
}

// check if the backup path exists and is writable
func (m *MenuManager) VerifyBackupPath() (bool, error) {
	return m.backupManager.VerifyBackupPath()
}

// enable or disables backups
func (m *MenuManager) ToggleBackups() error {
	return m.backupManager.ToggleBackups()
}

// change the backup directory
func (m *MenuManager) SetBackupDirectory(directory string) error {
	return m.backupManager.SetBackupDirectory(directory)
}

// ensure the backup directory exists and is writable
func (m *MenuManager) VerifyBackupDirectory() error {
	return m.backupManager.VerifyBackupDirectory()
}

// configure sudo to preserve the HARDN_CONFIG environment variable
func (m *MenuManager) SetupSudoPreservation() error {
	return m.environmentManager.SetupSudoPreservation()
}

// check if sudo is configured to preserve the HARDN_CONFIG environment variable
func (m *MenuManager) IsSudoPreservationEnabled() (bool, error) {
	return m.environmentManager.IsSudoPreservationEnabled()
}

// retrieve the current environment configuration
func (m *MenuManager) GetEnvironmentConfig() (*model.EnvironmentConfig, error) {
	return m.environmentManager.GetEnvironmentConfig()
}

// print the log file content to the console
func (m *MenuManager) PrintLogs() error {
	return m.logsManager.PrintLogs()
}

// retrieve the current log configuration
func (m *MenuManager) GetLogConfig() (*model.LogsConfig, error) {
	return m.logsManager.GetLogConfig()
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

// retrieve non-system users
func (m *MenuManager) GetNonSystemUsers() ([]model.User, error) {
	return m.hostInfoManager.GetNonSystemUsers()
}

// retrieve non-system groups
func (m *MenuManager) GetNonSystemGroups() ([]string, error) {
	return m.hostInfoManager.GetNonSystemGroups()
}

// retrieve the system uptime
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
