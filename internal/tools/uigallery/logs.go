package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/screen"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Logs mocks (refactor plan slice 1, unit (a), next/s1-gallery): a
// proposal of the Logs screen, main menu 10, for the maintainer's review,
// nothing of it wired into hardn. It starts from origin/main's screen,
// which prints the whole file (pkg/menu/logs_menu.go:32-63), and P-18.
//
// It only reads, so it has no plan, no y/N and no journal. Nothing on the
// new side runs a pager or reads a log for display, so all of the reading
// is the gallery's, each a stand-in for unit (b)'s work, labelled so here
// and in the page: readLog, the file hardn writes read through host.System;
// safeLine, what makes a line safe to show; parseLogLine, the fixed parser;
// and pagerFor, which says what a pager would be. No golden can run a pager:
// the gallery says what the screen says and the command hardn would run.
//
// "logs" lists the mocks with no input; "play logs" is the menu of the
// hosts and "play logs/<host>" the screen on one.

// logsPrefix names the mocks: "logs <mock>" in the listing, "logs" and
// "logs/<host>" in play.
const logsPrefix = "logs"

// logsMainHost is the host the main menu's 10 leads to.
const logsMainHost = logsPrefix + "/lines"

// logsLinks lead from a menu mock's item to the Logs screen on a host, by
// the mock's name and the item's key, beside links: the main menu's 10,
// which said the gallery had no screen for it until this unit.
var logsLinks = map[string]string{mainMenuMock + "/10": logsMainHost}

// hardnLog is the file hardn writes, whatever hardn.yml's logFile says
// (cmd/hardn/main.go:57).
const hardnLog = output.LegacyLogPath

// The screen's words: the main menu's label for the title, the decided
// heading and prompt (screen-backup.md §5, P-29 (a)); the rest proposed.
const (
	logsTitle    = "Logs"
	secLog       = "Log"
	rowFile      = "File"
	rowSize      = "Size"
	noLogYet     = "No log yet"
	logEmptyRow  = "Empty"
	cantReadIt   = "Can't read it: %s"
	logFileOff   = "hardn.yml's logFile says %s. hardn doesn't write there."
	lastOf       = "Last %d of %d lines"
	allLines     = "All %d lines"
	oneLine      = "1 line"
	aboutLogOpen = "The whole log in %s"
	aboutLogRead = "Read the log again"
	// The non-interactive forms' words, on stderr.
	cliNoLog    = "hardn: no log yet: %s"
	cliCantRead = "hardn: can't read %s: %s"
)

// The treatments the maintainer chooses between
// (gallery-system-details.md): how the log reads. Each host draws
// read-tail unless it names read-pager.
const (
	readTail  = "read-tail"  // a screen of the last lines, as many as the terminal holds; an item opens the pager
	readPager = "read-pager" // P-18 as written: the main menu's item runs the pager at once; the tail screen only without one
)

// logState is what readLog found.
type logState uint8

// The states.
const (
	logLines   logState = iota // lines to show
	logMissing                 // no file there
	logUnread                  // a file that cannot be read
	logEmpty                   // a file with nothing in it
)

// logHost is one simulated host the Logs mocks run on.
type logHost struct {
	// name is the host's, after logsPrefix: "lines".
	name string
	// label and about are its item in the menu of the hosts.
	label, about string
	// note says what the host is.
	note string
	// host returns the simulated host, fresh.
	host func() *fakeHost
	// read is the treatment it draws; "" is read-tail.
	read string
}

// logSample is a log as hardn's lines look (logging.go's LstdFlags and its
// five levels; output.LegacyLogPath's comment; a run log's lines through
// LogFileOnly, cmd/hardn/confirm.go:149-160, host/runlog.go:13-16), the
// messages hardn's own. Each is under 80 cells, so that a pipe's form fits
// a golden; some wrap on the screen.
var logSample = []string{
	"2026/10/20 11:58:02 INFO: Created ~/.hushlogin",
	"2026/10/20 11:58:40 SUCCESS: Firewall configured successfully",
	"2026/10/20 12:00:01 INFO: run 1: $ ufw allow 2230/tcp",
	"2026/10/20 12:00:01 INFO: run 1: | Rule added",
	"2026/10/20 12:00:01 INFO: run 1: exit 0",
	"2026/10/20 12:00:03 INSTALLED: Package ufw",
	"2026/10/20 12:00:04 WARNING: ufw is not active",
	"2026/10/20 12:00:05 ERROR: Failed to configure firewall: exit status 1",
}

// logOf is lines as a file holds them.
func logOf(lines []string) string { return strings.Join(lines, "\n") + "\n" }

// longLog is a log of 1204 lines, nothing rotating it (output.LegacyLogPath).
func longLog() string {
	var lines []string
	for i := range 1204 - len(logSample) {
		lines = append(lines, fmt.Sprintf("2026/10/%02d %02d:%02d:%02d INFO: run %d: exit 0", 1+i/96, i/4%24, i%4*15, i%60, i+1))
	}
	return logOf(append(lines, logSample...))
}

// controlLog is a log whose last lines hold what a command printed: colour,
// a terminal title and bell, a bidi override and bytes that are not UTF-8,
// which a run log copies in as it comes (host/runlog.go:62-64).
const controlLog = "2026/10/20 12:00:01 INFO: run 1: $ apt-get update\n" +
	"2026/10/20 12:00:02 INFO: run 1: | \x1b[31mErr:1\x1b[0m http://deb.example.net\n" +
	"2026/10/20 12:00:02 INFO: run 1: | \x1b]0;a title\x07done\x1b[2J\n" +
	"2026/10/20 12:00:03 INFO: run 2: | abc‮def\n" +
	"2026/10/20 12:00:03 INFO: run 2: | caf\xe9\n"

// logOn returns a function making a Debian 13 host with the log content
// and less installed.
func logOn(content string) func() *fakeHost {
	return func() *fakeHost {
		h := withLess(simulate(debian13Platform))
		h.f.AddFile(hardnLog, []byte(content), 0o644, 0, 0)
		return h
	}
}

// withLess installs less, as Debian's and Ubuntu's own package; [I]: its
// presence on each image is not read here.
func withLess(h *fakeHost) *fakeHost {
	h.f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	return h
}

// logHosts are the hosts, in the menu's order: the log's states, then the
// other pager's and the treatment on the same logs.
func logHosts() []logHost {
	const deb = hostFQDN + " runs Debian 13, less installed. "
	return []logHost{
		{name: "lines", label: "A log with lines", about: "Fits the screen", host: logOn(logOf(logSample[len(logSample)-4:])),
			note: deb + "Treatment read-tail: the log's 4 lines, all of them, WARNING and ERROR in the warning's colour."},
		{name: "long", label: "A long log", about: "1204 lines", host: logOn(longLog()),
			note: deb + "Treatment read-tail: as many of the last lines as the terminal holds: 5 at 80x24, 20 at 80x40 (-height 40). origin/main prints all 1204."},
		{name: "missing", label: "No log yet", about: "No file", host: func() *fakeHost { return withLess(simulate(debian13Platform)) },
			note: deb + "No file there. origin/main's main() makes it at every start as root, before flags (cmd/hardn/main.go:57; logging.go:38), so today this shows only where that open failed; next/p3-startup moves that write."},
		{name: "unreadable", label: "Unreadable", about: "A directory there", host: unreadableLog,
			note: deb + "/var/log/hardn.log is a directory. Root reads past a file's permissions [I], so a directory, an I/O error or an NFS root squash is what is left of \"unreadable\" (menu/logs_unreadable.golden, not as root)."},
		{name: "empty", label: "Empty", about: "No lines", host: logOn(""),
			note: deb + "The file is there and empty: what main()'s open leaves on a first start, before anything is logged (logging.go:38)."},
		{name: "control", label: "Control characters", about: "Escape sequences, shown safe", host: logOn(controlLog),
			note: deb + "Lines hold what commands printed: colour, a title and bell, a bidi override, bytes that are not UTF-8. They show as ^[ and <U+202E> and <0xe9>; origin/main prints them raw to the terminal (logging.go:146; file_logs_repository.go:96; menu/logs_escape.golden)."},
		{name: "elsewhere", label: "logFile elsewhere", about: "hardn.yml says another path", host: elsewhereLog,
			note: deb + "hardn.yml says logFile: /srv/log/hardn.log, where nothing writes. The screen reads the file hardn writes and says so; origin/main reads logFile, not what main() opens (P-18; config.go:324)."},
		{name: "alpine323", label: "Alpine 3.23", about: "BusyBox's less", host: alpineLog,
			note: hostFQDN + " runs Alpine 3.23, whose less is BusyBox's [I, unverified]. LESSSECURE is GNU less's: BusyBox's reading of it is [unverified]."},
		{name: "no-pager", label: "No pager", about: "No less installed", host: noPagerLog,
			note: hostFQDN + " runs Debian 13 with no less: no Open item, the last lines alone."},
		{name: "read-pager", label: "Treatment: pager first", about: "Item 10 runs less", host: logOn(longLog()), read: readPager,
			note: deb + "Treatment read-pager, P-18 as written: the main menu's 10 runs the pager at once, and returns there. A golden cannot run it: this says what would run."},
		{name: "read-pager-none", label: "Pager first, no pager", about: "The last lines instead", host: noPagerLog, read: readPager,
			note: hostFQDN + " runs Debian 13 with no less: treatment read-pager falls back to read-tail's screen, P-18's \"otherwise the last N lines\"."},
	}
}

// noPagerLog is Debian 13 with the sample log and no less.
func noPagerLog() *fakeHost {
	h := simulate(debian13Platform)
	h.f.AddFile(hardnLog, []byte(logOf(logSample)), 0o644, 0, 0)
	return h
}

// unreadableLog is Debian 13 with a directory where the log is.
func unreadableLog() *fakeHost {
	h := withLess(simulate(debian13Platform))
	h.f.AddDir(hardnLog, 0o755, 0, 0)
	return h
}

// elsewhereLog is Debian 13 whose hardn.yml sets logFile elsewhere.
func elsewhereLog() *fakeHost {
	return logOn(logOf(logSample))().withHardnYML("logFile: /srv/log/hardn.log\n")
}

// alpineLog is Alpine 3.23 with BusyBox's less: a link to /bin/busybox [I].
func alpineLog() *fakeHost {
	h := simulate(alpine323Platform)
	h.f.AddFile("/bin/busybox", []byte("ELF"), 0o755, 0, 0)
	h.f.AddSymlink("/usr/bin/less", "/bin/busybox")
	h.f.AddFile(hardnLog, []byte(logOf(logSample)), 0o644, 0, 0)
	return h
}

// findLogHost returns the host named name, after logsPrefix.
func findLogHost(name string) (logHost, bool) {
	for _, h := range logHosts() {
		if h.name == name {
			return h, true
		}
	}
	return logHost{}, false
}

// logReading is what readLog read: the stand-in for what unit (b) builds.
type logReading struct {
	lh        logHost
	path      string
	state     logState
	reason    string // why it cannot be read
	size      int64
	lines     []string // every line, safe to show
	elsewhere string   // hardn.yml's logFile, where it is not the file hardn writes
	pager     pagerChoice
}

// readLog reads the file hardn writes through sys, whatever hardn.yml's
// logFile says, which it reads only to say so. It changes nothing. It reads
// the whole file, as origin/main does: nothing rotates the log, so unit
// (b)'s reader should read its end alone, which host.System has no call for.
func readLog(ctx context.Context, sys host.System, lh logHost) logReading {
	r := logReading{lh: lh, path: hardnLog, pager: pagerFor(ctx, sys, noEnv, false)}
	if cfg, err := statusConfig(ctx, sys); err == nil && cfg.LogFile != "" && cfg.LogFile != hardnLog {
		r.elsewhere = cfg.LogFile
	}
	fi, err := sys.Stat(ctx, r.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		r.state = logMissing
		return r
	case err != nil:
		r.state, r.reason = logUnread, reasonOf(err)
		return r
	}
	r.size = fi.Size()
	data, err := sys.ReadFile(ctx, r.path)
	if err != nil {
		r.state, r.reason = logUnread, reasonOf(err)
		return r
	}
	for _, l := range host.TailLines(data, math.MaxInt) {
		r.lines = append(r.lines, safeLine(l))
	}
	if len(r.lines) == 0 {
		r.state = logEmpty
	}
	return r
}

// reasonOf is err's own words, without the path, which the row names.
func reasonOf(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

// safeLine makes a line of the log safe to show, keeping it readable: what
// a terminal would act on shows as it does in less and cat -v, ^[ for ESC,
// ^G for BEL; a character that draws as nothing, or reverses what follows,
// as its code point, as a plan shows it (component's raw); a byte that is
// not UTF-8 as <0xe9>. A stand-in for unit (b): components strip such
// text (component's clean, ctx.go:284), which would hide what a line held.
func safeLine(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&b, "<0x%02x>", s[0])
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20:
			b.WriteByte('^')
			b.WriteByte(byte(r) + 0x40)
		case r == 0x7f:
			b.WriteString("^?")
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			fmt.Fprintf(&b, "<U+%04X>", r)
		default:
			b.WriteRune(r)
		}
		s = s[n:]
	}
	return b.String()
}

// logLevels are the words logging.go writes after the date and time.
var logLevels = []string{"ERROR", "WARNING", "INFO", "SUCCESS", "INSTALLED"}

// logLine is one line of the log, parsed.
type logLine struct{ Time, Level, Message string }

// parseLogLine is the fixed parser, a stand-in for unit (b)'s: Go's
// log.LstdFlags date and time, the level, the message. A line of any other
// shape is not a parsed line: it stays as it is, a message with no level.
// origin/main's GetLogs splits on the first two spaces, so its Time is the
// date, its Level the clock and its Message "INFO: ..." (menu/logs_parser.golden;
// file_logs_repository.go:51-57), and nothing calls it but its wrappers and tests.
func parseLogLine(line string) logLine {
	f := strings.SplitN(line, " ", 3)
	if len(f) == 3 && len(f[0]) == 10 && len(f[1]) == 8 {
		if word, msg, ok := strings.Cut(f[2], ": "); ok {
			for _, l := range logLevels {
				if word == l {
					return logLine{Time: f[0] + " " + f[1], Level: l, Message: msg}
				}
			}
		}
	}
	return logLine{Message: line}
}

// pagerChoice is what would read the log; name is "" for no pager.
type pagerChoice struct {
	name string
	argv []string
	env  string // what it needs set, "NAME=value"
}

// command is the choice's command line, reading file.
func (p pagerChoice) command(file string) string {
	cmd := strings.Join(append(append([]string(nil), p.argv...), file), " ")
	if p.env != "" {
		return p.env + " " + cmd
	}
	return cmd
}

// noEnv is an environment with nothing in it.
func noEnv(string) string { return "" }

// pagerFor is what would read the log: with useEnv (treatment pager-env,
// P-18 as written) $PAGER where it is set, its words as the argv, through
// no shell; else less, found where hardn finds a command (host.SearchPath),
// with LESSSECURE=1 (treatment pager-less, proposed), which less(1) says
// turns off its shell commands, its pipe and its editor [unverified: not
// run]; else none. A stand-in: nothing on the new side runs one, and
// host.System's Run pipes a command's output and gives it no terminal.
func pagerFor(ctx context.Context, sys host.System, getenv func(string) string, useEnv bool) pagerChoice {
	if useEnv {
		if f := strings.Fields(getenv("PAGER")); len(f) > 0 {
			return pagerChoice{name: path.Base(f[0]), argv: f}
		}
	}
	for _, dir := range strings.Split(host.SearchPath, ":") {
		if _, err := sys.Stat(ctx, dir+"/less"); err == nil {
			return pagerChoice{name: "less", argv: []string{"less"}, env: "LESSSECURE=1"}
		}
	}
	return pagerChoice{}
}

// logRow is one line of the log as a row, wrapped under itself: WARNING and
// ERROR in the warning's colour, the rest muted.
func logRow(line string) component.KeyValue {
	switch parseLogLine(line).Level {
	case "WARNING", "ERROR":
		return component.KeyValue{Key: line, State: component.ValueWarn}
	}
	return component.KeyValue{Key: line, State: component.ValueNone}
}

// sizeWords is n bytes as a reader says it.
func sizeWords(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	case n < 1024*1024:
		return fmt.Sprintf("%d KiB", n/1024)
	}
	return fmt.Sprintf("%d MiB", n/(1024*1024))
}

// fileSection is the section that names the file and says its state.
func (r logReading) fileSection() screen.Section {
	rows := []component.KeyValue{{Key: rowFile, Value: r.path}}
	switch r.state {
	case logMissing:
		rows = append(rows, component.KeyValue{Key: noLogYet, State: component.ValueNone})
	case logUnread:
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(cantReadIt, r.reason), State: component.ValueWarn})
	case logEmpty:
		rows = append(rows, component.KeyValue{Key: logEmptyRow, State: component.ValueNone})
	default:
		count := oneLine
		if len(r.lines) != 1 {
			count = fmt.Sprintf("%d lines", len(r.lines))
		}
		rows = append(rows, component.KeyValue{Key: rowSize, Value: sizeWords(r.size) + ", " + count})
	}
	if r.elsewhere != "" {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(logFileOff, r.elsewhere), State: component.ValueWarn})
	}
	return screen.Section{Header: component.SectionHeader{Title: secLog}, Rows: component.BulletList{Items: rows}}
}

// tailSection is the section of the last n lines.
func (r logReading) tailSection(n int) screen.Section {
	title := fmt.Sprintf(lastOf, n, len(r.lines))
	switch {
	case n == len(r.lines) && n == 1:
		title = oneLine
	case n == len(r.lines):
		title = fmt.Sprintf(allLines, n)
	}
	var rows []component.KeyValue
	for _, l := range r.lines[len(r.lines)-n:] {
		rows = append(rows, logRow(l))
	}
	return screen.Section{Header: component.SectionHeader{Title: title}, Rows: component.BulletList{Items: rows}}
}

// fitTail is as many of the last lines as room rows hold, a wrapped line
// counting each of its rows, measured as the row draws: at least one.
func (r logReading) fitTail(c component.Ctx, room int) int {
	n := 0
	for n < len(r.lines) {
		rows := component.BulletList{Items: []component.KeyValue{}}
		for _, l := range r.lines[len(r.lines)-n-1:] {
			rows.Items = append(rows.Items, logRow(l))
		}
		if strings.Count(rows.Render(c), "\n")+1 > room {
			break
		}
		n++
	}
	return max(1, n)
}

// items are the screen's items: Open where there is something to open and
// something to open it, then Refresh.
func (r logReading) items() []logItem {
	var out []logItem
	add := func(label, about string, open bool) {
		out = append(out, logItem{item: component.MenuItem{Key: strconv.Itoa(len(out) + 1), Label: label, Description: about}, open: open})
	}
	if r.state == logLines && r.pager.name != "" {
		add(itemOpen, fmt.Sprintf(aboutLogOpen, r.pager.name), true)
	}
	add(itemRefresh, aboutLogRead, false)
	return out
}

// logItem is one item of the screen: open is Open, otherwise it is Refresh.
type logItem struct {
	item component.MenuItem
	open bool
}

// subMenu is the screen for c, as many of the last lines as c's height
// holds under its other blocks.
func (r logReading) subMenu(c component.Ctx) screen.SubMenu {
	var items []component.MenuItem
	for _, it := range r.items() {
		items = append(items, it.item)
	}
	vm := screen.SubMenu{
		Title:    component.TitleBar{Title: logsTitle},
		Sections: []screen.Section{r.fileSection()},
		Heading:  sdHeading,
		Menu:     component.Menu{Items: items, Exit: component.MenuItem{Key: "0", Label: "Return"}},
		Prompt:   sdPrompt,
	}
	if r.state == logLines {
		// The section's header and the gap each side of it (theme.SectionGap),
		// beside the rest of the screen, which is what is left of the height.
		room := c.Env.Height - (strings.Count(vm.Render(c), "\n") + 1) - 2*theme.SectionGap - 1
		vm.Sections = append(vm.Sections, r.tailSection(r.fitTail(c, room)))
	}
	return vm
}

// logsRun is one run of the Logs screen on a simulated host.
type logsRun struct {
	s  *session
	lh logHost
	h  *fakeHost
}

// openLogs returns the action that opens what name names: the hosts' menu
// for logsPrefix alone, the screen on one host for "logs/<host>".
func (s *session) openLogs(name string) func() error {
	if lh, ok := findLogHost(strings.TrimPrefix(name, logsPrefix+"/")); ok && name != logsPrefix {
		return func() error { return s.logsScreen(context.Background(), lh, lh.host()) }
	}
	return func() error {
		m := hostsMenu("Logs hosts", func(add func(label, about string, action func() error)) {
			for _, lh := range logHosts() {
				add(lh.label, lh.about, func() error { return s.logsScreen(context.Background(), lh, lh.host()) })
			}
		})
		return s.p.Run(func() nav.Screen { return m.vm.Screen(s.p.Ctx, m.menu) })
	}
}

// logsScreen runs the screen on h, lh's host, until the user goes back from
// it, or under read-pager with a pager at hand says what it would run and
// returns. Nothing is written or run; the plan-only sequence draws the same
// screen.
func (s *session) logsScreen(ctx context.Context, lh logHost, h *fakeHost) error {
	run := &logsRun{s: s, lh: lh, h: h}
	if err := s.writeNote(logsPrefix+"/"+lh.name, lh.note); err != nil {
		return err
	}
	if r := readLog(ctx, h.system(), lh); lh.read == readPager && r.state == logLines && r.pager.name != "" {
		_, err := io.WriteString(s.p.Out, pagerSays(s.p.Ctx, r.pager.command(r.path))+"\n")
		return err
	}
	return s.p.Run(func() nav.Screen { return run.draw(ctx) })
}

// pagerSays is the gallery's note for treatment read-pager, whose item runs
// the pager at once and returns to the main menu.
func pagerSays(c component.Ctx, command string) string {
	return component.Callout{Kind: component.MessageInfo, Body: []string{
		"The gallery runs no pager. hardn would run " + command + ", then draw the main menu again.",
	}}.Render(c)
}

// draw reads the host and returns the screen, its items bound.
func (run *logsRun) draw(ctx context.Context) nav.Screen {
	s := run.s
	r := readLog(ctx, run.h.system(), run.lh)
	vm := r.subMenu(s.p.Ctx)
	menu := nav.Menu{Exit: vm.Menu.Exit}
	for _, it := range r.items() {
		action := func() error { run.grow(); return nil }
		if it.open {
			action = func() error {
				_, err := io.WriteString(s.p.Out, strings.Repeat("\n", theme.SectionGap)+pagerNote(s.p.Ctx, r.pager.command(r.path))+"\n")
				return err
			}
		}
		menu.Options = append(menu.Options, nav.Option{Item: it.item, Action: action})
	}
	return vm.Screen(s.p.Ctx, menu)
}

// grow moves the simulated host on, as if another hardn wrote a line before
// a Refresh, so that the screen drawn again shows a change. A log that is
// not a file stays as it is.
func (run *logsRun) grow() {
	if e, ok := run.h.f.Entry(hardnLog); ok && e.Data != nil {
		run.h.f.AddFile(hardnLog, append(e.Data, []byte("2026/10/20 12:01:00 INFO: run 2: exit 0\n")...), 0o644, 0, 0)
	}
}

// logsStart returns the hosts' menu, or the host, that args name ("logs",
// "logs/<host>"), and whether they name one; an error for a host that is
// not one.
func logsStart(args []string) (string, bool, error) {
	if len(args) != 1 || (args[0] != logsPrefix && !strings.HasPrefix(args[0], logsPrefix+"/")) {
		return "", false, nil
	}
	if name := strings.TrimPrefix(args[0], logsPrefix+"/"); args[0] != logsPrefix {
		if _, ok := findLogHost(name); !ok {
			var names []string
			for _, h := range logHosts() {
				names = append(names, logsPrefix+"/"+h.name)
			}
			return "", false, fmt.Errorf("no logs host %q; the hosts are %s", args[0], strings.Join(names, ", "))
		}
	}
	return args[0], true, nil
}

// logsNote is play's note for the Logs mocks.
const logsNote = "Play mode, mocks: the Logs screen proposed for slice 1, read from a simulated host by the gallery's stand-ins, " +
	"unit (b)'s to build. It only reads: no plan, no y/N, no journal. No pager runs: an item says what it would run. " +
	"Nothing on this machine changes: every command and file is the fake's."

// readLogHost reads the host named name, fresh, as the screen does.
func readLogHost(ctx context.Context, name string) (logReading, error) {
	lh, ok := findLogHost(name)
	if !ok {
		return logReading{}, fmt.Errorf("no logs host %q", name)
	}
	return readLog(ctx, lh.host().system(), lh), nil
}

// onLogHost returns a drawing of the host named name's reading.
func onLogHost(name string, view func(logReading, component.Ctx) string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		r, err := readLogHost(ctx, name)
		if err != nil {
			return "", err
		}
		return view(r, c), nil
	}
}

// pagerEnv is one environment hardn may run in, as the pager table shows it.
type pagerEnv struct {
	label string
	env   map[string]string
	host  func() *fakeHost
}

// pagerEnvs are the environments the pager table shows: a root login, where
// PAGER is root's; sudo and doas, which drop it [I: sudo's env_reset dropped
// HARDN_CONFIG on the VMs, vmtest.md §4.9; PAGER was not run; doas keeps
// only what setenv or keepenv says, sudoers.md:102]; sudo where a rule or -E
// keeps it; and a host with no less.
func pagerEnvs() []pagerEnv {
	with := func(user string) map[string]string {
		return map[string]string{"SUDO_USER": user, "USER": "root", "PAGER": "most"}
	}
	return []pagerEnv{
		{"root login, PAGER=most", map[string]string{"USER": "root", "PAGER": "most"}, logOn("")},
		{"sudo, PAGER dropped", sudoAlice, logOn("")},
		{"doas, PAGER dropped", doasAlice, alpineLog},
		{"sudo, PAGER=most kept", with("alice"), logOn("")},
		{"no less, no PAGER", sudoAlice, noPagerLog},
	}
}

// pagerTable draws what each treatment would run in each environment.
func pagerTable(ctx context.Context, c component.Ctx) (string, error) {
	rows := func(useEnv bool) []component.KeyValue {
		var out []component.KeyValue
		for _, e := range pagerEnvs() {
			p := pagerFor(ctx, e.host().system(), lookup(e.env), useEnv)
			v := p.command(hardnLog)
			if p.name == "" {
				v = "None: the last lines alone"
			}
			out = append(out, component.KeyValue{Key: e.label, Value: v})
		}
		return out
	}
	return apart(
		component.SectionHeader{Title: "pager-less, proposed"}.Render(c),
		component.BulletList{Items: rows(false)}.Render(c),
		component.SectionHeader{Title: "pager-env, P-18 as written"}.Render(c),
		component.BulletList{Items: rows(true)}.Render(c),
	), nil
}

// logsMocks are the listing's mocks, in order: the screen on each host,
// Open, the pager table, the non-interactive forms, the parser and
// origin/main's in their own words.
func logsMocks() []listMock {
	var mocks []listMock
	for _, lh := range logHosts() {
		mocks = append(mocks, listMock{name: "screen-" + lh.name, note: "Mock: the Logs screen. " + lh.note,
			draw: onLogHost(lh.name, func(r logReading, c component.Ctx) string {
				if lh.read == readPager && r.state == logLines && r.pager.name != "" {
					return pagerSays(c, r.pager.command(r.path))
				}
				return r.subMenu(c).Render(c)
			})})
	}
	blocks := func(c component.Ctx, sections ...screen.Section) string {
		out := []string{component.TitleBar{Title: logsTitle}.Render(c)}
		for _, s := range sections {
			out = append(out, s.Render(c))
		}
		return apart(out...)
	}
	return append(mocks,
		listMock{name: "open", note: "Treatment read-tail: item 1, Open. A golden cannot run a pager: this is what the screen says and the command hardn would run, the screen drawn again when it exits.",
			draw: onLogHost("long", func(r logReading, c component.Ctx) string {
				return apart(r.subMenu(c).Render(c), pagerNote(c, r.pager.command(r.path)), r.subMenu(c).Render(c))
			})},
		listMock{name: "pager-commands", note: "What each pager treatment would run, by the environment. Under sudo and doas PAGER is dropped [I; not run for PAGER]. pager-env runs as root whatever a kept PAGER names, and most has its own shell escape [I]. LESSSECURE=1 is less(1)'s secure mode, turning off ! and | and v [unverified: not run; BusyBox's less unread]. The gallery's table: nothing runs.",
			draw: pagerTable},
		listMock{name: "cli-terminal-tail", note: "hardn -p to a terminal, treatment read-tail: the file and as many of the last lines as the terminal holds, no menu and no prompt, for a shell to carry on under.",
			draw: onLogHost("long", func(r logReading, c component.Ctx) string {
				return blocks(c, r.fileSection(), r.tailSection(r.fitTail(c, c.Env.Height-cliChrome)))
			})},
		listMock{name: "cli-terminal-pager", note: "hardn -p to a terminal, treatment read-pager: the pager at once, as the screen's. A golden cannot run it.",
			draw: onLogHost("long", func(r logReading, c component.Ctx) string {
				return component.Callout{Kind: component.MessageInfo, Body: []string{"The gallery runs no pager. hardn would run " + r.pager.command(r.path) + ", then exit."}}.Render(c)
			})},
		listMock{name: "cli-pipe", wide: term80, note: "hardn -p not to a terminal: every line, as written, in no style, no heading, so that a tool reads the log alone. origin/main's opens \"# Contents of\" (logging.go:145). Exit 0.",
			draw: onLogHost("lines", func(r logReading, _ component.Ctx) string { return strings.Join(r.lines, "\n") })},
		listMock{name: "cli-pipe-control", wide: term80, note: "The same on the control host: safe off a terminal too, since a pipe may end at one. Who wants the bytes has cat /var/log/hardn.log.",
			draw: onLogHost("control", func(r logReading, _ component.Ctx) string { return strings.Join(r.lines, "\n") })},
		listMock{name: "cli-missing", note: "hardn -p with no log: this on stderr, nothing on stdout, exit 0: a read that found nothing. origin/main logs [ERROR] Failed to read log file, into the log too, and carries on, exit 0 by reading (logging.go:139-143; menu/logs_print_flag_missing.golden).",
			draw: onLogHost("missing", func(r logReading, _ component.Ctx) string { return fmt.Sprintf(cliNoLog, r.path) })},
		listMock{name: "cli-unreadable", note: "hardn -p with a log it cannot read: this on stderr, nothing on stdout, exit 1.",
			draw: onLogHost("unreadable", func(r logReading, _ component.Ctx) string { return fmt.Sprintf(cliCantRead, r.path, r.reason) })},
		listMock{name: "parser", note: "The slice's broken parser. origin/main's GetLogs on a real line (menu/logs_parser.golden; file_logs_repository.go:51-57; nothing calls it but its wrappers and tests), and the stand-in's fix. The screen needs a level only to colour WARNING and ERROR.",
			draw: func(_ context.Context, c component.Ctx) (string, error) { return parserDrawing(c), nil }},
		listMock{name: "origin", wide: term80, note: "origin/main's Logs screen, its words as they are (logs_menu.go:33-62, file_logs_repository.go:95-96; menu/logs_open.golden), on the design system: the whole file, 4 lines here, raw.",
			draw: onLogHost("lines", func(r logReading, c component.Ctx) string {
				return apart(component.TitleBar{Title: "View Logs"}.Render(c),
					component.BulletList{Items: []component.KeyValue{{Key: "Log file", Value: r.path}}}.Render(c),
					component.SectionHeader{Title: "Log Contents:"}.Render(c),
					"# Contents of "+r.path+":", strings.Join(r.lines, "\n"),
					component.Message{Text: "Press Enter to return to the main menu..."}.Render(c))
			})},
		listMock{name: "origin-p", wide: term80, note: "origin/main's hardn -p (cmd/hardn/main.go:415-417; logging.go:145-146; menu/logs_print_flag.golden): the same, to a terminal or not. It needs root (cmd/hardn/main.go:148), though the file is 0644.",
			draw: onLogHost("lines", func(r logReading, _ component.Ctx) string {
				return "# Contents of " + r.path + ":\n\n" + strings.Join(r.lines, "\n")
			})},
		listMock{name: "origin-elsewhere", note: "origin/main's Logs screen with hardn.yml's logFile set where nothing writes: it reads that path, so the log hardn does write is not shown (file_logs_repository.go:81-84; P-18; menu/logs_other_path.golden).",
			draw: func(_ context.Context, c component.Ctx) (string, error) {
				const elsewhere = "/srv/log/hardn.log"
				return apart(component.TitleBar{Title: "View Logs"}.Render(c),
					component.BulletList{Items: []component.KeyValue{{Key: "Log file", Value: elsewhere}}}.Render(c),
					component.SectionHeader{Title: "Log Contents:"}.Render(c),
					component.Message{Kind: component.MessageBad, Text: "Error displaying logs: failed to access log file " + elsewhere + ": stat " + elsewhere + ": no such file or directory"}.Render(c),
					component.Message{Text: "Press Enter to return to the main menu..."}.Render(c)), nil
			}},
	)
}

// cliChrome is the rows hardn -p takes on a terminal besides the lines: the
// title, the file's two rows, two headers, their gaps, and a shell's prompt
// and the command above it.
const cliChrome = 11

// term80 is the width the raw forms need: their lines do not wrap.
const term80 = 80

// parserDrawing is a line, as GetLogs reads it and as the stand-in does.
func parserDrawing(c component.Ctx) string {
	const line = "2026/10/20 12:00:05 ERROR: Failed to configure firewall: exit status 1"
	p := parseLogLine(line)
	return apart(
		component.SectionHeader{Title: "The line"}.Render(c),
		component.BulletList{Items: []component.KeyValue{{Key: line, State: component.ValueNone}}}.Render(c),
		component.SectionHeader{Title: "GetLogs, origin/main"}.Render(c),
		component.BulletList{Items: []component.KeyValue{{Key: "Time", Value: "2026/10/20"}, {Key: "Level", Value: "12:00:05"},
			{Key: "Message", Value: "ERROR: Failed to configure firewall: exit status 1"}}}.Render(c),
		component.SectionHeader{Title: "parseLogLine, the stand-in"}.Render(c),
		component.BulletList{Items: []component.KeyValue{{Key: "Time", Value: p.Time}, {Key: "Level", Value: p.Level},
			{Key: "Message", Value: p.Message}}}.Render(c),
	)
}

// logsUsage is the listing's usage line.
const logsUsage = "usage: go run ./internal/tools/uigallery logs [flags] [mock | all]..."

// logsListing is the Logs listing.
var logsListing = listing{prefix: logsPrefix, usage: logsUsage, mocks: logsMocks}
