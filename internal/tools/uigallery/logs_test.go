package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/prompt"
	"github.com/abbott/hardn/pkg/ui/term"
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
// says what it would run; each state; no pager; treatment read-pager, with
// a pager and with none; and the hosts' menu.
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
		{"logs-read-pager", []string{"logs/read-pager"}, nil, nil},
		{"logs-read-pager-none", []string{"logs/read-pager-none"}, []string{"0"}, nil},
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
		state     logState
		lines     int
		pager     string
		elsewhere string
	}{
		{host: "lines", state: logLines, lines: 4, pager: "less"},
		{host: "long", state: logLines, lines: 1204, pager: "less"},
		{host: "missing", state: logMissing, pager: "less"},
		{host: "unreadable", state: logUnread, pager: "less"},
		{host: "empty", state: logEmpty, pager: "less"},
		{host: "control", state: logLines, lines: 5, pager: "less"},
		{host: "elsewhere", state: logLines, lines: 8, pager: "less", elsewhere: "/srv/log/hardn.log"},
		{host: "alpine323", state: logLines, lines: 8, pager: "less"},
		{host: "no-pager", state: logLines, lines: 8},
		{host: "read-pager", state: logLines, lines: 1204, pager: "less"},
		{host: "read-pager-none", state: logLines, lines: 8},
	}
	if len(tests) != len(logHosts()) {
		t.Fatalf("%d hosts tested, %d hosts", len(tests), len(logHosts()))
	}
	for _, tt := range tests {
		r, err := readLogHost(context.Background(), tt.host)
		if err != nil {
			t.Fatal(err)
		}
		if r.path != hardnLog || r.state != tt.state || len(r.lines) != tt.lines || r.pager.name != tt.pager || r.elsewhere != tt.elsewhere {
			t.Errorf("%s: path %s, state %d, %d lines, pager %q, logFile %q", tt.host, r.path, r.state, len(r.lines), r.pager.name, r.elsewhere)
		}
	}
}

// TestSafeLine: what a terminal would act on never reaches it, and what a
// line held still shows.
func TestSafeLine(t *testing.T) {
	for in, want := range map[string]string{
		"plain":              "plain",
		"\x1b[31mred\x1b[0m": "^[[31mred^[[0m",
		"\x1b]0;t\x07x":      "^[]0;t^Gx",
		"a\tb":               "a b",
		"a\x7fb":             "a^?b",
		"\u009b31m":          "<U+009B>31m",
		"abc‮def":       "abc<U+202E>def",
		"caf\xe9":            "caf<0xe9>",
		"café":               "café",
	} {
		if got := safeLine(in); got != want {
			t.Errorf("safeLine(%q) = %q, want %q", in, got, want)
		}
	}
	r, err := readLogHost(context.Background(), "control")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range r.lines {
		if strings.ContainsAny(l, "\x1b\x07‮") {
			t.Errorf("a control character in %q", l)
		}
	}
}

// TestParseLogLine: the fixed parser reads hardn's five levels, the date and
// the clock together, and leaves any other line whole; origin/main's reads
// the clock as the level (the parser mock).
func TestParseLogLine(t *testing.T) {
	for _, l := range logLevels {
		got := parseLogLine("2026/10/20 12:00:05 " + l + ": a: b")
		if got.Time != "2026/10/20 12:00:05" || got.Level != l || got.Message != "a: b" {
			t.Errorf("%s: %+v", l, got)
		}
	}
	for _, line := range []string{"", "not a line", "2026/10/20 12:00:05 DEBUG: x", "a b c: d"} {
		if got := parseLogLine(line); got.Level != "" || got.Message != line {
			t.Errorf("%q: %+v", line, got)
		}
	}
}

// TestPagerFor: treatment pager-less reads no environment, pager-env reads
// PAGER; less is found through the fake, with its secure mode; no pager is
// no choice.
func TestPagerFor(t *testing.T) {
	ctx := context.Background()
	env := lookup(map[string]string{"PAGER": "most -s"})
	less := logOn("")().system()
	if p := pagerFor(ctx, less, env, false); p.command(hardnLog) != "LESSSECURE=1 less "+hardnLog {
		t.Errorf("pager-less: %q", p.command(hardnLog))
	}
	if p := pagerFor(ctx, less, env, true); p.command(hardnLog) != "most -s "+hardnLog || p.name != "most" {
		t.Errorf("pager-env: %q", p.command(hardnLog))
	}
	if p := pagerFor(ctx, less, noEnv, true); p.name != "less" {
		t.Errorf("pager-env, no PAGER: %q", p.name)
	}
	if p := pagerFor(ctx, noPagerLog().system(), noEnv, false); p.name != "" {
		t.Errorf("no less: %q", p.name)
	}
	if p := pagerFor(ctx, alpineLog().system(), noEnv, false); p.name != "less" {
		t.Errorf("BusyBox's less, through its link: %q", p.name)
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
