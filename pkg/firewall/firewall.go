package firewall

import (
	"fmt"
	"os/exec"
	"strconv"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
)

// ConfigureUFW sets up the Uncomplicated Firewall with the specified configuration
func ConfigureUFW(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		utils.LogInfo("[DRY-RUN] Configure UFW firewall with the following settings:")
		utils.LogInfo("[DRY-RUN] - Set default incoming policy: %s", cfg.UfwDefaultIncomingPolicy)
		utils.LogInfo("[DRY-RUN] - Set default outgoing policy: %s", cfg.UfwDefaultOutgoingPolicy)
		utils.LogInfo("[DRY-RUN] - Allow the following ports:")
		for _, port := range cfg.UfwAllowedPorts {
			utils.LogInfo("[DRY-RUN]   * %d/tcp", port)
		}
		utils.LogInfo("[DRY-RUN] - Enable UFW firewall")
		if osInfo.OsType == "alpine" {
			utils.LogInfo("[DRY-RUN] - Configure UFW to start on boot using OpenRC")
		}
		return nil
	}

	utils.LogInfo("Configuring UFW firewall...")

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

	// Set default policies
	defaultInCmd := exec.Command("ufw", "default", cfg.UfwDefaultIncomingPolicy, "incoming")
	if err := defaultInCmd.Run(); err != nil {
		return fmt.Errorf("failed to set default incoming policy: %w", err)
	}

	defaultOutCmd := exec.Command("ufw", "default", cfg.UfwDefaultOutgoingPolicy, "outgoing")
	if err := defaultOutCmd.Run(); err != nil {
		return fmt.Errorf("failed to set default outgoing policy: %w", err)
	}

	// Allow ports
	for _, port := range cfg.UfwAllowedPorts {
		portStr := strconv.Itoa(port)
		allowCmd := exec.Command("ufw", "allow", portStr+"/tcp")
		if err := allowCmd.Run(); err != nil {
			utils.LogError("Failed to allow port %s/tcp: %v", portStr, err)
		}
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
			utils.LogError("Failed to add UFW to boot services: %v", err)
		}

		startCmd := exec.Command("rc-service", "ufw", "start")
		if err := startCmd.Run(); err != nil {
			utils.LogError("Failed to start UFW service: %v", err)
		}
	}

	utils.LogSuccess("UFW configured and enabled with firewall rules")
	return nil
}

// IsUfwEnabled checks if UFW is enabled
func IsUfwEnabled() (bool, error) {
	cmd := exec.Command("ufw", "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("failed to get UFW status: %w", err)
	}

	return fmt.Sprintf("%s", output) != "Status: inactive", nil
}