// pkg/domain/model/logs.go
package model

// LogEntry represents a single log entry
type LogEntry struct {
	Level   string
	Message string
	Time    string
}

// LogsConfig represents log configuration settings
type LogsConfig struct {
	LogFilePath string
}