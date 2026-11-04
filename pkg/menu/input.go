// pkg/menu/input.go
package menu

import (
	"errors"
	"fmt"
	"os"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// The main menu reads a line, typed and ended with Enter, through the
// process-wide prompt.Stdin, the one reader on stdin that pkg/config and
// the screens share (brief §2, Enter-based input; refactor plan risk R17).
// The terminal stays in cooked mode: nothing here runs stty, so the
// terminal echoes and edits the line itself.
//
// Ctrl-C at its prompt ends hardn as Ctrl-C ends any program, by SIGINT
// (prompt.Reraise), so that a waiting parent sees it killed by the signal,
// and a shell running hardn in a loop or a script stops; a shell reports
// it with status 130. The end of the input ends hardn with exit status 130
// (prompt.ExitCode), brief Phase 3's code for an abort. ShowMainMenu
// returns nothing to cmd/hardn, so it ends the process itself (endHardn).
var (
	// input returns the Reader the prompts read from. Tests replace it.
	input = prompt.Stdin
	// exit ends hardn. Tests replace it.
	exit = os.Exit
	// reraise ends hardn by SIGINT after Ctrl-C. Tests replace it.
	reraise = prompt.Reraise
)

// endHardn ends hardn after err, as a legacy prompt ends it after a read
// that failed: by SIGINT after Ctrl-C (reraise), with exit status 130 at
// the end of the input, and with 1, once err is printed, for any other
// error.
func endHardn(err error) {
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
