package component

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// The confirm countdown's words (DEV-041; proposal P-24 asks the
// maintainer to approve them), after brief §2 and Phase 2.4: unless
// confirmed, the change reverts automatically; to confirm it, the user
// opens a new SSH session, on the new port if it changed, and runs sudo
// hardn confirm. While there is time the title is a warning callout's.
const (
	confirmCommand = "sudo hardn confirm"
	confirmUrgent  = "Urgent"
	confirmReverts = "This change reverts automatically in"
	confirmKeep    = "To keep it, open a new SSH session and run:"
	confirmKeepOn  = "To keep it, open a new SSH session on port %d and run:"
)

// UrgentBelow is the number of seconds left under which the confirm
// countdown turns urgent.
const UrgentBelow = 30

// clockWidth is the cells the countdown's first sentence keeps for the
// time left: "99:59", the widest under 100 minutes.
const clockWidth = 5

// ConfirmCountdown is the notice the session that applied a lockout-risk
// change shows while it waits to be confirmed (brief §2 and Phase 2.4,
// confirm-or-revert): the time left before the automatic revert, as m:ss,
// and the command to run from a new session. It is a Warning callout
// (P-42 R6, the maintainer, 2026-10-22), the command under it outside the
// bar. Here with no colour and ASCII glyphs, 2 minutes 45 seconds before
// the revert, then 25 seconds:
//
//	0 2   6
//	# Warning
//	# This change reverts automatically in 2:45
//	# To keep it, open a new SSH session and run:
//
//	      $ sudo hardn confirm
//
//	# Urgent
//	# This change reverts automatically in 0:25
//	...
//
// The callout is drawn as Callout draws one: the bar at column 0, the
// title "Warning" in Callout.Warn's colours, and the sentences as its
// body, wrapped at spaces, the time in Confirm.Waiting. Under UrgentBelow
// seconds the bar and the title are Callout.Bad's, the title "Urgent",
// and the time in Confirm.Urgent, so urgency never rests on colour alone
// (DEV-041). Then a blank line and, at theme.LabelCol, Glyphs.Command in
// Plan.Command and the command in Confirm.Command, as code, never cut.
// The view-model holds the seconds left, not a deadline, so Render stays
// pure: on a terminal that can animate the caller renders it again each
// second. So that it can redraw the block in place, the first sentence is
// wrapped as though its time were five cells wide: at a given width the
// block keeps its lines, and so its height, for every Remaining under 100
// minutes, urgent or not; the title is one line, cut when too long. Only
// on a line too narrow for the time whole does it wrap where it falls.
type ConfirmCountdown struct {
	// Remaining is the number of seconds left before the change reverts;
	// less than zero shows 0:00.
	Remaining int
	// Port is the SSH port to connect to when the change moved it, such as
	// 2230; zero or less names no port.
	Port int
	// Command is the command to run from the new session; empty shows
	// brief §2's "sudo hardn confirm".
	Command string
}

// Render renders the countdown for c. See ConfirmCountdown for the
// layout.
func (vm ConfirmCountdown) Render(c Ctx) string {
	t, w := c.Theme.Confirm, c.width()
	k, title, timeStyle := c.Theme.Callout.Warn, MessageWarn.title(), t.Waiting
	if vm.Remaining < UrgentBelow {
		k, title, timeStyle = c.Theme.Callout.Bad, confirmUrgent, t.Urgent
	}
	left := max(0, vm.Remaining)
	clock := fmt.Sprintf("%d:%02d", left/60, left%60)
	keep := confirmKeep
	if vm.Port > 0 {
		keep = fmt.Sprintf(confirmKeepOn, vm.Port)
	}
	cmd := commandLines(vm.Command)
	if len(cmd) == 0 {
		cmd = []string{confirmCommand}
	}
	bar, body := c.calloutBar(k), c.Theme.Callout.Body
	limit := w - bar.width()
	out := []string{c.fit(append(bar, span{title, k.Title}), w)}
	// The first sentence ends with the clock in timeStyle. It is wrapped
	// with a stand-in in the clock's place, a run of digits clockWidth
	// wide (or the clock's width, if wider), so that its breaks do not
	// depend on the time; the clock then replaces it. The stand-in holds
	// no space or hyphen, so the wrap keeps it whole unless the line is
	// narrower than it; then the clock itself is wrapped.
	clockRun := strings.Repeat("0", max(clockWidth, lipgloss.Width(clock)))
	lines := wrap(confirmReverts+" "+clockRun, limit)
	if !strings.HasSuffix(lines[len(lines)-1], clockRun) {
		clockRun = clock
		lines = wrap(confirmReverts+" "+clock, limit)
	}
	for i, l := range lines {
		var tail string
		if i == len(lines)-1 && strings.HasSuffix(l, clockRun) {
			l, tail = l[:len(l)-len(clockRun)], clock
		}
		out = append(out, c.fit(append(bar, span{l, body}, span{tail, timeStyle}), w))
	}
	for _, l := range wrap(keep, limit) {
		out = append(out, c.fit(append(bar, span{l, body}), w))
	}
	out = append(out, "")
	prompt := span{c.Theme.Glyphs.Command + " ", c.Theme.Plan.Command}
	for i, l := range cmd {
		if i == 0 {
			out = append(out, c.codeFrom(theme.LabelCol, prompt, l, t.Command)...)
			continue
		}
		out = append(out, c.code(theme.LabelCol+lipgloss.Width(prompt.text), l, t.Command)...)
	}
	return strings.Join(out, "\n")
}
