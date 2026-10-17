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
// so the terminal echoes and edits the line itself. On a terminal,
// typeahead is discarded before a menu choice (ReadMenuInput) and a pause
// (ReadKey), which follow the long steps whose stray keys must not answer
// them (brief 1C). A free-text question (ReadInput) keeps it, as
// origin/main's one bufio reader did and pkg/config's questions do: such
// questions come back to back, as the create-user and firewall profile
// fields do, so each answer pasted ahead reaches the question it was
// meant for.
//
// Ctrl-C at a prompt ends hardn as Ctrl-C ends any program, by SIGINT
// (prompt.Reraise), so that a waiting parent sees it killed by the signal,
// as origin/main was at a pause or a ReadInput prompt (raw mode dropped
// Ctrl-C at a menu prompt), and a shell running hardn in a loop or a
// script stops; a shell reports it with status 130. The end of the input
// ends hardn with exit status 130 (prompt.ExitCode), brief Phase 3's code
// for an abort. The legacy screens return no error to carry an abort out
// of their nested loops, and ShowMainMenu returns nothing to cmd/hardn,
// so the read ends the process itself; no call site changes.
var (
	// input returns the Reader the prompts read from. Tests replace it.
	input = prompt.Stdin
	// exit ends hardn. Tests replace it.
	exit = os.Exit
	// reraise ends hardn by SIGINT after Ctrl-C. Tests replace it.
	reraise = prompt.Reraise
)

// readLine reads the answer to the prompt the caller has just printed,
// trimmed of spaces, discarding typeahead first when flush is set
// (prompt.Reader.Answer) and keeping it otherwise
// (prompt.Reader.AnswerNext). A line too long to read (prompt.MaxLine)
// reads as an empty answer. On Ctrl-C it ends hardn by SIGINT (reraise),
// at the end of the input with exit status 130, and on any other read
// error it prints the error and ends hardn with status 1, as cmd/hardn
// does for an error.
func readLine(flush bool) string {
	r := input()
	answer := r.AnswerNext
	if flush {
		answer = r.Answer
	}
	line, err := answer(os.Stdout)
	if err == nil || errors.Is(err, prompt.ErrLineTooLong) {
		return strings.TrimSpace(line)
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
	return ""
}

// ReadInput reads a line of input from the user, trimmed of spaces: the
// answer to a free-text question. It keeps typeahead, so that answers
// pasted ahead into back-to-back questions each reach their own.
func ReadInput() string {
	return readLine(false)
}

// ReadKey waits for Enter after a "Press Enter ..." pause and returns the
// line typed, trimmed of spaces. Despite its name it reads a whole line:
// every input is ended with Enter (brief §2). On a terminal it discards
// typeahead first.
func ReadKey() string {
	return readLine(true)
}

// ReadMenuInput reads a menu choice: a line, trimmed of spaces, with
// letters in lower case, so "Q" quits as "q" does. The choice takes
// effect on Enter, "q" included. On a terminal it discards typeahead
// first.
func ReadMenuInput() string {
	return strings.ToLower(readLine(true))
}
