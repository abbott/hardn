// pkg/system/system_details.go
package system

import (
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/domain/model"
	domainports "github.com/abbott/hardn/pkg/domain/ports/secondary"
)

// SystemDetails represents the complete system information
type SystemDetails struct {
	// User login port for retrieving login information
	userLoginPort domainports.UserLoginPort
	// System info
	OSName      string
	OSVersion   string
	Kernel      string
	Hostname    string
	Domain      string
	CurrentUser string

	// Network info
	MachineIP     string
	ClientIP      string
	IPAddresses   []string // Enhanced: All system IP addresses
	DNSServers    []string
	NetworkStatus string

	// User info
	Users []model.User // Enhanced: Non-system users with sudo status

	// CPU info
	CPUModel       string
	CPUCores       int
	CPUSockets     int
	CPUFrequency   float64
	CPUHypervisor  string
	LoadAvg1       float64
	LoadAvg5       float64
	LoadAvg15      float64
	LoadAvg1Graph  string
	LoadAvg5Graph  string
	LoadAvg15Graph string

	// Memory info
	MemoryTotal     uint64
	MemoryFree      uint64
	MemoryUsed      uint64
	MemoryPercent   float64
	MemoryTotalGB   float64
	MemoryUsedGB    float64
	MemoryFreeGB    float64
	MemoryGraphUsed string

	// Disk info
	ZFSPresent     bool
	ZFSHealth      string
	ZFSFilesystem  string
	ZFSAvailableGB float64
	ZFSUsedGB      float64
	ZFSPercent     float64
	RootPartition  string
	RootTotalGB    float64
	RootUsedGB     float64
	RootFreeGB     float64
	DiskPercent    float64
	DiskGraphUsed  string

	// Login and uptime
	LastLoginTime    string
	LastLoginIP      string
	UptimeFormatted  string
	UptimeLongFormat string // Enhanced: Verbose uptime format
	LastLoginPresent bool
	Uptime           time.Duration
}

// GenerateSystemStatus collects system information and returns a SystemDetails struct
func GenerateSystemStatus(hostInfoManager *application.HostInfoManager) (*SystemDetails, error) {
	info := &SystemDetails{
		ZFSFilesystem: "zroot/ROOT/os",                      // Default ZFS filesystem
		RootPartition: "/",                                  // Default root partition
		userLoginPort: secondary.NewLastlogCommandAdapter(), // Use lastlog adapter
	}

	// Collect all system information
	if err := info.collectOSInfo(hostInfoManager); err != nil {
		return nil, fmt.Errorf("failed to collect OS info: %w", err)
	}

	if err := info.collectNetworkInfo(hostInfoManager); err != nil {
		return nil, fmt.Errorf("failed to collect network info: %w", err)
	}

	if err := info.collectUserInfo(hostInfoManager); err != nil {
		return nil, fmt.Errorf("failed to collect user info: %w", err)
	}

	if err := info.collectCPUInfo(hostInfoManager); err != nil {
		return nil, fmt.Errorf("failed to collect CPU info: %w", err)
	}

	if err := info.collectMemoryInfo(hostInfoManager); err != nil {
		return nil, fmt.Errorf("failed to collect memory info: %w", err)
	}

	if err := info.collectDiskInfo(hostInfoManager); err != nil {
		return nil, fmt.Errorf("failed to collect disk info: %w", err)
	}

	if err := info.collectLoginInfo(); err != nil {
		return nil, fmt.Errorf("failed to collect login info: %w", err)
	}

	// Generate graph visualizations
	info.generateGraphs()

	return info, nil
}
