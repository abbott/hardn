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
    securityManager *SecurityManager
}

func NewMenuManager(
    userManager *UserManager,
    sshManager *SSHManager,
    firewallManager *FirewallManager,
    dnsManager *DNSManager,
    securityManager *SecurityManager,
) *MenuManager {
    return &MenuManager{
        userManager:     userManager,
        sshManager:      sshManager,
        firewallManager: firewallManager,
        dnsManager:      dnsManager,
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

func (m *MenuManager) DisableRootSsh() error {
    return m.sshManager.DisableRootAccess()
}

func (m *MenuManager) HardenSystem(config *model.HardeningConfig) error {
	return m.securityManager.HardenSystem(config)
}

func (m *MenuManager) ConfigureDNS(nameservers []string, domain string) error {
	return m.dnsManager.ConfigureDNS(nameservers, domain)
}

func (m *MenuManager) ConfigureFirewall(sshPort int, allowedPorts []int) error {
	return m.firewallManager.ConfigureSecureFirewall(sshPort, allowedPorts)
}

