package main

// hardn's console lines outside the screens: the root check, the config
// line, confirm's and rollback's reports, -g's and -r's errors
// (docs/engine/legacy-left.md, decision 5 (a), adopted 2026-11-04). Each is
// a component.Message on stderr, rendered for stderr (stderrEnv), as
// warnDeprecations writes its warnings: its kind's look on a colour
// terminal, plain off one. stdout holds a command's output alone (defect
// 28's rule). Each is a line of hardn's log too, at its kind's level
// (output.Log), as pkg/logging wrote it to both; the log's format is
// unchanged. Under the systemd timer and the boot unit stderr is the
// journal, as stdout is, so confirm's and rollback's lines still reach it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
)

// hardnLog is hardn's log file, LegacyLogPath, opened (openLog) once a
// command that writes it runs, after its flags are parsed (openStartLog);
// nil, writing nothing, until then, and for the commands that do not. Where it could not be opened it is
// stderr (logOnStderr), as pkg/logging's fallback was.
var hardnLog *output.Log

// logOnStderr is set when hardnLog is stderr: a console line (say), on
// stderr already, is then not logged, so it is not said twice there.
var logOnStderr bool

// openLog opens hardn's log at path, as hardn has since v0.3.x: its
// directory made 0755 where missing, the file opened O_CREATE|O_APPEND,
// 0644. Before it opens the file it rotates it (rotateLog), so the copy is
// made before this command's first line; a log not rotated is one warning,
// and the file is opened and appended to as it is. Each step that fails, as for a user other than root, is said on
// stderr as a warning, in pkg/logging's words ("Failed to create log
// directory: …", "Failed to open log file <path>: …"), and the log is then
// stderr, as pkg/logging's was: the log-only lines (revertFileLog, the
// engine's hangup and session lines) reach it there.
func openLog(path string) {
	var run component.Messages
	failed := func(text string) { run = append(run, component.Message{Kind: component.MessageWarn, Text: text}) }
	made := true
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			made = false
			failed(fmt.Sprintf("Failed to create log directory: %v", err))
		}
	}
	var notRotated error
	if made {
		notRotated = rotateLog(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if errors.Is(err, fs.ErrNotExist) && made {
		// Renamed away as it was opened, by another hardn rotating it: once more.
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	if err != nil {
		failed(fmt.Sprintf("Failed to open log file %s: %v", path, err))
		hardnLog, logOnStderr = output.NewLog(stderr), true
	} else {
		// Said only of a log that opened: one that did not has its own warning.
		if notRotated != nil {
			failed(output.NotRotated(path, notRotated))
		}
		hardnLog, logOnStderr = output.NewLog(f), false
	}
	writeConsole(run)
}

// rotateLog renames hardn's log at path to path.1 where it is over
// output.LogMaxSize, the older copies moved up and the oldest of
// output.LogKeep deleted (host.System.RotateFile, on a host with no run
// log). Its directory need not be root's alone: /var/log is its group's to
// write on some releases, and no name is followed. Only openLog calls it,
// so only a command that writes the log rotates it: never hardn check, the
// Logs screen or -p (docs/engine/design-log-rotation.md, decision 6).
func rotateLog(path string) error {
	if !filepath.IsAbs(path) {
		return nil
	}
	_, err := host.NewReal(host.Options{}).RotateFile(context.Background(), path, output.LogMaxSize, output.LogKeep, false)
	return err
}

// logLevel is the log file's level for a console line of kind k.
func logLevel(k component.MessageKind) string {
	switch k {
	case component.MessageOK:
		return output.LogSuccess
	case component.MessageWarn:
		return output.LogWarning
	case component.MessageBad:
		return output.LogError
	}
	return output.LogInfo
}

// say writes one console line of kind k, formatted as fmt.Sprintf formats
// it, on stderr and to hardn's log.
func say(k component.MessageKind, format string, args ...any) {
	sayRun(component.Message{Kind: k, Text: fmt.Sprintf(format, args...)})
}

// sayRun writes run, a report of several lines, on stderr as one
// component.Messages, so that a kind's title is said once over its lines,
// and each line to hardn's log.
func sayRun(run ...component.Message) {
	for _, m := range run {
		if !logOnStderr {
			hardnLog.Printf(logLevel(m.Kind), "%s", m.Text)
		}
	}
	writeConsole(run)
}

// writeConsole renders run for stderr and writes it there.
func writeConsole(run component.Messages) {
	env := stderrEnv()
	if out := run.Render(component.NewCtx(env)); out != "" {
		_, _ = io.WriteString(env.Writer(stderr), out+"\n")
	}
}

// Shorthands for a console line of each kind.
func sayError(format string, args ...any)   { say(component.MessageBad, format, args...) }
func sayWarning(format string, args ...any) { say(component.MessageWarn, format, args...) }
func sayInfo(format string, args ...any)    { say(component.MessageInfo, format, args...) }
func saySuccess(format string, args ...any) { say(component.MessageOK, format, args...) }
