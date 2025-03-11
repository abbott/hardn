// pkg/menu/dry_run_handler.go
package menu

import (
	"fmt"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// DryRunHandler handles the Dry Run functionality
type DryRunHandler struct {
	menuManager *application.MenuManager
	config      *config.Config
}

// NewDryRunHandler creates a new DryRunHandler
func NewDryRunHandler(
	menuManager *application.MenuManager,
	config *config.Config,
) *DryRunHandler {
	return &DryRunHandler{
		menuManager: menuManager,
		config:      config,
	}
}

// Handle creates and displays the dry-run configuration menu
func (h *DryRunHandler) Handle() {
	// Display contextual information about dry-run mode
	utils.PrintHeader()
	fmt.Println(style.Bolded("Dry-Run Mode Configuration", style.Blue))

	fmt.Println()
	fmt.Println(style.Dimmed("Dry-run mode allows you to preview changes without applying them to your system."))
	fmt.Println(style.Dimmed("This is useful for testing and understanding what actions will be performed."))

	// Check if any critical operations have been performed
	// This is just an example - you'd need to track this information
	criticalChanges := false // Placeholder for tracking if changes have been made

	if criticalChanges && h.config.DryRun {
		fmt.Printf("\n%s You've already performed operations in dry-run mode.\n",
			style.Colored(style.Yellow, style.SymInfo))
		fmt.Printf("%s Disabling dry-run mode will apply future changes for real.\n",
			style.BulletItem)
	}

	fmt.Println()
	fmt.Printf("%s Press any key to continue to dry-run configuration...", style.BulletItem)
	ReadKey()

	// Create and show the dry-run menu
	dryRunMenu := NewDryRunMenu(h.menuManager, h.config)
	dryRunMenu.Show()

	// After returning from the dry-run menu, inform about the status
	utils.PrintHeader()

	// Quick feedback on the configuration change before returning to main menu
	fmt.Printf("\n%s Dry-run mode is now %s\n",
		style.Colored(style.Cyan, style.SymInfo),
		style.Bolded(map[bool]string{
			true:  "ENABLED - Changes will only be simulated",
			false: "DISABLED - Changes will be applied to the system",
		}[h.config.DryRun], map[bool]string{
			true:  style.Green,
			false: style.Yellow,
		}[h.config.DryRun]))

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}
