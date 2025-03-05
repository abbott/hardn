package firewall

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
)

// ConfigureUFW sets up the Uncomplicated Firewall with the specified configuration
func ConfigureUFW(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Configure UFW firewall:")
		logging.LogInfo("[DRY-RUN] - Enable UFW firewall with default policies (deny incoming, allow outgoing)")

		// Show SSH port policy
		logging.LogInfo("[DRY-RUN] - Allow SSH on port %d/tcp", cfg.SshPort)

		if osInfo.OsType == "alpine" {
			logging.LogInfo("[DRY-RUN] - Configure UFW to start on boot using OpenRC")
		}

		// Show security recommendation if using default SSH port
		if cfg.SshPort == 22 {
			logging.LogInfo("[DRY-RUN] - SECURITY RECOMMENDATION: You are using the default SSH port (22)")
			logging.LogInfo("[DRY-RUN] - Consider setting a non-standard SSH port (e.g., 2208) in your configuration file")
			logging.LogInfo("[DRY-RUN] - This can help reduce automated SSH attacks targeting the default port")
		}

		// Log application profiles
		WriteUfwAppProfiles(cfg, osInfo)
		return nil
	}

	logging.LogInfo("Configuring UFW firewall...")

	// Show security recommendation if using the default SSH port
	if cfg.SshPort == 22 {
		logging.LogInfo("SECURITY RECOMMENDATION: You are using the default SSH port (22)")
		logging.LogInfo("Consider setting a non-standard SSH port (e.g., 2208) in your configuration file")
		logging.LogInfo("This can help reduce automated SSH attacks targeting the default port")
	}

	// Install UFW if not already installed
	ufwInstalled := false
	if _, err := exec.LookPath("ufw"); err == nil {
		ufwInstalled = true
	} else {
		if osInfo.OsType == "alpine" {
			// Install UFW with apk
			cmd := exec.Command("apk", "add", "ufw")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to install UFW on Alpine: %w", err)
			}
			ufwInstalled = true
		} else {
			// Install UFW with apt
			cmd := exec.Command("apt-get", "install", "-y", "ufw")
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("failed to install UFW on Debian/Ubuntu: %w", err)
			}
			ufwInstalled = true
		}
	}

	if !ufwInstalled {
		return fmt.Errorf("failed to install or find UFW")
	}

	// Set default policies (always deny incoming, allow outgoing)
	defaultInCmd := exec.Command("ufw", "default", "deny", "incoming")
	output, err := defaultInCmd.CombinedOutput() // Capture both stdout and stderr
	if err != nil {
		logging.LogError("Failed to set default incoming policy: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to set default incoming policy in UFW: %w", err)
	}
	logging.LogSuccess("Set default incoming policy to deny")

	defaultOutCmd := exec.Command("ufw", "default", "allow", "outgoing")
	output, err = defaultOutCmd.CombinedOutput() // Capture both stdout and stderr
	if err != nil {
		logging.LogError("Failed to set default outgoing policy: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to set default outgoing policy in UFW: %w", err)
	}
	logging.LogSuccess("Set default outgoing policy to allow")

	sshPortStr := strconv.Itoa(cfg.SshPort)
	sshAllowCmd := exec.Command("ufw", "allow", sshPortStr+"/tcp", "comment", "SSH")
	if err := sshAllowCmd.Run(); err != nil {
		logging.LogError("Failed to allow SSH port %s/tcp: %v", sshPortStr, err)
		return fmt.Errorf("failed to create UFW rule to allow SSH on port %s/tcp: %w", sshPortStr, err)
	} else {
		logging.LogSuccess("Configured UFW rule for SSH on port %s/tcp", sshPortStr)
	}

	// Configure application profiles
	if err := WriteUfwAppProfiles(cfg, osInfo); err != nil {
		logging.LogError("Failed to configure UFW application profiles: %v", err)
		// Continue with firewall setup even if app profiles fail
	}

	// Enable UFW
	enableCmd := exec.Command("ufw", "enable")
	// Force non-interactive mode for 'ufw enable'
	enableCmd.Env = append(enableCmd.Env, "DEBIAN_FRONTEND=noninteractive")
	// The 'yes' command pipes "y" to ufw enable, which would normally prompt for confirmation
	enableCmd = exec.Command("sh", "-c", "yes | ufw enable")
	if err := enableCmd.Run(); err != nil {
		return fmt.Errorf("failed to enable UFW: %w", err)
	}

	// Configure boot service on Alpine
	if osInfo.OsType == "alpine" {
		bootCmd := exec.Command("rc-update", "add", "ufw", "default")
		if err := bootCmd.Run(); err != nil {
			logging.LogError("Failed to add UFW to Alpine boot services: %v", err)
		}

		startCmd := exec.Command("rc-service", "ufw", "start")
		if err := startCmd.Run(); err != nil {
			logging.LogError("Failed to start UFW service on Alpine: %v", err)
		}
	}

	logging.LogSuccess("UFW configured and enabled with firewall rules")
	return nil
}

// WriteUfwAppProfiles writes user-defined UFW application profiles
func WriteUfwAppProfiles(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Write UFW application profiles to /etc/ufw/applications.d/hardn")

		// Check if we need to create a default SSH profile
		if len(cfg.UfwAppProfiles) == 0 && cfg.SshPort != 0 {
			logging.LogInfo("[DRY-RUN] - No application profiles defined, creating default SSH profile")
			logging.LogInfo("[DRY-RUN] - SSH profile for port %d/tcp", cfg.SshPort)
		} else if len(cfg.UfwAppProfiles) > 0 {
			for _, profile := range cfg.UfwAppProfiles {
				logging.LogInfo("[DRY-RUN] - Profile: %s (%s)", profile.Name, profile.Title)
				logging.LogInfo("[DRY-RUN]   Description: %s", profile.Description)
				logging.LogInfo("[DRY-RUN]   Ports: %s", strings.Join(profile.Ports, ", "))
			}
		}
		return nil
	}

	// If there are no profiles to write, return
	if len(cfg.UfwAppProfiles) == 0 {
		logging.LogInfo("No UFW application profiles to configure")
		return nil
	}

	logging.LogInfo("Writing UFW application profiles...")

	// Create applications.d directory if it doesn't exist
	if err := os.MkdirAll("/etc/ufw/applications.d", 0755); err != nil {
		return fmt.Errorf("failed to create UFW applications directory: %w", err)
	}

	// Backup existing profiles file if it exists
	utils.BackupFile("/etc/ufw/applications.d/hardn", cfg)

	// Create content for UFW applications file
	var content strings.Builder
	for _, profile := range cfg.UfwAppProfiles {
		content.WriteString(fmt.Sprintf("[%s]\n", profile.Name))
		content.WriteString(fmt.Sprintf("title=%s\n", profile.Title))
		content.WriteString(fmt.Sprintf("description=%s\n", profile.Description))
		content.WriteString(fmt.Sprintf("ports=%s\n", strings.Join(profile.Ports, ",")))
		content.WriteString("\n")
	}

	// Write the file
	if err := os.WriteFile("/etc/ufw/applications.d/hardn", []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write UFW application profiles file: %w", err)
	}

	// Apply the profiles
	for _, profile := range cfg.UfwAppProfiles {
		allowCmd := exec.Command("ufw", "allow", fmt.Sprintf("from any to any app '%s'", profile.Name))
		if err := allowCmd.Run(); err != nil {
			logging.LogError("Failed to enable UFW application profile %s: %v", profile.Name, err)
			return fmt.Errorf("failed to enable UFW application profile %s: %w", profile.Name, err)
		} else {
			logging.LogSuccess("Enabled UFW application profile: %s", profile.Name)
		}
	}

	logging.LogSuccess("UFW application profiles configured")
	return nil
}