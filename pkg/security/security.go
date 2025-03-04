package security

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
)

// SetupAppArmor installs and configures AppArmor
func SetupAppArmor(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Install and configure AppArmor:")
		if osInfo.OsType == "alpine" {
			logging.LogInfo("[DRY-RUN] - Install AppArmor package via apk")
			logging.LogInfo("[DRY-RUN] - Enable AppArmor service via OpenRC")
			logging.LogInfo("[DRY-RUN] - Set profiles to enforcing mode")
		} else {
			logging.LogInfo("[DRY-RUN] - Install AppArmor package via apt-get")
			logging.LogInfo("[DRY-RUN] - Enforce AppArmor profiles in /etc/apparmor.d/*")
		}
		return nil
	}

	logging.LogInfo("Setting up AppArmor...")

	// Install AppArmor
	if osInfo.OsType == "alpine" {
		// Alpine installation
		cmd := exec.Command("apk", "add", "apparmor")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install AppArmor on Alpine: %w", err)
		}

		// Enable AppArmor in OpenRC
		rcUpdateCmd := exec.Command("rc-update", "add", "apparmor", "default")
		if err := rcUpdateCmd.Run(); err != nil {
			logging.LogError("Failed to add AppArmor to boot services: %v", err)
		}

		rcServiceCmd := exec.Command("rc-service", "apparmor", "start")
		if err := rcServiceCmd.Run(); err != nil {
			logging.LogError("Failed to start AppArmor service: %v", err)
		}

		// Apply profiles (Alpine version)
		profilesDir := "/etc/apparmor.d"
		if _, err := os.Stat(profilesDir); !os.IsNotExist(err) {
			files, err := os.ReadDir(profilesDir)
			if err != nil {
				logging.LogError("Failed to read AppArmor profiles directory: %v", err)
			} else {
				for _, file := range files {
					if !file.IsDir() {
						profilePath := filepath.Join(profilesDir, file.Name())
						aaEnforceCmd := exec.Command("aa_enforce", profilePath)
						if err := aaEnforceCmd.Run(); err != nil {
							logging.LogError("Failed to enforce AppArmor profile %s: %v", profilePath, err)
						}
					}
				}
			}
		}
	} else {
		// Debian/Ubuntu installation
		cmd := exec.Command("apt-get", "install", "-y", "apparmor")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install AppArmor on Debian/Ubuntu: %w", err)
		}

		// Apply profiles
		aaEnforceCmd := exec.Command("aa-enforce", "/etc/apparmor.d/*")
		if err := aaEnforceCmd.Run(); err != nil {
			logging.LogError("Failed to enforce AppArmor profiles: %v", err)
			// Try individual profiles if wildcard fails
			profilesDir := "/etc/apparmor.d"
			if _, err := os.Stat(profilesDir); !os.IsNotExist(err) {
				files, err := os.ReadDir(profilesDir)
				if err != nil {
					logging.LogError("Failed to read AppArmor profiles directory: %v", err)
				} else {
					for _, file := range files {
						if !file.IsDir() {
							profilePath := filepath.Join(profilesDir, file.Name())
							aaEnforceCmd := exec.Command("aa-enforce", profilePath)
							if err := aaEnforceCmd.Run(); err != nil {
								logging.LogError("Failed to enforce AppArmor profile %s: %v", profilePath, err)
							}
						}
					}
				}
			}
		}
	}

	logging.LogSuccess("AppArmor installed and enabled")
	return nil
}

// SetupLynis installs and runs the Lynis security audit tool
func SetupLynis(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Install and run Lynis security audit tool:")
		if osInfo.OsType == "alpine" {
			logging.LogInfo("[DRY-RUN] - Install Lynis package via apk")
		} else {
			logging.LogInfo("[DRY-RUN] - Install Lynis package via apt-get")
		}
		logging.LogInfo("[DRY-RUN] - Run system security audit (lynis audit system)")
		logging.LogInfo("[DRY-RUN] - Audit results available in Lynis log files")
		return nil
	}

	logging.LogInfo("Setting up Lynis security audit tool...")

	// Install Lynis
	if osInfo.OsType == "alpine" {
		cmd := exec.Command("apk", "add", "lynis")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install Lynis on Alpine: %w", err)
		}
	} else {
		cmd := exec.Command("apt-get", "install", "-y", "lynis")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install Lynis on Debian/Ubuntu: %w", err)
		}
	}

	// Run Lynis audit
	auditCmd := exec.Command("lynis", "audit", "system")
	if err := auditCmd.Run(); err != nil {
		return fmt.Errorf("failed to run Lynis audit: %w", err)
	}

	logging.LogSuccess("Lynis installed and system audit completed")
	return nil
}
