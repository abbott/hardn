// pkg/domain/model/hardening_config.go
package model

// HardeningConfig represents a comprehensive system hardening configuration
type HardeningConfig struct {
    // User settings
    CreateUser     bool
    Username       string
    SudoNoPassword bool
    SshKeys        []string
    
    // SSH settings
    SshPort            int
    SshListenAddresses []string
    SshAllowedUsers    []string
    SshKeyPaths        []string
    
    // Firewall settings
    EnableFirewall bool
    AllowedPorts   []int
    
    // DNS settings
    ConfigureDns bool
    Nameservers  []string
    
    // Feature toggles
    EnableAppArmor           bool
    EnableLynis              bool
    EnableUnattendedUpgrades bool
}