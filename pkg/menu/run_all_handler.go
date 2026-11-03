// pkg/menu/run_all_handler.go
package menu

import (
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
)

// RunAllHandler handles the Run All functionality
type RunAllHandler struct {
	menuManager *application.MenuManager
	config      *config.Config
	osInfo      *osdetect.OSInfo
}

// NewRunAllHandler creates a new RunAllHandler
func NewRunAllHandler(
	menuManager *application.MenuManager,
	config *config.Config,
	osInfo *osdetect.OSInfo,
) *RunAllHandler {
	return &RunAllHandler{
		menuManager: menuManager,
		config:      config,
		osInfo:      osInfo,
	}
}

// Handle opens the Run All screen. It asks nothing first: origin/main's
// offer with no username, "Would you like to set a username now?", opened
// the legacy User Management, which refactor plan slice 5, unit (d)
// deletes (hard cutover, the maintainer, 2026-11-03), so Run All no longer
// asks it; the screen's own Username row, "Not set", says that it goes on
// without creating a user.
func (h *RunAllHandler) Handle() {
	// Create and show the Run All menu
	runAllMenu := NewRunAllMenu(h.menuManager, h.config, h.osInfo)
	runAllMenu.Show()
}
