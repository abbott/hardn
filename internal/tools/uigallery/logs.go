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
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

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
// which prints the whole file (pkg/menu/logs_menu.go:32-63), v0.3.2's item
// and P-18. Each mock's note opens with logsLabel, but those in origin/main's
// own words.
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

// logsLabel is the source label every mock carries: no frame; the plan's
// "Logs from v0.3.2 + origin/main".
const logsLabel = "Inferred from v0.3.2 and origin/main."

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
	secLog       = "File"
	rowFile      = "Path"
	rowSize      = "Size"
	noLogYet     = "No log yet"
	logEmptyRow  = "Empty"
	cantReadIt   = "Can't read it: %s"
	logFileOff   = "hardn.yml's logFile, %s, isn't used"
	lastOf       = "Last %d of %d lines"
	allLines     = "All %d lines"
	oneLine      = "1 line"
	aboutLogOpen = "The whole log in %s"
	aboutLogRead = "Read the log again"
	// The non-interactive forms' words, on stderr.
	cliNoLog    = "hardn: no log yet: %s"
	cliCantRead = "hardn: can't read %s: %s"
	cliRunWith  = "Run it with %s."
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
}

// startupLines are what every start of hardn with a hardn.yml writes before
// the screen or -p can read the log: config.go:246, osdetect.go:93-95, as the
// VM runs' logs hold them (vm-runs/s3/57-log-ubuntu26-vm.log:116-117,
// s3/97-log-alpine323-vm.log:7-8). With no hardn.yml a start writes three
// (vm-runs/s2/58-alpine-example.log).
var startupLines = []string{
	"INFO: Using configuration from: /etc/hardn/hardn.yml",
	"SUCCESS: debian trixie detected",
}

// logSample is a log as hardn's lines look (logging.go's LstdFlags and its
// levels; a run log's lines through LogFileOnly, cmd/hardn/confirm.go:149-160,
// host/runlog.go:13-16): the messages are hardn's where the code cited writes
// them (main.go's firewall lines, revert.go:336, startupLines), the commands,
// reasons and times synthetic. It ends as a real log does, with this start's
// own lines. Each is under 80 cells, so that a pipe's form fits a golden; some
// wrap on the screen.
var logSample = []string{
	"2026/10/20 11:58:02 " + startupLines[0],
	"2026/10/20 11:58:02 " + startupLines[1],
	"2026/10/20 12:00:01 INFO: run 1: $ ufw allow 2230/tcp",
	"2026/10/20 12:00:01 INFO: run 1: | Rule added",
	"2026/10/20 12:00:01 INFO: run 1: exit 0",
	"2026/10/20 12:00:03 SUCCESS: Firewall configured successfully",
	"2026/10/20 12:00:04 WARNING: hardn's boot unit was not removed: exit status 1",
	"2026/10/20 12:00:05 ERROR: Failed to configure firewall: exit status 1",
	"2026/10/20 12:05:11 " + startupLines[0],
	"2026/10/20 12:05:11 " + startupLines[1],
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
// a terminal title and bell, a bidi override, bytes that are not UTF-8, and
// typographic characters, which must keep their shape in both glyph sets; a
// run log copies them in as they come (host/runlog.go:62-64).
const controlLog = "2026/10/20 12:00:01 INFO: run 1: $ apt-get update\n" +
	"2026/10/20 12:00:02 INFO: run 1: | \x1b[31mErr:1\x1b[0m http://deb.example.net\n" +
	"2026/10/20 12:00:02 INFO: run 1: | \x1b]0;a title\x07done\x1b[2J\n" +
	"2026/10/20 12:00:03 INFO: run 2: | abc‮def\n" +
	"2026/10/20 12:00:03 INFO: run 2: | caf\xe9\n" +
	"2026/10/20 12:00:03 INFO: run 2: | don’t “quote” ≥ 5 → … x\n"

// logOn returns a function making a Debian 13 host with the log content
// and less installed.
func logOn(content string) func() *fakeHost {
	return func() *fakeHost {
		h := withLess(simulate(debian13Platform))
		h.f.AddFile(hardnLog, []byte(content), 0o644, 0, 0)
		return h
	}
}

// withLess installs less, as Debian's and Ubuntu's own package, GNU less 668
// on their VMs (vm-runs/s1/01-tools.log).
func withLess(h *fakeHost) *fakeHost {
	h.f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	return h
}

// logHosts are the hosts, in the menu's order: the log's states, then the
// hosts with no pager.
func logHosts() []logHost {
	const deb = hostFQDN + " runs Debian 13, less installed. "
	return []logHost{
		{name: "lines", label: "A log with lines", about: "Fits the screen", host: logOn(logOf(logSample[len(logSample)-4:])),
			note: deb + "A log of 4 lines, as many as the terminal holds: 3 at 80x24, all at 80x40, as three wrap. WARNING and ERROR in the warning's colour."},
		{name: "long", label: "A long log", about: "1204 lines", host: logOn(longLog()),
			note: deb + "As many of the last lines as the terminal holds: room for 6 rows at 80x24, 5 drawn, and 22 at 80x40 (-height 40), a wrapped line taking two; the newest are this start's own (config.go:246; osdetect.go:93-95). origin/main prints all 1204."},
		{name: "missing", label: "No log yet", about: "No file", host: func() *fakeHost { return withLess(simulate(debian13Platform)) },
			note: deb + "No file there. origin/main's main() makes it at every start as root, before flags (cmd/hardn/main.go:57; logging.go:38), so today this shows only where that open failed; next/p3-startup moves that write."},
		{name: "unreadable", label: "Unreadable", about: "A directory there", host: unreadableLog,
			note: deb + "/var/log/hardn.log is a directory. Root reads past a file's permissions [I], so a directory, an I/O error or an NFS root squash is what is left of \"unreadable\" (menu/logs_unreadable.golden, not as root)."},
		{name: "empty", label: "Empty", about: "No lines", host: logOn(""),
			note: deb + "The file is there and empty. Nearly unreachable today, as start-up has written to it by the time the screen can read it (cmd/hardn/main.go:57, :155, :185; config.go:246; osdetect.go:93-95): only if hardn's own open failed, the file was truncated while hardn runs, or under next/p3-startup."},
		{name: "control", label: "Control characters", about: "Escape sequences, shown safe", host: logOn(controlLog),
			note: deb + "Lines hold what commands printed: colour, a title and bell, a bidi override, bytes that are not UTF-8, typographic characters. Those a terminal would act on show as ^[ and <U+202E> and <0xe9>, the rest as they are, in either glyph set; origin/main prints them raw to the terminal (logging.go:146; file_logs_repository.go:96; menu/logs_escape.golden)."},
		{name: "elsewhere", label: "logFile elsewhere", about: "hardn.yml says another path", host: elsewhereLog,
			note: deb + "hardn.yml says logFile: /srv/log/hardn.log, where nothing writes. The screen reads the file hardn writes and says so; origin/main reads logFile, not what main() opens (P-18; config.go:324)."},
		{name: "alpine323", label: "Alpine 3.23", about: "BusyBox's less", host: alpineLog,
			note: hostFQDN + " runs Alpine 3.23, whose less is a link to BusyBox (vm-runs/s1/01-tools.log). LESSSECURE=1 changes nothing there: s still prompts Log file, E and :e Examine (vm-runs/s1/04-less2-alpine323-vm.log). No path was typed at s, so its write did not run; E did try a name that does not exist. That s writes and E opens any path as root is read (miscutils/less.c, master). So it has no Open item (decision 7 (a)). With Alpine's less package there is one (pager-commands)."},
		{name: "no-pager", label: "No pager", about: "No less installed", host: noPagerLog,
			note: hostFQDN + " runs Debian 13 with no less: no Open item, the last lines alone."},
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

// alpineLessPackage is Alpine 3.23 with the less package: GNU less's own file
// [I, not run: no VM has the package].
func alpineLessPackage() *fakeHost {
	h := simulate(alpine323Platform)
	h.f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	h.f.AddFile(hardnLog, []byte(logOf(logSample)), 0o644, 0, 0)
	return h
}

// alpineLog is Alpine 3.23 with BusyBox's less: a link to /bin/busybox
// (vm-runs/s1/01-tools.log).
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
func readLog(ctx context.Context, sys host.System) logReading {
	r := logReading{path: hardnLog, pager: pagerFor(ctx, sys)}
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
// ^G for BEL; a character that draws as nothing, or as a blank other than the
// space, as its code point, as a plan shows it (component's raw, invisible); a
// tab as 4 spaces, as raw; a byte that is not UTF-8 as <0xe9>. Everything else
// keeps its characters, typographic ones too. A stand-in for unit (b): the
// row components map or strip such text (component's text and clean,
// ctx.go:284), which would hide what a line held.
func safeLine(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		r, n := utf8.DecodeRuneInString(s)
		switch {
		case r == utf8.RuneError && n == 1:
			fmt.Fprintf(&b, "<0x%02x>", s[0])
		case r == '\t':
			b.WriteString("    ")
		case r < 0x20:
			b.WriteByte('^')
			b.WriteByte(byte(r) + 0x40)
		case r == 0x7f:
			b.WriteString("^?")
		case unicode.IsControl(r), r != ' ' && unicode.In(r, unicode.Cf, unicode.Zs, unicode.Zl, unicode.Zp, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point):
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

// pagerChoice is what would read the log; name is "" for no pager. busybox
// says the less found is BusyBox's, which has no secure mode, so is not chosen.
type pagerChoice struct {
	name    string
	argv    []string
	env     string // what it needs set, "NAME=value"
	busybox bool
}

// command is the choice's command line, reading file.
func (p pagerChoice) command(file string) string {
	cmd := strings.Join(append(append([]string(nil), p.argv...), file), " ")
	if p.env != "" {
		return p.env + " " + cmd
	}
	return cmd
}

// pagerFor is what would read the log: less, found where hardn finds a command
// (host.SearchPath), with LESSSECURE=1, which turned off !, |, v, E and :e in GNU
// less 668 on the VMs, as a user, not as root (vm-runs/s1/04-less2-*); else none.
// BusyBox's less has no such mode: the variable changed nothing on the VM, and
// miscutils/less.c (master) reads LESS alone from the environment, at :1836, its
// s and E keys writing and opening any path, :1554-1566, :1212, read: no path was
// typed at s, and E tried only a name that does not exist. So it does not choose
// it. It goes by the link, and where it cannot tell chooses none; unit (b) asks
// less --version (the page, §8). A stand-in: nothing on the new side runs one, and
// host.System's Run pipes a command's output and gives it no terminal.
func pagerFor(ctx context.Context, sys host.System) pagerChoice {
	for _, dir := range strings.Split(host.SearchPath, ":") {
		if _, err := sys.Stat(ctx, dir+"/less"); err != nil {
			continue
		}
		_, target, err := sys.ResolveSymlink(ctx, dir+"/less")
		switch {
		case err != nil && !errors.Is(err, syscall.EINVAL): // EINVAL: not a link
			return pagerChoice{}
		case err != nil || path.Base(target) != "busybox":
			return pagerChoice{name: "less", argv: []string{"less"}, env: "LESSSECURE=1"}
		}
		return pagerChoice{busybox: true}
	}
	return pagerChoice{}
}

// logRows are lines of the log as code, as an error report's captured output
// (component's code and raw; README §9.1, "captured output"): no bullet, from
// theme.LabelCol, every character kept in both glyph sets, never cut. WARNING
// and ERROR take the warning's colour, the rest the output's, muted.
func logRows(c component.Ctx, lines []string) []string {
	var out []string
	for _, l := range lines {
		p := c.Theme.Output.Render
		switch parseLogLine(l).Level {
		case "WARNING", "ERROR":
			p = c.Theme.KeyValue.ValueWarn.Render
		}
		out = append(out, codeRows(c, []string{l}, p)...)
	}
	return out
}

// codeRows lays out lines as component's code does (ctx.go:426), which is not
// exported and which a section's rows, a BulletList, cannot hold: from
// theme.LabelCol, breaking where the content width ends at the last space or
// else inside a word, each further piece opening with Glyphs.Wrap. A stand-in
// for unit (c): a section that holds code is a new block in pkg/ui, a deviation
// to log.
func codeRows(c component.Ctx, lines []string, p paint) []string {
	w := contentWidth(c.Env) - theme.LabelCol
	var out []string
	for _, s := range lines {
		s = strings.TrimRight(s, " ")
		if s == "" {
			out = append(out, "")
			continue
		}
		for lead := ""; ; {
			head := ansi.Truncate(s, max(0, w-ansi.StringWidth(lead)), "")
			rest, ok := strings.CutPrefix(s, head)
			if rest == "" || head == "" || !ok {
				out = append(out, draw(pad(theme.LabelCol), piece{lead + s, p}))
				break
			}
			if rest[0] != ' ' {
				if i := strings.LastIndexByte(head, ' '); i > 0 && strings.TrimLeft(head[:i], " ") != "" {
					head = head[:i]
				}
			}
			if head = strings.TrimRight(head, " "); head == "" {
				s = strings.TrimLeft(s, " ")
				continue
			}
			out = append(out, draw(pad(theme.LabelCol), piece{lead + head, p}))
			s, lead = s[len(head):], c.Theme.Glyphs.Wrap
		}
	}
	return out
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
		rows = append(rows, component.KeyValue{Key: rowSize, Value: sizeWords(r.size)})
	}
	if r.elsewhere != "" {
		rows = append(rows, component.KeyValue{Key: fmt.Sprintf(logFileOff, r.elsewhere), State: component.ValueWarn})
	}
	return screen.Section{Header: component.SectionHeader{Title: secLog}, Rows: component.BulletList{Items: rows}}
}

// tailHeader is the header over the last n lines.
func (r logReading) tailHeader(n int) component.SectionHeader {
	title := fmt.Sprintf(lastOf, n, len(r.lines))
	switch {
	case n == len(r.lines) && n == 1:
		title = oneLine
	case n == len(r.lines):
		title = fmt.Sprintf(allLines, n)
	}
	return component.SectionHeader{Title: title}
}

// tailRows are the last n lines, drawn.
func (r logReading) tailRows(c component.Ctx, n int) string {
	return strings.Join(logRows(c, r.lines[len(r.lines)-n:]), "\n")
}

// fitTail is as many of the last lines as room rows hold, a wrapped line
// counting each of its rows, measured as they draw: at least one.
func (r logReading) fitTail(c component.Ctx, room int) int {
	n := 0
	for n < len(r.lines) && len(logRows(c, r.lines[len(r.lines)-n-1:])) <= room {
		n++
	}
	return max(1, n)
}

// linesMark is the row that holds the lines' place in the sub-menu layout,
// whose sections hold a BulletList alone, until logScreen draws them there.
const linesMark = "LOG-LINES"

// logScreen is the sub-menu with the log's lines in the place of a row: the
// layout draws everything else.
type logScreen struct {
	screen.SubMenu
	mark, lines string
}

// swap puts the lines in the mark's place.
func (v logScreen) swap(s string) string {
	if v.mark == "" {
		return s
	}
	return strings.Replace(s, v.mark, v.lines, 1)
}

// Render renders the screen, as SubMenu's Render does.
func (v logScreen) Render(c component.Ctx) string { return v.swap(v.SubMenu.Render(c)) }

// Screen returns what nav.Run draws, as SubMenu's Screen does.
func (v logScreen) Screen(c component.Ctx, m nav.Menu) nav.Screen {
	s := v.SubMenu.Screen(c, m)
	s.Above = v.swap(s.Above)
	return s
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
func (r logReading) subMenu(c component.Ctx) logScreen {
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
	v := logScreen{SubMenu: vm}
	if r.state == logLines {
		// The section's header and the gap each side of it (theme.SectionGap),
		// beside the rest of the screen, which is what is left of the height.
		room := c.Env.Height - (strings.Count(vm.Render(c), "\n") + 1) - 2*theme.SectionGap - 1
		n := r.fitTail(c, room)
		mark := component.BulletList{Items: []component.KeyValue{{Key: linesMark, State: component.ValueNone}}}
		v.Sections = append(vm.Sections, screen.Section{Header: r.tailHeader(n), Rows: mark})
		v.mark, v.lines = mark.Render(c), r.tailRows(c, n)
	}
	return v
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
// it. Nothing is written or run; the plan-only sequence draws the same screen.
func (s *session) logsScreen(ctx context.Context, lh logHost, h *fakeHost) error {
	run := &logsRun{s: s, lh: lh, h: h}
	if err := s.writeNote(logsPrefix+"/"+lh.name, lh.note); err != nil {
		return err
	}
	return s.p.Run(func() nav.Screen { return run.draw(ctx) })
}

// pagerNote is the gallery's note where hardn would run a pager: no golden
// can run one, and the fake models none.
func pagerNote(c component.Ctx, command string) string {
	return component.Callout{Kind: component.MessageInfo, Body: []string{
		"The gallery runs no pager. hardn would run " + command + ", then draw this screen again.",
	}}.Render(c)
}

// draw reads the host and returns the screen, its items bound.
func (run *logsRun) draw(ctx context.Context) nav.Screen {
	s := run.s
	r := readLog(ctx, run.h.system())
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
const logsNote = "Play mode, mocks: the Logs screen proposed for slice 1, inferred from v0.3.2 and origin/main, read from a simulated host by the gallery's stand-ins, " +
	"unit (b)'s to build. It only reads: no plan, no y/N, no journal. No pager runs: an item says what it would run. " +
	"Nothing on this machine changes: every command and file is the fake's."

// readLogHost reads the host named name, fresh, as the screen does.
func readLogHost(ctx context.Context, name string) (logReading, error) {
	lh, ok := findLogHost(name)
	if !ok {
		return logReading{}, fmt.Errorf("no logs host %q", name)
	}
	return readLog(ctx, lh.host().system()), nil
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
	host  func() *fakeHost
}

// pagerEnvs are the environments the pager table shows: a root login, where
// PAGER is root's; sudo and doas, which drop it [I: sudo's env_reset dropped
// HARDN_CONFIG on the VMs, vmtest.md §4.9; PAGER was not run; sudo-rs took
// env_keep from a drop-in, vmtest.md:315, so a rule may keep it there too;
// doas keeps only what setenv or keepenv says, sudoers.md:102]; sudo where a
// rule or -E keeps it; and a host with no less. PAGER is not read in any.
func pagerEnvs() []pagerEnv {
	return []pagerEnv{
		{"root login, PAGER=most", logOn("")},
		{"sudo, PAGER dropped", logOn("")},
		{"sudo-rs, PAGER dropped [I]", logOn("")},
		{"doas, BusyBox's less", alpineLog},
		{"doas, the less package [I]", alpineLessPackage},
		{"sudo, PAGER=most kept", logOn("")},
		{"no less, no PAGER", noPagerLog},
	}
}

// pagerTable draws what would run in each environment.
func pagerTable(ctx context.Context, c component.Ctx) (string, error) {
	var rows []component.KeyValue
	for _, e := range pagerEnvs() {
		p := pagerFor(ctx, e.host().system())
		v := p.command(hardnLog)
		switch {
		case p.busybox && p.name == "":
			v = "None: BusyBox's has no secure mode"
		case p.name == "":
			v = "None: the last lines alone"
		}
		rows = append(rows, component.KeyValue{Key: e.label, Value: v})
	}
	return apart(
		component.SectionHeader{Title: "pager-less, adopted"}.Render(c),
		component.BulletList{Items: rows}.Render(c),
	), nil
}

// logsMocks are the listing's mocks, in order: the screen on each host,
// Open, the pager table, the non-interactive forms, the parser and
// origin/main's in their own words.
func logsMocks() []listMock {
	var mocks []listMock
	for _, lh := range logHosts() {
		mocks = append(mocks, listMock{name: "screen-" + lh.name, note: "The Logs screen. " + lh.note,
			draw: onLogHost(lh.name, func(r logReading, c component.Ctx) string { return r.subMenu(c).Render(c) })})
	}
	return append(mocks,
		listMock{name: "open", note: "Item 1, Open. A golden cannot run a pager: this is what the screen says and the command hardn would run, the screen drawn again when it exits.",
			draw: onLogHost("long", func(r logReading, c component.Ctx) string {
				return apart(r.subMenu(c).Render(c), pagerNote(c, r.pager.command(r.path)), r.subMenu(c).Render(c))
			})},
		listMock{name: "pager-commands", note: "What would run, by the environment: PAGER is not read, kept or dropped (decision 7 (a)). Under sudo, sudo-rs and doas it is dropped [I; not run for PAGER]. LESSSECURE=1 turned off !, |, v, E and :e in GNU less 668 on the VMs, as a user, not as root (vm-runs/s1/04-less2-*). On BusyBox's less it changed nothing, s and E still prompting; that they write and open any path as root is read (miscutils/less.c, master): no path was typed at s, and E tried only a name that does not exist. Alpine's less package is [I, not run]. The gallery's table: nothing runs.",
			draw: pagerTable},
		listMock{name: "cli-terminal-tail", note: "hardn -p to a terminal: the file and as many of the last lines as the terminal holds, no menu and no prompt, for a shell to carry on under.",
			draw: onLogHost("long", func(r logReading, c component.Ctx) string {
				n := r.fitTail(c, c.Env.Height-cliChrome)
				return apart(component.TitleBar{Title: logsTitle}.Render(c), r.fileSection().Render(c), r.tailHeader(n).Render(c), r.tailRows(c, n))
			})},
		listMock{name: "cli-pipe", wide: term80, note: "hardn -p not to a terminal: every line, as written, in no style, no heading, so that a tool reads the log alone: this needs unit (d) to run -p ahead of the config load, or with a silent console (decision 10). origin/main's opens with the config line, the clear sequence and the rest (origin-p). Exit 0.",
			draw: onLogHost("lines", func(r logReading, _ component.Ctx) string { return strings.Join(r.lines, "\n") })},
		listMock{name: "cli-pipe-control", wide: term80, note: "The same on the control host: safe off a terminal too, since a pipe may end at one. Who wants the bytes has cat /var/log/hardn.log.",
			draw: onLogHost("control", func(r logReading, _ component.Ctx) string { return strings.Join(r.lines, "\n") })},
		listMock{name: "cli-missing", note: "hardn -p with no log: this on stderr, nothing on stdout, exit 0: a read that found nothing. origin/main logs [ERROR] Failed to read log file, into the log too, and carries on, exit 0 by reading (logging.go:139-143; menu/logs_print_flag_missing.golden).",
			draw: onLogHost("missing", func(r logReading, _ component.Ctx) string { return fmt.Sprintf(cliNoLog, r.path) })},
		listMock{name: "cli-unreadable", note: "hardn -p with a log it cannot read: this on stderr, nothing on stdout, exit 1.",
			draw: onLogHost("unreadable", func(r logReading, _ component.Ctx) string { return fmt.Sprintf(cliCantRead, r.path, r.reason) })},
		listMock{name: "cli-unreadable-not-root", note: "The same as a user other than root, under decision 9 (a), where the log is not 0644: 0644 on the three VMs (vm-runs/s1/01-tools.log), it is what hardn asks, less the umask of the start that made it (logging.go:38; a tighter umask [I, not run]). Exit 1.",
			draw: func(_ context.Context, _ component.Ctx) (string, error) {
				return fmt.Sprintf(cliCantRead, hardnLog, "permission denied") + "\n" + fmt.Sprintf(cliRunWith, "sudo"), nil
			}},
		listMock{name: "parser", note: "The slice's broken parser. origin/main's GetLogs on a real line (menu/logs_parser.golden; file_logs_repository.go:51-57; nothing calls it but its wrappers and tests), and the stand-in's fix. The screen needs a level only to colour WARNING and ERROR.",
			draw: func(_ context.Context, c component.Ctx) (string, error) { return parserDrawing(c), nil }},
		listMock{name: "origin", own: true, wide: term80, note: "origin/main's Logs screen, its words as they are (logs_menu.go:33-62, file_logs_repository.go:95-96; menu/logs_open.golden), on the design system: the whole file, 4 lines here, raw.",
			draw: onLogHost("lines", func(r logReading, c component.Ctx) string {
				return apart(component.TitleBar{Title: "View Logs"}.Render(c),
					component.BulletList{Items: []component.KeyValue{{Key: "Log file", Value: r.path}}}.Render(c),
					component.SectionHeader{Title: "Log Contents:"}.Render(c),
					"# Contents of "+r.path+":", strings.Join(r.lines, "\n"),
					component.Message{Text: "Press Enter to return to the main menu..."}.Render(c))
			})},
		listMock{name: "origin-p", own: true, wide: term80, note: "origin/main's hardn -p, to a terminal or not, read, not run: the config line (config.go:246, :252 or :279), the clear sequence, here as the four characters \\x1b, and the ~~~ line even in a pipe (osdetect.go:50; utils.go:24-37), the OS line (osdetect.go:93-95), then what menu/logs_print_flag.golden holds of PrintLogs alone (logging.go:145-146). Colour left out. The config and OS lines also go into the log (logging.go:99-100), so each -p grows it by them. It needs root (cmd/hardn/main.go:148), though the file is 0644.",
			draw: onLogHost("lines", func(r logReading, _ component.Ctx) string {
				return strings.Join(append([]string{"[INFO] Using configuration from: /etc/hardn/hardn.yml",
					`\x1b[H\x1b[2J~~~ (a line of them)`, "", "[SUCCESS] debian trixie detected", "", "# Contents of " + r.path + ":", ""}, r.lines...), "\n")
			})},
		listMock{name: "origin-elsewhere", own: true, note: "origin/main's Logs screen with hardn.yml's logFile set where nothing writes: it reads that path, so the log hardn does write is not shown (file_logs_repository.go:81-84; P-18; menu/logs_other_path.golden).",
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
		strings.Join(logRows(c, []string{line}), "\n"),
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
var logsListing = listing{prefix: logsPrefix, usage: logsUsage, label: logsLabel, mocks: logsMocks}
