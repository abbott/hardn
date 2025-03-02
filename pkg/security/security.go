package security

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
)

// SetupAppArmor installs and configures AppArmor
func SetupAppArmor(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		utils.LogInfo("[DRY-RUN] Install and configure AppArmor:")
		if osInfo.OsType == "alpine" {
			utils.LogInfo("[DRY-RUN] - Install AppArmor package via apk")
			utils.LogInfo("[DRY-RUN] - Enable AppArmor service via OpenRC")
			utils.LogInfo("[DRY-RUN] - Set profiles to enforcing mode")
		} else {
			utils.LogInfo("[DRY-RUN] - Install AppArmor package via apt-get")
			utils.LogInfo("[DRY-RUN] - Enforce AppArmor profiles in /etc/apparmor.d/*")
		}
		return nil
	}

	utils.LogInfo("Setting up AppArmor...")

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
			utils.LogError("Failed to add AppArmor to boot services: %v", err)
		}

		rcServiceCmd := exec.Command("rc-service", "apparmor", "start")
		if err := rcServiceCmd.Run(); err != nil {
			utils.LogError("Failed to start AppArmor service: %v", err)
		}

		// Apply profiles (Alpine version)
		profilesDir := "/etc/apparmor.d"
		if _, err := os.Stat(profilesDir); !os.IsNotExist(err) {
			files, err := os.ReadDir(profilesDir)
			if err != nil {
				utils.LogError("Failed to read AppArmor profiles directory: %v", err)
			} else {
				for _, file := range files {
					if !file.IsDir() {
						profilePath := filepath.Join(profilesDir, file.Name())
						aaEnforceCmd := exec.Command("aa_enforce", profilePath)
						if err := aaEnforceCmd.Run(); err != nil {
							utils.LogError("Failed to enforce AppArmor profile %s: %v", profilePath, err)
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
			utils.LogError("Failed to enforce AppArmor profiles: %v", err)
			// Try individual profiles if wildcard fails
			profilesDir := "/etc/apparmor.d"
			if _, err := os.Stat(profilesDir); !os.IsNotExist(err) {
				files, err := os.ReadDir(profilesDir)
				if err != nil {
					utils.LogError("Failed to read AppArmor profiles directory: %v", err)
				} else {
					for _, file := range files {
						if !file.IsDir() {
							profilePath := filepath.Join(profilesDir, file.Name())
							aaEnforceCmd := exec.Command("aa-enforce", profilePath)
							if err := aaEnforceCmd.Run(); err != nil {
								utils.LogError("Failed to enforce AppArmor profile %s: %v", profilePath, err)
							}
						}
					}
				}
			}
		}
	}

	utils.LogSuccess("AppArmor installed and enabled")
	return nil
}

// SetupLynis installs and runs the Lynis security audit tool
func SetupLynis(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		utils.LogInfo("[DRY-RUN] Install and run Lynis security audit tool:")
		if osInfo.OsType == "alpine" {
			utils.LogInfo("[DRY-RUN] - Install Lynis package via apk")
		} else {
			utils.LogInfo("[DRY-RUN] - Install Lynis package via apt-get")
		}
		utils.LogInfo("[DRY-RUN] - Run system security audit (lynis audit system)")
		utils.LogInfo("[DRY-RUN] - Audit results available in Lynis log files")
		return nil
	}

	utils.LogInfo("Setting up Lynis security audit tool...")

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

	utils.LogSuccess("Lynis installed and system audit completed")
	return nil
}