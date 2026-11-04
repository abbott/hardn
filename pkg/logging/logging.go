// Package logging is what is left of origin/main's logger: the two calls
// the legacy packages still make, until they go (docs/engine/legacy-left.md
// §2, step 7). hardn's log file is pkg/output's Log, and its console lines
// cmd/hardn's (say), component.Messages on stderr (decision 5 (a), adopted
// 2026-11-04).
package logging

import "fmt"

// silentMode turns LogSuccess's line off.
var silentMode bool

// SetSilentMode turns LogSuccess's line off, or back on: pkg/adapter's.
func SetSilentMode(silent bool) {
	silentMode = silent
}

// LogSuccess prints "[SUCCESS] " and the message on stdout, unless silent:
// pkg/osdetect's DetectOS, which start-up no longer runs, and which only
// pkg/security's status reaches.
func LogSuccess(format string, v ...interface{}) {
	if !silentMode {
		fmt.Printf("[SUCCESS] %s\n", fmt.Sprintf(format, v...))
	}
}
