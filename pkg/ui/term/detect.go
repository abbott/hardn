package term

import (
	"os"

	"charm.land/lipgloss/v2"
	xterm "github.com/charmbracelet/x/term"
)

// queryDarkBackground asks the terminal for its background colour. It is a
// variable so tests can check when Detect calls it.
var queryDarkBackground = lipgloss.HasDarkBackground

// Detect observes the process (whether stdin and stdout are terminals, the
// terminal size and os.Environ), resolves the result with opts (see Resolve)
// and returns the Env to pass to every renderer. It is the only function in
// this package that touches the terminal. Call it once at startup, before
// the first prompt.
//
// Background detection is opt-in: Detect queries the terminal only when the
// theme resolves to auto (--theme=auto or HARDN_THEME=auto), stdin and
// stdout are both terminals and the Env is not plain. It then calls
// lipgloss.HasDarkBackground once and keeps the dark theme if the query
// fails. The query is not free, which is why it is opt-in. In Lip Gloss
// v2.0.6 (query.go, terminal.go) it puts the terminal into raw mode with
// ISIG, ICANON and ECHO off (x/term term_unix.go:31), writes an OSC 11
// background request and a DA1 request, and reads the reply for up to 2 s
// (defaultQueryTimeout, query.go:49). It tries stdin first and, if that
// fails or times out, stdout for another 2 s (terminal.go:60-64): up to
// about 4 s in which Ctrl-C does not interrupt, keys typed ahead are read
// and discarded, and a reply that arrives after the timeout is left in stdin
// for the next reader.
func Detect(opts Options, stdin, stdout *os.File) Env {
	f := Facts{
		StdoutTTY: xterm.IsTerminal(stdout.Fd()),
		StdinTTY:  xterm.IsTerminal(stdin.Fd()),
		Environ:   os.Environ(),
	}
	if f.StdoutTTY {
		if w, h, err := xterm.GetSize(stdout.Fd()); err == nil {
			f.Width, f.Height = w, h
		}
	}
	e := Resolve(opts, f)
	if e.queriesBackground() {
		e.Dark = queryDarkBackground(stdin, stdout)
	}
	return e
}

// queriesBackground reports whether Detect asks the terminal for its
// background colour (see Detect).
func (e Env) queriesBackground() bool {
	return e.Theme == ThemeAuto && e.TTY && e.InputTTY && !e.Plain
}
