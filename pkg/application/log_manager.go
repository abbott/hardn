// pkg/application/logs_manager.go
package application

import (
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/domain/service"
)

// LogsManager is an application service for log operations
type LogsManager struct {
	logsService service.LogsService
}

// NewLogsManager creates a new LogsManager
func NewLogsManager(logsService service.LogsService) *LogsManager {
	return &LogsManager{
		logsService: logsService,
	}
}

// GetLogs retrieves logs from the system
func (m *LogsManager) GetLogs() ([]model.LogEntry, error) {
	return m.logsService.GetLogs()
}

// GetLogConfig retrieves the current log configuration
func (m *LogsManager) GetLogConfig() (*model.LogsConfig, error) {
	return m.logsService.GetLogConfig()
}

// PrintLogs prints the logs to the console
func (m *LogsManager) PrintLogs() error {
	return m.logsService.PrintLogs()
}