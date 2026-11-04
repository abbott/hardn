package output

import (
	"fmt"
	"io"
	"log"
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

// NewLog is a Log writing to w: cmd/hardn's open LegacyLogPath (its
// directory made 0755 where missing, the file O_CREATE|O_APPEND, 0644, as
// since v0.3.x), which it opens itself, as nothing linked by the gallery
// may (uigallery's TestPlayTouchesNothing).
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
