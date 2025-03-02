package packages

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/utils"
)

// IsPackageInstalled checks if a package is installed
func IsPackageInstalled(packageName string) bool {
	var cmd *exec.Cmd
	
	// Check for dpkg (Debian/Ubuntu) first
	if _, err := exec.LookPath("dpkg"); err == nil {
		cmd = exec.Command("dpkg", "-l", packageName)
		output, err := cmd.CombinedOutput()
		if err == nil && strings.Contains(string(output), packageName) {
			return true
		}
	}
	
	// Check for apk (Alpine)
	if _, err := exec.LookPath("apk"); err == nil {
		cmd = exec.Command("apk", "info", "-e", packageName)
		if err := cmd.Run(); err == nil {
			return true
		}
	}
	
	return false
}

// WriteSources writes the appropriate repository sources based on OS type
func WriteSources(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		if osInfo.OsType == "alpine" {
			utils.LogInfo("[DRY-RUN] Configure Alpine repositories in /etc/apk/repositories:")
			utils.LogInfo("[DRY-RUN] - Add main repository: https://dl-cdn.alpinelinux.org/alpine/v%s/main", osInfo.OsVersion[:strings.LastIndex(osInfo.OsVersion, ".")])
			utils.LogInfo("[DRY-RUN] - Add community repository: https://dl-cdn.alpinelinux.org/alpine/v%s/community", osInfo.OsVersion[:strings.LastIndex(osInfo.OsVersion, ".")])
			if cfg.AlpineTestingRepo {
				utils.LogInfo("[DRY-RUN] - Add testing repository: https://dl-cdn.alpinelinux.org/alpine/edge/testing")
			}
		} else if osInfo.IsProxmox {
			utils.LogInfo("[DRY-RUN] Configure Proxmox repositories in /etc/apt/sources.list:")
			for _, repo := range cfg.ProxmoxSrcRepos {
				utils.LogInfo("[DRY-RUN] - Add: %s", strings.ReplaceAll(repo, "CODENAME", osInfo.OsCodename))
			}
		} else {
			utils.LogInfo("[DRY-RUN] Configure %s repositories in /etc/apt/sources.list:", osInfo.OsType)
			for _, repo := range cfg.DebianRepos {
				utils.LogInfo("[DRY-RUN] - Add: %s", strings.ReplaceAll(repo, "CODENAME", osInfo.OsCodename))
			}
		}
		return nil
	}
	
	if osInfo.OsType == "alpine" {
		utils.LogInfo("Configuring Alpine repositories...")
		
		// Format Alpine version for repositories
		versionPrefix := osInfo.OsVersion
		if idx := strings.LastIndex(versionPrefix, "."); idx != -1 {
			versionPrefix = versionPrefix[:idx]
		}
		
		// Create Alpine repository file content
		content := fmt.Sprintf(`# Main repositories
https://dl-cdn.alpinelinux.org/alpine/v%s/main
https://dl-cdn.alpinelinux.org/alpine/v%s/community

# Security updates
https://dl-cdn.alpinelinux.org/alpine/v%s/main
https://dl-cdn.alpinelinux.org/alpine/v%s/community
`, versionPrefix, versionPrefix, versionPrefix, versionPrefix)
		
		// Add testing repo if enabled
		if cfg.AlpineTestingRepo {
			content += `
# Testing repository (use with caution)
https://dl-cdn.alpinelinux.org/alpine/edge/testing
`
			utils.LogInfo("Alpine testing repository enabled")
		}
		
		// Write the file
		if err := os.WriteFile("/etc/apk/repositories", []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write Alpine repositories: %w", err)
		}
		
		// Update package index
		cmd := exec.Command("apk", "update")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to update Alpine package index: %w", err)
		}
		
		utils.LogSuccess("Alpine repositories configured successfully")
	} else if osInfo.IsProxmox {
		utils.LogInfo("Writing Proxmox sources list to /etc/apt/sources.list")
		
		// Prepare content by replacing CODENAME placeholder
		var content strings.Builder
		for _, repo := range cfg.ProxmoxSrcRepos {
			content.WriteString(strings.ReplaceAll(repo, "CODENAME", osInfo.OsCodename))
			content.WriteString("\n")
		}
		
		// Backup original file
		utils.BackupFile("/etc/apt/sources.list", cfg)
		
		// Write the file
		if err := os.WriteFile("/etc/apt/sources.list", []byte(content.String()), 0644); err != nil {
			return fmt.Errorf("failed to write Proxmox sources list: %w", err)
		}
		
		utils.LogSuccess("Proxmox repositories configured successfully")
	} else {
		utils.LogInfo("Writing %s sources list to /etc/apt/sources.list", osInfo.OsCodename)
		
		// Prepare content by replacing CODENAME placeholder
		var content strings.Builder
		for _, repo := range cfg.DebianRepos {
			content.WriteString(strings.ReplaceAll(repo, "CODENAME", osInfo.OsCodename))
			content.WriteString("\n")
		}
		
		// Backup original file
		utils.BackupFile("/etc/apt/sources.list", cfg)
		
		// Write the file
		if err := os.WriteFile("/etc/apt/sources.list", []byte(content.String()), 0644); err != nil {
			return fmt.Errorf("failed to write Debian/Ubuntu sources list: %w", err)
		}
		
		utils.LogSuccess("Debian/Ubuntu repositories configured successfully")
	}
	
	return nil
}

// WriteProxmoxRepos writes Proxmox-specific repositories
func WriteProxmoxRepos(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if !osInfo.IsProxmox {
		return nil
	}
	
	if cfg.DryRun {
		utils.LogInfo("[DRY-RUN] Write Proxmox Ceph repository to /etc/apt/sources.list.d/ceph.list")
		utils.LogInfo("[DRY-RUN] Write Proxmox Enterprise repository to /etc/apt/sources.list.d/pve-enterprise.list")
		return nil
	}
	
	utils.LogInfo("Writing Proxmox Ceph repository to /etc/apt/sources.list.d/ceph.list")
	
	// Prepare content for Ceph repository
	var cephContent strings.Builder
	for _, repo := range cfg.ProxmoxCephRepo {
		cephContent.WriteString(strings.ReplaceAll(repo, "CODENAME", osInfo.OsCodename))
		cephContent.WriteString("\n")
	}
	
	// Backup original files
	utils.BackupFile("/etc/apt/sources.list.d/ceph.list", cfg)
	
	// Write Ceph repository
	if err := os.MkdirAll("/etc/apt/sources.list.d", 0755); err != nil {
		return fmt.Errorf("failed to create sources.list.d directory: %w", err)
	}
	
	if err := os.WriteFile("/etc/apt/sources.list.d/ceph.list", []byte(cephContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Proxmox Ceph repository: %w", err)
	}
	
	// Prepare content for Enterprise repository
	utils.LogInfo("Writing Proxmox Enterprise repository to /etc/apt/sources.list.d/pve-enterprise.list")
	
	var enterpriseContent strings.Builder
	for _, repo := range cfg.ProxmoxEnterpriseRepo {
		enterpriseContent.WriteString(strings.ReplaceAll(repo, "CODENAME", osInfo.OsCodename))
		enterpriseContent.WriteString("\n")
	}
	
	// Backup original file
	utils.BackupFile("/etc/apt/sources.list.d/pve-enterprise.list", cfg)
	
	// Write Enterprise repository
	if err := os.WriteFile("/etc/apt/sources.list.d/pve-enterprise.list", []byte(enterpriseContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Proxmox Enterprise repository: %w", err)
	}
	
	utils.LogSuccess("Proxmox-specific repositories configured")
	return nil
}

// HoldProxmoxPackages holds Proxmox packages to prevent removal
func HoldProxmoxPackages(osInfo *osdetect.OSInfo, patterns []string) error {
	if !osInfo.IsProxmox {
		return nil
	}
	
	utils.LogInfo("Holding Proxmox packages to prevent removal...")
	
	for _, pattern := range patterns {
		// Get packages matching the pattern
		cmd := exec.Command("dpkg-query", "-W", "-f=${binary:Package}\n")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to query packages: %w", err)
		}
		
		// Mark packages as held
		for _, pkg := range strings.Split(string(output), "\n") {
			if pkg == "" {
				continue
			}
			
			if strings.HasPrefix(pkg, pattern) {
				holdCmd := exec.Command("apt-mark", "hold", pkg)
				if err := holdCmd.Run(); err != nil {
					utils.LogError("Failed to hold package %s: %v", pkg, err)
				}
			}
		}
	}
	
	utils.LogSuccess("Proxmox packages protected")
	return nil
}

// UnholdProxmoxPackages releases Proxmox packages after script completion
func UnholdProxmoxPackages(osInfo *osdetect.OSInfo, patterns []string) error {
	if !osInfo.IsProxmox {
		return nil
	}
	
	utils.LogInfo("Unholding Proxmox packages...")
	
	for _, pattern := range patterns {
		// Get packages matching the pattern
		cmd := exec.Command("dpkg-query", "-W", "-f=${binary:Package}\n")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to query packages: %w", err)
		}
		
		// Mark packages as unhold
		for _, pkg := range strings.Split(string(output), "\n") {
			if pkg == "" {
				continue
			}
			
			if strings.HasPrefix(pkg, pattern) {
				unholdCmd := exec.Command("apt-mark", "unhold", pkg)
				if err := unholdCmd.Run(); err != nil {
					utils.LogError("Failed to unhold package %s: %v", pkg, err)
				}
			}
		}
	}
	
	utils.LogSuccess("Proxmox packages released")
	return nil
}

// InstallPackages installs a list of packages based on OS type
func InstallPackages(packages []string, osInfo *osdetect.OSInfo, cfg *config.Config) error {
	if len(packages) == 0 {
		return nil
	}

	// Check for dry-run mode
	if cfg.DryRun {
			utils.LogInfo("[DRY-RUN] Install: %s", strings.Join(packages, ", "))
			return nil
	}
	
	utils.LogInfo("Installing %s packages: %s", osInfo.OsType, strings.Join(packages, ", "))
	
	if osInfo.OsType == "alpine" {
		cmd := exec.Command("apk", append([]string{"add", "--no-cache"}, packages...)...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install Alpine packages: %v\n%s", err, output)
		}
	} else {
		// Hold Proxmox packages if necessary
		if osInfo.IsProxmox {
			HoldProxmoxPackages(osInfo, []string{"proxmox", "pve"})
		}
		
		// Update package lists
		updateCmd := exec.Command("apt-get", "update")
		updateOutput, err := updateCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to update package lists: %v\n%s", err, updateOutput)
		}
		
		// Install locales first for Debian/Ubuntu
		localesCmd := exec.Command("apt-get", "install", "--yes", "locales")
		localesOutput, err := localesCmd.CombinedOutput()
		if err != nil {
			utils.LogError("Failed to install locales: %v\n%s", err, localesOutput)
		} else {
			utils.LogInstall("locales")
		}
		
		// Configure locales
		sedCmd := exec.Command("sed", "-i", "/en_US.UTF-8/s/^# //g", "/etc/locale.gen")
		if err := sedCmd.Run(); err != nil {
			utils.LogError("Failed to configure locales: %v", err)
		}
		
		localeGenCmd := exec.Command("locale-gen")
		if err := localeGenCmd.Run(); err != nil {
			utils.LogError("Failed to generate locales: %v", err)
		}
		
		// Install packages
		installCmd := exec.Command("apt-get", append([]string{"install", "--yes"}, packages...)...)
		installOutput, err := installCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to install Debian/Ubuntu packages: %v\n%s", err, installOutput)
		}
		
		// Clean up
		autoremoveCmd := exec.Command("apt-get", "autoremove", "--yes")
		if err := autoremoveCmd.Run(); err != nil {
			utils.LogError("Failed to autoremove packages: %v", err)
		}
		
		cleanCmd := exec.Command("apt-get", "clean")
		if err := cleanCmd.Run(); err != nil {
			utils.LogError("Failed to clean package cache: %v", err)
		}
		
		rmCmd := exec.Command("rm", "-rf", "/var/lib/apt/lists/*")
		if err := rmCmd.Run(); err != nil {
			utils.LogError("Failed to remove apt lists: %v", err)
		}
		
		// Unhold Proxmox packages
		if osInfo.IsProxmox {
			UnholdProxmoxPackages(osInfo, []string{"proxmox", "pve"})
		}
	}
	
	utils.LogInstall(strings.Join(packages, ", "))
	utils.LogSuccess("Linux packages installed successfully!")
	return nil
}

// InstallPythonPackages installs Python packages with potential UV support
func InstallPythonPackages(cfg *config.Config, osInfo *osdetect.OSInfo) error {
	if cfg.DryRun {
		if osInfo.OsType == "alpine" {
			utils.LogInfo("[DRY-RUN] Install Alpine Python packages: %s", strings.Join(cfg.AlpinePythonPackages, ", "))
		} else {
			pyList := cfg.PythonPackages
			if os.Getenv("WSL") == "" {
				pyList = append(pyList, cfg.NonWslPythonPackages...)
			}
			utils.LogInfo("[DRY-RUN] Install Python packages: %s", strings.Join(pyList, ", "))
			
			if cfg.UseUvPackageManager {
				utils.LogInfo("[DRY-RUN] Use UV package manager for Python package installation")
				if len(cfg.PythonPipPackages) > 0 {
					utils.LogInfo("[DRY-RUN] Install Python pip packages with UV: %s", strings.Join(cfg.PythonPipPackages, ", "))
				}
			} else {
				utils.LogInfo("[DRY-RUN] Use standard pip for Python package installation")
				if len(cfg.PythonPipPackages) > 0 {
					utils.LogInfo("[DRY-RUN] Install Python pip packages with pip: %s", strings.Join(cfg.PythonPipPackages, ", "))
				}
			}
		}
		return nil
	}
	
	if osInfo.OsType == "alpine" {
		// Use Alpine's package manager for Python packages
		if len(cfg.AlpinePythonPackages) > 0 {
			utils.LogInfo("Installing Alpine Python packages...")
			return InstallPackages(cfg.AlpinePythonPackages, osInfo, cfg)
		} else {
			utils.LogInfo("No Alpine Python packages defined in config")
		}
	} else {
		// For Debian/Ubuntu systems
		pyList := cfg.PythonPackages
		if os.Getenv("WSL") == "" {
			pyList = append(pyList, cfg.NonWslPythonPackages...)
		}
		
		// Install system packages first
		cmd := exec.Command("apt-get", "update")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to update package lists: %w", err)
		}
		
		cmd = exec.Command("apt-get", append([]string{"install", "--yes"}, pyList...)...)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install Python system packages: %w", err)
		}
		
		// If UV package manager is enabled, install and use it for Python packages
		if cfg.UseUvPackageManager {
			utils.LogInfo("UV package manager enabled for Python - installing if needed")
			
			// Check if UV is installed
			_, err := exec.LookPath("uv")
			if err != nil {
				utils.LogInfo("Installing UV Python package manager...")
				
				// Check if pip3 is installed
				_, err := exec.LookPath("pip3")
				if err != nil {
					utils.LogInfo("Installing pip3 first...")
					pip3Cmd := exec.Command("apt-get", "install", "-y", "python3-pip")
					if err := pip3Cmd.Run(); err != nil {
						return fmt.Errorf("failed to install pip3: %w", err)
					}
				}
				
				// Install UV
				uvCmd := exec.Command("pip3", "install", "uv")
				if err := uvCmd.Run(); err != nil {
					utils.LogError("Failed to install UV package manager, will use pip instead")
					cfg.UseUvPackageManager = false
				} else {
					utils.LogInstall("UV package manager")
				}
			} else {
				utils.LogInfo("UV package manager already installed")
			}
			
			// Check if there are Python pip packages to install
			if len(cfg.PythonPipPackages) > 0 {
				utils.LogInfo("Installing Python pip packages with UV...")
				uvPipCmd := exec.Command("uv", append([]string{"pip", "install"}, cfg.PythonPipPackages...)...)
				if err := uvPipCmd.Run(); err != nil {
					return fmt.Errorf("failed to install Python pip packages with UV: %w", err)
				}
				utils.LogInstall("Python pip packages via UV: %s", strings.Join(cfg.PythonPipPackages, ", "))
			}
		} else {
			// Use standard pip if UV is not enabled
			if len(cfg.PythonPipPackages) > 0 {
				utils.LogInfo("Installing Python pip packages with pip...")
				
				// Check if pip3 is installed
				_, err := exec.LookPath("pip3")
				if err != nil {
					utils.LogInfo("Installing pip3 first...")
					pip3Cmd := exec.Command("apt-get", "install", "-y", "python3-pip")
					if err := pip3Cmd.Run(); err != nil {
						return fmt.Errorf("failed to install pip3: %w", err)
					}
				}
				
				// Install pip packages
				pipCmd := exec.Command("pip3", append([]string{"install"}, cfg.PythonPipPackages...)...)
				if err := pipCmd.Run(); err != nil {
					return fmt.Errorf("failed to install Python pip packages with pip: %w", err)
				}
				utils.LogInstall("Python pip packages: %s", strings.Join(cfg.PythonPipPackages, ", "))
			}
		}
	}
	
	utils.LogSuccess("Python packages installation completed")
	return nil
}