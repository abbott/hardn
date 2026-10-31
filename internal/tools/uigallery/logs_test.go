package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/theme"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// The Logs mocks (logs.go; slice 1, unit (a)). Nothing here runs a command
// or writes a file outside the fake.

// TestLogsGolden pins each listing mock on the core set and under LANG=C:
// the screen on each host, Open, the pager table, hardn -p's forms, the
// parser and origin/main's.
func TestLogsGolden(t *testing.T) { goldenListing(t, logsListing) }

// TestPlayLogsGolden plays the reading end to end and pins each transcript:
// from the main menu's 10, on a log of lines, leaving; Refresh, a line
// having been written; the same under plan only; a long log's Open, which
// says what it would run; each state; no pager; and the hosts' menu.
func TestPlayLogsGolden(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		lines []string
		cells []uitest.Cell
	}{
		{"logs", nil, []string{"10", "0", "0"}, nil},
		{"logs-refresh", []string{"logs/lines"}, []string{"2", "0"}, append(uitest.Core(), asciiColour)},
		{"logs-plan-only", []string{"-sequence", "dry-run"}, []string{"10", "2", "0", "0"}, nil},
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

// TestLogHosts holds each host's reading to what its note says: the state,
// the number of lines, what would read it, and hardn.yml's logFile, which is
// said but never read as the log.
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
	for _, tt := range tests {
		r, err := readLogHost(context.Background(), tt.host)
		if err != nil {
			t.Fatal(err)
		}
		if r.Path != hardnLog || r.State != tt.state || r.Total != tt.total || (r.pager != nil) != tt.pager || r.elsewhere != tt.elsewhere {
			t.Errorf("%s: path %s, state %d, %d lines, pager %v, logFile %q", tt.host, r.Path, r.State, r.Total, r.pager, r.elsewhere)
		}
	}
}

// TestControlHostIsSafe: what a terminal would act on never reaches it, on the
// screen or off it, and what a line held still shows: logview's own tests hold
// Safe to the rest.
func TestControlHostIsSafe(t *testing.T) {
	r, err := readLogHost(context.Background(), "control")
	if err != nil {
		t.Fatal(err)
	}
	piped, err := pipeLines(context.Background(), logOn(controlLog)().system())
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range append(lineTexts(r.Last), strings.Split(piped, "\n")...) {
		if strings.ContainsAny(l, "\x1b\x07\u202e") {
			t.Errorf("a control character in %q", l)
		}
	}
	for _, want := range []string{"^[[31mErr:1^[[0m", "^[]0;a title^Gdone^[[2J", "abc<U+202E>def", "caf<0xe9>", "don’t “quote” ≥ 5 → … x"} {
		if !strings.Contains(piped, want) {
			t.Errorf("no %q in %q", want, piped)
		}
	}
}

// TestPipeIsEveryLine: off a terminal every line of a long log, as many as the
// file holds, where the screen's reading keeps logview.MaxLast.
func TestPipeIsEveryLine(t *testing.T) {
	piped, err := pipeLines(context.Background(), logOn(longLog())().system())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(piped, "\n") + 1; got != 1204 {
		t.Errorf("%d lines, want 1204", got)
	}
	r, err := readLogHost(context.Background(), "long")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Last) != logview.MaxLast || r.Total != 1204 {
		t.Errorf("%d kept of %d", len(r.Last), r.Total)
	}
}

// TestLogRowsKeepTheCharacters: a log's line is code, as captured output is:
// its typographic characters stay in both glyph sets, with no bullet, and a
// line too long breaks after the wrap glyph, losing no character but spaces.
func TestLogRowsKeepTheCharacters(t *testing.T) {
	const typo = "2026/10/20 12:00:03 INFO: run 2: | don’t “quote” ≥ 5 → … x"
	for _, unicode := range []bool{true, false} {
		lines := drawnLines(24, unicode, func(c component.Ctx) string { return strings.Join(logRows(c, []logview.Line{{Text: typo}}), "\n") })
		if got := strings.Join(lines, ""); !strings.Contains(got, "don’t “quote” ≥ 5 → … x") || strings.Contains(got, " - ") {
			t.Errorf("unicode %v: %q", unicode, got)
		}
		long := strings.Repeat("word ", 30) + strings.Repeat("x", 80)
		wrap := "↪"
		if !unicode {
			wrap = "|"
		}
		lines = drawnLines(24, unicode, func(c component.Ctx) string { return strings.Join(logRows(c, []logview.Line{{Text: long}}), "\n") })
		joined := ""
		for i, l := range lines {
			if ansi.StringWidth(l) > theme.ContentWidth {
				t.Errorf("unicode %v: %d cells: %q", unicode, ansi.StringWidth(l), l)
			}
			l = strings.TrimPrefix(l, strings.Repeat(" ", theme.LabelCol))
			if i > 0 {
				if !strings.HasPrefix(l, wrap) {
					t.Errorf("unicode %v: no wrap glyph on %q", unicode, l)
				}
				l = strings.TrimPrefix(l, wrap)
			}
			joined += l
		}
		if strings.ReplaceAll(joined, " ", "") != strings.ReplaceAll(long, " ", "") {
			t.Errorf("unicode %v: lost characters: %q", unicode, joined)
		}
	}
}

// TestNotesOpenWithTheirSource: every mock's note carries its source label,
// but those in origin/main's own words, whose notes open by saying so.
func TestNotesOpenWithTheirSource(t *testing.T) {
	for _, l := range []listing{sdListing, logsListing} {
		for _, m := range l.mocks() {
			note := l.note(m)
			if m.own && !strings.HasPrefix(note, "origin/main's") || !m.own && !strings.HasPrefix(note, l.label) {
				t.Errorf("%s %s: %q", l.prefix, m.name, note)
			}
		}
	}
}

// TestPagerOnTheHosts: logview.Pager on each simulated less: GNU's on Debian 13
// and the less package [I], with the exact command; BusyBox's, through its link,
// and no less give none. TERM is what the mock passes; a variable naming a program
// is not read, as Pager takes none.
func TestPagerOnTheHosts(t *testing.T) {
	ctx := context.Background()
	want := "LESSSECURE=1 LESSHISTFILE=- TERM=" + galleryTerm + " LC_ALL=C.UTF-8 less -- " + hardnLog
	for name, h := range map[string]func() *fakeHost{"Debian 13": logOn(""), "Alpine's less package": alpineLessPackage} {
		if c, ok := logview.Pager(ctx, h().system(), galleryTerm); !ok || commandWords(c) != want {
			t.Errorf("%s: %q, %t", name, commandWords(c), ok)
		}
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
		{logsPrefix, "screen-lines", "-list"},
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
	if code := run([]string{logsPrefix, "-list"}, nil, offTerminal, &out, &errs); code != exitOK || !strings.Contains(out.String(), "screen-lines\n") {
		t.Errorf("-list: exit %d, %q", code, out.String())
	}
}

// TestLogsHeights: the screen on every host at 80x24 and at 80x40, in Unicode
// and ASCII: the prompt on its last line, within the terminal's rows, the
// long log taking as many of its lines as fit, a wrapped line counting each
// of its rows: more at 40 than at 24.
func TestLogsHeights(t *testing.T) {
	shown := map[int]string{24: "Last 3 of 1204 lines", 40: "Last 17 of 1204 lines"}
	for _, lh := range logHosts() {
		r, err := readLogHost(context.Background(), lh.name)
		if err != nil {
			t.Fatal(err)
		}
		for _, rows := range []int{24, 40} {
			for _, unicode := range []bool{true, false} {
				lines := drawnLines(rows, unicode, func(c component.Ctx) string { return r.subMenu(c).Render(c) })
				if len(lines) > rows || !strings.HasSuffix(lines[len(lines)-1], " or q]:") {
					t.Errorf("%s at 80x%d, unicode %v: %d lines, the last %q", lh.name, rows, unicode, len(lines), lines[len(lines)-1])
				}
				if lh.name == "long" {
					if !strings.Contains(strings.Join(lines, "\n"), shown[rows]) {
						t.Errorf("long at 80x%d, unicode %v: %d lines, want %q:\n%s", rows, unicode, len(lines), shown[rows], strings.Join(lines, "\n"))
					}
				}
			}
		}
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
			r := readLog(context.Background(), h.system())
			var lines []string
			for _, it := range r.items() {
				lines = append(lines, it.item.Key)
			}
			lines = append(lines, "0")
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
		}
	}
}
