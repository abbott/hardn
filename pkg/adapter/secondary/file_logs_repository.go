// pkg/adapter/secondary/file_logs_repository.go
package secondary

import (
	"bufio"
	"fmt"
	"strings"
	
	"github.com/abbott/hardn/pkg/domain/model"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/port/secondary"
)

// FileLogsRepository implements LogsRepository using file operations
type FileLogsRepository struct {
	fs interfaces.FileSystem
	logFilePath string
}

// NewFileLogsRepository creates a new FileLogsRepository
func NewFileLogsRepository(
	fs interfaces.FileSystem,
	logFilePath string,
) secondary.LogsRepository {
	return &FileLogsRepository{
		fs: fs,
		logFilePath: logFilePath,
	}
}

// GetLogs retrieves logs from the configured log file
func (r *FileLogsRepository) GetLogs() ([]model.LogEntry, error) {
	// Check if log file exists
	_, err := r.fs.Stat(r.logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to access log file: %w", err)
	}
	
	// Read log file
	data, err := r.fs.ReadFile(r.logFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read log file: %w", err)
	}
	
	// Parse log entries
	var entries []model.LogEntry
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		
		// Parse log line (assuming a simple format of "TIME LEVEL: MESSAGE")
		parts := strings.SplitN(line, " ", 3)
		if len(parts) >= 3 {
			// Extract time and message parts
			timeStr := parts[0]
			levelStr := strings.TrimSuffix(parts[1], ":")
			messageStr := parts[2]
			
			// Create log entry
			entry := model.LogEntry{
				Time: timeStr,
				Level: levelStr,
				Message: messageStr,
			}
			
			entries = append(entries, entry)
		}
	}
	
	return entries, nil
}

// GetLogConfig retrieves the current log configuration
func (r *FileLogsRepository) GetLogConfig() (*model.LogsConfig, error) {
	return &model.LogsConfig{
		LogFilePath: r.logFilePath,
	}, nil
}

// PrintLogs prints the logs to the console
func (r *FileLogsRepository) PrintLogs() error {
	// Check if log file exists
	_, err := r.fs.Stat(r.logFilePath)
	if err != nil {
		return fmt.Errorf("failed to access log file %s: %w", r.logFilePath, err)
	}
	
	// Read log file
	data, err := r.fs.ReadFile(r.logFilePath)
	if err != nil {
		return fmt.Errorf("failed to read log file %s: %w", r.logFilePath, err)
	}
	
	// Print log contents
	fmt.Printf("\n# Contents of %s:\n\n", r.logFilePath)
	fmt.Println(string(data))
	
	return nil
}