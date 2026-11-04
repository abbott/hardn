package logging

import (
	"fmt"
	"io"
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

// FallbackOutput is where the log goes when InitLogging cannot open its
// file: stderr, unless the program sets it before InitLogging, as
// cmd/hardn does to its own stderr writer, which drops what it is given
// once hardn's terminal has hung up (next/s3-hangup).
var FallbackOutput io.Writer = os.Stderr

// InitLogging initializes the logger for the application
func InitLogging(logPath string) {
	// Create log directory if it doesn't exist
	dir := filepath.Dir(logPath)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create log directory: %v\n", err)
		}
	}

	// Open log file
	var err error
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open log file %s: %v\n", logPath, err)
		logFile = nil
	}

	// Create logger
	if logFile != nil {
		logger = log.New(logFile, "", log.LstdFlags)
	} else {
		logger = log.New(FallbackOutput, "", log.LstdFlags)
	}
}

// SetSilentMode enables or disables console logging output
func SetSilentMode(silent bool) {
	silentMode = silent
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

// LogFileOnly logs an info message to the log file alone, never the
// console, as LogInfo writes it there: for a record too long for the
// screen, such as the commands `hardn confirm`'s clean-up runs and their
// output, which the command sums up on the console itself (refactor plan
// P3, next/p3-confirm-cleanup).
func LogFileOnly(format string, v ...interface{}) {
	if logger != nil {
		logger.Printf("INFO: %s", fmt.Sprintf(format, v...))
	}
}
