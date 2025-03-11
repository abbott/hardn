// pkg/menu/run_all_handler.go
package menu

import (
	"fmt"
	"strings"

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

// Handle processes the Run All menu option with prerequisite checks
func (h *RunAllHandler) Handle() {
	// Check for prerequisites
	if h.config.Username == "" && !h.config.DryRun {
		// For actual runs (not dry-run), having a username is essential
		fmt.Printf("\n%s No username defined for user creation\n",
			style.Colored(style.Yellow, style.SymWarning))
		fmt.Printf("%s Would you like to set a username now? (y/n): ", style.BulletItem)

		confirm := ReadInput()
		if strings.ToLower(confirm) == "y" || strings.ToLower(confirm) == "yes" {
			// Launch the user menu to set a username first
			userMenu := NewUserMenu(h.menuManager, h.config, h.osInfo)
			userMenu.Show()

			// If still no username, abort Run All
			if h.config.Username == "" {
				fmt.Printf("\n%s Run All requires a username for user creation. Operation cancelled.\n",
					style.Colored(style.Red, style.SymCrossMark))
				fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
				ReadKey()
				return
			}
		} else {
			// User chose not to set a username, continue with warning
			fmt.Printf("\n%s Continuing without user creation\n",
				style.Colored(style.Yellow, style.SymWarning))
		}
	}

	// Create and show the Run All menu
	runAllMenu := NewRunAllMenu(h.menuManager, h.config, h.osInfo)
	runAllMenu.Show()
}
