// pkg/menu/run_all_handler.go
package menu

import (
	"fmt"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/osdetect"
	"github.com/abbott/hardn/pkg/style"
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

// Handle opens the Run All screen. With no username in a live session it
// first says that Run All goes on without creating a user, in the words
// origin/main's "n" answer printed: the offer that came before them,
// "Would you like to set a username now?", opened the legacy User
// Management, which refactor plan slice 5, unit (d) deletes (hard cutover,
// the maintainer, 2026-11-03), so Run All no longer asks it.
func (h *RunAllHandler) Handle() {
	if h.config.Username == "" && !h.config.DryRun {
		fmt.Printf("\n%s Continuing without user creation\n",
			style.Colored(style.Yellow, style.SymWarning))
	}

	// Create and show the Run All menu
	runAllMenu := NewRunAllMenu(h.menuManager, h.config, h.osInfo)
	runAllMenu.Show()
}
