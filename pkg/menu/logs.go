// pkg/menu/logs.go

package menu

import (
	"fmt"

	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/style"
	"github.com/abbott/hardn/pkg/utils"
)

// ViewLogsMenu displays the contents of the log file
func ViewLogsMenu(cfg *config.Config) {
	utils.PrintHeader()
	fmt.Println(style.Bolded("View Logs", style.Blue))

	// Display log file path
	fmt.Printf("\n%s Log file: %s\n", 
		style.BulletItem, style.Colored(style.Cyan, cfg.LogFile))
	
	// Print separator before log content
	fmt.Println(style.Bolded("\nLog Contents:", style.Blue))
	fmt.Println(style.Dimmed("-----------------------------------------------------"))

	// Use the logging package to print the logs
	logging.PrintLogs(cfg.LogFile)

	fmt.Printf("\n%s Press any key to return to the main menu...", style.BulletItem)
	ReadKey()
}