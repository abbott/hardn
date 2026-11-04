package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/term"
)

// plainEnv is stderr's environment off a terminal: no colour, plain.
func plainEnv() term.Env { return term.Resolve(term.Options{}, term.Facts{}) }

// colourEnv is stderr's environment on a colour terminal.
func colourEnv() term.Env {
	return term.Resolve(term.Options{}, term.Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: 24, Environ: []string{"TERM=xterm-256color", "LANG=C.UTF-8"}})
}

// csi is a CSI escape sequence: an SGR colour, a cursor move or a clear.
var csi = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// stripANSI is s without its CSI sequences, as the screens' text reads.
// cmd/hardn may not import Charm's x/ansi (depguard's charm rule).
func stripANSI(s string) string { return csi.ReplaceAllString(s, "") }

// flat is console output s as one line of words: each callout's "# " bar
// dropped, so that a Message reads "<its title> <its text>" (Warning,
// Operation Failed, Operation Successful) and an info line its text alone,
// however the lines were wrapped: a line the wrap broke after a hyphen
// (20261018T120000Z-1a2b3c4d, --abandon) is joined to the next as it was.
func flat(s string) string {
	s = regexp.MustCompile("\x1b\\[[0-9;]*m").ReplaceAllString(s, "")
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "#"))
		if l == "" {
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte(' ')
		}
		b.WriteString(l)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// TestSay: a console line is a component.Message on stderr, its kind's look
// on a colour terminal, plain off one, and a line of hardn's log at its
// kind's level; a run says each kind's title once over its lines.
func TestSay(t *testing.T) {
	envWas, stderrWas, logWas := stderrEnv, stderr, hardnLog
	t.Cleanup(func() { stderrEnv, stderr, hardnLog = envWas, stderrWas, logWas })
	path := filepath.Join(t.TempDir(), "hardn.log")
	openLog(path)
	var out bytes.Buffer
	stderr = &out

	stderrEnv = plainEnv
	sayError("Failed to load configuration: %s", "no such file")
	saySuccess("Change %s confirmed", "x")
	sayRun(component.Message{Kind: component.MessageWarn, Text: "a"}, component.Message{Kind: component.MessageWarn, Text: "b"}, component.Message{Text: "c"})
	sayInfo("Using configuration from: /etc/hardn/hardn.yml")
	want := "# Operation Failed\n# Failed to load configuration: no such file\n" +
		"# Operation Successful\n# Change x confirmed\n" +
		"# Warning\n# a\n# b\n\n  c\n" +
		"  Using configuration from: /etc/hardn/hardn.yml\n"
	if out.String() != want {
		t.Errorf("plain:\n%q\nwant\n%q", out.String(), want)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var levels []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		levels = append(levels, strings.SplitN(strings.SplitN(l, " ", 3)[2], ": ", 2)[0])
	}
	if strings.Join(levels, " ") != "ERROR SUCCESS WARNING WARNING INFO INFO" {
		t.Errorf("log:\n%s", b)
	}

	out.Reset()
	stderrEnv = colourEnv
	sayWarning("careful")
	if !strings.Contains(out.String(), "\x1b[") || flat(out.String()) != "Warning careful" {
		t.Errorf("colour: %q", out.String())
	}
}

// TestOpenLog: the log is appended to, in a directory made where missing;
// where the directory cannot be made or the file opened, as for a user
// other than root, each is a warning on stderr in pkg/logging's words, and
// the log writes nothing.
func TestOpenLog(t *testing.T) {
	envWas, stderrWas, logWas := stderrEnv, stderr, hardnLog
	t.Cleanup(func() { stderrEnv, stderr, hardnLog = envWas, stderrWas, logWas })
	var out bytes.Buffer
	stderr, stderrEnv = &out, plainEnv

	path := filepath.Join(t.TempDir(), "log", "hardn.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openLog(path)
	hardnLog.Printf("INFO", "after")
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "before\n") || !strings.HasSuffix(string(b), " INFO: after\n") || out.Len() != 0 {
		t.Errorf("log %q, stderr %q", b, out.String())
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, tc := range []struct {
		name, path, want string
	}{
		{"a directory", dir, "Warning Failed to open log file " + dir + ": "},
		{"no directory", filepath.Join(file, "dir", "hardn.log"), "Warning Failed to create log directory: mkdir " + file + ": not a directory Failed to open log file " + filepath.Join(file, "dir", "hardn.log") + ": "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			openLog(tc.path)
			// Compared without white space: a path longer than the width is cut.
			nospace := func(s string) string { return strings.Join(strings.Fields(s), "") }
			if hardnLog != nil || !strings.HasPrefix(nospace(flat(out.String())), nospace(tc.want)) {
				t.Errorf("log %v; stderr %q, want %q", hardnLog, flat(out.String()), tc.want)
			}
		})
	}
}
