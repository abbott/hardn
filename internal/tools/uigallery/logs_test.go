package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Logs screen on the simulated hosts, and hardn -p's forms (logs.go; slice 1,
// unit (c)). Nothing here runs a command or writes a file outside the fake. The
// screen's own tests are pkg/screens/logs'.

// TestLogsGolden pins each listing form on the core set and under LANG=C: hardn -p's
// forms, through the real Print.
func TestLogsGolden(t *testing.T) { goldenListing(t, logsListing) }

// TestPlayLogsGolden plays the reading end to end and pins each transcript:
// from the main menu's 9, on a log of lines, leaving; Refresh, a line
// having been written; the same under plan only; a long log's Open, which
// says what it would run; each state; no pager; and the hosts' menu.
func TestPlayLogsGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"logs", nil, []string{"9", "0", "0"}, nil},
		{"logs-refresh", []string{"logs/lines"}, []string{"2", "0"}, append(uitest.Core(), asciiColour)},
		{"logs-plan-only", []string{"-sequence", "dry-run"}, []string{"9", "2", "0", "0"}, nil},
		{"logs-open", []string{"logs/long"}, []string{"1", "0"}, nil},
		{"logs-missing", []string{"logs/missing"}, []string{"1", "0"}, nil},
		{"logs-unreadable", []string{"logs/unreadable"}, []string{"1", "q"}, nil},
		{"logs-empty", []string{"logs/empty"}, []string{"0"}, nil},
		{"logs-control", []string{"logs/control"}, []string{"0"}, nil},
		{"logs-elsewhere", []string{"logs/elsewhere"}, []string{"0"}, nil},
		{"logs-alpine", []string{"logs/alpine323"}, []string{"1", "0"}, nil},
		{"logs-no-pager", []string{"logs/no-pager"}, []string{"1", "0"}, nil},
		{"logs-hosts", []string{"logs"}, []string{"6", "0", "0"}, nil},
	}
	for _, tt := range tests {
		cells := tt.cells
		if cells == nil {
			cells = uitest.Core()
		}
		uitest.Golden(t, "play/"+tt.name, cells, func(env term.Env) string {
			args := append(lookArgs(t, env), tt.args...)
			code, out, errs, unread := playGallery(tt.lines, args...)
			if code != exitOK || errs != "" || unread != 0 {
				t.Errorf("%s, %v: exit %d, stderr %q, %d lines unread", tt.name, args, code, errs, unread)
			}
			return out
		})
	}
}

// TestLogHosts holds each host's reading to what its note says: the state, the number
// of lines, what would read it, and hardn.yml's logFile, which is said but never read
// as the log.
func TestLogHosts(t *testing.T) {
	tests := []struct {
		host      string
		state     logview.State
		total     int
		pager     bool
		elsewhere string
	}{
		{host: "lines", state: logview.Lines, total: 4, pager: true},
		{host: "long", state: logview.Lines, total: 1204, pager: true},
		{host: "missing", state: logview.NoLog},
		{host: "unreadable", state: logview.Unreadable},
		{host: "empty", state: logview.Empty},
		{host: "control", state: logview.Lines, total: 6, pager: true},
		{host: "elsewhere", state: logview.Lines, total: 10, pager: true, elsewhere: "/srv/log/hardn.log"},
		{host: "alpine323", state: logview.Lines, total: 10},
		{host: "no-pager", state: logview.Lines, total: 10},
	}
	if len(tests) != len(logHosts()) {
		t.Fatalf("%d hosts tested, %d hosts", len(tests), len(logHosts()))
	}
	ctx := context.Background()
	for _, tt := range tests {
		lh, ok := findLogHost(tt.host)
		if !ok {
			t.Fatalf("no host %q", tt.host)
		}
		sys := lh.host().system()
		r := logview.Read(ctx, sys)
		_, pager := logview.Pager(ctx, sys, galleryTerm)
		cfg, err := statusConfig(ctx, sys)
		if err != nil {
			t.Fatal(err)
		}
		if r.Path != hardnLog || r.State != tt.state || r.Total != tt.total || (r.State == logview.Lines && pager != tt.pager) || logview.Elsewhere(cfg.LogFile) != tt.elsewhere {
			t.Errorf("%s: path %s, state %d, %d lines, pager %v, logFile %q", tt.host, r.Path, r.State, r.Total, pager, cfg.LogFile)
		}
	}
}

// TestControlHostIsSafe: what a terminal would act on never reaches it, on the screen
// or off it, and what a line held still shows: logview's own tests hold Safe to the
// rest.
func TestControlHostIsSafe(t *testing.T) {
	code, out, errs, unread := playGallery([]string{"0"}, "-profile", "plain", "logs/control")
	if code != exitOK || errs != "" || unread != 0 {
		t.Fatalf("exit %d, stderr %q, %d lines unread", code, errs, unread)
	}
	piped, _, err := printLogs(context.Background(), logOn(controlLog)().system(), plainOff(), "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(out+piped, "\x1b\x07\u202e") {
		t.Errorf("a control character in:\n%s\n%s", out, piped)
	}
	for _, want := range []string{"^[[31mErr:1^[[0m", "^[]0;a title^Gdone^[[2J", "abc<U+202E>def", "caf<0xe9>", "don’t “quote” ≥ 5 → … x"} {
		if !strings.Contains(piped, want) {
			t.Errorf("no %q in %q", want, piped)
		}
	}
}

// plainOff is hardn's environment off a terminal.
func plainOff() term.Env { return term.Resolve(term.Options{}, term.Facts{Width: 80}) }

// TestPipeIsEveryLine: off a terminal every line of a long log, as many as the file
// holds, where the screen's reading keeps logview.MaxLast.
func TestPipeIsEveryLine(t *testing.T) {
	ctx := context.Background()
	piped, printed, err := printLogs(ctx, logOn(longLog())().system(), plainOff(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(piped, "\n") + 1; got != 1204 || printed.Total != 1204 {
		t.Errorf("%d lines, Print says %d, want 1204", got, printed.Total)
	}
	if r := logview.Read(ctx, logOn(longLog())().system()); len(r.Last) != logview.MaxLast || r.Total != 1204 {
		t.Errorf("%d kept of %d", len(r.Last), r.Total)
	}
}

// TestNotesOpenWithTheirSource: every form's note carries its source label.
func TestNotesOpenWithTheirSource(t *testing.T) {
	for _, l := range []listing{sdListing, logsListing} {
		for _, m := range l.mocks() {
			if note := l.note(m); !strings.HasPrefix(note, l.label) {
				t.Errorf("%s %s: %q", l.prefix, m.name, note)
			}
		}
	}
}

// TestPagerOnTheHosts: logview.Pager on each simulated less: GNU's on Debian 13, with
// the exact command; BusyBox's, through its link, and no less give none. TERM is what
// play passes; a variable naming a program is not read, as Pager takes none.
func TestPagerOnTheHosts(t *testing.T) {
	ctx := context.Background()
	want := "LESSSECURE=1 LESSHISTFILE=- TERM=" + galleryTerm + " LC_ALL=C.UTF-8 less -- " + hardnLog
	c, ok := logview.Pager(ctx, logOn("")().system(), galleryTerm)
	if got := strings.Join(slices.Concat(c.Env, c.Argv), " "); !ok || got != want {
		t.Errorf("Debian 13: %q, %t", got, ok)
	}
	for name, h := range map[string]func() *fakeHost{"BusyBox's less": alpineLog, "no less": noPagerLog} {
		if c, ok := logview.Pager(ctx, h().system(), galleryTerm); ok {
			t.Errorf("%s: %+v", name, c)
		}
	}
}

// TestLogsUsage: the listing and play refuse what they do not take, with
// status 2 and nothing on stdout; -list prints the mocks.
func TestLogsUsage(t *testing.T) {
	for _, args := range [][]string{
		{logsPrefix, "nope"},
		{logsPrefix, "cli-pipe", "-list"},
		{logsPrefix, "screen-lines"},
		{"play", "logs/nope"},
		{"play", "-fail", "1", "logs/lines"},
		{"play", "-sequence", "failure", "logs/lines"},
	} {
		var out, errs bytes.Buffer
		read := false
		code := run(args, func() *prompt.Reader { read = true; return noInput() }, offTerminal, &out, &errs)
		if code != exitUsage || out.Len() != 0 || errs.Len() == 0 || read {
			t.Errorf("%v: exit %d, stdout %q, stderr %q, stdin read %t", args, code, out.String(), errs.String(), read)
		}
	}
	var out, errs bytes.Buffer
	if code := run([]string{logsPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || !strings.Contains(out.String(), "cli-pipe\n") {
		t.Errorf("-list: exit %d, %q", code, out.String())
	}
}

// TestLogsWriteNothing: on every host, live and under plan only, the screen
// drawn and every item chosen, Open and Refresh among them, write nothing and
// run nothing but reads, less --version among them: a pager is said, not run.
// Refresh's new line is the model's, put there unrecorded, as another hardn
// would write it.
func TestLogsWriteNothing(t *testing.T) {
	for _, lh := range logHosts() {
		for _, dryRun := range []bool{false, true} {
			h := lh.host()
			lines := []string{"1", "0"}
			if _, open := logview.Pager(context.Background(), h.system(), galleryTerm); open && logview.Read(context.Background(), h.system()).State == logview.Lines {
				lines = []string{"1", "2", "0"}
			}
			s, out, code := playWith(t, logsPrefix+"/"+lh.name, func(s *session) func() error {
				if dryRun {
					s.sequence = "dry-run"
				}
				return func() error { return s.logsScreen(context.Background(), lh, h) }
			}, lines...)
			if code != exitOK || len(s.engineRuns) != 0 || s.holds != 0 {
				t.Errorf("%s, plan only %t: exit %d, %d engines, the hold taken %d times:\n%s", lh.name, dryRun, code, len(s.engineRuns), s.holds, out)
			}
			assertOnlyReads(t, lh.name, h)
			if strings.Contains(out, "hardn would run") != (len(lines) == 3) {
				t.Errorf("%s: Open's note, with the keys %q:\n%s", lh.name, lines, out)
			}
		}
	}
}
