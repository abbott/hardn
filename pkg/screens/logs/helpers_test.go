package logs_test

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/screens/logs"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/nav"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The hosts the Logs screen's tests run on, on hostfake, as the gallery's Logs hosts
// are drawn (gallery-system-details.md §3.1): a log of hardn's lines, GNU less 668 as
// Debian 13 and Ubuntu 26.04 print it (vm-runs/s1/01-tools.log:22, :88), BusyBox's as
// Alpine 3.23 does (:151-153). Synthetic values only.

// lessCommand is what Open runs, argv and environment, readers-system-details.md item
// 11's: the term the caller gave, checked.
var lessCommand = []string{"less", "--", logview.File}

func lessEnv(term string) []string {
	return []string{"LESSSECURE=1", "LESSHISTFILE=-", "TERM=" + term, "LC_ALL=C.UTF-8", "PATH=" + host.SearchPath}
}

// logSample is a log as hardn's lines look, ending with a start's own lines.
var logSample = []string{
	"2026/10/20 12:00:01 INFO: run 1: $ ufw allow 2230/tcp",
	"2026/10/20 12:00:01 INFO: run 1: | Rule added",
	"2026/10/20 12:00:01 INFO: run 1: exit 0",
	"2026/10/20 12:00:03 SUCCESS: Firewall configured successfully",
	"2026/10/20 12:00:04 WARNING: hardn's boot unit was not removed: exit status 1",
	"2026/10/20 12:00:05 ERROR: Failed to configure firewall: exit status 1",
	"2026/10/20 12:05:11 INFO: Using configuration from: /etc/hardn/hardn.yml",
	"2026/10/20 12:05:11 SUCCESS: debian trixie detected",
}

// logOf is lines as a file holds them.
func logOf(lines []string) string { return strings.Join(lines, "\n") + "\n" }

// longLog is a log of 1204 lines.
func longLog() string {
	var lines []string
	for i := range 1204 - len(logSample) {
		lines = append(lines, fmt.Sprintf("2026/10/%02d %02d:%02d:%02d INFO: run %d: exit 0", 1+i/96, i/4%24, i%4*15, i%60, i+1))
	}
	return logOf(append(lines, logSample...))
}

// controlLog holds what a command printed: colour, a title and bell, a bidi override,
// bytes that are not UTF-8, typographic characters.
const controlLog = "2026/10/20 12:00:01 INFO: run 1: $ apt-get update\n" +
	"2026/10/20 12:00:02 INFO: run 1: | \x1b[31mErr:1\x1b[0m\n" +
	"2026/10/20 12:00:02 INFO: run 1: | \x1b]0;a title\x07done\x1b[2J\n" +
	"2026/10/20 12:00:03 INFO: run 2: | abc\u202edef\n" +
	"2026/10/20 12:00:03 INFO: run 2: | caf\xe9\n" +
	"2026/10/20 12:00:03 INFO: run 2: | don’t “quote” ≥ 5 → … x\n"

// newLog returns a host with content in the log and GNU less.
func newLog(content string) *hostfake.System {
	h := newHost()
	h.AddFile(logview.File, []byte(content), 0o644, 0, 0)
	return h
}

// newHost returns a host with no log and GNU less: Open runs and exits 0.
func newHost() *hostfake.System {
	h := hostfake.New(host.Options{})
	h.Handler = func(argv []string, _ []byte) hostfake.Response {
		switch {
		case slices.Equal(argv, []string{"less", "--version"}):
			return hostfake.Response{Stdout: "less 668 (GNU regular expressions)\n"}
		case slices.Equal(argv, lessCommand):
			return hostfake.Response{}
		}
		return hostfake.Response{Err: fmt.Errorf("%s: %w", strings.Join(argv, " "), hostfake.ErrUnscripted), ExitCode: -1}
	}
	return h
}

// The gallery's hosts, by name.
func hostNamed(t *testing.T, name string) *hostfake.System {
	t.Helper()
	switch name {
	case "lines":
		return newLog(logOf(logSample[len(logSample)-4:]))
	case "long":
		return newLog(longLog())
	case "missing":
		return newHost()
	case "unreadable":
		h := newHost()
		h.AddDir(logview.File, 0o755, 0, 0)
		return h
	case "empty":
		return newLog("")
	case "control":
		return newLog(controlLog)
	case "elsewhere":
		return newLog(logOf(logSample))
	case "alpine323":
		h := newLog(logOf(logSample))
		h.Handler = func(argv []string, _ []byte) hostfake.Response {
			return hostfake.Response{Stderr: "less: unrecognized option: version\n", ExitCode: 1}
		}
		return h
	case "no-pager":
		h := newLog(logOf(logSample))
		h.Handler = func(argv []string, _ []byte) hostfake.Response {
			return hostfake.Response{Err: fmt.Errorf("host: %q: not in %s: %w", argv[0], host.SearchPath, host.ErrNotFound)}
		}
		return h
	}
	t.Fatalf("no host %q", name)
	return nil
}

// terminal is an interactive terminal's environment.
func terminal(rows int) term.Env {
	return term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: rows, Environ: []string{"TERM=xterm-256color", "COLORTERM=truecolor", "LANG=C.UTF-8"}})
}

// off is hardn's environment off a terminal, stdout a pipe.
func off() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80, Height: 24}) }

// played is one run of the screen: what it wrote with the answers echoed, what Run
// returned, how many lines were left unread.
type played struct {
	out    string
	err    error
	unread int
}

// play runs the screen on sys in a plain environment with 24 rows, TERM xterm-256color,
// no logFile, the user typing lines.
func play(sys host.System, lines ...string) played {
	return playWith(sys, off(), logs.Options{Term: "xterm-256color"}, lines...)
}

// playWith is play in env, with o's LogFile and Term.
func playWith(sys host.System, env term.Env, o logs.Options, lines ...string) played {
	var out bytes.Buffer
	ty := uitest.NewTypist(&out, lines...)
	o.Prompter = nav.Prompter{Ctx: component.NewCtx(env), In: prompt.New(ty, false), Out: env.Writer(&out)}
	o.System = sys
	err := logs.Run(context.Background(), o)
	return played{out.String(), err, ty.Unread()}
}

// sgr is a colour or attribute's escape sequence.
var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// strip is s with its colours gone, and only those.
func strip(s string) string { return sgr.ReplaceAllString(s, "") }

// counts are how many times a run read the log, asked less --version and ran less.
type counts struct{ scans, checks, opens int }

func countOn(h *hostfake.System) counts {
	var c counts
	for _, call := range h.Calls() {
		switch {
		case call.Op == "scan-lines":
			c.scans++
		case call.Op == "run" && slices.Equal(call.Argv, []string{"less", "--version"}):
			c.checks++
		case call.Op == "run-interactive":
			c.opens++
		}
	}
	return c
}

// assertOnlyReads fails t for anything h did but read the log, ask less --version and,
// allowed times, run less.
func assertOnlyReads(t *testing.T, name string, h *hostfake.System, allowed int) {
	t.Helper()
	opens := 0
	for _, c := range h.Calls() {
		switch {
		case c.Op == "scan-lines" && c.Path == logview.File:
		case c.Op == "run" && slices.Equal(c.Argv, []string{"less", "--version"}):
		case c.Op == "run-interactive":
			opens++
		default:
			t.Errorf("%s: %s", name, c)
		}
	}
	if opens != allowed {
		t.Errorf("%s: less ran %d times, want %d", name, opens, allowed)
	}
}

// refusingScan is a host whose log refuses to be read, as to a user other than root
// where it is tighter than 0644.
type refusingScan struct{ *hostfake.System }

func (h refusingScan) ScanLines(_ context.Context, name string, _ func(host.Line) bool) (fs.FileInfo, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrPermission}
}

// failingScan gives its lines, then fails, as a read part way does.
type failingScan struct {
	*hostfake.System
	lines []string
}

func (h failingScan) ScanLines(_ context.Context, name string, yield func(host.Line) bool) (fs.FileInfo, error) {
	for _, l := range h.lines {
		if !yield(host.Line{Text: []byte(l)}) {
			break
		}
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fmt.Errorf("input/output error")}
}
