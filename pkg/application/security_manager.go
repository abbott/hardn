// pkg/application/security_manager.go
package application

import (
    "github.com/abbott/hardn/pkg/domain/model"
)

// SecurityManager provides high-level security operations combining multiple services
type SecurityManager struct {
    userManager     *UserManager
    sshManager      *SSHManager
    firewallManager *FirewallManager
    dnsManager      *DNSManager
}

// NewSecurityManager creates a new SecurityManager
func NewSecurityManager(
    userManager *UserManager,
    sshManager *SSHManager,
    firewallManager *FirewallManager,
    dnsManager *DNSManager,
) *SecurityManager {
    return &SecurityManager{
        userManager:     userManager,
        sshManager:      sshManager,
        firewallManager: firewallManager,
        dnsManager:      dnsManager,
    }
}

// HardenSystem applies comprehensive system hardening
func (m *SecurityManager) HardenSystem(config *model.HardeningConfig) error {
    // Create non-root user if requested
    if config.CreateUser && config.Username != "" {
        if err := m.userManager.CreateUser(
            config.Username,
            true,
            config.SudoNoPassword,
            config.SshKeys,
        ); err != nil {
            return err
        }
    }
    
    // Configure SSH with secure settings
    if err := m.sshManager.ConfigureSSH(
        config.SshPort,
        config.SshListenAddresses,
        false, // Never allow root login
        config.SshAllowedUsers,
        config.SshKeyPaths,
    ); err != nil {
        return err
    }
    
    // Configure firewall
    if config.EnableFirewall {
        if err := m.firewallManager.ConfigureSecureFirewall(
            config.SshPort,
            config.AllowedPorts,
        ); err != nil {
            return err
        }
    }
    
    // Configure DNS if enabled
    if config.ConfigureDns {
        if err := m.dnsManager.ConfigureDNS(
            config.Nameservers,
            "lan",
        ); err != nil {
            return err
        }
    }
    
    return nil
}