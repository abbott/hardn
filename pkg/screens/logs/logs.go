// Package logs runs hardn's Logs screen on a host (refactor plan slice 1, System
// Details and Logs, unit (c), next/s1-screen): main menu 10 and hardn -p, as the
// maintainer adopted the mock on 2026-10-30 (docs/engine/gallery-system-details.md
// §5, decisions 6 (a), 7 (a), 8 (a), 10 (a) and the Made here calls, with §6's
// words), drawn by pkg/ui/screen (screen.Logs) from what pkg/engine/logview reads
// of /var/log/hardn.log, writing nothing, under plan only too. Run shows the
// screen, and item Open gives GNU less the terminal, by that key alone; Print
// writes -p's forms, and returns what its stderr and exit status need.
//
// The package reaches the host only through host.System and logview (depguard rule
// screens-through-host), hardn's environment and hardn.yml only through what its
// caller gives (Options.Term, Options.LogFile), and renders only through pkg/ui.
package logs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/screens/screenkit"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/term"
)

// -p's words on stderr (gallery-system-details.md §6, decided with decision 11).
const (
	cliNoLog    = "hardn: no log yet: %s"
	cliCantRead = "hardn: can't read %s: %s"
	cliRunWith  = "Run it with %s."
)

// pagerEnded is what the screen says where the pager ran and did not exit 0: how it
// ended, as os/exec words it (adopted 2026-10-31).
const pagerEnded = "%s ended: %s"

// pagerRefusals are what Open's seam and pager refuse, in the words the user is
// shown (readers-system-details.md §9, adopted 2026-10-30). Each is the end of the
// error it names, host's errNoTerminal and logview's errNoPager, neither exported;
// TestOpenRefused holds both to the real errors.
var pagerRefusals = []string{"stdin and stdout must both be a terminal", "less is not GNU less"}

// Options is what the Logs screen runs with: what cmd/hardn has when its Logs menu
// item is chosen.
type Options struct {
	// Prompter asks and writes the screen through the one line reader, rendering
	// with its Ctx, whose Env is the terminal's and whose height fits the lines.
	Prompter nav.Prompter
	// System is the host: the log, and the pager. A draw takes a host with no run
	// log, as less --version is the host's to log (readers-system-details.md item 12).
	System host.System
	// LogFile is hardn.yml's logFile as the caller has it: said in a row where it
	// names another path (decision 6 (a)). hardn reads /var/log/hardn.log whatever it
	// says, and this package reads no hardn.yml.
	LogFile string
	// Term is hardn's TERM as the caller has it, for the pager, which checks it
	// (logview.Pager): a screen package may not take os (depguard
	// screens-through-host).
	Term string
}

// Run shows the Logs screen until 0 (nil) or q (nav.ErrQuit); the end of the input
// and Ctrl-C abort (prompt.ErrAborted). Each round reads the log once with
// logview.Read, says what logview.Elsewhere says of LogFile and, only where there are
// lines, asks logview.Pager, writing nothing, and draws the screen under what the last
// round left. Plan only draws the same, as nothing is written: Options has no mode.
// Refresh draws again. Open, where a pager was found, runs it by that key alone, says
// why where it refuses or fails, and draws again from a fresh reading. Run returns an
// error wrapping fs.ErrInvalid, writing nothing, without a Prompter's In and Out or a
// System.
func Run(ctx context.Context, o Options) error {
	if o.Prompter.In == nil || o.Prompter.Out == nil || o.System == nil {
		return fmt.Errorf("logs screen: %w: it needs a Prompter with In and Out, and a System", fs.ErrInvalid)
	}
	r := &runner{o: o, s: screenkit.Session{Name: "logs screen", Prompter: o.Prompter}}
	return o.Prompter.Run(func() nav.Screen { return r.draw(ctx) })
}

// runner is one run of the Logs screen.
type runner struct {
	o Options
	s screenkit.Session
	// said is the rows Open's words took, which the next draw leaves on the terminal.
	said int
}

// draw reads the log and returns the screen, its items bound.
func (r *runner) draw(ctx context.Context) nav.Screen {
	reading := logview.Read(ctx, r.o.System)
	var pager *logview.Command
	if reading.State == logview.Lines { // decision 37: nothing to open runs no less --version
		if c, ok := logview.Pager(ctx, r.o.System, r.o.Term); ok {
			pager = &c
		}
	}
	view := viewOf(reading, r.o.LogFile, pager)
	// The lines fit under what Open said, which would otherwise scroll off unread.
	fit := r.o.Prompter.Ctx
	fit.Env.Height -= r.said
	r.said = 0
	sub := view.SubMenu(fit)
	menu := nav.Menu{Exit: sub.Menu.Exit}
	for _, it := range sub.Menu.Items {
		action := func() error { return nil }
		if it.Key == view.OpenKey() {
			action = r.open(ctx, *pager)
		}
		menu.Options = append(menu.Options, nav.Option{Item: it, Action: action})
	}
	return sub.Screen(r.o.Prompter.Ctx, menu)
}

// open returns what Open does: c has the terminal until it ends (Command.Run, which
// asks less --version again), then the next round reads the log again. A refusal or
// the pager's failure is said, and the screen goes on. It does not call the prompt's
// Flush: the next round's prompt does (prompt.Reader.Ask, through nav.Prompter.Ask),
// so that a second q typed at less's end does not reach the menu.
func (r *runner) open(ctx context.Context, c logview.Command) func() error {
	return func() error {
		if err := c.Run(ctx, r.o.System); err != nil {
			words := screenkit.Warnings(pagerWords(err, c.Argv[0]))
			r.said = strings.Count(screenkit.Apart(r.o.Prompter.Ctx, words), "\n")
			return r.s.Say(words)()
		}
		return nil
	}
}

// pagerWords is err as the screen says it: the decided words where it is a refusal
// of the seam's or the pager's, without the wrapping of who said it; how program
// ended where it ran and did not exit 0; else as host says it, safe to show.
func pagerWords(err error, program string) string {
	for _, words := range pagerRefusals {
		if strings.HasSuffix(err.Error(), words) {
			return words
		}
	}
	var ended *host.ExitError
	if errors.As(err, &ended) {
		how := fmt.Sprintf("exit status %d", ended.ExitCode)
		if ended.Err != nil {
			how = ended.Err.Error()
		}
		return logview.Safe(fmt.Sprintf(pagerEnded, program, how))
	}
	return logview.Safe(err.Error())
}

// viewOf is the screen's view of a reading: logFile says what hardn.yml says of the
// log's path, pager is what Open would run, nil for none. Each line is already safe
// (logview.Read), and what else is shown is made so.
func viewOf(r logview.Reading, logFile string, pager *logview.Command) screen.Logs {
	v := screen.Logs{Path: logview.Safe(r.Path), Reason: logview.Safe(r.Reason), Size: r.Size, Total: r.Total, Elsewhere: logview.Elsewhere(logFile)}
	switch r.State {
	case logview.NoLog:
		v.State = screen.LogsNoLog
	case logview.Empty:
		v.State = screen.LogsEmpty
	case logview.Unreadable:
		v.State = screen.LogsUnreadable
	}
	for _, l := range r.Last {
		v.Last = append(v.Last, component.CodeLine{Text: l.Text, Warn: l.Level == logview.LevelWarning || l.Level == logview.LevelError})
	}
	if pager != nil {
		v.Pager = logview.Safe(pager.Argv[0])
	}
	return v
}

// PrintOptions is what Print runs with: what hardn -p has.
type PrintOptions struct {
	// Out is hardn's stdout, and Env its render environment: Env.TTY picks the form.
	Out io.Writer
	Env term.Env
	// System is the host. LogFile is as Options has it: said in a row on a terminal.
	System  host.System
	LogFile string
}

// Printed is what Print found, so that its caller writes stderr and sets the exit
// status of decision 10 (a) without reading the file again.
type Printed struct {
	logview.Reading
}

// Stderr are the lines to write on stderr, in the decided words (gallery-system-
// details.md §6), none where the log was read or holds nothing: "hardn: no log yet:
// <path>"; "hardn: can't read <path>: <reason>", then, where the read was refused,
// "Run it with <tool>." where tool names the tool, its caller's to tell.
func (p Printed) Stderr(tool string) []string {
	path := logview.Safe(p.Path)
	switch p.State {
	case logview.NoLog:
		return []string{fmt.Sprintf(cliNoLog, path)}
	case logview.Unreadable:
		lines := []string{fmt.Sprintf(cliCantRead, path, logview.Safe(p.Reason))}
		if errors.Is(p.Err, fs.ErrPermission) && tool != "" {
			lines = append(lines, fmt.Sprintf(cliRunWith, logview.Safe(tool)))
		}
		return lines
	}
	return nil
}

// ExitCode is decision 10 (a)'s status: 1 where the log could not be read, 0 where
// there is no log yet, as a read that found nothing, and otherwise.
func (p Printed) ExitCode() int {
	if p.State == logview.Unreadable {
		return 1
	}
	return 0
}

// Print writes hardn -p's form of the log, writing nothing else. To a terminal, by
// logview.Read, the file's rows and the last lines the terminal holds, no menu and no
// prompt, room left for the shell's prompt (cli-terminal-tail), and nothing on stdout
// where there is no log or it cannot be read. Off one, by logview.Each, every line as
// it reads, safe to show, no heading and no style (cli-pipe, cli-pipe-control). It
// returns an error wrapping fs.ErrInvalid, writing nothing, without Out or a System,
// and a failed write, the Printed as far as it went. What to do with Printed is the
// caller's, unit (d)'s: Stderr's lines and ExitCode.
func Print(ctx context.Context, o PrintOptions) (Printed, error) {
	if o.Out == nil || o.System == nil {
		return Printed{}, fmt.Errorf("logs print: %w: it needs Out and a System", fs.ErrInvalid)
	}
	if !o.Env.TTY {
		var failed error
		reading := logview.Each(ctx, o.System, func(l logview.Line) bool {
			_, failed = io.WriteString(o.Out, l.Text+"\n")
			return failed == nil
		})
		if failed != nil {
			failed = fmt.Errorf("logs print: write: %w", failed)
		}
		return Printed{reading}, failed
	}
	reading := logview.Read(ctx, o.System)
	if reading.State == logview.NoLog || reading.State == logview.Unreadable {
		return Printed{reading}, nil
	}
	view := viewOf(reading, o.LogFile, nil)
	if _, err := io.WriteString(o.Env.Writer(o.Out), view.Terminal(component.NewCtx(o.Env))+"\n"); err != nil {
		return Printed{reading}, fmt.Errorf("logs print: write: %w", err)
	}
	return Printed{reading}, nil
}
