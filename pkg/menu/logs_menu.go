// pkg/menu/logs_menu.go
package menu

import (
	"fmt"

	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// LogsMenu handles viewing log information
type LogsMenu struct {
	menuManager *application.MenuManager
	config      *config.Config
}

// NewLogsMenu creates a new LogsMenu
func NewLogsMenu(
	menuManager *application.MenuManager,
	config *config.Config,
) *LogsMenu {
	return &LogsMenu{
		menuManager: menuManager,
		config:      config,
	}
}

// Show displays the logs menu and handles user input
func (m *LogsMenu) Show() {
	utils.PrintHeader()
	fmt.Println(style.Bolded("View Logs", style.Blue))

	// Get log configuration 
	logConfig, err := m.menuManager.GetLogConfig()
	if err != nil {
		fmt.Printf("\n%s Error getting log configuration: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
		// Create a domain model LogsConfig from the application config
		logConfig = &model.LogsConfig{
			LogFilePath: m.config.LogFile,
		}
	}

	// Display log file path
	fmt.Printf("\n%s Log file: %s\n", 
		style.BulletItem, style.Colored(style.Cyan, logConfig.LogFilePath))
	
	// Print separator before log content
	fmt.Println(style.Bolded("\nLog Contents:", style.Blue))
	fmt.Println(style.Dimmed("-----------------------------------------------------"))

	// Use the menu manager to print the logs
	err = m.menuManager.PrintLogs()
	if err != nil {
		fmt.Printf("\n%s Error displaying logs: %v\n", 
			style.Colored(style.Red, style.SymCrossMark), err)
	}

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}