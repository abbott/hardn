// pkg/application/menu_manager.go
package application

import (
	"time"

	"github.com/abbott/hardn/pkg/domain/model"
)

// MenuManager orchestrates menu-related operations
type MenuManager struct {
	hostInfoManager *HostInfoManager
}

// NewMenuManager returns a MenuManager over the host-info manager.
func NewMenuManager(hostInfoManager *HostInfoManager) *MenuManager {
	return &MenuManager{hostInfoManager: hostInfoManager}
}

// retrieve host information
func (m *MenuManager) GetHostInfo() (*model.HostInfo, error) {
	return m.hostInfoManager.GetHostInfo()
}

// format the uptime in a human-readable format
func (m *MenuManager) FormatUptime(uptime time.Duration) string {
	return m.hostInfoManager.FormatUptime(uptime)
}
