package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	logsscreen "github.com/abbott/hardn/pkg/screens/logs"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
)

// The Logs screen on the simulated hosts (refactor plan slice 1, unit (c),
// next/s1-screen): hardn's Logs screen itself (pkg/screens/logs), drawn from what
// logview reads of each host, on the hosts the mocks were drawn for, which it
// replaces (docs/engine/screen-system-details.md). "play logs" is the menu of the
// hosts and "play logs/<host>" the screen on one; the main menu's 10 leads to it on
// a log of lines, under plan only too (-sequence dry-run).
//
// "uigallery logs" lists hardn -p's forms, each through the real Print: its stdout
// where it writes one, and its stderr's words. The listing's other mocks were retired
// with the screen: git keeps them (logs.go, next/s1-parity), and
// gallery-system-details.md records what each showed.

// logsPrefix names the forms and hosts: "logs <mock>" in the listing, "logs" and
// "logs/<host>" in play.
const logsPrefix = "logs"

// logsLabel is the source label every form in the listing carries: no frame; the
// plan's "Logs from v0.3.2 + origin/main".
const logsLabel = "Inferred from v0.3.2 and origin/main."

// logsMainHost is the host the main menu's 10 leads to.
const logsMainHost = logsPrefix + "/lines"

// logsLinks lead from a menu mock's item to the Logs screen on a host, by
// the mock's name and the item's key, beside links: the main menu's 10,
// which said the gallery had no screen for it until unit (a).
var logsLinks = map[string]string{mainMenuMock + "/10": logsMainHost}

// hardnLog is the file hardn writes, whatever hardn.yml's logFile says
// (cmd/hardn/main.go:57).
const hardnLog = logview.File

// galleryTerm is the TERM play gives the screen: hardn gives its own (cmd/hardn/readers.go).
const galleryTerm = "xterm-256color"

// logHost is one simulated host the Logs screen runs on.
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
// on their VMs, its first line as they print it (vm-runs/s1/01-tools.log:22, :88).
func withLess(h *fakeHost) *fakeHost {
	h.f.AddFile("/usr/bin/less", []byte("ELF"), 0o755, 0, 0)
	h.f.Respond(hostfake.Response{Stdout: "less 668 (GNU regular expressions)\n"}, "less", "--version")
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
			note: hostFQDN + " runs Alpine 3.23, whose less is a link to BusyBox (vm-runs/s1/01-tools.log). LESSSECURE=1 changes nothing there: s still prompts Log file, E and :e Examine (vm-runs/s1/04-less2-alpine323-vm.log). No path was typed at s, so its write did not run; E did try a name that does not exist. That s writes and E opens any path as root is read (miscutils/less.c, master). It refuses less --version, so it has no Open item (decision 7 (a)). With Alpine's less package there is one [I, not run]."},
		{name: "no-pager", label: "No pager", about: "No less installed", host: noPagerLog,
			note: hostFQDN + " runs Debian 13 with no less: no Open item, the last lines alone."},
	}
}

// noPagerLog is Debian 13 with the sample log and no less.
func noPagerLog() *fakeHost {
	h := simulate(debian13Platform)
	h.f.AddFile(hardnLog, []byte(logOf(logSample)), 0o644, 0, 0)
	h.f.Respond(hostfake.Response{Err: fmt.Errorf("host: less: %w", host.ErrNotFound)}, "less", "--version") // as Run says of a host with no less
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

// alpineLog is Alpine 3.23 with BusyBox's less: a link to /bin/busybox, which
// refuses --version (vm-runs/s1/01-tools.log:151-153), exit 1
// (15-readers-alpine323-vm-*.log).
func alpineLog() *fakeHost {
	h := simulate(alpine323Platform)
	h.f.AddFile("/bin/busybox", []byte("ELF"), 0o755, 0, 0)
	h.f.AddSymlink("/usr/bin/less", "/bin/busybox")
	h.f.Respond(hostfake.Response{Stderr: "less: unrecognized option: version\n", ExitCode: 1}, "less", "--version")
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

// logsMoving is a Logs host as play reads it. Each read of the log after the first,
// but the one after a pager, finds another hardn has written a line, so that a Refresh
// draws a change: the screen has no Refresh of its own to hook, and what moves is the
// gallery's fake's, put there unrecorded. The gallery runs no pager: RunInteractive
// says what hardn would run.
type logsMoving struct {
	engineSys
	s      *session
	scans  int
	opened bool
}

// ScanLines is the host's, a line longer from the second read on, as grow has it.
func (m *logsMoving) ScanLines(ctx context.Context, name string, yield func(host.Line) bool) (fs.FileInfo, error) {
	if m.scans++; m.scans > 1 && !m.opened {
		m.grow()
	}
	m.opened = false
	return m.engineSys.ScanLines(ctx, name, yield)
}

// RunInteractive runs nothing: the gallery's note says the command, as no golden can
// run a pager and the fake models none.
func (m *logsMoving) RunInteractive(_ context.Context, env []string, name string, args ...string) error {
	m.opened = true
	_, err := io.WriteString(m.s.p.Out, strings.Repeat("\n", theme.SectionGap)+pagerNote(m.s.p.Ctx, strings.Join(slices.Concat(env, []string{name}, args), " "))+"\n")
	return err
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

// logsScreen runs hardn's Logs screen on h, lh's host, until the user goes back from
// it: its note above it; hardn.yml's logFile as the gallery's status reads it
// (statusConfig), hardn's caller's in hardn; TERM the gallery's. Nothing is written or
// run; the plan-only sequence draws the same screen, as Options has no mode.
func (s *session) logsScreen(ctx context.Context, lh logHost, h *fakeHost) error {
	if err := s.writeNote(logsPrefix+"/"+lh.name, lh.note); err != nil {
		return err
	}
	logFile := ""
	if cfg, err := statusConfig(ctx, h.system()); err == nil {
		logFile = cfg.LogFile
	}
	return logsscreen.Run(ctx, logsscreen.Options{Prompter: s.p, System: &logsMoving{engineSys: h.system(), s: s}, LogFile: logFile, Term: galleryTerm})
}

// pagerNote is the gallery's note where hardn would run a pager: no golden
// can run one, and the fake models none.
func pagerNote(c component.Ctx, command string) string {
	return component.Callout{Kind: component.MessageInfo, Body: []string{
		"The gallery runs no pager. hardn would run " + command + ", then draw this screen again.",
	}}.Render(c)
}

// grow moves the simulated host on, as if another hardn wrote a line before
// a Refresh, so that the screen drawn again shows a change. A log that is
// not a file stays as it is.
func (m *logsMoving) grow() {
	if e, ok := m.System.Entry(hardnLog); ok && e.Data != nil {
		m.System.AddFile(hardnLog, append(e.Data, []byte("2026/10/20 12:01:00 INFO: run 2: exit 0\n")...), 0o644, 0, 0)
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

// logsNote is play's note for the Logs screen.
const logsNote = "Play mode: hardn's Logs screen (pkg/screens/logs), inferred from v0.3.2 and origin/main, read from a simulated host by the real reader (pkg/engine/logview). " +
	"It only reads: no plan, no y/N, no journal. No pager runs: Open says what hardn would run. " +
	"Nothing on this machine changes: every command and file is the fake's."

// asUserLog is a host whose log refuses to be read, as to a user other than root
// where it is tighter than 0644.
type asUserLog struct{ engineSys }

// ScanLines refuses, as an open of the log does.
func (s asUserLog) ScanLines(_ context.Context, name string, _ func(host.Line) bool) (fs.FileInfo, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

// printLogs is what hardn -p writes on sys in env, on stdout, and what Print found:
// the real Print, whose caller's the stderr and exit status are.
func printLogs(ctx context.Context, sys host.System, env term.Env, logFile string) (string, logsscreen.Printed, error) {
	var out strings.Builder
	printed, err := logsscreen.Print(ctx, logsscreen.PrintOptions{Out: &out, Env: env, System: sys, LogFile: logFile})
	return strings.TrimSuffix(out.String(), "\n"), printed, err
}

// onLogPrint returns a drawing of what hardn -p writes on stdout on the host named
// name, to a terminal of c's size or off one.
func onLogPrint(name string, terminal bool) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		lh, ok := findLogHost(name)
		if !ok {
			return "", fmt.Errorf("no logs host %q", name)
		}
		out, _, err := printLogs(ctx, lh.host().system(), printEnv(c, terminal), "")
		return out, err
	}
}

// onLogStderr returns a drawing of what hardn -p writes on stderr on sys, tool the
// caller's word for what to run it with: Printed.Stderr's lines, off a terminal.
func onLogStderr(sys func() host.System, tool string) func(context.Context, component.Ctx) (string, error) {
	return func(ctx context.Context, c component.Ctx) (string, error) {
		_, printed, err := printLogs(ctx, sys(), printEnv(c, false), "")
		return strings.Join(printed.Stderr(tool), "\n"), err
	}
}

// logsMocks are the listing's forms, in order: hardn -p to a terminal and off one,
// then its stderr's words, each through the real Print.
func logsMocks() []listMock {
	onHost := func(name string) func() host.System {
		return func() host.System {
			lh, _ := findLogHost(name)
			return lh.host().system()
		}
	}
	return []listMock{
		{name: "cli-terminal-tail", note: "hardn -p to a terminal: the file and as many of the last lines as the terminal holds, no menu and no prompt, for a shell to carry on under.",
			draw: onLogPrint("long", true)},
		{name: "cli-pipe", wide: term80, note: "hardn -p not to a terminal: every line, as written, in no style, no heading, so that a tool reads the log alone: -p runs ahead of the config load (decision 10). Exit 0.",
			draw: onLogPrint("lines", false)},
		{name: "cli-pipe-control", wide: term80, note: "The same on the control host: safe off a terminal too, since a pipe may end at one. Who wants the bytes has cat /var/log/hardn.log.",
			draw: onLogPrint("control", false)},
		{name: "cli-missing", note: "hardn -p with no log: this on stderr, nothing on stdout, exit 0: a read that found nothing. origin/main logs [ERROR] Failed to read log file, into the log too, and carries on, exit 0 by reading (logging.go:139-143; menu/logs_print_flag_missing.golden).",
			draw: onLogStderr(onHost("missing"), "")},
		{name: "cli-unreadable", note: "hardn -p with a log it cannot read: this on stderr, nothing on stdout, exit 1.",
			draw: onLogStderr(onHost("unreadable"), "")},
		{name: "cli-unreadable-not-root", note: "The same as a user other than root, under decision 9 (a), where the log is not 0644: 0644 on the three VMs (vm-runs/s1/01-tools.log), it is what hardn asks, less the umask of the start that made it (logging.go:38; a tighter umask [I, not run]). Exit 1.",
			draw: onLogStderr(func() host.System { return asUserLog{logOn("")().system()} }, "sudo")},
	}
}

// term80 is the width the raw forms need: their lines do not wrap.
const term80 = 80

// logsUsage is the listing's usage line.
const logsUsage = "usage: go run ./internal/tools/uigallery logs [flags] [form | all]..."

// logsListing is the Logs listing.
var logsListing = listing{prefix: logsPrefix, usage: logsUsage, label: logsLabel, mocks: logsMocks}
