// pkg/menu/input.go
package menu

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// Every legacy prompt reads a line, typed and ended with Enter, through
// the process-wide prompt.Stdin, the one reader on stdin that pkg/config
// and the new screens share (brief §2, Enter-based input; refactor plan
// risk R17). The terminal stays in cooked mode: nothing here runs stty,
// so the terminal echoes and edits the line itself, and on a terminal
// typeahead is discarded before each read (brief 1C).
//
// Ctrl-C at a prompt, or the end of the input, ends hardn with exit
// status 130 (prompt.ExitCode). The legacy screens return no error to
// carry an abort out of their nested loops, and ShowMainMenu returns
// nothing to cmd/hardn, so the read ends the process itself, as SIGINT's
// default action did at a ReadInput prompt before; no call site changes.
var (
	// input returns the Reader the prompts read from. Tests replace it.
	input = prompt.Stdin
	// exit ends hardn. Tests replace it.
	exit = os.Exit
)

// readLine reads the answer to the prompt the caller has just printed,
// trimmed of spaces. A line too long to read (prompt.MaxLine) reads as
// an empty answer. On an abort it ends hardn with exit status 130, and on
// any other read error it prints the error and ends hardn with status 1,
// as cmd/hardn does for an error.
func readLine() string {
	line, err := input().Answer(os.Stdout)
	if err == nil || errors.Is(err, prompt.ErrLineTooLong) {
		return strings.TrimSpace(line)
	}
	code, aborted := prompt.ExitCode(err)
	if !aborted {
		fmt.Println(err)
		code = 1
	}
	exit(code)
	return ""
}

// ReadInput reads a line of input from the user, trimmed of spaces.
func ReadInput() string {
	return readLine()
}

// ReadKey waits for Enter after a "Press Enter ..." pause and returns the
// line typed, trimmed of spaces. Despite its name it reads a whole line:
// every input is ended with Enter (brief §2).
func ReadKey() string {
	return readLine()
}

// ReadMenuInput reads a menu choice: a line, trimmed of spaces, with
// letters in lower case, so "Q" quits as "q" does. The choice takes
// effect on Enter, "q" included.
func ReadMenuInput() string {
	return strings.ToLower(readLine())
}
