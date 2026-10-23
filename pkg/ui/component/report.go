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
// Here with no colour and ASCII glyphs, after the four steps of an SSH
// port change, whose widest title is "Remove the rule for port 22":
//
//	0 2   6                            35
//	# Operation Failed
//	# Reloading ssh.service failed: exit status 1
//
//	  [ ] Set Port 2230                Rolled back
//	  [ ] Allow 2230/tcp in UFW        Rolled back
//
//	      $ systemctl reload ssh
//	      ...
//	      Job for ssh.service failed because the control process
//	      | exited with error code.
//
//	  Check the log file at /var/log/hardn.log for details
//	  Run sudo sshd -t to see the error.
//
// It is built from the other components (DEV-040), in Figma's grammar
// (P-42 R4, the maintainer, 2026-10-22). What failed is a Callout of kind
// MessageBad with its own title, "Operation Failed", and Failed as its
// body. Each rolled-back change is a StepProgress row in StepRolledBack,
// Glyphs.Partial and "Rolled back" in the status column the steps above
// it take, given as Steps: the rows and the steps share one column (R2),
// and stack, each word under its name, where the steps do. A change with
// no name shows "Unknown", so the report hides none. The output is code,
// as a plan's lines are: the command after Glyphs.Command, then the last
// Tail lines of Output, at theme.LabelCol in Output, muted, never cut but
// broken after Glyphs.Wrap; a line of Glyphs.Ellipsis first says that
// earlier lines were left out, and the log holds them all. The log path
// is origin/main's sentence and the next step a sentence of its own, muted
// prose at theme.Indent, as an info Message. The path names a file to
// open, so it keeps its characters with the ASCII glyph set too, where
// the typography of the text around it is mapped (Ctx.text). Parts with
// nothing to show are left out, and theme.SectionGap blank lines separate
// the rest. An ErrorReport always reports a failure, so even the zero
// value shows the callout.
type ErrorReport struct {
	// Failed says what failed and why, such as "Reloading ssh.service
	// failed: exit status 1".
	Failed string
	// RolledBack names the changes undone after the failure, in the order
	// they were undone, such as "Set Port 2230"; an empty name shows
	// "Unknown".
	RolledBack []string
	// Steps are the steps the report follows, as the StepProgress above it
	// shows them: the report does not draw them, but its rolled-back rows
	// take their status column. None lines the rows up on their own names.
	Steps []Step
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
	blocks := []string{Callout{Kind: MessageBad, Body: []string{vm.Failed}}.Render(c), vm.rolledBack(c), vm.output(c)}
	var info []string
	if p := clean(vm.LogPath); p != "" {
		// The sentence is prose, made plain text for c; the path names a
		// file to open, so it keeps its characters, through clean alone.
		info = append(info, c.prose(fmt.Sprintf(c.text(checkLog), p))...)
	}
	if next := c.text(vm.Next); next != "" {
		info = append(info, c.prose(next)...)
	}
	blocks = append(blocks, strings.Join(info, "\n"))
	return strings.Join(nonEmpty(blocks), strings.Repeat("\n", theme.SectionGap+1))
}

// rolledBack renders the changes undone, each a StepProgress row in
// StepRolledBack, on the status column of vm.Steps and the names.
func (vm ErrorReport) rolledBack(c Ctx) string {
	names := make([]span, len(vm.RolledBack))
	all := titles(c, vm.Steps)
	for i, name := range vm.RolledBack {
		names[i] = span{c.text(name), c.Theme.KeyValue.Key}
		if names[i].text == "" {
			names[i] = span{unknown, c.Theme.Status.Unknown}
		}
		all = append(all, names[i].text)
	}
	sc := newStepColumn(c, all)
	var out []string
	for _, name := range names {
		out = append(out, sc.row(c, StepRolledBack, 0, name, "")...)
	}
	return strings.Join(out, "\n")
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
