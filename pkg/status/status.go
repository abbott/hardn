package status

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
)

// SecurityStatus represents the security status of various system components
type SecurityStatus struct {
	RootLoginEnabled      bool
	FirewallEnabled       bool
	FirewallConfigured    bool
	SecureUsers           bool
	AppArmorEnabled       bool
	UnattendedUpgrades    bool
	SudoConfigured        bool
	SshPortNonDefault     bool
	PasswordAuthDisabled  bool
}

// CheckSecurityStatus examines the system and returns the security status
func CheckSecurityStatus(cfg *config.Config, osInfo *osdetect.OSInfo) (*SecurityStatus, error) {
	status := &SecurityStatus{}
	
	// Check SSH root login status
	status.RootLoginEnabled = checkRootLoginEnabled(osInfo)
	
	// Check firewall status
	status.FirewallEnabled, status.FirewallConfigured = checkFirewallStatus()
	
	// Check user security (non-root users with sudo)
	status.SecureUsers = checkUserSecurity()
	
	// Check AppArmor status
	status.AppArmorEnabled = checkAppArmorStatus(osInfo)
	
	// Check unattended upgrades
	status.UnattendedUpgrades = checkUnattendedUpgrades(osInfo)
	
	// Check sudo configuration
	status.SudoConfigured = checkSudoConfiguration()
	
	// Check SSH port configuration
	status.SshPortNonDefault = (cfg.SshPort != 22)
	
	// Check password authentication
	status.PasswordAuthDisabled = checkPasswordAuth(osInfo)
	
	return status, nil
}

// DisplaySecurityStatus prints the security status above the main menu
func DisplaySecurityStatus(status *SecurityStatus, formatter *style.StatusFormatter) {
	// Create a formatter with all the labels we'll use
	if formatter == nil {
		formatter = style.NewStatusFormatter([]string{
				"SSH Root Login",
				"Firewall",
				"Users", 
				"SSH Port",
				"SSH Auth",
				"AppArmor",
				"Auto Updates",
		}, 2)
}
    // Display root login status
    if status.RootLoginEnabled {
        fmt.Println(formatter.FormatWarning("SSH Root Login", "Enabled", "vulnerable"))
    } else {
        fmt.Println(formatter.FormatSuccess("SSH Root Login", "Disabled", "secure"))
    }
    
    // Display firewall status
    if !status.FirewallEnabled {
        fmt.Println(formatter.FormatWarning("Firewall", "Disabled", "vulnerable"))
    } else if !status.FirewallConfigured {
        fmt.Println(formatter.FormatWarning("Firewall", "Enabled but not hardened", ""))
    } else {
        fmt.Println(formatter.FormatSuccess("Firewall", "Enabled and configured", "secure"))
    }
    
    // Display user security
    if !status.SecureUsers {
        fmt.Println(formatter.FormatWarning("Users", "No non-root sudo users found", ""))
    } else {
        fmt.Println(formatter.FormatSuccess("Users", "Non-root sudo users configured", ""))
    }
    
    // Display SSH port status
    if !status.SshPortNonDefault {
        fmt.Println(formatter.FormatWarning("SSH Port", "Default (22)", "recommended to change"))
    } else {
        fmt.Println(formatter.FormatSuccess("SSH Port", "Non-default", ""))
    }
    
    // Display password authentication status
    if !status.PasswordAuthDisabled {
        fmt.Println(formatter.FormatWarning("SSH Auth", "Password auth enabled", "vulnerable"))
    } else {
        fmt.Println(formatter.FormatSuccess("SSH Auth", "Key-only authentication", ""))
    }
    
    // Display AppArmor status
    if !status.AppArmorEnabled {
        fmt.Println(formatter.FormatWarning("AppArmor", "Not enabled", ""))
    } else {
        fmt.Println(formatter.FormatSuccess("AppArmor", "Enabled", ""))
    }
    
    // Display unattended upgrades status
    if !status.UnattendedUpgrades {
        fmt.Println(formatter.FormatWarning("Auto Updates", "Not configured", ""))
    } else {
        fmt.Println(formatter.FormatSuccess("Auto Updates", "Configured", ""))
    }
}

func GetSecurityRiskLevel(status *SecurityStatus) (string, string, string) {
	// Calculate overall score
	score := 0
	if !status.RootLoginEnabled {
		score++
	}
	if status.FirewallEnabled {
		score++
	}
	if status.FirewallConfigured {
		score++
	}
	if status.SecureUsers {
		score++
	}
	if status.AppArmorEnabled {
		score++
	}
	if status.UnattendedUpgrades {
		score++
	}
	if status.SshPortNonDefault {
		score++
	}
	if status.PasswordAuthDisabled {
		score++
	}
	
	// Determine risk level
	var riskLevel, description, colorCode string
	if score <= 2 {
		riskLevel = "Critical"
		description = "No security detected"
		colorCode = style.Red  // Using DeepRed for Critical
	} else if score <= 4 {
		riskLevel = "High"
		description = "Insufficient measures in place"
		colorCode = style.Red  // Using DeepRed for High
	} else if score <= 6 {
		riskLevel = "Moderate"
		description = "Some protections active"
		colorCode = style.Yellow
	} else if score <= 8 {
		riskLevel = "Low"
		description = "Strong measures in place met"
		// description = "Most best practices met."
		colorCode = style.Green
	} else {
		riskLevel = "Minimal"
		description = "System well-hardened"
		colorCode = style.Green
	}
	
	return riskLevel, description, colorCode
}

// checkRootLoginEnabled checks if SSH root login is enabled
func checkRootLoginEnabled(osInfo *osdetect.OSInfo) bool {
	var sshConfigPath string
	if osInfo.OsType == "alpine" {
		sshConfigPath = "/etc/ssh/sshd_config"
	} else {
		// For Debian/Ubuntu, check both main config and config.d
		sshConfigPath = "/etc/ssh/sshd_config"
		if _, err := os.Stat("/etc/ssh/sshd_config.d/manage.conf"); err == nil {
			sshConfigPath = "/etc/ssh/sshd_config.d/manage.conf"
		}
	}
	
	file, err := os.Open(sshConfigPath)
	if err != nil {
		return true // Assume vulnerable if can't check
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PermitRootLogin") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "no" {
				return false
			}
			return true
		}
	}
	
	return true // Default to vulnerable if not explicitly set
}

// checkFirewallStatus checks if the firewall is enabled and properly configured
func checkFirewallStatus() (bool, bool) {
	enabled := false
	configured := false
	
	// Check if UFW is installed and enabled
	cmd := exec.Command("ufw", "status")
	output, err := cmd.CombinedOutput()
	if err == nil {
		statusOutput := string(output)
		enabled = strings.Contains(statusOutput, "Status: active")
		
		// Check basic configuration
		policyLines := 0
		if strings.Contains(statusOutput, "deny (incoming)") {
			policyLines++
		}
		if strings.Contains(statusOutput, "allow (outgoing)") {
			policyLines++
		}
		// Check that we have at least one rule for SSH
		if strings.Contains(statusOutput, "ALLOW") && strings.Contains(statusOutput, "/tcp") {
			policyLines++
		}
		configured = policyLines >= 3
	}
	
	// Check for iptables if UFW not found
	if !enabled {
		iptablesCmd := exec.Command("iptables", "-L")
		iptablesOutput, err := iptablesCmd.CombinedOutput()
		if err == nil {
			rules := strings.Count(string(iptablesOutput), "Chain")
			enabled = rules > 3
			// Look for SSH related rules
			configured = strings.Contains(strings.ToLower(string(iptablesOutput)), "ssh")
		}
	}
	
	return enabled, configured
}

// checkUserSecurity checks if there are non-root users with sudo access
func checkUserSecurity() bool {
	// Check /etc/sudoers.d for non-root user entries
	sudoersDir := "/etc/sudoers.d"
	if _, err := os.Stat(sudoersDir); err == nil {
		entries, err := os.ReadDir(sudoersDir)
		if err == nil && len(entries) > 0 {
			// Check if any of these entries are not for root
			for _, entry := range entries {
				if entry.Name() != "README" && entry.Name() != "root" {
					return true
				}
			}
		}
	}
	
	// Alternative check: look for users in sudo/wheel group
	groupFile, err := os.Open("/etc/group")
	if err != nil {
		return false
	}
	defer groupFile.Close()
	
	scanner := bufio.NewScanner(groupFile)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "sudo:") || strings.HasPrefix(line, "wheel:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 4 && parts[3] != "" && !strings.Contains(parts[3], "root") {
				return true
			}
		}
	}
	
	return false
}

// checkAppArmorStatus checks if AppArmor is enabled
func checkAppArmorStatus(osInfo *osdetect.OSInfo) bool {
	// If Alpine, check if AppArmor is installed and enabled
	if osInfo.OsType == "alpine" {
		// Check if AppArmor package is installed
		cmd := exec.Command("apk", "info", "-e", "apparmor")
		if err := cmd.Run(); err != nil {
			return false
		}
		
		// Check if AppArmor is in runlevel
		rcCmd := exec.Command("rc-status", "default")
		output, err := rcCmd.CombinedOutput()
		if err != nil {
			return false
		}
		
		return strings.Contains(string(output), "apparmor")
	} else {
		// For Debian/Ubuntu, check AppArmor status
		cmd := exec.Command("aa-status")
		if err := cmd.Run(); err != nil {
			return false
		}
		
		return true
	}
}

// checkUnattendedUpgrades checks if unattended upgrades are configured
func checkUnattendedUpgrades(osInfo *osdetect.OSInfo) bool {
	if osInfo.OsType == "alpine" {
		// Check for daily cron job
		if _, err := os.Stat("/etc/periodic/daily/apk-upgrade"); err == nil {
			return true
		}
		return false
	} else {
		// Check for unattended-upgrades package and configuration
		cmd := exec.Command("dpkg", "-l", "unattended-upgrades")
		if err := cmd.Run(); err != nil {
			return false
		}
		
		// Check if service is enabled
		svcCmd := exec.Command("systemctl", "is-enabled", "unattended-upgrades")
		if err := svcCmd.Run(); err != nil {
			return false
		}
		
		return true
	}
}

// checkSudoConfiguration checks if sudo is configured securely
func checkSudoConfiguration() bool {
	// Check if sudo is installed
	sudoCmd := exec.Command("which", "sudo")
	if err := sudoCmd.Run(); err != nil {
		return false
	}
	
	// Check if sudoers file exists
	if _, err := os.Stat("/etc/sudoers"); err != nil {
		return false
	}
	
	return true
}

// checkPasswordAuth checks if password authentication is disabled
func checkPasswordAuth(osInfo *osdetect.OSInfo) bool {
	var sshConfigPath string
	if osInfo.OsType == "alpine" {
		sshConfigPath = "/etc/ssh/sshd_config"
	} else {
		// For Debian/Ubuntu, check both main config and config.d
		sshConfigPath = "/etc/ssh/sshd_config"
		if _, err := os.Stat("/etc/ssh/sshd_config.d/manage.conf"); err == nil {
			sshConfigPath = "/etc/ssh/sshd_config.d/manage.conf"
		}
	}
	
	file, err := os.Open(sshConfigPath)
	if err != nil {
		return false // Assume vulnerable if can't check
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PasswordAuthentication") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "no" {
				return true
			}
			return false
		}
	}
	
	return false // Default to vulnerable if not explicitly set
}