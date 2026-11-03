// pkg/adapter/secondary/ufw_firewall_repository.go
package secondary

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// UFWFirewallRepository implements FirewallRepository using UFW
type UFWFirewallRepository struct {
	fs        interfaces.FileSystem
	commander interfaces.Commander
	pve       *ProxmoxHost
}

// NewUFWFirewallRepository creates a new UFWFirewallRepository. pve says
// whether the host is Proxmox VE; nil makes the repository check for
// itself.
func NewUFWFirewallRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
	pve *ProxmoxHost,
) secondary.FirewallRepository {
	return &UFWFirewallRepository{
		fs:        fs,
		commander: commander,
		pve:       proxmoxOrProbe(pve, fs, commander, ""),
	}
}

// check if UFW is installed
func (r *UFWFirewallRepository) IsUFWInstalled() bool {
	_, err := r.commander.Execute("which", "ufw")
	return err == nil
}

// SaveFirewallConfig applies the specified firewall configuration. On
// Proxmox VE it is refused before any ufw command (proxmoxTreatedAsDebian).
func (r *UFWFirewallRepository) SaveFirewallConfig(config model.FirewallConfig) error {
	if r.pve.IsProxmox() {
		return errUFWOnProxmox
	}

	// Ensure UFW is installed
	if !r.IsUFWInstalled() {
		return fmt.Errorf("UFW firewall is not installed")
	}

	// Set default policies
	if _, err := r.commander.Execute("ufw", "default", config.DefaultIncoming, "incoming"); err != nil {
		return fmt.Errorf("failed to set incoming policy: %w", err)
	}

	if _, err := r.commander.Execute("ufw", "default", config.DefaultOutgoing, "outgoing"); err != nil {
		return fmt.Errorf("failed to set outgoing policy: %w", err)
	}

	// Reset rules (disable and enable later)
	if _, err := r.commander.Execute("ufw", "disable"); err != nil {
		return fmt.Errorf("failed to disable UFW: %w", err)
	}

	// Reset rules
	if _, err := r.commander.Execute("ufw", "reset"); err != nil {
		return fmt.Errorf("failed to reset UFW rules: %w", err)
	}

	// Apply application profiles
	if err := r.applyAppProfiles(config.ApplicationProfiles); err != nil {
		return err
	}

	// Add rules
	for _, rule := range config.Rules {
		if err := r.AddRule(rule); err != nil {
			return err
		}
	}

	// Enable firewall if configured
	if config.Enabled {
		if err := r.EnableFirewall(); err != nil {
			return err
		}
	}

	return nil
}

// retrieve the current firewall configuration
func (r *UFWFirewallRepository) GetFirewallConfig() (*model.FirewallConfig, error) {
	// parse the output of 'ufw status verbose'
	return &model.FirewallConfig{
		Enabled:         true,
		DefaultIncoming: "deny",
		DefaultOutgoing: "allow",
	}, nil
}

// AddRule adds a firewall rule. On Proxmox VE it is refused
// (proxmoxTreatedAsDebian).
func (r *UFWFirewallRepository) AddRule(rule model.FirewallRule) error {
	if r.pve.IsProxmox() {
		return errUFWOnProxmox
	}
	var args []string

	// Build command arguments
	args = append(args, rule.Action)

	// Add port specification
	portSpec := fmt.Sprintf("%d/%s", rule.Port, rule.Protocol)
	args = append(args, portSpec)

	// Add source IP if specified
	if rule.SourceIP != "" {
		args = append(args, "from", rule.SourceIP)
	}

	// Add description if specified
	if rule.Description != "" {
		args = append(args, "comment", rule.Description)
	}

	// Execute command
	if _, err := r.commander.Execute("ufw", args...); err != nil {
		return fmt.Errorf("failed to add rule %s %s: %w", rule.Action, portSpec, err)
	}

	return nil
}

// RemoveRule removes a firewall rule. On Proxmox VE it is refused
// (proxmoxTreatedAsDebian).
func (r *UFWFirewallRepository) RemoveRule(rule model.FirewallRule) error {
	if r.pve.IsProxmox() {
		return errUFWOnProxmox
	}
	var args []string

	// Build command arguments
	args = append(args, "delete", rule.Action)

	// Add port specification
	portSpec := fmt.Sprintf("%d/%s", rule.Port, rule.Protocol)
	args = append(args, portSpec)

	// Add source IP if specified
	if rule.SourceIP != "" {
		args = append(args, "from", rule.SourceIP)
	}

	// Execute command
	if _, err := r.commander.Execute("ufw", args...); err != nil {
		return fmt.Errorf("failed to remove rule %s %s: %w", rule.Action, portSpec, err)
	}

	return nil
}

// AddProfile adds a firewall application profile. On Proxmox VE it is
// refused (proxmoxTreatedAsDebian).
func (r *UFWFirewallRepository) AddProfile(profile model.FirewallProfile) error {
	if r.pve.IsProxmox() {
		return errUFWOnProxmox
	}
	// Apply a single profile
	return r.applyAppProfiles([]model.FirewallProfile{profile})
}

// applyAppProfiles applies firewall application profiles
func (r *UFWFirewallRepository) applyAppProfiles(profiles []model.FirewallProfile) error {
	if len(profiles) == 0 {
		return nil
	}

	// Create applications directory if it doesn't exist
	appsDir := "/etc/ufw/applications.d"
	if err := r.fs.MkdirAll(appsDir, 0755); err != nil {
		return fmt.Errorf("failed to create UFW applications directory: %w", err)
	}

	// Create profile file
	profilesPath := filepath.Join(appsDir, "hardn")

	var content strings.Builder
	for _, profile := range profiles {
		content.WriteString(fmt.Sprintf("[%s]\n", profile.Name))
		content.WriteString(fmt.Sprintf("title=%s\n", profile.Title))
		content.WriteString(fmt.Sprintf("description=%s\n", profile.Description))
		content.WriteString(fmt.Sprintf("ports=%s\n\n", strings.Join(profile.Ports, ",")))
	}

	// Write profiles file
	if err := r.fs.WriteFile(profilesPath, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write UFW application profiles: %w", err)
	}

	// Apply each profile
	for _, profile := range profiles {
		args := []string{"allow", "from", "any", "to", "any", "app", profile.Name}
		if _, err := r.commander.Execute("ufw", args...); err != nil {
			return fmt.Errorf("failed to apply profile %s: %w", profile.Name, err)
		}
	}

	return nil
}

// EnableFirewall enables the firewall. On Proxmox VE it is refused
// (proxmoxTreatedAsDebian).
func (r *UFWFirewallRepository) EnableFirewall() error {
	if r.pve.IsProxmox() {
		return errUFWOnProxmox
	}
	// Use non-interactive mode
	// The 'yes | ufw enable' approach is replaced with a direct command
	if _, err := r.commander.Execute("sh", "-c", "yes | ufw enable"); err != nil {
		return fmt.Errorf("failed to enable UFW: %w", err)
	}

	return nil
}

// DisableFirewall disables the firewall. It is not refused on Proxmox VE:
// turning UFW off hands the packet filter back to pve-firewall, which is
// the way out for a host where origin/main enabled UFW (V10).
func (r *UFWFirewallRepository) DisableFirewall() error {
	if _, err := r.commander.Execute("ufw", "disable"); err != nil {
		return fmt.Errorf("failed to disable UFW: %w", err)
	}

	return nil
}
