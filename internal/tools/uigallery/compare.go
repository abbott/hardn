package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The comparison mode (P-42): for the apply's step list, the error
// report's rolled-back lines, the one-line messages and the confirm
// countdown, the pattern hardn draws today (P-23, P-24), rendered by the
// real components as pkg/output and pkg/ui/flow compose them, beside a
// proposed pattern after the grammar of Figma's frames, which this file
// draws itself: no component has it yet, and the maintainer asked that
// none change until the two are compared.
//
// The proposed grammar, in short (P-42, R1 to R7): a row is two columns on
// the grid, a marker centred in the marker column, the subject at
// theme.LabelCol in KeyValue.Key and its state as a coloured word, regular
// weight, in one status column that every row of the screen shares, with
// any aside one space after it, faint; no colon follows a state word. A
// notice is a Callout, and a neutral line muted prose at theme.Indent. The
// countdown is a Warning callout, an Urgent one in its last seconds, with
// the command under it, outside the bar.
//
// The gallery may not name Lip Gloss's types (depguard), so the mock
// styles its runs through the theme's slots' Render methods, held as
// values (paint), and measures and cuts with x/ansi.

// compareUsage is the comparison mode's usage line.
const compareUsage = "usage: go run ./internal/tools/uigallery compare [flags] [steps | failure | success | messages | countdown | all]..."

// The values of -marker: the key-value bullet on every row, or Figma's
// status markers for the states that say how a step ended.
const (
	markerBullet = "bullet"
	markerStatus = "status"
)

// The values of -column: the status column two cells after the widest
// title (theme.ValueCol at the least), or Figma's value column,
// theme.ValueCol, with the list stacked when a title reaches it.
const (
	columnAligned = "aligned"
	columnFigma   = "figma"
)

// mock is the proposed side's two open choices (P-42's (a) and (b)):
// -marker's and -column's values.
type mock struct {
	marker string // markerBullet or markerStatus
	column string // columnAligned or columnFigma
}

// compareGap is the cells between the two renders set side by side.
const compareGap = 4

// compareSection is one section of the comparison: its name, and the
// current and the proposed render of its data in a render context, the
// proposed one with the flags' choices. Each render is its blocks, in
// order, which theme.SectionGap blank lines set apart (apart); side by
// side, the blocks of the two pair up, so that each lines up with its
// counterpart.
type compareSection struct {
	name     string
	current  func(c component.Ctx) []string
	proposed func(c component.Ctx, m mock) []string
}

// compareSections returns the sections, in the order "all" shows them.
func compareSections() []compareSection {
	return []compareSection{
		{"steps", currentSteps, proposedSteps},
		{"failure", currentFailure, proposedFailure},
		{"success", currentSuccess, proposedSuccess},
		{"messages", currentMessages, proposedMessages},
		{"countdown", currentCountdown, proposedCountdowns},
	}
}

// The comparison's data, synthetic, from the engine's scenarios
// failed-reload-debian13 and ssh-port-debian13 (engine.go), whose goldens
// hold the same titles and sentences: the five steps of the 2230 change on
// Debian 13, its reload's failure and what was rolled back, and the
// countdown and the deadline of the change applied.
var cmpTitles = []string{
	"Install hardn's boot unit",
	"ufw: allow 2230/tcp",
	"Add SSH port 2230",
	"Reload sshd (ssh.service)",
	"sshd -T shows ports 22, 2230",
}

const (
	// cmpFrame is the spinner frame the data picks for a running step,
	// shown only when Env.Animate is set, which no environment of the
	// gallery's sets.
	cmpFrame = 0
	// cmpCode is the failed step's aside: the error's code.
	cmpCode    = "COMMAND_FAILED"
	cmpFailed  = "Reload sshd (ssh.service): reload: systemctl reload ssh.service: host: systemctl reload ssh.service: exit status 1 (COMMAND_FAILED)"
	cmpCommand = "systemctl reload ssh.service"
	cmpOutput  = "Job for ssh.service failed because the control process exited with error code."
	cmpLogPath = "/var/log/hardn.log"
	// cmpLeft and cmpPort are the change applied's countdown: 3:01 left,
	// to be confirmed on port 2230.
	cmpLeft = 181
	cmpPort = 2230
	// cmpUntil is pkg/output's sentence after the countdown, for the
	// change applied.
	cmpUntil = "Unless confirmed, change 20261020T120000Z-xxxxxxxx is rolled back at 12:03:01 UTC. " +
		"To undo it now, run sudo hardn rollback 20261020T120000Z-xxxxxxxx."
)

// cmpRolledBack are the failed apply's changes undone, in the order they
// were undone.
var cmpRolledBack = []string{"Add SSH port 2230", "ufw: allow 2230/tcp"}

// cmpMessages are the one-line messages compared: the five kinds, in the
// texts of their component states (fixture.States's message/*), but for
// the cancelled one, whose "Operation cancelled." the callout's title
// already says; then two sentences hardn says as info messages, the dry
// run's footer and an empty plan's.
var cmpMessages = []component.Message{
	{Kind: component.MessageInfo, Text: "No automatic backups will be created"},
	{Kind: component.MessageOK, Text: "DNS configured successfully"},
	{Kind: component.MessageWarn, Text: "No nameservers configured"},
	{Kind: component.MessageBad, Text: "Failed to configure sudo: exit status 1"},
	{Kind: component.MessageCancelled, Text: "No changes were made."},
	{Kind: component.MessageInfo, Text: "No actual changes were made to your system."},
	{Kind: component.MessageInfo, Text: "Nothing needs changing."},
}

// cmpCountdowns are the countdowns compared: 2:45 left, without a port and
// with one, and 0:05, urgent.
var cmpCountdowns = []component.ConfirmCountdown{
	{Remaining: 165},
	{Remaining: 165, Port: cmpPort},
	{Remaining: 5},
}

// The words the proposed side says that the components hold unexported:
// confirm.go's countdown sentences and report.go's log sentence (P-24).
// TestCompareWords checks that the current side still says them.
const (
	cmpReverts  = "This change reverts automatically in"
	cmpKeep     = "To keep it, open a new SSH session and run:"
	cmpKeepOn   = "To keep it, open a new SSH session on port %d and run:"
	cmpConfirm  = "sudo hardn confirm"
	cmpUrgent   = "Urgent"
	cmpCheckLog = "Check the log file at %s for details"
)

// cmpSteps returns the five steps in states, in order; a failed one has
// the error's code as its detail, as pkg/output gives it.
func cmpSteps(states ...component.StepState) []component.Step {
	out := make([]component.Step, len(states))
	for i, s := range states {
		out[i] = component.Step{Title: cmpTitles[i], State: s}
		if s == component.StepFailed {
			out[i].Detail = cmpCode
		}
	}
	return out
}

// The steps at each point compared: an apply under way, the apply at its
// reload's failure, and the apply done.
func runningSteps() []component.Step {
	return cmpSteps(component.StepDone, component.StepDone, component.StepRunning, component.StepPending, component.StepPending)
}

func failedSteps() []component.Step {
	return cmpSteps(component.StepDone, component.StepDone, component.StepDone, component.StepFailed, component.StepSkipped)
}

func doneSteps() []component.Step {
	return cmpSteps(component.StepDone, component.StepDone, component.StepDone, component.StepDone, component.StepDone)
}

// sectionBreak is what sets two blocks apart: theme.SectionGap blank
// lines between them, as the components and pkg/output set them.
var sectionBreak = strings.Repeat("\n", theme.SectionGap+1)

// apart joins blocks with theme.SectionGap blank lines between them.
func apart(blocks ...string) string { return strings.Join(blocks, sectionBreak) }

// The current side: the real components, composed as pkg/output's Human
// and pkg/ui/flow compose them.

func currentSteps(c component.Ctx) []string {
	return []string{component.StepProgress{Steps: runningSteps(), Frame: cmpFrame}.Render(c)}
}

// currentFailure is the failure as the flow shows it: the steps, then the
// ErrorReport, with the log path the flow's adapter gives it. The report
// is given as its parts, the callout, the rolled-back lines, the output
// and the log line, which it sets apart as blocks are, so that each pairs
// with the proposed side's; joined again they are the report as rendered.
func currentFailure(c component.Ctx) []string {
	report := component.ErrorReport{
		Failed: cmpFailed, RolledBack: cmpRolledBack, Command: cmpCommand, Output: cmpOutput, LogPath: cmpLogPath,
	}.Render(c)
	return append([]string{component.StepProgress{Steps: failedSteps()}.Render(c)}, strings.Split(report, sectionBreak)...)
}

// currentSuccess is the change applied as play shows it: the flow's steps
// and "Operation Successful", then what pkg/output's AfterFlow adds, the
// countdown and the deadline.
func currentSuccess(c component.Ctx) []string {
	return []string{component.StepProgress{Steps: doneSteps()}.Render(c), component.Callout{Kind: component.MessageOK}.Render(c),
		component.ConfirmCountdown{Remaining: cmpLeft, Port: cmpPort}.Render(c), component.Message{Text: cmpUntil}.Render(c)}
}

// currentMessages are the messages, each a block of its own, so that each
// pairs with the proposed side's.
func currentMessages(c component.Ctx) []string {
	var out []string
	for _, m := range cmpMessages {
		out = append(out, m.Render(c))
	}
	return out
}

func currentCountdown(c component.Ctx) []string {
	var out []string
	for _, cd := range cmpCountdowns {
		out = append(out, cd.Render(c))
	}
	return out
}

// The proposed side (P-42).

func proposedSteps(c component.Ctx, m mock) []string {
	steps := runningSteps()
	return []string{newStatusRows(c, m, steps).render(steps)}
}

// proposedFailure is R4: the steps at the failure; the bad callout as now;
// the rolled-back rows, on the steps' status column; the failing command
// and its output as the ErrorReport renders them; the log sentence as
// prose. Stacked, a rolled-back row's title is cut as a step's is (R2),
// where ErrorReport names every change it undid in full (DEV-040): P-42
// leaves that open.
func proposedFailure(c component.Ctx, m mock) []string {
	steps := failedSteps()
	var undone []component.Step
	for _, t := range cmpRolledBack {
		undone = append(undone, component.Step{Title: t, State: component.StepRolledBack})
	}
	rows := newStatusRows(c, m, slices.Concat(steps, undone))
	return []string{rows.render(steps), component.Callout{Kind: component.MessageBad, Body: []string{cmpFailed}}.Render(c),
		rows.render(undone), commandBlock(c, cmpCommand, cmpOutput), prose(c, fmt.Sprintf(cmpCheckLog, cmpLogPath))}
}

// proposedSuccess is R7: the steps, "Operation Successful" as now, the
// countdown (R6) and the deadline as prose.
func proposedSuccess(c component.Ctx, m mock) []string {
	steps := doneSteps()
	return []string{newStatusRows(c, m, steps).render(steps), component.Callout{Kind: component.MessageOK}.Render(c),
		countdownCallout(c, component.ConfirmCountdown{Remaining: cmpLeft, Port: cmpPort}), prose(c, cmpUntil)}
}

// proposedMessages is R5: an info message as prose, every other kind as
// the kind's Callout, its own title over the text.
func proposedMessages(c component.Ctx, _ mock) []string {
	var out []string
	for _, m := range cmpMessages {
		if m.Kind == component.MessageInfo {
			out = append(out, prose(c, m.Text))
			continue
		}
		out = append(out, component.Callout{Kind: m.Kind, Body: []string{m.Text}}.Render(c))
	}
	return out
}

func proposedCountdowns(c component.Ctx, _ mock) []string {
	var out []string
	for _, cd := range cmpCountdowns {
		out = append(out, countdownCallout(c, cd))
	}
	return out
}

// paint is a style slot's Render method, held as a value, which styles a
// run of text without naming Lip Gloss's Style.
type paint func(...string) string

// piece is a run of plain text and the slot that paints it; a nil paint
// leaves it as it is.
type piece struct {
	text  string
	paint paint
}

// pad is n spaces in no style.
func pad(n int) piece { return piece{text: strings.Repeat(" ", max(0, n))} }

// draw renders a line of pieces. An empty piece renders as nothing, rather
// than as an empty pair of escape sequences.
func draw(line ...piece) string {
	var b strings.Builder
	for _, p := range line {
		switch {
		case p.text == "":
		case p.paint == nil:
			b.WriteString(p.text)
		default:
			b.WriteString(p.paint(p.text))
		}
	}
	return b.String()
}

// widthOf is the cells line takes.
func widthOf(line []piece) int {
	w := 0
	for _, p := range line {
		w += ansi.StringWidth(p.text)
	}
	return w
}

// termWidth is env's width, or term.DefaultWidth when it has none, as the
// components take it.
func termWidth(env term.Env) int {
	if env.Width < 1 {
		return term.DefaultWidth
	}
	return env.Width
}

// contentWidth is the width the components lay out in: the terminal's,
// capped at theme.ContentWidth under theme.CapContent.
func contentWidth(env term.Env) int {
	if theme.CapContent {
		return min(termWidth(env), theme.ContentWidth)
	}
	return termWidth(env)
}

// cut shortens plain text s to at most limit cells, ending it with the
// ellipsis glyph when it is cut, as the components cut: the spaces before
// the cut are dropped, and with no text left the result is as much of the
// glyph as fits.
func cut(c component.Ctx, s string, limit int) string {
	if ansi.StringWidth(s) <= limit {
		return s
	}
	e := c.Theme.Glyphs.Ellipsis
	kept := strings.TrimRight(ansi.Truncate(s, max(0, limit-ansi.StringWidth(e)), ""), " ")
	if kept == "" {
		return ansi.Truncate(e, max(0, limit), "")
	}
	return kept + e
}

// wrapText breaks plain text s into lines of at most limit cells at its
// spaces alone, as R5 has prose wrap, and inside a word only when the
// word is wider than a line on its own, which it then fills line by line.
// Unlike the components' wrap (x/ansi's and Lip Gloss's), it never breaks
// after a hyphen, so that a change's id stays whole. The words are s's
// runs of non-spaces, one space apart on a line. With limit under 1, s is
// one line.
func wrapText(s string, limit int) []string {
	if limit < 1 {
		return []string{s}
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && ansi.StringWidth(line)+1+ansi.StringWidth(word) <= limit {
			line += " " + word
			continue
		}
		if line != "" {
			out = append(out, line)
		}
		for ansi.StringWidth(word) > limit {
			head := ansi.Truncate(word, limit, "")
			if head == "" {
				head = string([]rune(word)[:1])
			}
			out, word = append(out, head), word[len(head):]
		}
		line = word
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// markerTo returns glyph in p centred in the marker column, as the
// components place a marker (DEV-020), and the spaces that bring the line
// to theme.LabelCol.
func markerTo(glyph string, p paint) []piece {
	lead := theme.MarkerCol + max(0, theme.MarkerWidth-ansi.StringWidth(glyph))/2
	return []piece{pad(lead), {glyph, p}, pad(theme.LabelCol - lead - ansi.StringWidth(glyph))}
}

// widestState is the cells of the widest step state word, "Rolled back".
// The status column must hold it for a list to take its two-column form,
// so that the form, and the list's height, stay whatever its steps'
// states.
func widestState() int {
	w := 0
	for s := component.StepPending; s <= component.StepSkipped; s++ {
		w = max(w, ansi.StringWidth(s.String()))
	}
	return w
}

// statusRows lays out the step rows of one screen as P-42 proposes (R1 to
// R3): a marker centred in the marker column, the title at theme.LabelCol
// in KeyValue.Key, and the state word, regular weight, in its state's
// slot at col, the status column every row of the screen shares, the
// failed step's aside one space after it in Status.Detail. Stacked, the
// state word and the aside go on the line under the title, at
// theme.StackCol. A row is never wrapped: an aside, then a title, is cut
// with the ellipsis glyph when it cannot fit.
type statusRows struct {
	c       component.Ctx
	marker  string // -marker's value
	col     int    // the status column
	stacked bool
}

// newStatusRows returns the rows of a screen whose rows are steps (R2):
// the status column is theme.ValueCol, or two cells after the widest
// title when that is further right; the rows stack below
// theme.StackBelow, or when the status column cannot hold the widest
// state word within the content width. With -column figma (P-42's choice
// b2) the status column is theme.ValueCol whatever the titles, and the
// whole list also stacks when a title reaches it, as BulletList stacks
// when a key does: the 2230 change's titles do, so its rows stack at
// every width.
func newStatusRows(c component.Ctx, m mock, steps []component.Step) statusRows {
	widest := 0
	for _, s := range steps {
		widest = max(widest, ansi.StringWidth(s.Title))
	}
	col, reached := max(theme.ValueCol, theme.LabelCol+widest+2), false
	if m.column == columnFigma {
		col, reached = theme.ValueCol, theme.LabelCol+widest >= theme.ValueCol
	}
	stacked := termWidth(c.Env) < theme.StackBelow || reached || col+widestState() > contentWidth(c.Env)
	return statusRows{c: c, marker: m.marker, col: col, stacked: stacked}
}

// render renders steps, a row each.
func (r statusRows) render(steps []component.Step) string {
	var out []string
	for _, s := range steps {
		out = append(out, r.row(s)...)
	}
	return strings.Join(out, "\n")
}

// row renders step s: one line, or two when stacked.
func (r statusRows) row(s component.Step) []string {
	t, w := r.c.Theme, contentWidth(r.c.Env)
	glyph, p := r.glyph(s.State)
	head := markerTo(glyph, p)
	word := piece{s.State.String(), statePaint(r.c, s.State)}
	if !r.stacked {
		line := slices.Concat(head, []piece{{s.Title, t.KeyValue.Key.Render}, pad(r.col - theme.LabelCol - ansi.StringWidth(s.Title)), word})
		return []string{draw(r.aside(line, s.Detail, w)...)}
	}
	title := piece{cut(r.c, s.Title, w-theme.LabelCol), t.KeyValue.Key.Render}
	word.text = cut(r.c, word.text, w-theme.StackCol)
	under := []piece{pad(theme.StackCol), word}
	return []string{draw(append(head, title)...), draw(r.aside(under, s.Detail, w)...)}
}

// aside returns line with aside after it, one space on, in Status.Detail,
// cut to end within w cells; an aside with no room for a cell of its text
// beside the ellipsis glyph is left out.
func (r statusRows) aside(line []piece, aside string, w int) []piece {
	room := w - widthOf(line) - 1
	if aside == "" || (ansi.StringWidth(aside) > room && room <= ansi.StringWidth(r.c.Theme.Glyphs.Ellipsis)) {
		return line
	}
	return append(line, pad(1), piece{cut(r.c, aside, room), r.c.Theme.Status.Detail.Render})
}

// glyph returns the marker of a step in state s and its paint (R3): with
// -marker bullet, Glyphs.Bullet in KeyValue.Bullet; with -marker status,
// Figma's status markers for a step done, failed or rolled back, the
// bullet for the others. Either way a running step shows Glyphs.Running,
// or with Env.Animate the spinner's frame the data picks, in Text.Primary.
func (r statusRows) glyph(s component.StepState) (string, paint) {
	t := r.c.Theme
	g := t.Glyphs
	switch {
	case s == component.StepRunning:
		if frames := strings.Split(g.Spinner, ""); r.c.Env.Animate && len(frames) > 0 {
			return frames[cmpFrame%len(frames)], t.Text.Primary.Render
		}
		return g.Running, t.Text.Primary.Render
	case r.marker != markerStatus:
	case s == component.StepDone:
		return g.Configured, t.Status.Configured.Render
	case s == component.StepFailed:
		return g.NotConfigured, t.Status.NotConfigured.Render
	case s == component.StepRolledBack:
		return g.Partial, t.Status.Partial.Render
	}
	return g.Bullet, t.KeyValue.Bullet.Render
}

// statePaint returns the slot of state s's word (R1): Figma's value
// colours for a step done, failed or rolled back, and the text tones for
// the others.
func statePaint(c component.Ctx, s component.StepState) paint {
	t := c.Theme
	switch s {
	case component.StepDone:
		return t.KeyValue.ValueOK.Render
	case component.StepFailed:
		return t.KeyValue.ValueBad.Render
	case component.StepRolledBack:
		return t.KeyValue.ValueWarn.Render
	case component.StepSkipped:
		return t.Text.Faint.Render
	case component.StepRunning:
		return t.Text.Primary.Render
	}
	return t.Text.Muted.Render
}

// commandBlock is the failing command and the tail of its output exactly
// as ErrorReport renders them (R4): the report of the command alone, less
// the callout every report opens with.
func commandBlock(c component.Ctx, command, output string) string {
	alone := component.ErrorReport{}.Render(c)
	block, ok := strings.CutPrefix(component.ErrorReport{Command: command, Output: output}.Render(c), alone+sectionBreak)
	if !ok {
		return ""
	}
	return block
}

// prose renders text as P-42 proposes an info message (R5): Text.Muted
// from theme.Indent, wrapped at spaces to the content width, with no glyph
// and no word.
func prose(c component.Ctx, text string) string {
	var out []string
	for _, l := range wrapText(text, contentWidth(c.Env)-theme.Indent) {
		out = append(out, draw(pad(theme.Indent), piece{l, c.Theme.Text.Muted.Render}))
	}
	return strings.Join(out, "\n")
}

// countdownCallout renders cd as P-42 proposes the countdown (R6): a
// callout drawn as Callout draws one, the bar at column 0 and the text
// from column 2, titled "Warning" in Callout.Warn.Title; its body, in
// Callout.Body, the countdown's two sentences, the time in Confirm.Waiting;
// then a blank line and, at theme.LabelCol, Glyphs.Command in Plan.Command
// and the command in Confirm.Command. Under component.UrgentBelow seconds
// the bar is Callout.Bad's, the title "Urgent" in Callout.Bad.Title and
// the time in Confirm.Urgent. The first sentence is wrapped as though its
// time were "99:59", as the component wraps it, so the block keeps its
// height as the time runs down and turns urgent.
func countdownCallout(c component.Ctx, cd component.ConfirmCountdown) string {
	t, w := c.Theme, contentWidth(c.Env)
	kind, title, timePaint := t.Callout.Warn, component.MessageWarn.String(), paint(t.Confirm.Waiting.Render)
	if cd.Remaining < component.UrgentBelow {
		kind, title, timePaint = t.Callout.Bad, cmpUrgent, t.Confirm.Urgent.Render
	}
	bar := []piece{{" ", kind.Bar.Render}, pad(1)}
	if !c.Env.HasColor() {
		bar[0].text = t.Glyphs.Bar
	}
	limit := w - widthOf(bar)
	left := max(0, cd.Remaining)
	mmss := fmt.Sprintf("%d:%02d", left/60, left%60)
	keep := cmpKeep
	if cd.Port > 0 {
		keep = fmt.Sprintf(cmpKeepOn, cd.Port)
	}
	cmd := strings.TrimSpace(cd.Command)
	if cmd == "" {
		cmd = cmpConfirm
	}
	out := []string{draw(append(slices.Clone(bar), piece{cut(c, title, limit), kind.Title.Render})...)}
	// The stand-in is a run of digits as wide as "99:59", or as the time
	// when wider, with no space to break at; the time replaces it.
	standIn := strings.Repeat("0", max(len("99:59"), ansi.StringWidth(mmss)))
	lines := wrapText(cmpReverts+" "+standIn, limit)
	if !strings.HasSuffix(lines[len(lines)-1], standIn) {
		standIn = mmss
		lines = wrapText(cmpReverts+" "+mmss, limit)
	}
	for i, l := range lines {
		tail := ""
		if i == len(lines)-1 && strings.HasSuffix(l, standIn) {
			l, tail = strings.TrimSuffix(l, standIn), mmss
		}
		out = append(out, draw(slices.Concat(bar, []piece{{l, t.Callout.Body.Render}, {tail, timePaint}})...))
	}
	for _, l := range wrapText(keep, limit) {
		out = append(out, draw(slices.Concat(bar, []piece{{l, t.Callout.Body.Render}})...))
	}
	out = append(out, "")
	return strings.Join(append(out, commandLine(c, cmd)...), "\n")
}

// commandLine lays out cmd from theme.LabelCol: Glyphs.Command in
// Plan.Command, a space, and the command in Confirm.Command. It is code,
// never cut: past the content width the rest goes on the next line after
// Glyphs.Wrap and a space, as the components continue a line of code.
// Unlike the components (DEV-038) it breaks where the width ends, not at a
// space: the countdown's command fits from 26 columns.
func commandLine(c component.Ctx, cmd string) []string {
	t, w := c.Theme, contentWidth(c.Env)
	lead := []piece{pad(theme.LabelCol), {t.Glyphs.Command, t.Plan.Command.Render}, pad(1)}
	var out []string
	for cmd != "" {
		head := ansi.Truncate(cmd, max(1, w-widthOf(lead)), "")
		if head == "" {
			head = string([]rune(cmd)[:1])
		}
		out = append(out, draw(append(lead, piece{head, t.Confirm.Command.Render})...))
		cmd = cmd[len(head):]
		lead = []piece{pad(theme.LabelCol), {t.Glyphs.Wrap, t.Plan.Command.Render}, pad(1)}
	}
	return out
}

// compareNote is the body of the note compare opens with: what it
// compares, and the marker and the status column the proposed rows take.
func compareNote(m mock) []string {
	markers := "the key-value bullet on every row, as here; Figma's status markers take its place, for a step done, failed or rolled back, with -marker status."
	if m.marker == markerStatus {
		markers = "Figma's status markers for a step done, failed or rolled back, as here, in place of the key-value bullet, which -marker bullet shows."
	}
	column := "two cells after the widest title, as here; with -column figma it is Figma's column 27, which these titles reach, so their rows stack at every width."
	if m.column == columnFigma {
		column = "Figma's column 27, as here, which these titles reach, so their rows stack at every width; -column aligned puts it two cells after the widest title."
	}
	return []string{
		"Compare mode: each section shows the current pattern, as hardn's components render it today (P-23, P-24; built), " +
			"and beside it or under it the proposed one (P-42): a mock after Figma's rows, drawn by the gallery. No component changed.",
		"Proposed: the subject at column 6 and its state, a coloured word with no colon, in one column after it; " +
			"notices as callouts, neutral lines as muted prose.",
		"Markers: " + markers,
		"Status column: " + column,
	}
}

// selectSections returns the sections args name, in the order "all" shows
// them: every section for no argument or "all". An argument that starts
// with "-" is a flag the flag package left unparsed, after a name.
func selectSections(all []compareSection, args []string) ([]compareSection, error) {
	if len(args) == 0 || slices.Contains(args, "all") {
		return all, nil
	}
	for _, a := range args {
		switch {
		case slices.ContainsFunc(all, func(s compareSection) bool { return s.name == a }):
		case strings.HasPrefix(a, "-"):
			return nil, fmt.Errorf("flag %s comes after a section's name; flags go before them", a)
		default:
			var names []string
			for _, s := range all {
				names = append(names, s.name)
			}
			return nil, fmt.Errorf("no section %q; the sections are %s", a, strings.Join(names, ", "))
		}
	}
	return slices.DeleteFunc(slices.Clone(all), func(s compareSection) bool { return !slices.Contains(args, s.name) }), nil
}

// compare is the comparison mode: it parses args, writes the note and the
// sections they name to stdout, a write each, reports errors on stderr,
// and returns the exit status. Where -width is not given, it takes the
// width of the terminal stdout is from size, as play does (look.sizeTo).
func compare(args []string, size sizer, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("uigallery compare", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, compareUsage)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "print the names of the sections it would render, and exit")
	look := lookFlags(fs, true)
	// -height is play's, which sizes the main menu by it; compare takes it,
	// checks it and draws nothing by it, and its help says so.
	fs.Lookup("height").Usage = fmt.Sprintf("the terminal height in lines (default: the terminal's own when stdout is a terminal, else %d); compare draws nothing by it", term.DefaultHeight)
	marker := fs.String("marker", markerBullet, "the proposed rows' marker: bullet, the key-value bullet on every row, or status, Figma's status markers for a step done, failed or rolled back")
	column := fs.String("column", columnAligned, "the proposed rows' status column: aligned, two cells after the widest title (column 27 at the least), or figma, Figma's column 27, the list stacked when a title reaches it")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	usage := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "uigallery: compare: "+format+"\n", a...)
		return exitUsage
	}
	secs, err := selectSections(compareSections(), fs.Args())
	switch {
	case err != nil:
		return usage("%v", err)
	case *marker != markerBullet && *marker != markerStatus:
		return usage("-marker %q is not bullet or status", *marker)
	case *column != columnAligned && *column != columnFigma:
		return usage("-column %q is not aligned or figma", *column)
	}
	m := mock{marker: *marker, column: *column}
	if *list {
		for _, s := range secs {
			fmt.Fprintln(stdout, s.name)
		}
		return exitOK
	}
	look.sizeTo(fs, size)
	c, err := look.cell()
	if err != nil {
		return usage("%v", err)
	}
	g, ok := look.gallery(stderr)
	if !ok {
		return exitError
	}
	write := func(what string, render func() (string, error)) bool {
		out, err := render()
		if err == nil {
			_, err = io.WriteString(c.env.Writer(stdout), out)
		}
		if err != nil {
			fmt.Fprintf(stderr, "uigallery: compare: %s in %s: %v\n", what, c.name, err)
		}
		return err == nil
	}
	if !write("the note", func() (string, error) { return g.renderNote(c, m) }) {
		return exitError
	}
	for _, s := range secs {
		if !write(s.name, func() (string, error) { return g.renderPair(s, c, m) }) {
			return exitError
		}
	}
	return exitOK
}

// renderNote returns compare's note in cell c, an info Callout, and a
// blank line after it.
func (g gallery) renderNote(c cell, m mock) (string, error) {
	ctx, err := g.ctx(c.env)
	if err != nil {
		return "", err
	}
	return component.Callout{Kind: component.MessageInfo, Body: compareNote(m)}.Render(ctx) + sectionBreak, nil
}

// renderPair returns section s in cell c under the gallery's header,
// "compare <section>": its current render under a SectionHeader titled
// "Current" and its proposed one under "Proposed", and a blank line after
// it. When c is wide enough for two content columns and compareGap
// between them, the two are side by side, each rendered at
// theme.ContentWidth, block beside block where they have as many, so that
// each block lines up with its counterpart (TestComparePairs checks that
// every section's do); else one above the other, at c's width.
func (g gallery) renderPair(s compareSection, c cell, m mock) (string, error) {
	ctx, err := g.ctx(c.env)
	if err != nil {
		return "", err
	}
	env, wide := c.env, c.env.Width >= 2*theme.ContentWidth+compareGap
	if wide {
		env.Width = theme.ContentWidth
	}
	side, err := g.ctx(env)
	if err != nil {
		return "", err
	}
	current := append([]string{component.SectionHeader{Title: "Current"}.Render(side)}, s.current(side)...)
	proposed := append([]string{component.SectionHeader{Title: "Proposed"}.Render(side)}, s.proposed(side, m)...)
	body := apart(apart(current...), apart(proposed...))
	if wide {
		if len(current) != len(proposed) {
			current, proposed = []string{apart(current...)}, []string{apart(proposed...)}
		}
		var rows []string
		for i := range current {
			rows = append(rows, beside(current[i], proposed[i], theme.ContentWidth, compareGap))
		}
		body = apart(rows...)
	}
	return apart(header(ctx, "compare "+s.name), body) + sectionBreak, nil
}

// beside sets the lines of left and right side by side: each line of left
// padded to width cells, gap cells, then the line of right. A line with
// nothing on the right keeps no trailing spaces.
func beside(left, right string, width, gap int) string {
	l, r := strings.Split(left, "\n"), strings.Split(right, "\n")
	out := make([]string, max(len(l), len(r)))
	for i := range out {
		var a, b string
		if i < len(l) {
			a = l[i]
		}
		if i < len(r) {
			b = r[i]
		}
		out[i] = a
		if b != "" {
			out[i] += strings.Repeat(" ", max(0, width-ansi.StringWidth(a))+gap) + b
		}
	}
	return strings.Join(out, "\n")
}
