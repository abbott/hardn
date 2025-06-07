package security

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
)

// SetupAppArmor installs and configures AppArmor
func SetupAppArmor(cfg *config.Config, osInfo *osdetect.OSInfo, commander interfaces.Commander) error {
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
		if _, err := commander.Execute("apk", "add", "apparmor"); err != nil {
			return fmt.Errorf("failed to install AppArmor on Alpine: %w", err)
		}

		// Enable AppArmor in OpenRC
		if _, err := commander.Execute("rc-update", "add", "apparmor", "default"); err != nil {
			logging.LogError("Failed to add AppArmor to Alpine boot services: %v", err)
		}

		if _, err := commander.Execute("rc-service", "apparmor", "start"); err != nil {
			logging.LogError("Failed to start AppArmor service on Alpine: %v", err)
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
						if _, err := commander.Execute("aa_enforce", profilePath); err != nil {
							logging.LogError("Failed to enforce AppArmor profile %s: %v", profilePath, err)
						}
					}
				}
			}
		}
	} else {
		// Debian/Ubuntu installation
		if _, err := commander.Execute("apt-get", "install", "-y", "apparmor"); err != nil {
			return fmt.Errorf("failed to install AppArmor on Debian/Ubuntu: %w", err)
		}

		// Apply profiles
		if _, err := commander.Execute("aa-enforce", "/etc/apparmor.d/*"); err != nil {
			logging.LogError("Failed to enforce AppArmor profiles with wildcard: %v", err)
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
							if _, err := commander.Execute("aa-enforce", profilePath); err != nil {
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
func SetupLynis(cfg *config.Config, osInfo *osdetect.OSInfo, commander interfaces.Commander) error {
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
		if _, err := commander.Execute("apk", "add", "lynis"); err != nil {
			return fmt.Errorf("failed to install Lynis on Alpine: %w", err)
		}
	} else {
		if _, err := commander.Execute("apt-get", "install", "-y", "lynis"); err != nil {
			return fmt.Errorf("failed to install Lynis on Debian/Ubuntu: %w", err)
		}
	}

	// Run Lynis audit
	output, err := commander.Execute("lynis", "audit", "system")
	if err != nil {
		return fmt.Errorf("failed to run Lynis audit: %w\nOutput: %s", err, string(output))
	}

	logging.LogSuccess("Lynis installed and system audit completed")
	return nil
}
