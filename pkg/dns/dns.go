package dns

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
)

// ConfigureDNS configures DNS settings based on the configuration
func ConfigureDNS(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		logging.LogInfo("[DRY-RUN] Configure DNS with the following settings:")
		logging.LogInfo("[DRY-RUN] - Domain: lan")
		logging.LogInfo("[DRY-RUN] - Search: lan")
		logging.LogInfo("[DRY-RUN] - Primary nameserver: %s", cfg.Nameservers[0])
		if len(cfg.Nameservers) > 1 {
			logging.LogInfo("[DRY-RUN] - Secondary nameserver: %s", cfg.Nameservers[1])
		}

		// Check systemd-resolved
		cmd := exec.Command("systemctl", "is-active", "systemd-resolved")
		if err := cmd.Run(); err == nil {
			logging.LogInfo("[DRY-RUN] systemd-resolved detected - Configure via /etc/systemd/resolved.conf")
			logging.LogInfo("[DRY-RUN] Restart systemd-resolved service")
		} else if _, err := exec.LookPath("resolvconf"); err == nil {
			// Check resolvconf
			logging.LogInfo("[DRY-RUN] resolvconf detected - Configure via /etc/resolvconf/resolv.conf.d/head")
			logging.LogInfo("[DRY-RUN] Update resolvconf with 'resolvconf -u'")
		} else {
			// Direct configuration
			logging.LogInfo("[DRY-RUN] Write DNS configuration directly to /etc/resolv.conf")
		}
		return nil
	}

	logging.LogInfo("Configuring DNS settings...")

	if len(cfg.Nameservers) == 0 {
		return fmt.Errorf("no nameservers configured in configuration")
	}

	primaryNameserver := cfg.Nameservers[0]

	// Check if systemd-resolved is active
	cmd := exec.Command("systemctl", "is-active", "systemd-resolved")
	if err := cmd.Run(); err == nil {
		logging.LogInfo("systemd-resolved detected, configuring via resolved.conf")
		utils.BackupFile("/etc/systemd/resolved.conf", cfg)

		// Create resolved.conf content
		content := "[Resolve]\n"
		content += fmt.Sprintf("DNS=%s", strings.Join(cfg.Nameservers, " "))
		content += "\nDomains=lan\n"

		// Write resolved.conf
		if err := os.WriteFile("/etc/systemd/resolved.conf", []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write resolved.conf for nameserver %s: %w", primaryNameserver, err)
		}

		// Restart systemd-resolved
		restartCmd := exec.Command("systemctl", "restart", "systemd-resolved")
		if err := restartCmd.Run(); err != nil {
			return fmt.Errorf("failed to restart systemd-resolved for nameserver %s: %w", primaryNameserver, err)
		}
	} else if _, err := exec.LookPath("resolvconf"); err == nil {
		// resolvconf is installed
		logging.LogInfo("resolvconf detected, using resolvconf mechanism")
		utils.BackupFile("/etc/resolvconf/resolv.conf.d/head", cfg)

		// Create head file content
		content := "domain lan\nsearch lan\n"
		content += fmt.Sprintf("nameserver %s\n", cfg.Nameservers[0])
		if len(cfg.Nameservers) > 1 {
			content += fmt.Sprintf("nameserver %s\n", cfg.Nameservers[1])
		}

		// Write head file
		if err := os.MkdirAll("/etc/resolvconf/resolv.conf.d", 0755); err != nil {
			return fmt.Errorf("failed to create resolvconf directory for nameserver %s: %w", primaryNameserver, err)
		}
		if err := os.WriteFile("/etc/resolvconf/resolv.conf.d/head", []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write resolvconf head file with nameserver %s: %w", primaryNameserver, err)
		}

		// Update resolvconf
		resolvCmd := exec.Command("resolvconf", "-u")
		if err := resolvCmd.Run(); err != nil {
			return fmt.Errorf("failed to update resolvconf with nameserver %s: %w", primaryNameserver, err)
		}
	} else {
		// Direct approach
		logging.LogInfo("Using direct DNS configuration")
		utils.BackupFile("/etc/resolv.conf", cfg)

		// Create resolv.conf content
		content := "domain lan\nsearch lan\n"
		content += fmt.Sprintf("nameserver %s\n", cfg.Nameservers[0])
		if len(cfg.Nameservers) > 1 {
			content += fmt.Sprintf("nameserver %s\n", cfg.Nameservers[1])
		}

		// Write resolv.conf
		if err := os.WriteFile("/etc/resolv.conf", []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write resolv.conf with nameserver %s: %w", primaryNameserver, err)
		}
	}

	logging.LogSuccess("DNS configured successfully with nameserver %s", primaryNameserver)
	return nil
}