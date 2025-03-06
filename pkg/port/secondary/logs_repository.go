// pkg/port/secondary/logs_repository.go
package secondary

import "github.com/abbott/hardn/pkg/domain/model"

// LogsRepository defines the interface for log operations
type LogsRepository interface {
	// GetLogs retrieves logs from the configured log file
	GetLogs() ([]model.LogEntry, error)
	
	// GetLogConfig retrieves the current log configuration
	GetLogConfig() (*model.LogsConfig, error)
	
	// PrintLogs prints the logs to the console
	PrintLogs() error
}