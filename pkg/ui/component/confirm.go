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
// hardn confirm. While there is time the word is a warning Message's.
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
// and the command to run from a new session. Here with no colour and ASCII
// glyphs, 2 minutes 45 seconds before the revert, then 25 seconds:
//
//	0 2   6
//	  [*] Warning: This change reverts automatically in 2:45
//	      To keep it, open a new SSH session and run:
//	      sudo hardn confirm
//
//	  [!] Urgent: This change reverts automatically in 0:25
//
// The first line is laid out as a Message: Glyphs.Warn centred in the
// marker column, then the word "Warning" and a colon at theme.LabelCol,
// the sentence, and the time, the glyph, word and time in Confirm.Waiting.
// Under UrgentBelow seconds the glyph is Glyphs.Urgent and the word
// "Urgent", in Confirm.Urgent, so urgency never rests on colour alone
// (DEV-041). The sentences wrap at spaces under the word; the command, in
// Confirm.Command, is code, never cut. The view-model holds the seconds
// left, not a deadline, so Render stays pure: on a terminal that can
// animate the caller renders it again each second. So that it can redraw
// the block in place, the first sentence is wrapped as though its word
// were the longer of "Warning:" and "Urgent:" and its time five cells
// wide: at a given width the block keeps its lines, and so its height,
// for every Remaining under 100 minutes, urgent or not. Only on a line
// too narrow for the word or the time whole do they wrap where they fall.
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
	glyph, word, style := c.Theme.Glyphs.Warn, MessageWarn.String(), t.Waiting
	if vm.Remaining < UrgentBelow {
		glyph, word, style = c.Theme.Glyphs.Urgent, confirmUrgent, t.Urgent
	}
	left := max(0, vm.Remaining)
	clock := fmt.Sprintf("%d:%02d", left/60, left%60)
	keep := confirmKeep
	if vm.Port > 0 {
		keep = fmt.Sprintf(confirmKeepOn, vm.Port)
	}
	cmd := vm.Command
	if len(commandLines(cmd)) == 0 {
		cmd = confirmCommand
	}
	lead := marker(glyph, style).to(theme.LabelCol)
	var out []string
	// The first sentence opens with the word and its colon and ends with
	// the clock, both in style. It is wrapped with stand-ins in their
	// places, a run of letters as wide as the longer word and a run of
	// digits clockWidth wide (or the clock's width, if wider), so that its
	// breaks depend on neither; the word and the clock then replace them.
	// No stand-in holds a space or a hyphen, so the wrap keeps each whole
	// unless the line is narrower than it; then the word and the clock
	// themselves are wrapped.
	word += ":"
	wordRun := strings.Repeat("W", max(lipgloss.Width(MessageWarn.String()+":"), lipgloss.Width(confirmUrgent+":")))
	clockRun := strings.Repeat("0", max(clockWidth, lipgloss.Width(clock)))
	lines := wrap(wordRun+" "+confirmReverts+" "+clockRun, w-theme.LabelCol)
	if !strings.HasPrefix(lines[0], wordRun) || !strings.HasSuffix(lines[len(lines)-1], clockRun) {
		wordRun, clockRun = word, clock
		lines = wrap(word+" "+confirmReverts+" "+clock, w-theme.LabelCol)
	}
	for i, l := range lines {
		var head, tail string
		if i == 0 && strings.HasPrefix(l, wordRun) {
			head, l = word, l[len(wordRun):]
		}
		if i == len(lines)-1 && strings.HasSuffix(l, clockRun) {
			l, tail = l[:len(l)-len(clockRun)], clock
		}
		out = append(out, c.fit(append(lead, span{head, style}, span{l, t.Text}, span{tail, style}), w))
		lead = line{gap(theme.LabelCol)}
	}
	for _, l := range wrap(keep, w-theme.LabelCol) {
		out = append(out, c.fit(line{gap(theme.LabelCol), {l, t.Text}}, w))
	}
	out = append(out, c.command(theme.LabelCol, "", cmd, t.Command)...)
	return strings.Join(out, "\n")
}
