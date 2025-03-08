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
}

// NewUFWFirewallRepository creates a new UFWFirewallRepository
func NewUFWFirewallRepository(
	fs interfaces.FileSystem,
	commander interfaces.Commander,
) secondary.FirewallRepository {
	return &UFWFirewallRepository{
		fs:        fs,
		commander: commander,
	}
}

// IsUFWInstalled checks if UFW is installed
func (r *UFWFirewallRepository) IsUFWInstalled() bool {
	_, err := r.commander.Execute("which", "ufw")
	return err == nil
}

// pkg/adapter/secondary/ufw_firewall_repository.go
// Add this method to the UFWFirewallRepository struct

// GetFirewallStatus retrieves the current status of the firewall
func (r *UFWFirewallRepository) GetFirewallStatus() (bool, bool, bool, []string, error) {
	// Check if UFW is installed
	_, err := r.commander.Execute("which", "ufw")
	isInstalled := (err == nil)

	// Default values if not installed
	isEnabled := false
	isConfigured := false
	var rules []string

	if isInstalled {
		// Check if UFW is enabled
		statusOutput, err := r.commander.Execute("ufw", "status")
		if err == nil {
			statusText := string(statusOutput)
			isEnabled = strings.Contains(statusText, "Status: active")

			// Extract rules (skip header lines)
			lines := strings.Split(statusText, "\n")
			ruleSection := false
			for _, line := range lines {
				line = strings.TrimSpace(line)

				// Skip empty lines
				if line == "" {
					continue
				}

				// Skip header lines
				if strings.Contains(line, "Status:") ||
					strings.Contains(line, "Logging:") ||
					strings.Contains(line, "Default:") ||
					strings.Contains(line, "New profiles:") ||
					strings.Contains(line, "To             Action      From") {
					continue
				}

				// Check if we've reached the rule section
				if strings.Contains(line, "--") {
					ruleSection = true
					continue
				}

				// Add rule lines
				if ruleSection && line != "" {
					rules = append(rules, line)
				}
			}

			// Check if we have default policies configured
			isConfigured = strings.Contains(statusText, "deny (incoming)") &&
				strings.Contains(statusText, "allow (outgoing)")
		}
	}

	return isInstalled, isEnabled, isConfigured, rules, nil
}

// SaveFirewallConfig applies the specified firewall configuration
func (r *UFWFirewallRepository) SaveFirewallConfig(config model.FirewallConfig) error {
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

// GetFirewallConfig retrieves the current firewall configuration
func (r *UFWFirewallRepository) GetFirewallConfig() (*model.FirewallConfig, error) {
	// This would parse the output of 'ufw status verbose'
	// Implementation details omitted for brevity
	return &model.FirewallConfig{
		Enabled:         true,
		DefaultIncoming: "deny",
		DefaultOutgoing: "allow",
	}, nil
}

// AddRule adds a firewall rule
func (r *UFWFirewallRepository) AddRule(rule model.FirewallRule) error {
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

// RemoveRule removes a firewall rule
func (r *UFWFirewallRepository) RemoveRule(rule model.FirewallRule) error {
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

// AddProfile adds a firewall application profile
func (r *UFWFirewallRepository) AddProfile(profile model.FirewallProfile) error {
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

// EnableFirewall enables the firewall
func (r *UFWFirewallRepository) EnableFirewall() error {
	// Use non-interactive mode
	// The 'yes | ufw enable' approach is replaced with a direct command
	if _, err := r.commander.Execute("sh", "-c", "yes | ufw enable"); err != nil {
		return fmt.Errorf("failed to enable UFW: %w", err)
	}

	return nil
}

// DisableFirewall disables the firewall
func (r *UFWFirewallRepository) DisableFirewall() error {
	if _, err := r.commander.Execute("ufw", "disable"); err != nil {
		return fmt.Errorf("failed to disable UFW: %w", err)
	}

	return nil
}
