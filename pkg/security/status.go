package security

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
)

// SecurityStatus represents the security status of various system components
type SecurityStatus struct {
	RootLoginEnabled     bool
	FirewallEnabled      bool
	FirewallConfigured   bool
	SecureUsers          bool
	AppArmorEnabled      bool
	UnattendedUpgrades   bool
	SudoConfigured       bool
	SshPortNonDefault    bool
	PasswordAuthDisabled bool
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

// DisplaySecurityStatusWithCustomPrinter is like DisplaySecurityStatus but uses a custom print function
func DisplaySecurityStatusWithCustomPrinter(cfg *config.Config, status *SecurityStatus, formatter *style.StatusFormatter, printFn func(string), indent int) {
	if formatter == nil {
		formatter = style.NewStatusFormatter([]string{
			"Users",
			"Sudo",
			"Sudo Method",
			"Firewall",
			"SSH Login",
			"SSH Auth",
			"SSH Port",
			"AppArmor",
			"Auto Updates",
		}, 2)
	}

	// Create indentation prefix if needed

	// Custom print function that applies indentation
	indentedPrintFn := printFn
	if indent > 0 {
		indentedPrintFn = style.IndentPrinter(printFn, indent)
	}

	// Display user security
	if !status.SecureUsers {
		indentedPrintFn(formatter.FormatWarning("Users", "Not Configured", "root user only", "dark"))
	} else {
		indentedPrintFn(formatter.FormatConfigured("Users", "Configured", "non-root, sudo", "dark"))
	}

	// Display sudo configuration
	if !status.SudoConfigured {
		indentedPrintFn(formatter.FormatWarning("Sudo", "Not Installed", "", "dark"))
	// } else {
	// 	indentedPrintFn(formatter.FormatConfigured("Sudo", "Installed", "", "dark"))
	}

	// Display sudo method
	// Create repository to check sudo method
	repo := secondary.NewOSUserRepository(osdetect.NewRealFileSystem(), osdetect.NewRealCommander(), osdetect.GetOS().OsType)

	// First try to find a non-root user with sudo
	nonRootUser := ""
	users, err := repo.GetNonSystemUsers()
	if err == nil {
		for _, u := range users {
			if u.HasSudo && u.Username != "root" {
				nonRootUser = u.Username
				break
			}
		}
	}

	// If no non-root sudo user found, fall back to root
	userToCheck := nonRootUser
	if userToCheck == "" {
		userToCheck = "root"
	}

	exUser, err := repo.GetExtendedUserInfo(userToCheck)
	if err == nil && exUser.HasSudo {
		if exUser.SudoNoPassword {
			indentedPrintFn(formatter.FormatWarning("Sudo Method", "Configured", "no password", "dark"))
		} else {
			indentedPrintFn(formatter.FormatConfigured("Sudo Method", "Configured", "password required", "dark"))
		}
	} else {
		indentedPrintFn(formatter.FormatWarning("Sudo Method", "Not Configured", "unknown", "dark"))
	}

	// Display firewall status
	if !status.FirewallEnabled {
		indentedPrintFn(formatter.FormatWarning("Firewall", "Not Configured", "vulnerable", "dark"))
	} else if !status.FirewallConfigured {
		indentedPrintFn(formatter.FormatWarning("Firewall", "Enabled", "configure policies", "dark"))
	} else {
		indentedPrintFn(formatter.FormatConfigured("Firewall", "Configured", "deny policy", "dark"))
	}

	// Display root login status
	if status.RootLoginEnabled {
		indentedPrintFn(formatter.FormatWarning("SSH Login", "Not Configured", "root allowed", "dark"))
	} else {
		indentedPrintFn(formatter.FormatConfigured("SSH Login", "Configured", "root disallowed", "dark"))
	}

	// Display password authentication status
	if !status.PasswordAuthDisabled {
		indentedPrintFn(formatter.FormatWarning("SSH Auth", "Not Configured", "password auth enabled", "dark"))
	} else {
		indentedPrintFn(formatter.FormatConfigured("SSH Auth", "Configured", "key-only auth", "dark"))
	}

	// Display SSH port status
	if !status.SshPortNonDefault {
		indentedPrintFn(formatter.FormatWarning("SSH Port", "Not Configured", "default (22)", "dark"))
	} else {
		sshStatus := "non-default " + "(" + strconv.Itoa(cfg.SshPort) + ")"
		indentedPrintFn(formatter.FormatConfigured("SSH Port", "Configured", sshStatus, "dark"))
	}

	// Display AppArmor status
	if !status.AppArmorEnabled {
		indentedPrintFn(formatter.FormatWarning("AppArmor", "Not Configured", "", "dark"))
	} else {
		indentedPrintFn(formatter.FormatConfigured("AppArmor", "Configured", "", "dark"))
	}

	// Display unattended upgrades status
	if !status.UnattendedUpgrades {
		indentedPrintFn(formatter.FormatWarning("Auto Updates", "Not Configured", "", "dark"))
	} else {
		indentedPrintFn(formatter.FormatConfigured("Auto Updates", "Configured", "", "dark"))
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
		description = "no security"
		colorCode = style.Red
	} else if score <= 4 {
		riskLevel = "High"
		description = "weak security"
		colorCode = style.Red
	} else if score <= 6 {
		riskLevel = "Moderate"
		description = "medium security"
		colorCode = style.Yellow
	} else if score <= 8 {
		riskLevel = "Low"
		description = "strong security"
		colorCode = style.Green
	} else {
		riskLevel = "Minimal"
		description = "hardened security"
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
		if _, err := os.Stat("/etc/ssh/sshd_config.d/hardn.conf"); err == nil {
			sshConfigPath = "/etc/ssh/sshd_config.d/hardn.conf"
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
	cmd := exec.Command("ufw", "status", "verbose")
	output, err := cmd.CombinedOutput()
	if err == nil {
		statusOutput := string(output)
		enabled = strings.Contains(statusOutput, "Status: active")

		// Check basic configuration
		policyLines := 0

		// With verbose output, the default policies appear as:
		// "Default: deny (incoming), allow (outgoing), disabled (routed)"
		if strings.Contains(statusOutput, "Default:") {
			if strings.Contains(statusOutput, "deny (incoming)") {
				policyLines++
			}
			if strings.Contains(statusOutput, "allow (outgoing)") {
				policyLines++
			}
		}

		// Check that we have at least one rule for SSH
		if strings.Contains(statusOutput, "ALLOW IN") &&
			strings.Contains(statusOutput, "/tcp") {
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
// checkAppArmorStatus checks if AppArmor is properly configured and enforcing
func checkAppArmorStatus(osInfo *osdetect.OSInfo) bool {
	// If Alpine, check if AppArmor is installed, enabled, and has profiles
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

		if !strings.Contains(string(output), "apparmor") {
			return false
		}

		// Check if AppArmor is running and has profiles loaded
		statusCmd := exec.Command("aa-status")
		statusOutput, err := statusCmd.CombinedOutput()
		if err != nil {
			return false
		}

		// Check for enforcing profiles
		statusText := string(statusOutput)
		if !strings.Contains(statusText, "profiles are in enforce mode") {
			return false
		}

		// Make sure there are actual profiles enforced (not 0)
		return !strings.Contains(statusText, "0 profiles are in enforce mode")
	} else {
		// For Debian/Ubuntu, check AppArmor status
		cmd := exec.Command("aa-status")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return false
		}

		// Check if the service is loaded and active
		statusText := string(output)
		if !strings.Contains(statusText, "apparmor module is loaded") {
			return false
		}

		// Check for loaded profiles in enforcing mode
		if !strings.Contains(statusText, "profiles are in enforce mode") {
			return false
		}

		// Ensure there's at least 1 profile in enforce mode
		return !strings.Contains(statusText, "0 profiles are in enforce mode")
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
		if _, err := os.Stat("/etc/ssh/sshd_config.d/hardn.conf"); err == nil {
			sshConfigPath = "/etc/ssh/sshd_config.d/hardn.conf"
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

func CheckRootLoginEnabled(osInfo *osdetect.OSInfo) bool {
	var sshConfigPath string
	if osInfo.OsType == "alpine" {
		sshConfigPath = "/etc/ssh/sshd_config"
	} else {
		// For Debian/Ubuntu, check both main config and config.d
		sshConfigPath = "/etc/ssh/sshd_config"
		if _, err := os.Stat("/etc/ssh/sshd_config.d/hardn.conf"); err == nil {
			sshConfigPath = "/etc/ssh/sshd_config.d/hardn.conf"
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
