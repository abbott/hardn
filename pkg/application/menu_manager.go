// pkg/application/menu_manager.go
package application

import (
	"fmt"

	"github.com/abbott/hardn/pkg/domain/model"
)
// MenuManager orchestrates menu-related operations
type MenuManager struct {
	userManager     *UserManager
	sshManager      *SSHManager
	firewallManager *FirewallManager
	dnsManager      *DNSManager
	packageManager  *PackageManager
	securityManager *SecurityManager
}

func NewMenuManager(
	userManager *UserManager,
	sshManager *SSHManager,
	firewallManager *FirewallManager,
	dnsManager *DNSManager,
	packageManager *PackageManager,
	securityManager *SecurityManager,
) *MenuManager {
	return &MenuManager{
			userManager:     userManager,
			sshManager:      sshManager,
			firewallManager: firewallManager,
			dnsManager:      dnsManager,
			packageManager:  packageManager,
			securityManager: securityManager,
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

// ConfigureFirewall configures the firewall with secure settings
func (m *MenuManager) ConfigureFirewall(sshPort int, allowedPorts []int) error {
	return m.firewallManager.ConfigureSecureFirewall(sshPort, allowedPorts)
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