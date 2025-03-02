package osdetect

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/abbott/hardn/pkg/utils"
)

// OSInfo holds information about the detected operating system
type OSInfo struct {
	OsType      string // debian, ubuntu, alpine
	OsCodename  string // release name, e.g., bullseye, focal, etc.
	OsVersion   string // version number
	IsProxmox   bool   // is proxmox environment
}

// DetectOS detects the operating system and returns its information
func DetectOS() (*OSInfo, error) {
	utils.PrintHeader()
	
	// Check if /etc/os-release exists
	if _, err := os.Stat("/etc/os-release"); os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot detect OS type: /etc/os-release not found")
	}
	
	// Read /etc/os-release
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return nil, fmt.Errorf("failed to open /etc/os-release: %w", err)
	}
	defer file.Close()
	
	// Parse /etc/os-release
	var id, versionId, versionCodename string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			id = strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
		} else if strings.HasPrefix(line, "VERSION_ID=") {
			versionId = strings.Trim(strings.TrimPrefix(line, "VERSION_ID="), "\"")
		} else if strings.HasPrefix(line, "VERSION_CODENAME=") {
			versionCodename = strings.Trim(strings.TrimPrefix(line, "VERSION_CODENAME="), "\"")
		}
	}
	
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read /etc/os-release: %w", err)
	}
	
	// Create OSInfo
	osInfo := &OSInfo{
		OsType:     id,
		OsVersion:  versionId,
		OsCodename: versionCodename,
		IsProxmox:  false,
	}
	
	// For Alpine, use release version as codename
	if osInfo.OsType == "alpine" {
		osInfo.OsCodename = osInfo.OsVersion
		utils.LogSuccess("Alpine Linux %s detected", osInfo.OsVersion)
	} else if osInfo.OsType == "debian" || osInfo.OsType == "ubuntu" {
		utils.LogSuccess("%s %s detected", osInfo.OsType, osInfo.OsCodename)
	}
	
	// Check if the system is Proxmox
	if _, err := os.Stat("/etc/pve"); !os.IsNotExist(err) {
		osInfo.IsProxmox = true
		utils.LogSuccess("Proxmox environment detected")
	}
	
	return osInfo, nil
}