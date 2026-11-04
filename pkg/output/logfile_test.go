package output

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/engine/logview"
)

// TestLogLine: a line is Go's log.LstdFlags date and time, the level, ": "
// and the message, as pkg/logging wrote it, so that logview reads its level;
// each line given to Lines is a line of its own; a nil Log writes nothing.
func TestLogLine(t *testing.T) {
	var b bytes.Buffer
	l := NewLog(&b)
	l.Printf(LogError, "hardn %s", "-g")
	_, _ = l.Lines(LogInfo).Write([]byte("exec ufw status\nStatus: active\n"))
	l.Printf(LogSuccess, "done")
	l.Printf(LogWarning, "careful")
	lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
	stamp := `^[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2} `
	want := []struct{ level, rest string }{
		{"ERROR", "hardn -g"}, {"INFO", "exec ufw status"}, {"INFO", "Status: active"}, {"SUCCESS", "done"}, {"WARNING", "careful"},
	}
	if len(lines) != len(want) {
		t.Fatalf("lines %q", lines)
	}
	for i, w := range want {
		if !regexp.MustCompile(stamp + w.level + ": " + regexp.QuoteMeta(w.rest) + "$").MatchString(lines[i]) {
			t.Errorf("line %d %q, want %s: %s", i, lines[i], w.level, w.rest)
		}
		if got := logview.ParseLine(lines[i]); got != w.level {
			t.Errorf("logview.ParseLine(%q) = %q", lines[i], got)
		}
	}
	var none *Log
	none.Printf(LogInfo, "nothing")
	_, _ = none.Lines(LogInfo).Write([]byte("nothing\n"))
}

// TestOpenLog: the log is appended to, in a directory made where missing;
// where the directory cannot be made or the file opened, as for a user
// other than root, OpenLog says so in pkg/logging's words, writes nothing
// to stdout or stderr, and its Log writes nothing (defect 28).
func TestOpenLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log", "hardn.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, errs := OpenLog(path)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	l.Printf(LogInfo, "after")
	if b, _ := os.ReadFile(path); !strings.HasPrefix(string(b), "before\n") || !strings.HasSuffix(string(b), " INFO: after\n") {
		t.Errorf("log %q", b)
	}

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path string
		want       []string
	}{
		{"a directory", t.TempDir(), []string{"Failed to open log file "}},
		{"no directory", filepath.Join(file, "dir", "hardn.log"), []string{"Failed to create log directory: ", "Failed to open log file " + filepath.Join(file, "dir", "hardn.log") + ": "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, errs := OpenLog(tc.path)
			if l != nil || len(errs) != len(tc.want) {
				t.Fatalf("OpenLog = %v, %v", l, errs)
			}
			for i, want := range tc.want {
				if !strings.HasPrefix(errs[i].Error(), want) {
					t.Errorf("error %d %q, want %q", i, errs[i], want)
				}
			}
			l.Printf(LogInfo, "nowhere")
		})
	}
}
