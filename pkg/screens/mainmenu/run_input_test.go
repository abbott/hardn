package mainmenu

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The main menu as hardn runs it: Run, its error ending hardn as
// cmd/hardn's runMenu ends it (prompt.Reraise after Ctrl-C, exit status
// 130 at the end of the input, the error printed and 1 otherwise), with
// the input, the exit and the signal stubbed (useReader) and stdout
// captured (session).
var (
	// input returns the Reader the main menu reads from (Options.Input).
	input = prompt.Stdin
	// exit ends hardn: stubbed by useReader.
	exit = func(code int) { panic(fmt.Sprintf("exit %d not stubbed", code)) }
	// reraise ends hardn by SIGINT after Ctrl-C: stubbed by useReader.
	reraise = func() { panic("reraise not stubbed") }
)

// show runs m as hardn runs the main menu, and ends hardn as cmd/hardn
// does when it returns an error (end).
func show(m *Options) { end(Run(context.Background(), *m)) }

// end ends hardn after err, as cmd/hardn's runMenu does: by SIGINT after
// Ctrl-C (reraise), with exit status 130 at the end of the input, and with
// 1, once err is printed, for any other error; nil ends nothing.
func end(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, prompt.ErrInterrupted) {
		reraise()
	}
	code, aborted := prompt.ExitCode(err)
	if !aborted {
		fmt.Println(err)
		code = 1
	}
	exit(code)
}

// stdout is os.Stdout as it is when written to, so that captureStdout
// takes what the main menu writes (Options.Out).
type stdout struct{}

func (stdout) Write(p []byte) (int, error) { return os.Stdout.Write(p) }

// The items' fields, each set as a test's screenItem sets one.
func setUsers(o *Options, run func() error)         { o.Users = run }
func setSSH(o *Options, run func() error)           { o.SSH = run }
func setDNS(o *Options, run func() error)           { o.DNS = run }
func setBackup(o *Options, run func() error)        { o.Backup = run }
func setUpdates(o *Options, run func() error)       { o.Updates = run }
func setRunAll(o *Options, run func() error)        { o.RunAll = run }
func setEnvironment(o *Options, run func() error)   { o.Environment = run }
func setSystemDetails(o *Options, run func() error) { o.SystemDetails = run }
func setLogs(o *Options, run func() error)          { o.Logs = run }

// sgr matches the CSI sequences a terminal's drawing holds.
var sgr = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI is s without its escape sequences: the text a terminal shows.
func stripANSI(s string) string { return sgr.ReplaceAllString(s, "") }

// exited is what the stubbed exit panics with, in place of ending the
// test binary: the exit status hardn would have ended with, or sigint.
type exited int

// sigint is what the stubbed reraise panics with: hardn ended by SIGINT
// after Ctrl-C (prompt.Reraise), which a parent sees as a signal, not an
// exit status.
const sigint exited = -2

// useReader makes the main menu read from r and stubs exit and reraise,
// until the test ends.
func useReader(t *testing.T, r *prompt.Reader) {
	t.Helper()
	prevInput, prevExit, prevReraise := input, exit, reraise
	input = func() *prompt.Reader { return r }
	exit = func(code int) { panic(exited(code)) }
	reraise = func() { panic(sigint) }
	t.Cleanup(func() { input, exit, reraise = prevInput, prevExit, prevReraise })
}

// scripted makes the main menu read script, as from a pipe or a file:
// not a terminal, so nothing is flushed.
func scripted(t *testing.T, script string) {
	t.Helper()
	useReader(t, prompt.New(strings.NewReader(script), false))
}

// session runs fn as hardn runs a screen and returns what it printed, and
// the exit status it ended hardn with, if it did.
func session(t *testing.T, fn func()) (out string, code int, ended bool) {
	t.Helper()
	out = captureStdout(t, func() {
		defer func() {
			if r := recover(); r != nil {
				e, ok := r.(exited)
				if !ok {
					panic(r)
				}
				code, ended = int(e), true
			}
		}()
		fn()
	})
	return out, code, ended
}

// captureStdout returns what fn writes to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	defer func() { os.Stdout = orig }()
	fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return <-done
}
