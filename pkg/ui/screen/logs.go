package screen

import (
	"fmt"
	"strconv"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Logs screen (refactor plan slice 1, unit (c), next/s1-screen): main menu 10
// and hardn -p to a terminal, read only, as the maintainer adopted the mock on
// 2026-10-30 (docs/engine/gallery-system-details.md §5, decisions 6 (a), 7 (a),
// 8 (a), 10 (a), with §6's words). There is no Figma frame: it is inferred from
// v0.3.2 and origin/main. pkg/ui cannot reach the host: pkg/screens/logs reads it
// (pkg/engine/logview), fills Logs and runs the items.

// The Logs screen's words (gallery-system-details.md §6, decided with decision
// 11): the main menu's label for the title; Backup's heading and prompt.
const (
	logsTitle        = "Logs"
	logsFileSection  = "File"
	logsPathKey      = "Path"
	logsSizeKey      = "Size"
	logsNoLog        = "No log yet"
	logsEmpty        = "Empty"
	logsCantRead     = "Can't read it: %s"
	logsElsewhere    = "hardn.yml's logFile, %s, isn't used"
	logsLastOf       = "Last %d of %d lines"
	logsAll          = "All %d lines"
	logsOne          = "1 line"
	logsOpen         = "Open"
	logsOpenAbout    = "The whole log in %s"
	logsRefresh      = "Refresh"
	logsRefreshAbout = "Read the log again"
)

// logsShellRows are the rows hardn -p leaves on a terminal for what stands around
// it: the command above it and the shell's prompt after it.
const logsShellRows = 2

// LogsState is what the reading found of the log, by which the screen draws its
// rows and items.
type LogsState uint8

const (
	// LogsLines: the file holds lines (Logs.Last).
	LogsLines LogsState = iota
	// LogsNoLog: no file at the path, "No log yet".
	LogsNoLog
	// LogsEmpty: the file holds nothing, "Empty".
	LogsEmpty
	// LogsUnreadable: the path cannot be read, "Can't read it: <Reason>".
	LogsUnreadable
)

// Logs is the Logs screen's reading of the log, which pkg/screens/logs fills from
// logview's, every string safe to show.
type Logs struct {
	// Path is the file read.
	Path string
	// State is what the reading found.
	State LogsState
	// Reason says why the path cannot be read.
	Reason string
	// Size is the file's size in bytes, and Total the lines it holds.
	Size  int64
	Total int
	// Last is its last lines, oldest first: the screen shows as many of their end
	// as the terminal holds.
	Last []component.CodeLine
	// Elsewhere is hardn.yml's logFile where it names another path, said in a row;
	// "" says nothing.
	Elsewhere string
	// Pager is the program item Open names, "less"; "" has no Open item (decision
	// 7 (a)).
	Pager string
}

// sizeWords is n bytes as a reader says it: "49 KiB".
func sizeWords(n int64) string {
	switch {
	case n == 1:
		return "1 byte"
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%d KiB", n/1024)
	}
	return fmt.Sprintf("%d MiB", n/(1024*1024))
}

// fileSection is the section that names the file and says its state.
func (l Logs) fileSection() Section {
	rows := []component.KeyValue{{Key: logsPathKey, Value: l.Path}}
	switch l.State {
	case LogsNoLog:
		rows = append(rows, component.KeyValue{Key: logsNoLog, State: component.ValueNone})
	case LogsUnreadable:
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(logsCantRead, l.Reason), State: component.ValueWarn})
	case LogsEmpty:
		rows = append(rows, component.KeyValue{Key: logsEmpty, State: component.ValueNone})
	default:
		rows = append(rows, component.KeyValue{Key: logsSizeKey, Value: sizeWords(l.Size)})
	}
	if l.Elsewhere != "" {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(logsElsewhere, l.Elsewhere), State: component.ValueWarn})
	}
	return Section{Header: component.SectionHeader{Title: logsFileSection}, Rows: component.BulletList{Items: rows}}
}

// fit is how many of the last lines room rows hold, a wrapped line counting each
// of its rows, measured as they draw: at least one there is, whatever the room, as
// the newest line is never left out.
func (l Logs) fit(c component.Ctx, room int) int {
	if len(l.Last) == 0 {
		return 0
	}
	n, rows := 0, 0
	for n < len(l.Last) {
		rows += len(component.CodeBlock{Lines: l.Last[len(l.Last)-n-1 : len(l.Last)-n]}.Rows(c))
		if rows > room {
			break
		}
		n++
	}
	return max(1, n)
}

// tailTitle is the header over the last n lines.
func (l Logs) tailTitle(n int) string {
	switch {
	case n == l.Total && n == 1:
		return logsOne
	case n == l.Total:
		return fmt.Sprintf(logsAll, n)
	}
	return fmt.Sprintf(logsLastOf, n, l.Total)
}

// tail is the section of the last lines room rows hold, room counting neither its
// header nor the gaps either side of it. The lines are code (component.CodeBlock,
// DEV-066).
func (l Logs) tail(c component.Ctx, room int) Section {
	n := l.fit(c, room)
	return Section{Header: component.SectionHeader{Title: l.tailTitle(n)}, Code: component.CodeBlock{Lines: l.Last[len(l.Last)-n:]}}
}

// tailChrome is the rows a tail section takes beside its lines: its header and the
// gap either side of it.
const tailChrome = 2*theme.SectionGap + 1

// OpenKey is item Open's key, "" where there is no such item.
func (l Logs) OpenKey() string {
	if l.Pager == "" {
		return ""
	}
	return "1"
}

// items are the screen's items: Open where a pager was found, then Refresh.
func (l Logs) items() []component.MenuItem {
	var out []component.MenuItem
	if l.Pager != "" {
		out = append(out, component.MenuItem{Key: l.OpenKey(), Label: logsOpen, Description: fmt.Sprintf(logsOpenAbout, l.Pager)})
	}
	return append(out, component.MenuItem{Key: strconv.Itoa(len(out) + 1), Label: logsRefresh, Description: logsRefreshAbout})
}

// SubMenu returns the screen for c: the title bar; "File", its rows by State; with
// lines, "Last N of M lines", "All N lines" or "1 line" over as many of the last
// lines as c's height holds under the screen's other blocks (decision 8 (a)); Open
// where a pager was found, Refresh; Return; "Select an option"; "Option".
// No pause: the screen ends at a menu (P-28 (a), for this screen alone). Plan only
// draws the same: no sub-menu shows the mode.
func (l Logs) SubMenu(c component.Ctx) SubMenu {
	vm := SubMenu{
		Title:    trail(logsTitle),
		Sections: []Section{l.fileSection()},
		Heading:  fixtureHeading,
		Menu:     component.Menu{Items: l.items(), Exit: returnItem()},
		Prompt:   menuPrompt,
	}
	if l.State == LogsLines {
		vm.Sections = append(vm.Sections, l.tail(c, c.Env.Height-lines(vm.Render(c))-tailChrome))
	}
	return vm
}

// Terminal renders what hardn -p writes to a terminal: the title bar, "File" and
// the last lines c's height holds, with no menu and no prompt, room left for the
// shell (decision 10 (a)).
func (l Logs) Terminal(c component.Ctx) string {
	head := join(titleBar(trail(logsTitle), TitleTrail).Render(c), l.fileSection().Render(c))
	if l.State != LogsLines {
		return head
	}
	return join(head, l.tail(c, c.Env.Height-lines(head)-tailChrome-logsShellRows).Render(c))
}
