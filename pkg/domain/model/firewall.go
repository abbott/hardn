// pkg/domain/model/firewall.go
package model

// FirewallRule represents a firewall rule
type FirewallRule struct {
	Action      string // allow, deny
	Protocol    string // tcp, udp, icmp
	Port        int
	SourceIP    string // source IP or subnet
	Description string
}

// FirewallProfile represents a firewall application profile
type FirewallProfile struct {
	Name        string
	Title       string
	Description string
	Ports       []string // formatted as "port/protocol"
}

// FirewallConfig represents the full firewall configuration
type FirewallConfig struct {
	Enabled             bool
	DefaultIncoming     string // allow, deny
	DefaultOutgoing     string // allow, deny
	Rules               []FirewallRule
	ApplicationProfiles []FirewallProfile
}
