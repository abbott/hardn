// pkg/application/host_info_manager.go
package application

import (
	"fmt"
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// HostInfoManager is an application service for retrieving host information
type HostInfoManager struct {
	hostInfoService service.HostInfoService
}

// NewHostInfoManager creates a new HostInfoManager
func NewHostInfoManager(hostInfoService service.HostInfoService) *HostInfoManager {
	return &HostInfoManager{
		hostInfoService: hostInfoService,
	}
}

// GetHostInfo retrieves comprehensive host information
func (m *HostInfoManager) GetHostInfo() (*model.HostInfo, error) {
	return m.hostInfoService.GetHostInfo()
}

// FormatUptime formats the uptime in a human-readable format
func (m *HostInfoManager) FormatUptime(uptime time.Duration) string {
	days := int(uptime.Hours() / 24)
	hours := int(uptime.Hours()) % 24
	minutes := int(uptime.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%d days, %d hours, %d minutes", days, hours, minutes)
	} else if hours > 0 {
		return fmt.Sprintf("%d hours, %d minutes", hours, minutes)
	} else {
		return fmt.Sprintf("%d minutes", minutes)
	}
}

// FormatBytes formats byte size to human readable format
func (m *HostInfoManager) FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
