package output

import (
	"bytes"
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
