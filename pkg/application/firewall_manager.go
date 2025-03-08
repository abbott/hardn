// pkg/application/firewall_manager.go
package application

import (
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// FirewallManager is an application service for firewall configuration
type FirewallManager struct {
	firewallService service.FirewallService
}

// NewFirewallManager creates a new FirewallManager
func NewFirewallManager(firewallService service.FirewallService) *FirewallManager {
	return &FirewallManager{
		firewallService: firewallService,
	}
}

// ConfigureFirewall applies a complete firewall configuration
func (m *FirewallManager) ConfigureFirewall(
	defaultIncoming string,
	defaultOutgoing string,
	rules []model.FirewallRule,
	profiles []model.FirewallProfile,
) error {
	config := model.FirewallConfig{
		Enabled:             true,
		DefaultIncoming:     defaultIncoming,
		DefaultOutgoing:     defaultOutgoing,
		Rules:               rules,
		ApplicationProfiles: profiles,
	}

	return m.firewallService.ConfigureFirewall(config)
}

// ConfigureSecureFirewall sets up a firewall with secure defaults
func (m *FirewallManager) ConfigureSecureFirewall(sshPort int, allowedPorts []int, profiles []model.FirewallProfile) error {
	// Create default SSH rule
	sshRule := model.FirewallRule{
		Action:      "allow",
		Protocol:    "tcp",
		Port:        sshPort,
		SourceIP:    "",
		Description: "SSH access",
	}

	// Create additional rules for allowed ports
	var rules []model.FirewallRule
	rules = append(rules, sshRule)

	for _, port := range allowedPorts {
		rule := model.FirewallRule{
			Action:      "allow",
			Protocol:    "tcp",
			Port:        port,
			SourceIP:    "",
			Description: "Custom allowed port",
		}
		rules = append(rules, rule)
	}

	// Create default configuration
	config := model.FirewallConfig{
		Enabled:             true,
		DefaultIncoming:     "deny",
		DefaultOutgoing:     "allow",
		Rules:               rules,
		ApplicationProfiles: profiles, // Use the profiles parameter here
	}

	return m.firewallService.ConfigureFirewall(config)
}

// AddSSHRule adds a rule to allow SSH access
func (m *FirewallManager) AddSSHRule(port int) error {
	rule := model.FirewallRule{
		Action:      "allow",
		Protocol:    "tcp",
		Port:        port,
		SourceIP:    "",
		Description: "SSH access",
	}

	return m.firewallService.AddRule(rule)
}

// EnableFirewall enables the firewall
func (m *FirewallManager) EnableFirewall() error {
	return m.firewallService.EnableFirewall()
}

// DisableFirewall disables the firewall
func (m *FirewallManager) DisableFirewall() error {
	return m.firewallService.DisableFirewall()
}

// GetFirewallStatus retrieves the current status of the firewall
func (m *FirewallManager) GetFirewallStatus() (bool, bool, bool, []string, error) {
	return m.firewallService.GetFirewallStatus()
}
