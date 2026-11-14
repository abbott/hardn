package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/output"
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

// inOrder returns the first of want that out does not show after what
// came before it, each compared with its white space, a callout's bar
// at a wrapped line's start among it, as single spaces; "" when it shows
// them all, in order.
func inOrder(out string, want ...string) string {
	f := strings.Join(strings.Fields(strings.ReplaceAll(out, "\n# ", " ")), " ")
	at := 0
	for _, w := range want {
		i := strings.Index(f[at:], strings.Join(strings.Fields(w), " "))
		if i < 0 {
			return w
		}
		at += i + 1
	}
	return ""
}

// nospace is s without white space. The wrap cuts a word longer than the
// width, a temporary file's path among them, at a place that depends on
// TMPDIR's length and t.TempDir's digits; flat joins the pieces with a
// space, so text that runs on from such a path is compared this way.
func nospace(s string) string { return strings.Join(strings.Fields(s), "") }

// TestSay: a console line is a component.Message on stderr, its kind's look
// on a colour terminal, plain off one, and a line of hardn's log at its
// kind's level; a run says each kind's title once over its lines.
func TestSay(t *testing.T) {
	envWas, stderrWas, logWas, onWas := stderrEnv, stderr, hardnLog, logOnStderr
	t.Cleanup(func() { stderrEnv, stderr, hardnLog, logOnStderr = envWas, stderrWas, logWas, onWas })
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
// the log is stderr: a log-only line reaches it, and a console line is said
// there once.
func TestOpenLog(t *testing.T) {
	envWas, stderrWas, logWas, onWas := stderrEnv, stderr, hardnLog, logOnStderr
	t.Cleanup(func() { stderrEnv, stderr, hardnLog, logOnStderr = envWas, stderrWas, logWas, onWas })
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
			if !logOnStderr || !strings.HasPrefix(nospace(flat(out.String())), nospace(tc.want)) {
				t.Errorf("log on stderr %v; stderr %q, want %q", logOnStderr, flat(out.String()), tc.want)
			}
			out.Reset()
			if _, err := (revertFileLog{}).Write([]byte("ran: systemctl reload ssh\n")); err != nil {
				t.Fatal(err)
			}
			hardnLog.Printf("INFO", "hangup during the apply: hardn ends")
			if s := out.String(); !strings.Contains(s, " INFO: ran: systemctl reload ssh\n") || !strings.Contains(s, " INFO: hangup during the apply: hardn ends\n") {
				t.Errorf("log-only lines on stderr: %q", s)
			}
			out.Reset()
			sayError("Failed to load configuration: %s", "no such file")
			if s := out.String(); strings.Count(s, "Failed to load configuration") != 1 || strings.Contains(s, "ERROR:") {
				t.Errorf("console line on stderr: %q", s)
			}
		})
	}
}

// sized makes path a file of size bytes, a hole: nothing is written.
func sized(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, size); err != nil {
		t.Fatal(err)
	}
}

// TestOpenLogRotates: a log at output.LogMaxSize is opened as it is; one
// byte over, it is renamed to hardn.log.1 before the command's first line,
// its mode kept, the four older copies moved up and the fourth deleted,
// with nothing said (docs/engine/design-log-rotation.md).
func TestOpenLogRotates(t *testing.T) {
	envWas, stderrWas, logWas, onWas := stderrEnv, stderr, hardnLog, logOnStderr
	t.Cleanup(func() { stderrEnv, stderr, hardnLog, logOnStderr = envWas, stderrWas, logWas, onWas })
	var out bytes.Buffer
	stderr, stderrEnv = &out, plainEnv
	size := func(path string) int64 {
		fi, err := os.Lstat(path)
		if err != nil {
			return -1
		}
		return fi.Size()
	}

	path := filepath.Join(t.TempDir(), "hardn.log")
	sized(t, path, output.LogMaxSize)
	openLog(path)
	hardnLog.Printf("INFO", "first")
	if size(path) <= output.LogMaxSize || size(path+".1") != -1 || out.Len() != 0 {
		t.Errorf("at the limit: log %d bytes, .1 %d bytes, stderr %q", size(path), size(path+".1"), out.String())
	}

	path = filepath.Join(t.TempDir(), "hardn.log")
	sized(t, path, output.LogMaxSize+1)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= output.LogKeep; i++ {
		if err := os.WriteFile(path+"."+strconv.Itoa(i), []byte(strconv.Itoa(i)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	openLog(path)
	hardnLog.Printf("INFO", "first")
	if b, _ := os.ReadFile(path); !strings.HasSuffix(string(b), " INFO: first\n") || strings.Count(string(b), "\n") != 1 || logOnStderr || out.Len() != 0 {
		t.Errorf("the new log %q, on stderr %v, stderr %q", b, logOnStderr, out.String())
	}
	if fi, err := os.Lstat(path + ".1"); err != nil || fi.Size() != output.LogMaxSize+1 || fi.Mode().Perm() != 0o640 {
		t.Errorf("hardn.log.1: %v, %v", fi, err)
	}
	for i := 2; i <= output.LogKeep; i++ {
		if b, _ := os.ReadFile(path + "." + strconv.Itoa(i)); string(b) != strconv.Itoa(i-1) {
			t.Errorf("hardn.log.%d holds %q, want what .%d held", i, b, i-1)
		}
	}
	if size(path+"."+strconv.Itoa(output.LogKeep+1)) != -1 {
		t.Error("a fifth copy was kept")
	}
}

// TestOpenLogNotRotated: a log that cannot be rotated, here a copy that is
// a symlink, is one warning, and the log is opened and appended to as it
// is: nothing renamed, the link's target untouched (decision 7).
func TestOpenLogNotRotated(t *testing.T) {
	envWas, stderrWas, logWas, onWas := stderrEnv, stderr, hardnLog, logOnStderr
	t.Cleanup(func() { stderrEnv, stderr, hardnLog, logOnStderr = envWas, stderrWas, logWas, onWas })
	var out bytes.Buffer
	stderr, stderrEnv = &out, plainEnv

	dir := t.TempDir()
	path, target := filepath.Join(dir, "hardn.log"), filepath.Join(dir, "shadow")
	sized(t, path, output.LogMaxSize+1)
	if err := os.WriteFile(target, []byte("root:x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path+".2"); err != nil {
		t.Fatal(err)
	}
	openLog(path)
	hardnLog.Printf("INFO", "first")
	said := nospace(flat(out.String()))
	if said != nospace("Warning "+path+" was not rotated: "+path+".2: refusing a symlink") || logOnStderr {
		t.Errorf("stderr %q, log on stderr %v", flat(out.String()), logOnStderr)
	}
	if fi, err := os.Lstat(path); err != nil || fi.Size() <= output.LogMaxSize+1 {
		t.Errorf("the line was not appended to the log as it is: %v, %v", fi, err)
	}
	if b, _ := os.ReadFile(path + ".1"); string(b) != "1" {
		t.Errorf("hardn.log.1 moved: %q", b)
	}
	if b, _ := os.ReadFile(target); string(b) != "root:x\n" {
		t.Errorf("the link's target: %q", b)
	}
	if fi, err := os.Lstat(path + ".2"); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("hardn.log.2: %v, %v", fi, err)
	}
}
