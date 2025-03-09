// pkg/domain/model/host_info.go
package model

import (
	"time"
)

// HostInfo represents system information about the host
type HostInfo struct {
	// Network information
	IPAddresses []string
	DNSServers  []string
	Hostname    string
	Domain      string

	// User information
	Users  []User // Reusing existing User model
	Groups []string

	// System information
	OSName     string
	OSVersion  string
	Uptime     time.Duration
	KernelInfo string

	// Additional information
	CPUInfo     string
	MemoryTotal int64
	MemoryFree  int64
	DiskTotal   map[string]int64 // Disk space by mount point
	DiskFree    map[string]int64 // Free space by mount point
}
