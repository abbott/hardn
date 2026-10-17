package host

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// RunLog appends one entry per command to a writer. Every line of an entry
// starts with "run <n>: ", so entries of commands that overlap stay apart:
//
//	run 3: $ ufw allow 2230/tcp
//	run 3: stdin: 12 bytes (not logged)
//	run 3: | Rule added
//	run 3: exit 0
//
// The header is written before the command starts, the output and the exit
// line after it ends. Logging is best effort: a failing writer never stops a
// command (a rollback must still run on a full disk); the first write error is
// kept for Err. A RunLog is safe for concurrent use.
type RunLog struct {
	mu  sync.Mutex
	w   io.Writer
	seq int
	err error
}

// NewRunLog returns a RunLog writing to w; nil discards.
func NewRunLog(w io.Writer) *RunLog {
	if w == nil {
		w = io.Discard
	}
	return &RunLog{w: w}
}

// Begin writes the header of a new entry and returns its number. stdinLen is
// the length of the command's stdin, or -1 when it has none.
func (l *RunLog) Begin(argv []string, stdinLen int) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	var b bytes.Buffer
	fmt.Fprintf(&b, "run %d: $ %s\n", l.seq, FormatArgv(argv))
	if stdinLen >= 0 {
		fmt.Fprintf(&b, "run %d: stdin: %d bytes (not logged)\n", l.seq, stdinLen)
	}
	l.write(b.Bytes())
	return l.seq
}

// End writes the output and outcome of entry id. err is Run's error.
func (l *RunLog) End(id int, res Result, err error) {
	var b bytes.Buffer
	if len(res.Combined) > 0 {
		for _, line := range strings.Split(strings.TrimSuffix(string(res.Combined), "\n"), "\n") {
			fmt.Fprintf(&b, "run %d: | %s\n", id, line)
		}
	}
	switch {
	case err == nil:
		fmt.Fprintf(&b, "run %d: exit %d\n", id, res.ExitCode)
	case isExit(err):
		fmt.Fprintf(&b, "run %d: exit %d (%v)\n", id, res.ExitCode, err)
	default:
		fmt.Fprintf(&b, "run %d: not run or not finished: %v\n", id, err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.write(b.Bytes())
}

// Err returns the first error the writer returned, if any.
func (l *RunLog) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

func (l *RunLog) write(p []byte) {
	if _, err := l.w.Write(p); err != nil && l.err == nil {
		l.err = err
	}
}

func isExit(err error) bool {
	var xe *ExitError
	return errors.As(err, &xe)
}

// FormatArgv renders argv with one space between elements. An element is
// quoted (Go syntax) when it is empty or holds anything a shell would split
// or interpret, so the element boundaries stay unambiguous; nothing is ever
// passed to a shell.
func FormatArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n\r\"'\\|&;<>()$`*?[]#~{}!") {
			parts[i] = strconv.Quote(a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}

// TailLines returns the last n lines of out, without their line endings
// ("\n" or "\r\n"); nil when out is empty or n <= 0.
func TailLines(out []byte, n int) []string {
	if len(out) == 0 || n <= 0 {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}
