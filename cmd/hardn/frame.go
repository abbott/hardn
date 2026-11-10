package main

import (
	"io"
	"os"
	"sync"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The output frame: what hardn writes to a terminal in a run starts after a
// blank line and ends with one, so that it doesn't sit flush against the
// shell's prompt above or below (the maintainer, 2026-11-10). stdout and
// stderr share one frame, as a terminal shows them as one stream: the
// opening blank line comes before the first byte either writes, and the
// closing one after the last, on the stream written last. A pipe, a regular
// file, the journal and the null device get nothing, so redirected output,
// JSON and what the revert timer logs stay as they were.
//
// The main menu keeps its own rows (it clears the screen, and leaves two
// rows above the prompt as it ends), and system-details' JSON is one
// document: both disarm the frame. pkg/config's first-run question and its
// prompts are written inside it (config.Stdout, BeforeInterrupt).

// frame is the output frame's state.
type frame struct {
	mu     sync.Mutex
	opened bool      // the opening blank line is written, or the output began with one
	done   bool      // closed or disarmed: writes pass through
	tail   []byte    // the last two bytes written inside the frame
	last   io.Writer // the stream written last
	echo   uint64    // echoed's count at the last write
	// echoed counts the answers whose line an echoed Enter ended
	// (prompt.Echoed when nil): one since the last write ended its line.
	echoed func() uint64
}

// outFrame is the run's frame, around termStdout, termStderr and cobra's
// streams where each is a terminal.
var outFrame = &frame{}

// frameTerminal reports whether f is a terminal, and so inside the frame
// (isTerminal, as the rollback notice asks).
func frameTerminal(f *os.File) bool { return isTerminal(f) }

// framed is w, a stream on a terminal, inside f.
type framed struct {
	f *frame
	w io.Writer
}

// Write writes p to the stream, after the opening blank line when p is the
// frame's first output and doesn't start with a blank line of its own.
func (fw framed) Write(p []byte) (int, error) {
	f := fw.f
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done || len(p) == 0 {
		return fw.w.Write(p)
	}
	if !f.opened {
		f.opened = true
		if p[0] != '\n' {
			if _, err := io.WriteString(fw.w, "\n"); err != nil {
				return 0, err
			}
		}
	}
	n, err := fw.w.Write(p)
	if n > 0 {
		f.tail = append(f.tail, p[max(0, n-2):n]...)
		f.tail = f.tail[max(0, len(f.tail)-2):]
		f.last = fw.w
		f.echo = f.answers()
	}
	return n, err
}

// answers is echoed's count.
func (f *frame) answers() uint64 {
	if f.echoed != nil {
		return f.echoed()
	}
	return prompt.Echoed()
}

// close ends the frame with the newlines that leave one blank line after
// what was written: none after a blank line already (cobra's usage error),
// one after a line's newline, or after a prompt the answer's echoed Enter
// ended, two after a line left open. Nothing when nothing was written.
// Writes after it pass through.
func (f *frame) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return
	}
	f.done = true
	if f.last == nil {
		return
	}
	n := 2
	switch t := f.tail; {
	case f.answers() != f.echo:
		n = 1
	case len(t) == 2 && t[0] == '\n' && t[1] == '\n':
		n = 0
	case len(t) > 0 && t[len(t)-1] == '\n':
		n = 1
	}
	_, _ = io.WriteString(f.last, "\n\n"[:n])
}

// disarm ends the frame with nothing more written: for output that keeps its
// own spacing (the main menu) or holds one document (JSON).
func (f *frame) disarm() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = true
}

// stdStream is os.Stdout or os.Stderr as it is at each write, inside the
// frame where it is a terminal: cobra's help, errors and usage, which it
// writes itself (main.go's init). Looked up at each write, as the tests
// swap the two.
type stdStream func() *os.File

func (s stdStream) Write(p []byte) (int, error) {
	f := s()
	if frameTerminal(f) {
		return framed{outFrame, f}.Write(p)
	}
	return f.Write(p)
}

// endRun closes the frame, then ends hardn with code: the exit seams'
// default (exit, revertExit).
func endRun(code int) {
	outFrame.close()
	os.Exit(code)
}

// endByInterrupt closes the frame, then ends hardn by SIGINT
// (prompt.Reraise), after which nothing runs.
func endByInterrupt() {
	outFrame.close()
	prompt.Reraise()
}
