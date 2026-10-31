package characterization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/adapter/secondary"
	"github.com/abbott/hardn/pkg/application"
	"github.com/abbott/hardn/pkg/config"
	"github.com/abbott/hardn/pkg/domain/service"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/interfaces"
	"github.com/abbott/hardn/pkg/logging"
	"github.com/abbott/hardn/pkg/menu"
)

// The Logs screen (main menu 10) and -p/--print-logs, refactor plan slice
// 1's evidence: in a child process of this test binary, on the real file
// system under a temp dir that stands for /, as TestLegacyMenuBackup and
// TestLegacyMenuEnvironment ran theirs. The screen is built as
// main_menu.go:715 builds it, its LogsManager as service_factory.go:217-229
// builds it, on hardn's configured logFile; which screen key 10 opens is
// pkg/menu's main_menu_layout_test.go's. -p is logging.PrintLogs, the call
// cmd/hardn/main.go:417 makes with cfg.LogFile.

const (
	logsChild = "HARDN_CHARACTERIZATION_LOGS"
	logsRoot  = "HARDN_CHARACTERIZATION_LOGS_ROOT"
	logsParse = "HARDN_CHARACTERIZATION_LOGS_PARSER"
)

// writtenPath is where hardn writes, under the temp root: main() always
// passes "/var/log/hardn.log" (cmd/hardn/main.go:57).
const writtenPath = "/var/log/hardn.log"

// logCase is one run of the Logs screen or of -p.
type logCase struct {
	name, screen, script string
	// logFile is cfg.LogFile, under the temp root; writtenPath unless the
	// case says.
	logFile string
	// seeds are files under the temp root before the run.
	seeds map[string]string
	// unreadable makes the seed at logFile mode 0000.
	unreadable bool
	// startup opens the log as main() does (main.go:57) and writes a line
	// through pkg/logging, before the screen or -p runs.
	startup bool
	// flag runs logging.PrintLogs in place of the screen.
	flag bool
	// large holds the log's line count: the golden holds its first and last
	// lines and counts the rest.
	large          int
	notes, defects []string
}

// logLines are the lines of a log as pkg/logging writes them (TestLegacyLogsParser
// records that it does), through LogInfo and LogError, and the run log's,
// through revertFileLog: those are the trunk's, not origin/main's (trunkRun).
const logLines = "2026/10/29 08:12:44 INFO: Configuring DNS\n" +
	"2026/10/29 08:12:44 INFO: run 1: $ resolvectl status\n" +
	"2026/10/29 08:12:44 INFO: run 1: | Global\n" +
	"2026/10/29 08:12:44 INFO: run 1: exit 0\n" +
	"2026/10/29 08:12:45 ERROR: Failed to restart systemd-resolved\n"

func logCases() []logCase {
	const (
		whole     = "The whole file is printed at once, no pager, no tail and no count (file_logs_repository.go:89-96; logging.go:139-146): only the end stays on a terminal's screen, and what scrolled off is whatever the terminal keeps."
		extraLine = "Println adds a blank line to the file's last newline (file_logs_repository.go:96; logging.go:146)."
		reads     = "Stat, then read, of the whole file into memory (file_logs_repository.go:83-92)."
		twoHeads  = "The path prints twice with different words, \"Log file:\" and \"# Contents of\" (logs_menu.go:48-49; file_logs_repository.go:95), and two rules of heading stand above the lines (logs_menu.go:52-53)."
		fixedPath = "main() always opens /var/log/hardn.log (cmd/hardn/main.go:57); the screen and -p read cfg.LogFile (service_factory.go:219-222; main.go:417), so with logFile set otherwise the two part."
		created   = "At the default path start-up has already created the file, as root (cmd/hardn/main.go:57; logging.go:38), so missing needs logFile elsewhere (logs_other_path) or the open at start-up failing (logging.go:39-42): read."
		noExit    = "logging.PrintLogs returns nothing (logging.go:138-143), and the caller goes on (cmd/hardn/main.go:417), so -p on a log it cannot read exits 0 (read: cmd/hardn is not run here). V25's class, CLI exits 0 after failure; no V-number names -p."
		trunkRun  = "The \"run <n>:\" lines are the trunk's (next/v0.4), not origin/main's: pkg/host/runlog.go came with 6a04b467, LogFileOnly and revertFileLog with e264b8bb (next/p3-confirm-cleanup), none of them on origin/main. What prints them is origin/main's, on whatever the file holds."
		wholeP    = "This golden holds logging.PrintLogs alone, not all of -p. On the root command's path (read, not run) stdout first gets the config line, \"[INFO] Using configuration from: <path>\" or the no-config lines (config.go:246, :252, :279), then the clear sequence and the ~~~ line even in a pipe (osdetect.go:50; utils.go:24-37), then \"[SUCCESS] <os> detected\" (osdetect.go:93-95), and those LogInfo and LogSuccess lines are also appended to the log on each -p (logging.go:99-100)."
		emptyOnly = "Start-up has already logged by the time the screen can read the file (cmd/hardn/main.go:57, :155, :185; config.go:246; osdetect.go:93-95; read), so an empty log needs hardn's open to have failed or the file truncated while hardn runs."
		rootOnly  = "-p is on the root command's Run, after the root check, the config load and DetectOS (cmd/hardn/main.go:148-152, :155, :184-188; read): a file of mode 0644 hardn refuses to print to another user, and a missing hardn.yml gets its offer first. TestDryRunAloneIsNotRefused holds -n -p reaching the root check."
	)
	withLog := map[string]string{writtenPath: logLines}
	return []logCase{
		{name: "logs_open", screen: "Logs (main menu 10), a log with lines, Enter", script: "\n", seeds: withLog,
			defects: []string{whole},
			notes:   []string{trunkRun, twoHeads, extraLine, reads, "One prompt: Enter returns to the main menu (logs_menu.go:62-63). No refresh, search or follow."}},
		{name: "logs_large", screen: "Logs (main menu 10), a large log, Enter", script: "\n", large: 20000,
			defects: []string{whole}, notes: []string{trunkRun}},
		{name: "logs_missing", screen: "Logs (main menu 10), no log at logFile, Enter", script: "\n",
			notes: []string{created, "The words are Stat's: the path, shown above as Log file, is named twice more in the one line (logs_menu.go:58-59; file_logs_repository.go:85)."}},
		{name: "logs_unreadable", screen: "Logs (main menu 10), a log the user cannot read, Enter", script: "\n", seeds: withLog, unreadable: true,
			notes: []string{"Mode 0000, as a user other than root: hardn is root by here (cmd/hardn/main.go:148-152), so this needs a read that fails as root, such as an NFS root squash [I, not run]."}},
		{name: "logs_empty", screen: "Logs (main menu 10), an empty log, Enter", script: "\n", seeds: map[string]string{writtenPath: ""},
			notes: []string{"An empty log prints its heading and nothing else: no words say it is empty.", emptyOnly}},
		{name: "logs_other_path", screen: "Logs (main menu 10), logFile another path with no file there, hardn writing where it writes, Enter", script: "\n",
			logFile: "/srv/log/custom.log", startup: true,
			defects: []string{fixedPath + " The line just written is not shown; the screen says the log is not there."}},
		{name: "logs_other_path_stale", screen: "Logs (main menu 10), logFile another path holding an old log, hardn writing where it writes, Enter", script: "\n",
			logFile: "/srv/log/custom.log", startup: true, seeds: map[string]string{"/srv/log/custom.log": "2025/01/02 03:04:05 INFO: An old line\n"},
			defects: []string{fixedPath + " The screen shows the other file's lines as hardn's, nothing saying the two part."}},
		{name: "logs_escape", screen: "Logs (main menu 10), a line holding a terminal escape, Enter", script: "\n",
			seeds:   map[string]string{writtenPath: "2026/10/29 08:12:44 INFO: run 1: | \x1b]0;a title a command set\x07\x1b[31mred\n"},
			defects: []string{"The file's bytes go to the terminal as they are (file_logs_repository.go:96; logging.go:146): origin/main's defect, on any bytes the file holds. No V-number holds this."},
			notes:   []string{trunkRun, "Where such bytes come from is the trunk's: the run log holds commands' output (runlog.go:58-62), which pkg/logging does not clean (logging.go:131-135), so on the trunk a command's output can move hardn's terminal when the log is read, here its title and colour; as root, on an admin's terminal. On origin/main a narrow search found no line logging a command's output ([unverified] as complete)."}},
		{name: "logs_print_flag", screen: "logging.PrintLogs, -p/--print-logs, a log with lines", flag: true, seeds: withLog,
			defects: []string{whole},
			notes:   []string{wholeP, trunkRun, rootOnly, extraLine, "A second copy of this function, pkg/utils.PrintLogs (utils.go:117), and a third in FileLogsRepository.PrintLogs (file_logs_repository.go:81), which the screen uses; see logs_parser.golden for the callers."}},
		{name: "logs_print_flag_missing", screen: "logging.PrintLogs, -p/--print-logs, no log at logFile, start-up's log open", flag: true,
			logFile: "/srv/log/custom.log", startup: true,
			defects: []string{noExit, "Failing to read the log writes an ERROR line to the log (logging.go:141; :78)."},
			notes:   []string{"With -p the error is the logger's, in different words from the screen's (logs_missing), and no colour here, where stdout is a pipe and pkg/logging decides; cmd/hardn sets colour on unless --no-color (main.go:108-114; read, not run)."}},
	}
}

// logStamp is log.LstdFlags' date and time.
var logStamp = regexp.MustCompile(`\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}`)

// TestLegacyMenuLogs records origin/main's Logs screen and -p, each case in
// a child process of this test binary whose stdin is the script, not a
// terminal, whose working directory and HOME are temp dirs, and whose PATH
// holds a stand-in for less, $PAGER naming it, to show it is not run. Each
// golden holds the screen as printed (ESC as \x1b), the repository's calls,
// and what hardn wrote to its log, the date and time masked. Reads run on
// the real file system, under a temp dir that stands for /.
func TestLegacyMenuLogs(t *testing.T) {
	if name := os.Getenv(logsChild); name != "" {
		runLogsChild(name, os.Getenv(logsRoot))
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range logCases() {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unreadable && os.Geteuid() == 0 {
				t.Skip("root reads a file of mode 0000")
			}
			dir := t.TempDir()
			root, bin, cwd, pagerLog := filepath.Join(dir, "root"), filepath.Join(dir, "bin"), filepath.Join(dir, "cwd"), filepath.Join(dir, "pager.log")
			for _, d := range []string{root, bin, cwd} {
				if err := os.Mkdir(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(exe, filepath.Join(bin, "less")); err != nil {
				t.Fatal(err)
			}
			logFile := tc.logFile
			if logFile == "" {
				logFile = writtenPath
			}
			seeds := tc.seeds
			var large strings.Builder
			if tc.large > 0 {
				for i := 1; i <= tc.large; i++ {
					fmt.Fprintf(&large, "2026/10/29 08:12:44 INFO: run %d: $ true\n", i)
				}
				seeds = map[string]string{writtenPath: large.String()}
			}
			for name, content := range seeds {
				p := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tc.unreadable {
				p := filepath.Join(root, filepath.FromSlash(logFile))
				if err := os.Chmod(p, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(p, 0o644) })
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyMenuLogs$")
			var env []string
			for _, kv := range os.Environ() {
				k, _, _ := strings.Cut(kv, "=")
				switch k {
				case "PATH", "HOME", "PAGER", "LESS":
				default:
					env = append(env, kv)
				}
			}
			cmd.Env = append(env, logsChild+"="+tc.name, logsRoot+"="+root, toolLog+"="+pagerLog, toolAnswers+"={}",
				"PATH="+bin, "PAGER=less", "HOME="+filepath.Join(dir, "home"), "NO_COLOR=", "RUNEWIDTH_EASTASIAN=0")
			cmd.Dir = cwd
			cmd.Stdin = strings.NewReader(tc.script)
			out, err := cmd.Output()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			screen, trailer, _ := strings.Cut(string(out), childTrailer)
			paged, err := os.ReadFile(pagerLog)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				t.Fatal(err)
			}
			if len(paged) != 0 {
				t.Fatalf("the pager ran: %s", paged)
			}
			if tc.large > 0 {
				screen = elideLog(t, screen, large.String(), tc.large)
			}

			var b strings.Builder
			b.WriteString(baselineHeader + "\n")
			b.WriteString("# Generated by TestLegacyMenuLogs (pkg/characterization); regenerate with -update. Do not edit.\n")
			if tc.flag {
				fmt.Fprintf(&b, "# Operation: %s (pkg/logging), run in a child process, stdout a pipe\n", tc.screen)
			} else {
				fmt.Fprintf(&b, "# Screen: %s (pkg/menu), run in a child process\n", tc.screen)
				fmt.Fprintf(&b, "# Script: stdin %q, not a terminal, so nothing typed is echoed\n", tc.script)
			}
			fmt.Fprintf(&b, "# logFile: /tmp-root%s; hardn writes /tmp-root%s (/tmp-root stands for /)\n", logFile, writtenPath)
			b.WriteString("# $PAGER: less, on PATH, a stand-in that records being run; not run.\n")
			if tc.large > 0 {
				fmt.Fprintf(&b, "# Log: %d lines, %d bytes; every line printed, in order, checked by the test; the golden keeps the first three and the last three.\n", tc.large, large.Len())
			}
			for _, d := range tc.defects {
				fmt.Fprintf(&b, "# KNOWN-DEFECT: %s\n", d)
			}
			for _, n := range tc.notes {
				fmt.Fprintf(&b, "# NOTE: %s\n", n)
			}
			b.WriteString("# The screen as printed, ESC as the four characters \\x1b, each line ending in $; <no-newline> marks text with no final newline.\n")
			b.WriteString("# ----\n")
			b.WriteString(strings.ReplaceAll(visible(screen), "\a", `\a`))
			b.WriteString("# ----\n")
			b.WriteString(trailer)
			if tc.startup || tc.flag {
				written, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(writtenPath)))
				switch {
				case errors.Is(err, fs.ErrNotExist):
					b.WriteString("# What hardn wrote to its log: no file\n")
				case err != nil:
					t.Fatal(err)
				default:
					b.WriteString("# What hardn's log holds afterwards, the date and time masked:\n")
					for _, line := range strings.Split(strings.TrimSuffix(string(written), "\n"), "\n") {
						if line != "" {
							b.WriteString("#   " + logStamp.ReplaceAllString(line, "<date> <time>") + "\n")
						}
					}
				}
			}
			got := strings.ReplaceAll(b.String(), root, "/tmp-root")
			for _, d := range []string{dir, root} {
				if resolved, err := filepath.EvalSymlinks(d); err == nil && strings.Contains(got, resolved) {
					t.Fatalf("golden would contain the temp dir %s", resolved)
				}
			}
			assertGolden(t, "menu/"+tc.name+".golden", got)
		})
	}
}

// elideLog checks that screen holds the whole log, in order, and returns it
// with all but the log's first and last three lines replaced by a count.
func elideLog(t *testing.T, screen, log string, lines int) string {
	t.Helper()
	at := strings.Index(screen, log)
	if at < 0 {
		t.Fatalf("the screen does not hold the whole log of %d lines", lines)
	}
	logLines := strings.SplitAfter(strings.TrimSuffix(log, "\n"), "\n")
	var kept strings.Builder
	for _, l := range logLines[:3] {
		kept.WriteString(l)
	}
	fmt.Fprintf(&kept, "<%d lines this golden leaves out>\n", lines-6)
	for _, l := range logLines[lines-3:] {
		kept.WriteString(l)
	}
	if !strings.HasSuffix(kept.String(), "\n") {
		kept.WriteString("\n")
	}
	return screen[:at] + kept.String() + screen[at+len(log):]
}

// runLogsChild runs the Logs screen, or -p, of case name, in the child
// process, under root: hardn's defaults with logFile under root, its
// LogsManager built as infrastructure.ServiceFactory builds it, on the real
// file system, recorded. Then it reports, after childTrailer, what the
// repository asked.
func runLogsChild(name, root string) {
	var tc logCase
	for _, c := range logCases() {
		if c.name == name {
			tc = c
		}
	}
	if tc.name == "" || root == "" {
		fmt.Fprintf(os.Stderr, "no logs case %q in %q\n", name, root)
		os.Exit(2)
	}
	logFile := tc.logFile
	if logFile == "" {
		logFile = writtenPath
	}
	cfg := config.DefaultConfig()
	cfg.Username, cfg.SshPort, cfg.LogFile = "alice", 2230, filepath.Join(root, filepath.FromSlash(logFile))

	if tc.startup {
		logging.InitLogging(filepath.Join(root, filepath.FromSlash(writtenPath)))
		logging.LogFileOnly("Line written at start-up")
	}
	if tc.flag {
		logging.PrintLogs(cfg.LogFile)
		logging.CloseLogging()
		return
	}
	log := &trace{}
	repo := secondary.NewFileLogsRepository(recordingFS{inner: interfaces.OSFileSystem{}, log: log}, cfg.LogFile)
	logs := application.NewLogsManager(service.NewLogsServiceImpl(repo))
	mm := application.NewMenuManager(nil, nil, nil, nil, nil, nil, logs, nil)
	menu.NewLogsMenu(mm, cfg).Show()
	logging.CloseLogging()

	fmt.Print(childTrailer)
	fmt.Printf("# The repository's calls:\n%s", log)
}

// TestLegacyLogsParser records what FileLogsRepository.GetLogs makes of the
// lines hardn really writes. In a child process, the lines are written
// through pkg/logging's own functions and, for the run log, through
// host.RunLog into a writer doing what cmd/hardn's revertFileLog does
// (confirm.go:158-163, in package main: each line, LogFileOnly), then read
// back through GetLogs. The golden holds each written line, then each entry,
// then GetLogs's callers, by go/parser over cmd and pkg, test files aside.
func TestLegacyLogsParser(t *testing.T) {
	if dir := os.Getenv(logsParse); dir != "" {
		runLogsParserChild(dir)
		os.Exit(0)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestLegacyLogsParser$")
	cmd.Env = append(os.Environ(), logsParse+"="+dir, "NO_COLOR=")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}

	var b strings.Builder
	b.WriteString(baselineHeader + "\n")
	b.WriteString("# Generated by TestLegacyLogsParser (pkg/characterization); regenerate with -update. Do not edit.\n")
	b.WriteString("# Operation: FileLogsRepository.GetLogs (pkg/adapter/secondary/file_logs_repository.go:32-71), on lines written by pkg/logging and host.RunLog, run in a child process\n")
	b.WriteString("# Writers: every line in hardn's log comes through pkg/logging's logger, log.LstdFlags, so \"DATE TIME LEVEL: message\" (logging.go:46, :78-133). origin/main itself writes through LogInfo, LogWarning, LogError, LogSuccess and LogInstall. The trunk's alone, not on origin/main: LogFileOnly (e264b8bb), whose lines say INFO whatever they report, and the run log (runlog.go:47-69, 6a04b467), which has no writer of its own: revertFileLog sends each of its lines to LogFileOnly (confirm.go:158-163). The format defect below holds on origin/main's lines and the trunk's alike.\n")
	b.WriteString("# KNOWN-DEFECT: GetLogs splits on the first two spaces, for \"TIME LEVEL: MESSAGE\" (file_logs_repository.go:51-57), a format hardn does not write: Time holds the date, Level the time, and Message the level and the message. No entry has a level.\n")
	b.WriteString("# KNOWN-DEFECT: A line that is not one of the logger's, the second line of a message with a line break, is taken as an entry if it has three words and dropped if it has fewer (:53).\n")
	b.WriteString("# KNOWN-DEFECT: A line over bufio.Scanner's 64 KiB limit ends the read with no error: scanner.Err is not checked (:47-70), so it and everything after it are lost.\n")
	b.WriteString("# NOTE: No live path calls GetLogs: the callers below are the two forwarding methods, and LogsManager.GetLogs has no caller. pkg/domain/service/logs_service_test.go tests it on a mock repository. The screen and -p print the raw file. pkg/utils.PrintLogs (utils.go:117) has no caller either.\n")
	b.WriteString("# NOTE: No V-number holds these; plan slice 1's row names the broken parser.\n")
	b.WriteString("# The date and time are masked. The long line is shortened here; the entries show what survives it.\n")
	b.WriteString("# ----\n")
	entryClock := regexp.MustCompile(`"Level":"\d{2}:\d{2}:\d{2}"`)
	entryDate := regexp.MustCompile(`"Time":"\d{4}/\d{2}/\d{2}"`)
	written := logStamp.ReplaceAllString(strings.ReplaceAll(string(out), dir, "$TMP"), "<date> <time>")
	written = entryClock.ReplaceAllString(written, `"Level":"<time>"`)
	b.WriteString(entryDate.ReplaceAllString(written, `"Time":"<date>"`))
	b.WriteString("# ----\n")
	b.WriteString("# The calls of GetLogs and PrintLogs (file, what is called), by go/parser, test files aside:\n")
	for _, line := range logCallers(t) {
		b.WriteString("#   " + line + "\n")
	}
	got := b.String()
	if strings.Contains(got, dir) {
		t.Fatalf("golden would contain the temp dir %s", dir)
	}
	assertGolden(t, "menu/logs_parser.golden", got)
}

// fileLogWriter does what cmd/hardn's revertFileLog does (confirm.go:158-163).
type fileLogWriter struct{}

func (fileLogWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		logging.LogFileOnly("%s", line)
	}
	return len(p), nil
}

// runLogsParserChild writes the lines and prints them and GetLogs' entries.
func runLogsParserChild(dir string) {
	logging.SetSilentMode(true)
	path := filepath.Join(dir, "hardn.log")
	logging.InitLogging(path)
	logging.LogInfo("Configuring DNS")
	logging.LogWarning("No backup taken")
	logging.LogError("Failed to restart %s", "systemd-resolved")
	logging.LogSuccess("Script completed selected hardening operations.")
	logging.LogInstall("ufw")
	logging.LogFileOnly("hangup during the apply: hardn ends")
	runlog := host.NewRunLog(fileLogWriter{})
	id := runlog.Begin([]string{"ufw", "allow", "2230/tcp"}, -1)
	runlog.End(id, host.Result{Combined: []byte("Rule added\n")}, nil)
	logging.LogInfo("A message\nwith a second line of more words\ntwo words\n\none more")
	logging.CloseLogging()

	report := func(path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("# Written, line by line:")
		for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
			if len(line) > 200 {
				line = line[:40] + fmt.Sprintf("... (a line of %d bytes)", len(line))
			}
			fmt.Printf("%q\n", line)
		}
		repo := secondary.NewFileLogsRepository(interfaces.OSFileSystem{}, path)
		entries, err := repo.GetLogs()
		fmt.Printf("# GetLogs: %d entries, error %v\n", len(entries), err)
		for _, e := range entries {
			if len(e.Message) > 200 {
				e.Message = e.Message[:40] + fmt.Sprintf("... (%d bytes)", len(e.Message))
			}
			encoded, _ := json.Marshal(e)
			fmt.Println(string(encoded))
		}
	}
	report(path)

	long := filepath.Join(dir, "long.log")
	logging.InitLogging(long)
	logging.LogFileOnly("The line before")
	logging.LogFileOnly("%s", strings.Repeat("x", 70000))
	logging.LogFileOnly("The line after")
	logging.CloseLogging()
	report(long)
}

// logCallers lists, by file, the calls of GetLogs and PrintLogs in cmd and
// pkg, test files aside. No line numbers: cmd/hardn/main.go moves with other
// slices' work.
func logCallers(t *testing.T) []string {
	t.Helper()
	const repo = "../.."
	var lines []string
	for _, top := range []string{"cmd", "pkg"} {
		err := filepath.WalkDir(filepath.Join(repo, top), func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			tree, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(repo, p)
			if err != nil {
				return err
			}
			ast.Inspect(tree, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "GetLogs" && sel.Sel.Name != "PrintLogs") {
					return true
				}
				x, ok := sel.X.(*ast.Ident)
				name := sel.Sel.Name
				switch {
				case ok:
					name = x.Name + "." + name
				default:
					if inner, ok := sel.X.(*ast.SelectorExpr); ok {
						if id, ok := inner.X.(*ast.Ident); ok {
							name = id.Name + "." + inner.Sel.Name + "." + name
						}
					}
				}
				lines = append(lines, filepath.ToSlash(rel)+": "+name+"()")
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(lines)
	return lines
}
