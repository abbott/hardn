// pkg/port/secondary/firewall_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// FirewallRepository defines the interface for firewall configuration operations
type FirewallRepository interface {

		// GetFirewallStatus retrieves the current status of the firewall
		GetFirewallStatus() (isInstalled bool, isEnabled bool, isConfigured bool, rules []string, err error)
		
    // SaveFirewallConfig persists the firewall configuration
    SaveFirewallConfig(config model.FirewallConfig) error
    
    // GetFirewallConfig retrieves the current firewall configuration
    GetFirewallConfig() (*model.FirewallConfig, error)
    
    // AddRule adds a firewall rule
    AddRule(rule model.FirewallRule) error
    
    // RemoveRule removes a firewall rule
    RemoveRule(rule model.FirewallRule) error
    
    // AddProfile adds a firewall application profile
    AddProfile(profile model.FirewallProfile) error
    
    // EnableFirewall enables the firewall
    EnableFirewall() error
    
    // DisableFirewall disables the firewall
    DisableFirewall() error
}