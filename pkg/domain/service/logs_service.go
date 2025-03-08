// pkg/domain/service/logs_service.go
package service

import "github.com/abbott/hardn/pkg/domain/model"

// LogsService defines operations for log management
type LogsService interface {
	// GetLogs retrieves logs from the configured log file
	GetLogs() ([]model.LogEntry, error)

	// GetLogConfig retrieves the current log configuration
	GetLogConfig() (*model.LogsConfig, error)

	// PrintLogs prints the logs to the console
	PrintLogs() error
}

// LogsServiceImpl implements LogsService
type LogsServiceImpl struct {
	repository LogsRepository
}

// NewLogsServiceImpl creates a new LogsServiceImpl
func NewLogsServiceImpl(repository LogsRepository) *LogsServiceImpl {
	return &LogsServiceImpl{
		repository: repository,
	}
}

// LogsRepository defines the repository operations needed by LogsService
type LogsRepository interface {
	GetLogs() ([]model.LogEntry, error)
	GetLogConfig() (*model.LogsConfig, error)
	PrintLogs() error
}

// Implementation of LogsService methods
func (s *LogsServiceImpl) GetLogs() ([]model.LogEntry, error) {
	return s.repository.GetLogs()
}

func (s *LogsServiceImpl) GetLogConfig() (*model.LogsConfig, error) {
	return s.repository.GetLogConfig()
}

func (s *LogsServiceImpl) PrintLogs() error {
	return s.repository.PrintLogs()
}
