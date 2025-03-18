package logging

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/fatih/color"
)

var (
	logger  *log.Logger
	logFile *os.File
	// Add silent mode flag
	silentMode bool
)

// InitLogging initializes the logger for the application
func InitLogging(logPath string) {
	// Create log directory if it doesn't exist
	dir := filepath.Dir(logPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("Failed to create log directory: %v\n", err)
		}
	}

	// Open log file
	var err error
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("Failed to open log file %s: %v\n", logPath, err)
		logFile = nil
	}

	// Create logger
	if logFile != nil {
		logger = log.New(logFile, "", log.LstdFlags)
	} else {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
}

// CloseLogging closes the log file
func CloseLogging() {
	if logFile != nil {
		if err := logFile.Close(); err != nil {
			fmt.Printf("Failed to close log file: %v\n", err)
		}
	}
}

// SetSilentMode enables or disables console logging output
func SetSilentMode(silent bool) {
	silentMode = silent
}

// IsSilent returns the current silent mode state
func IsSilent() bool {
	return silentMode
}

// LogError logs an error message
func LogError(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if !silentMode {
		color.Red("[ERROR] %s", msg)
	}
	if logger != nil {
		logger.Printf("ERROR: %s", msg)
	}
}

// LogWarning logs a warning message
func LogWarning(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if !silentMode {
		color.Yellow("[WARNING] %s", msg)
	}
	if logger != nil {
		logger.Printf("WARNING: %s", msg)
	}
}

// LogInfo logs an info message
func LogInfo(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if !silentMode {
		color.Blue("[INFO] %s", msg)
	}
	if logger != nil {
		logger.Printf("INFO: %s", msg)
	}
}

// LogSuccess logs a success message
func LogSuccess(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if !silentMode {
		color.Green("[SUCCESS] %s", msg)
	}
	if logger != nil {
		logger.Printf("SUCCESS: %s", msg)
	}
}

// LogInstall logs a package installation
func LogInstall(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	if !silentMode {
		color.Cyan("[INSTALLED] %s", msg)
	}
	if logger != nil {
		logger.Printf("INSTALLED: %s", msg)
	}
}

// PrintLogs prints the content of the log file
func PrintLogs(logPath string) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		LogError("Failed to read log file %s: %v", logPath, err)
		return
	}

	fmt.Printf("\n# Contents of %s:\n\n", logPath)
	fmt.Println(string(data))
}
