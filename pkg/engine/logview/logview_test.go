package logview_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/abbott/hardn/pkg/engine/logview"
	"github.com/abbott/hardn/pkg/host"
	"github.com/abbott/hardn/pkg/host/hostfake"
	"github.com/abbott/hardn/pkg/output"
)

// Hosts here are hostfake's but for the real files the refusals name. The
// log's lines are synthetic, in the forms of
// _support/hardn-audit-2026-10-16/vm-runs/s3/57-log-ubuntu26-vm.log.
const logPath = "/var/log/hardn.log"

func logHost(t *testing.T, content string) *hostfake.System {
	t.Helper()
	f := hostfake.New(host.Options{})
	f.AddDir("/var", 0o755, 0, 0)
	f.AddDir("/var/log", 0o755, 0, 0)
	f.AddFile(logPath, []byte(content), 0o644, 0, 0)
	return f
}

// TestFile: the path is the one pkg/output and the audit's records call hardn's
// log, written out here so that a wrong one fails.
func TestFile(t *testing.T) {
	if logview.File != logPath {
		t.Errorf("File = %q", logview.File)
	}
	if output.LegacyLogPath != logPath {
		t.Errorf("output.LegacyLogPath = %q", output.LegacyLogPath)
	}
}

// TestRead holds each state the screen draws, on hostfake.
func TestRead(t *testing.T) {
	ctx := context.Background()
	line := func(text, level string) logview.Line { return logview.Line{Text: text, Level: level} }
	dir := func() host.System {
		f := hostfake.New(host.Options{})
		f.AddDir("/var", 0o755, 0, 0)
		f.AddDir(logPath[:len("/var/log")], 0o755, 0, 0)
		f.AddDir(logPath, 0o755, 0, 0)
		return f
	}
	link := func(target string) func() host.System {
		return func() host.System {
			f := logHost(t, "")
			if err := f.Remove(ctx, logPath); err != nil {
				t.Fatal(err)
			}
			f.AddSymlink(logPath, target)
			f.AddFile("/etc/shadow", []byte("root:x\n"), 0o600, 0, 0)
			return f
		}
	}
	lines := func(content string) func() host.System {
		return func() host.System { return logHost(t, content) }
	}
	long := strings.Repeat("x", host.MaxLine)
	for _, tt := range []struct {
		name string
		host func() host.System
		want logview.Reading
	}{
		{"no log", func() host.System { return hostfake.New(host.Options{}) }, logview.Reading{State: logview.NoLog}},
		{"empty", lines(""), logview.Reading{State: logview.Empty}},
		{"a line", lines("2026/10/20 12:00:05 INFO: one\n"), logview.Reading{State: logview.Lines, Size: 30, Total: 1,
			Last: []logview.Line{line("2026/10/20 12:00:05 INFO: one", "INFO")}}},
		{"no newline at the end, CRLF, an empty line, another shape", lines("2026/10/20 12:00:05 ERROR: a\r\n\r\nwith a second line\n2026/10/20 12:00:06 WARNING: b"),
			logview.Reading{State: logview.Lines, Size: 81, Total: 4, Last: []logview.Line{
				line("2026/10/20 12:00:05 ERROR: a", "ERROR"), line("", ""),
				line("with a second line", ""), line("2026/10/20 12:00:06 WARNING: b", "WARNING"),
			}}},
		{"what a terminal acts on", lines("2026/10/20 12:00:05 INFO: \x1b[31mred\x07 caf\xe9\n"), logview.Reading{State: logview.Lines, Size: 41, Total: 1,
			Last: []logview.Line{line("2026/10/20 12:00:05 INFO: ^[[31mred^G caf<0xe9>", "INFO")}}},
		{"a line past the cap, the read going on", lines(long + "y\nnext\n"), logview.Reading{State: logview.Lines, Size: int64(host.MaxLine + 7), Total: 2,
			Last: []logview.Line{line(long+"<cut>", ""), line("next", "")}}},
		{"a directory", dir, logview.Reading{State: logview.Unreadable, Reason: "is a directory"}},
		{"a symlink", link("/etc/shadow"), logview.Reading{State: logview.Unreadable, Reason: "refusing a symlink"}},
		{"a dangling symlink", link("/nowhere"), logview.Reading{State: logview.Unreadable, Reason: "refusing a symlink"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := logview.Read(ctx, tt.host())
			want := tt.want
			want.Path = logPath
			if want.State == logview.Unreadable == (got.Err == nil) {
				t.Errorf("Err = %v", got.Err)
			}
			want.Err = got.Err
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Read = %+v\nwant   %+v", got, want)
			}
		})
	}
}

// TestReadLong: the last lines of a log far longer than Read keeps, the total
// counted to the end.
func TestReadLong(t *testing.T) {
	var b strings.Builder
	const n = 3*logview.MaxLast + 17
	for i := range n {
		fmt.Fprintf(&b, "2026/10/20 12:00:05 INFO: line %d\n", i)
	}
	r := logview.Read(context.Background(), logHost(t, b.String()))
	if r.State != logview.Lines || r.Total != n || r.Size != int64(b.Len()) || len(r.Last) != logview.MaxLast {
		t.Fatalf("state %d, total %d, size %d, %d lines", r.State, r.Total, r.Size, len(r.Last))
	}
	for i, l := range r.Last {
		if want := fmt.Sprintf("2026/10/20 12:00:05 INFO: line %d", n-logview.MaxLast+i); l.Text != want {
			t.Fatalf("Last[%d] = %q, want %q", i, l.Text, want)
		}
	}
	few := logview.Read(context.Background(), logHost(t, "one\ntwo\n"))
	if few.Total != 2 || len(few.Last) != 2 || few.Last[0].Text != "one" || few.Last[1].Text != "two" {
		t.Errorf("%+v", few)
	}
}

// TestReadWritesNothing: a draw is one scan, on a host with a run log, which
// gets no line.
func TestReadWritesNothing(t *testing.T) {
	var log bytes.Buffer
	f := hostfake.New(host.Options{RunLog: &log})
	f.AddDir("/var", 0o755, 0, 0)
	f.AddDir("/var/log", 0o755, 0, 0)
	f.AddFile(logPath, []byte("one\n"), 0o644, 0, 0)
	logview.Read(context.Background(), f)
	calls := f.Calls()
	if len(calls) != 1 || calls[0].Op != "scan-lines" || calls[0].Path != logPath {
		t.Errorf("calls = %v", calls)
	}
	if log.Len() != 0 {
		t.Errorf("run log: %q", log.String())
	}
}

// TestReadDoneContext: a done ctx reads as not read, in ctx's words.
func TestReadDoneContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := logview.Read(ctx, logHost(t, "one\n"))
	if r.State != logview.Unreadable || r.Reason != "context canceled" || !errors.Is(r.Err, context.Canceled) {
		t.Errorf("%+v", r)
	}
}

// TestReadRealFiles runs the refusals on real files in t.TempDir(), each
// without blocking: a FIFO, a directory, a symlink, a file with no permission
// (not as root, which reads past it). The reasons are the operating system's,
// but for the FIFO's.
func TestReadRealFiles(t *testing.T) {
	ctx := context.Background()
	sys := host.NewReal(host.Options{})
	dir := t.TempDir()
	path := func(rel string) string { return filepath.Join(dir, rel) }
	for rel, content := range map[string]string{"log": "one\ntwo", "empty": "", "secret": "root:x\n"} {
		if err := os.WriteFile(path(rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(path("fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path("dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path("secret"), path("link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path("closed"), []byte("x\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		rel    string
		state  logview.State
		reason string
		total  int
	}{
		{"log", logview.Lines, "", 2},
		{"empty", logview.Empty, "", 0},
		{"missing", logview.NoLog, "", 0},
		{"fifo", logview.Unreadable, "not a regular file", 0},
		{"dir", logview.Unreadable, "is a directory", 0},
		{"link", logview.Unreadable, "refusing a symlink", 0},
		{"closed", logview.Unreadable, "permission denied", 0},
		{"log/under", logview.Unreadable, "not a directory", 0},
	} {
		t.Run(tt.rel, func(t *testing.T) {
			if tt.rel == "closed" && os.Geteuid() == 0 {
				t.Skip("root reads past a file's permissions")
			}
			done := make(chan logview.Reading, 1)
			go func() { done <- logview.ReadFile(ctx, sys, path(tt.rel)) }()
			select {
			case r := <-done:
				if r.State != tt.state || r.Reason != tt.reason || r.Total != tt.total {
					t.Errorf("%+v", r)
				}
				if tt.rel == "closed" && !errors.Is(r.Err, fs.ErrPermission) {
					t.Errorf("Err = %v", r.Err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("Read did not return")
			}
		})
	}
}

// TestElsewhere: a path other than the file hardn writes is said, safe to show;
// the empty value and the same path are no difference.
func TestElsewhere(t *testing.T) {
	for in, want := range map[string]string{
		"":                      "",
		"/var/log/hardn.log":    "",
		"/var/log//hardn.log":   "",
		"/var/log/./hardn.log":  "",
		"/srv/log/hardn.log":    "/srv/log/hardn.log",
		"hardn.log":             "hardn.log",
		"/var/log/hardn.log.1":  "/var/log/hardn.log.1",
		"/srv/\x1b]0;x\x07.log": "/srv/^[]0;x^G.log",
		"/srv/log/\u202ehardn":  "/srv/log/<U+202E>hardn",
	} {
		if got := logview.Elsewhere(in); got != want {
			t.Errorf("Elsewhere(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestParseLine: hardn's five levels, the date and the clock together, the
// message whole; real forms (vm-runs/s3/57-log-ubuntu26-vm.log), with hardn's
// run log's lines among them; any other shape stays whole, with no level.
func TestParseLine(t *testing.T) {
	for _, l := range []string{logview.LevelError, logview.LevelWarning, logview.LevelInfo, logview.LevelSuccess, logview.LevelInstalled} {
		got := logview.ParseLine("2026/10/20 12:00:05 " + l + ": a: b")
		if want := (logview.Entry{Time: "2026/10/20 12:00:05", Level: l, Message: "a: b"}); got != want {
			t.Errorf("%s: %+v", l, got)
		}
	}
	for line, want := range map[string]logview.Entry{
		"2026/10/20 12:00:05 INFO: run 5: $ systemctl is-active ssh.service": {Time: "2026/10/20 12:00:05", Level: "INFO", Message: "run 5: $ systemctl is-active ssh.service"},
		"2026/10/20 12:00:05 INFO: run 5: | active":                          {Time: "2026/10/20 12:00:05", Level: "INFO", Message: "run 5: | active"},
		"2026/10/20 12:00:05 INFO: run 5: | ERROR: a command's own":          {Time: "2026/10/20 12:00:05", Level: "INFO", Message: "run 5: | ERROR: a command's own"},
		"2026/10/20 12:00:05 SUCCESS: debian trixie detected":                {Time: "2026/10/20 12:00:05", Level: "SUCCESS", Message: "debian trixie detected"},
		"2026/10/20 12:00:05 INFO: ":                                         {Time: "2026/10/20 12:00:05", Level: "INFO"},
	} {
		if got := logview.ParseLine(line); got != want {
			t.Errorf("%q: %+v", line, got)
		}
	}
	for _, line := range []string{
		"", "not a line", "2026/10/20 12:00:05 DEBUG: x", "a b c: d", "with a second line",
		"2026/10/20 12:00:05 INFO:x", "2026-10-20 12:00:05 INFO: x", "abcdefghij 12345678 ERROR: x", " 2026/10/20 12:00:05 INFO: x",
	} {
		if got := logview.ParseLine(line); got.Level != "" || got.Message != line || got.Time != "" {
			t.Errorf("%q: %+v", line, got)
		}
	}
}

// TestEveryLineAsItReads: -p's pipe form: every line, Show, written as it reads,
// with a line past the cap marked and the rest there (unit (d) writes this; the
// words of the stream are its).
func TestEveryLineAsItReads(t *testing.T) {
	long := strings.Repeat("x", host.MaxLine)
	f := logHost(t, "one\n"+long+"y\n\x1b[31mred\r\n\nlast")
	var out bytes.Buffer
	r := logview.Each(context.Background(), f, func(l logview.Line) bool {
		fmt.Fprintln(&out, l.Text)
		return true
	})
	if want := "one\n" + long + "<cut>\n^[[31mred\n\nlast\n"; out.String() != want {
		t.Errorf("%d bytes, want %d", out.Len(), len(want))
	}
	if r.State != logview.Lines || r.Total != 5 || r.Last != nil {
		t.Errorf("%+v", r)
	}
}

// TestEachStops: a caller's false ends the pass, no error, as a write that
// failed; the error is the caller's.
func TestEachStops(t *testing.T) {
	n := 0
	r := logview.Each(context.Background(), logHost(t, "one\ntwo\nthree\n"), func(logview.Line) bool { n++; return n < 2 })
	if n != 2 || r.State != logview.Lines || r.Err != nil {
		t.Errorf("%d lines, %+v", n, r)
	}
}

// failingScan is a host whose read of the log gives lines, then fails.
type failingScan struct {
	*hostfake.System
	after []string
	err   error
}

func (f failingScan) ScanLines(ctx context.Context, name string, yield func(host.Line) bool) (fs.FileInfo, error) {
	for _, l := range f.after {
		if !yield(host.Line{Text: []byte(l)}) {
			return nil, nil
		}
	}
	return nil, f.err
}

// TestEachFails: Each says what Read says of every failure, a read that fails
// part way too, which gives no Total: -p's stderr and the screen's row are one
// set of words, and a part is no total.
func TestEachFails(t *testing.T) {
	ctx := context.Background()
	f := hostfake.New(host.Options{})
	ioErr := &fs.PathError{Op: "read", Path: logPath, Err: syscall.EIO}
	refused := &fs.PathError{Op: "open", Path: logPath, Err: fs.ErrPermission}
	for _, tt := range []struct {
		name   string
		after  []string
		err    error
		reason string
	}{
		{"part way", []string{"one", "two"}, ioErr, "input/output error"},
		{"at the open", nil, refused, "permission denied"},
		{"not a regular file", nil, fmt.Errorf("host: %w", fs.ErrInvalid), "not a regular file"},
		{"a link", nil, &fs.PathError{Op: "open", Path: logPath, Err: host.ErrSymlink}, host.ErrSymlink.Error()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sys := failingScan{f, tt.after, tt.err}
			var lines []string
			each := logview.Each(ctx, sys, func(l logview.Line) bool {
				lines = append(lines, l.Text)
				return true
			})
			read := logview.Read(ctx, sys)
			if !slices.Equal(lines, tt.after) {
				t.Errorf("gave %q", lines)
			}
			for name, r := range map[string]logview.Reading{"Each": each, "Read": read} {
				if r.State != logview.Unreadable || r.Reason != tt.reason || r.Total != 0 || r.Last != nil || !errors.Is(r.Err, tt.err) {
					t.Errorf("%s: %+v", name, r)
				}
			}
		})
	}
}

// TestEachMissing: no file, or nothing in it, is what Read says.
func TestEachMissing(t *testing.T) {
	ctx := context.Background()
	never := func(logview.Line) bool { t.Error("a line"); return false }
	if r := logview.Each(ctx, hostfake.New(host.Options{}), never); r.State != logview.NoLog {
		t.Errorf("%+v", r)
	}
	if r := logview.Each(ctx, logHost(t, ""), never); r.State != logview.Empty {
		t.Errorf("%+v", r)
	}
}

// TestHoldsAnything: Last Login's blank guard on the same seam: whether a file
// can be read and holds anything is its first line, a binary file's included.
func TestHoldsAnything(t *testing.T) {
	holds := func(sys host.System, name string) (bool, error) {
		found := false
		_, err := sys.ScanLines(context.Background(), name, func(host.Line) bool {
			found = true
			return false
		})
		return found, err
	}
	f := logHost(t, "")
	f.AddFile("/var/log/wtmp", bytes.Repeat([]byte{0}, 1<<20), 0o664, 0, 0)
	f.AddFile("/var/log/btmp", nil, 0o660, 0, 0)
	if got, err := holds(f, "/var/log/wtmp"); !got || err != nil {
		t.Errorf("wtmp: %t, %v", got, err)
	}
	if got, err := holds(f, "/var/log/btmp"); got || err != nil {
		t.Errorf("btmp: %t, %v", got, err)
	}
	if got, err := holds(f, "/var/log/lastlog"); got || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lastlog: %t, %v", got, err)
	}
}
