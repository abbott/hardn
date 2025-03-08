package updates

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
)

// SetupUnattendedUpgrades configures automatic system updates
func SetupUnattendedUpgrades(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Configure automatic security updates:")
		if osInfo.OsType == "alpine" {
			logging.LogInfo("[DRY-RUN] - Create daily cron job at /etc/periodic/daily/apk-upgrade")
			logging.LogInfo("[DRY-RUN] - Cron job: apk update && apk upgrade --available")
			logging.LogInfo("[DRY-RUN] - Ensure crond is enabled at boot via OpenRC")
		} else {
			logging.LogInfo("[DRY-RUN] - Install unattended-upgrades package via apt-get")
			logging.LogInfo("[DRY-RUN] - Configure unattended-upgrades via dpkg-reconfigure")
		}
		return nil
	}

	logging.LogInfo("Setting up automatic system updates...")

	if osInfo.OsType == "alpine" {
		logging.LogInfo("Setting up periodic updates for Alpine...")

		// Create daily update script directory if it doesn't exist
		if err := os.MkdirAll("/etc/periodic/daily", 0755); err != nil {
			return fmt.Errorf("failed to create periodic directory for Alpine updates: %w", err)
		}

		// Create update script content
		scriptContent := `#!/bin/sh
apk update && apk upgrade --available
`

		// Write the update script
		if err := os.WriteFile("/etc/periodic/daily/apk-upgrade", []byte(scriptContent), 0755); err != nil {
			return fmt.Errorf("failed to write Alpine upgrade script: %w", err)
		}

		// Make sure crond is running
		rcUpdateCmd := exec.Command("rc-update", "add", "crond", "default")
		if err := rcUpdateCmd.Run(); err != nil {
			logging.LogError("Failed to add crond to Alpine boot services: %v", err)
		}

		rcServiceCmd := exec.Command("rc-service", "crond", "start")
		if err := rcServiceCmd.Run(); err != nil {
			logging.LogError("Failed to start crond service on Alpine: %v", err)
		}

		logging.LogSuccess("Alpine periodic updates configured")
	} else {
		// Install unattended-upgrades on Debian/Ubuntu
		installCmd := exec.Command("apt-get", "install", "-y", "unattended-upgrades")
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("failed to install unattended-upgrades package: %w", err)
		}

		// Configure unattended-upgrades
		// Set non-interactive mode
		os.Setenv("DEBIAN_FRONTEND", "noninteractive")

		// Use debconf-set-selections to configure unattended-upgrades
		debconfCmd := exec.Command("debconf-set-selections")
		debconfCmd.Stdin = strings.NewReader(`unattended-upgrades unattended-upgrades/enable_auto_updates boolean true
unattended-upgrades unattended-upgrades/origins_pattern string origin=Debian,codename=${distro_codename},label=Debian-Security
`)
		if err := debconfCmd.Run(); err != nil {
			logging.LogError("Failed to set unattended-upgrades preferences: %v", err)
		}

		// Run dpkg-reconfigure
		reconfigureCmd := exec.Command("dpkg-reconfigure", "-f", "noninteractive", "unattended-upgrades")
		if err := reconfigureCmd.Run(); err != nil {
			return fmt.Errorf("failed to reconfigure unattended-upgrades: %w", err)
		}

		// Enable the unattended-upgrades service
		enableCmd := exec.Command("systemctl", "enable", "unattended-upgrades")
		if err := enableCmd.Run(); err != nil {
			logging.LogError("Failed to enable unattended-upgrades service: %v", err)
		}

		logging.LogSuccess("Unattended upgrades configured")
	}

	return nil
}

// UpdateSystem performs a manual system update
func UpdateSystem(osInfo *osdetect.OSInfo) error {
	logging.LogInfo("Updating system packages...")

	if osInfo.OsType == "alpine" {
		// Alpine update
		updateCmd := exec.Command("apk", "update")
		if err := updateCmd.Run(); err != nil {
			return fmt.Errorf("failed to update Alpine package list: %w", err)
		}

		upgradeCmd := exec.Command("apk", "upgrade")
		if err := upgradeCmd.Run(); err != nil {
			return fmt.Errorf("failed to upgrade Alpine packages: %w", err)
		}
	} else {
		// Debian/Ubuntu update
		updateCmd := exec.Command("apt-get", "update")
		if err := updateCmd.Run(); err != nil {
			return fmt.Errorf("failed to update Debian/Ubuntu package list: %w", err)
		}

		upgradeCmd := exec.Command("apt-get", "upgrade", "-y")
		if err := upgradeCmd.Run(); err != nil {
			return fmt.Errorf("failed to upgrade Debian/Ubuntu packages: %w", err)
		}
	}

	logging.LogSuccess("System packages updated successfully")
	return nil
}
