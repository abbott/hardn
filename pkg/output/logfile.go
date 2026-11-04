package output

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// hardn's log file, LegacyLogPath: pkg/logging's file half, moved here
// beside the audit log (docs/engine/legacy-left.md §2, step 6; decision 5
// (a), adopted 2026-11-04), the line format byte for byte, so that the
// Logs screen and -p (pkg/engine/logview) read it as before. The console
// lines that pkg/logging wrote with each are cmd/hardn's, on stderr.

// The levels a log line takes after its date and time (logview.ParseLine).
const (
	LogError   = "ERROR"
	LogWarning = "WARNING"
	LogInfo    = "INFO"
	LogSuccess = "SUCCESS"
)

// Log is hardn's log file: a line each, Go's log.LstdFlags date and time,
// then the level, ": " and the message. A nil *Log, or one whose file could
// not be opened, writes nothing. It is safe for concurrent use, as the
// log.Logger it wraps is.
type Log struct {
	l *log.Logger
}

// OpenLog opens the log at path, as hardn has since v0.3.x: its directory
// made 0755 where missing, the file opened O_CREATE|O_APPEND, 0644. Each
// step that fails is an error in the returned list, worded as pkg/logging
// worded it ("Failed to create log directory: …", "Failed to open log file
// <path>: …"); the Log is then one that writes nothing, where pkg/logging
// wrote the log's lines to stderr, which now has the console's lines.
func OpenLog(path string) (*Log, []error) {
	var errs []error
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			errs = append(errs, logOpenError{"Failed to create log directory", err})
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, append(errs, logOpenError{"Failed to open log file " + path, err})
	}
	return NewLog(f), errs
}

// logOpenError is a step of OpenLog that failed, in pkg/logging's words.
type logOpenError struct {
	what string
	err  error
}

func (e logOpenError) Error() string { return e.what + ": " + e.err.Error() }
func (e logOpenError) Unwrap() error { return e.err }

// NewLog is a Log writing to w.
func NewLog(w io.Writer) *Log { return &Log{l: log.New(w, "", log.LstdFlags)} }

// Printf writes one line at level, the message formatted as fmt.Sprintf
// formats it.
func (l *Log) Printf(level, format string, args ...any) {
	if l == nil || l.l == nil {
		return
	}
	l.l.Printf("%s: %s", level, fmt.Sprintf(format, args...))
}

// Lines is an io.Writer that writes each line of what it is given to l at
// level, as a run log or the engine's log hands over its lines.
func (l *Log) Lines(level string) io.Writer { return logLines{l, level} }

type logLines struct {
	l     *Log
	level string
}

func (w logLines) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		w.l.Printf(w.level, "%s", line)
	}
	return len(p), nil
}
