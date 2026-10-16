package component

import (
	"fmt"
	"strings"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// DefaultTail is the number of lines of captured output an ErrorReport
// shows when its Tail is not set.
const DefaultTail = 10

// checkLog is the sentence that points to the log, origin/main's after
// Run All (pkg/menu run_all_menu.go:295), around the log's path.
const checkLog = "Check the log file at %s for details"

// ErrorReport is the canonical flow's report of an apply that failed
// (brief 1B, 1C): what failed, what was rolled back, the tail of the
// failing command's output, the log path and the suggested next step.
// Here with no colour and ASCII glyphs:
//
//	0 2   6
//	# [!] Operation Failed
//	# Reloading ssh.service failed: exit status 1
//
//	  [<] Rolled back: Set Port 2230
//	  [<] Rolled back: Allow 2230/tcp in UFW
//
//	      $ systemctl reload ssh
//	      ...
//	      Job for ssh.service failed because the control process
//	      exited with error code.
//
//	  [i] Info: Check the log file at /var/log/hardn.log for details
//	  [i] Info: Run sudo sshd -t to see the error.
//
// It is built from the other components (DEV-040). What failed is a
// Callout of kind MessageBad with its own title, "Operation Failed", and
// Failed as its body. Each rolled-back change is a StepProgress line in
// StepRolledBack. The output is code, as a plan's lines are: the command
// after Glyphs.Command, then the last Tail lines of Output, at
// theme.LabelCol in Output, muted, never cut but broken after Glyphs.Wrap;
// a line of Glyphs.Ellipsis first says that earlier lines were left out,
// and the log holds them all. The log path is origin/main's sentence and
// the next step a sentence of its own, each a Message of kind MessageInfo.
// Parts with nothing to show are left out, and theme.SectionGap blank
// lines separate the rest. An ErrorReport always reports a failure, so
// even the zero value shows the callout.
type ErrorReport struct {
	// Failed says what failed and why, such as "Reloading ssh.service
	// failed: exit status 1".
	Failed string
	// RolledBack names the changes undone after the failure, in the order
	// they were undone, such as "Set Port 2230".
	RolledBack []string
	// Command is the command whose output Output holds, such as
	// "systemctl reload ssh"; empty shows none.
	Command string
	// Output is what the command wrote, as captured. A line ending in a
	// carriage return shows only what follows it, as a terminal shows a
	// progress line it overwrote.
	Output string
	// Tail is the most lines of Output shown, from its end; zero or less
	// is DefaultTail.
	Tail int
	// LogPath is the log hardn wrote, such as "/var/log/hardn.log"; empty
	// shows no log line.
	LogPath string
	// Next is the suggested next step, a sentence such as "Run sudo sshd
	// -t to see the error."; empty shows none.
	Next string
}

// Render renders the report for c. See ErrorReport for the layout.
func (vm ErrorReport) Render(c Ctx) string {
	blocks := []string{Callout{Kind: MessageBad, Body: []string{vm.Failed}}.Render(c)}
	var steps []Step
	for _, s := range vm.RolledBack {
		steps = append(steps, Step{Title: s, State: StepRolledBack})
	}
	blocks = append(blocks, StepProgress{Steps: steps}.Render(c), vm.output(c))
	var info []string
	if p := clean(vm.LogPath); p != "" {
		info = append(info, Message{Text: fmt.Sprintf(checkLog, p)}.Render(c))
	}
	info = append(info, Message{Text: vm.Next}.Render(c))
	blocks = append(blocks, strings.Join(nonEmpty(info), "\n"))
	return strings.Join(nonEmpty(blocks), strings.Repeat("\n", theme.SectionGap+1))
}

// output renders the command and the tail of its output.
func (vm ErrorReport) output(c Ctx) string {
	out := c.command(theme.LabelCol, c.Theme.Glyphs.Command+" ", vm.Command, c.Theme.Plan.Command)
	lines := strings.Split(strings.TrimRight(vm.Output, "\r\n"), "\n")
	if strings.TrimSpace(vm.Output) == "" {
		lines = nil
	}
	tail := vm.Tail
	if tail <= 0 {
		tail = DefaultTail
	}
	if len(lines) > tail {
		out = append(out, c.code(theme.LabelCol, c.Theme.Glyphs.Ellipsis, c.Theme.Output)...)
		lines = lines[len(lines)-tail:]
	}
	for _, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		l = l[strings.LastIndex(l, "\r")+1:]
		out = append(out, c.code(theme.LabelCol, raw(l), c.Theme.Output)...)
	}
	return strings.Join(out, "\n")
}

// nonEmpty returns the blocks that show something.
func nonEmpty(blocks []string) []string {
	var out []string
	for _, b := range blocks {
		if b != "" {
			out = append(out, b)
		}
	}
	return out
}
