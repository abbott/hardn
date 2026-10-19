package host

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTailLines(t *testing.T) {
	tests := []struct {
		name string
		out  string
		n    int
		want []string
	}{
		{"empty", "", 5, nil},
		{"n zero", "a\n", 0, nil},
		{"fewer lines than n", "a\nb\n", 5, []string{"a", "b"}},
		{"last n", "1\n2\n3\n4\n", 2, []string{"3", "4"}},
		{"no final newline", "1\n2", 1, []string{"2"}},
		{"crlf", "a\r\nb\r\n", 5, []string{"a", "b"}},
		{"blank lines kept", "a\n\nb\n", 3, []string{"a", "", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TailLines([]byte(tt.out), tt.n); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TailLines(%q, %d) = %q, want %q", tt.out, tt.n, got, tt.want)
			}
		})
	}
}

func TestFormatArgv(t *testing.T) {
	tests := []struct {
		argv []string
		want string
	}{
		{[]string{"ufw", "allow", "2230/tcp"}, "ufw allow 2230/tcp"},
		{[]string{"sh", "-c", "yes | ufw enable"}, `sh -c "yes | ufw enable"`},
		{[]string{"adduser", "--gecos", "", "alice"}, `adduser --gecos "" alice`},
		{[]string{"echo", "a;b", "$(id)"}, `echo "a;b" "$(id)"`},
	}
	for _, tt := range tests {
		if got := FormatArgv(tt.argv); got != tt.want {
			t.Errorf("FormatArgv(%q) = %s, want %s", tt.argv, got, tt.want)
		}
	}
}

func TestOptionsTail(t *testing.T) {
	for _, tt := range []struct{ in, want int }{{0, DefaultTailLines}, {-1, 0}, {5, 5}} {
		if got := (Options{TailLines: tt.in}).Tail(); got != tt.want {
			t.Errorf("Options{TailLines: %d}.Tail() = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestRunLogFormat(t *testing.T) {
	var buf bytes.Buffer
	l := NewRunLog(&buf)
	a := l.Begin([]string{"ufw", "status"}, -1)
	b := l.Begin([]string{"visudo", "-c", "-f", "-"}, 12)
	l.End(b, Result{Combined: []byte("parsed OK\n"), ExitCode: 0}, nil)
	l.End(a, Result{Combined: []byte("Status: inactive"), ExitCode: 1},
		&ExitError{Argv: []string{"ufw", "status"}, ExitCode: 1})
	c := l.Begin([]string{"nope"}, -1)
	l.End(c, Result{ExitCode: -1}, errors.New("host: \"nope\": command not found"))

	want := strings.Join([]string{
		"run 1: $ ufw status",
		"run 2: $ visudo -c -f -",
		"run 2: stdin: 12 bytes (not logged)",
		"run 2: | parsed OK",
		"run 2: exit 0",
		"run 1: | Status: inactive",
		"run 1: exit 1 (host: ufw status: exit status 1)",
		"run 3: $ nope",
		`run 3: not run or not finished: host: "nope": command not found`,
	}, "\n") + "\n"
	if got := buf.String(); got != want {
		t.Errorf("run log:\n%s\nwant:\n%s", got, want)
	}
	if l.Err() != nil {
		t.Errorf("Err() = %v, want nil", l.Err())
	}
}

type failWriter struct{ n int }

func (w *failWriter) Write(p []byte) (int, error) {
	w.n++
	return 0, errors.New("disk full")
}

func TestRunLogBestEffort(t *testing.T) {
	w := &failWriter{}
	l := NewRunLog(w)
	l.End(l.Begin([]string{"true"}, -1), Result{}, nil)
	if l.Err() == nil || w.n != 2 {
		t.Fatalf("Err() = %v after %d writes, want the first write error after 2", l.Err(), w.n)
	}
}

func TestExitError(t *testing.T) {
	inner := &exec.ExitError{}
	var err error = &ExitError{Argv: []string{"sshd", "-t"}, ExitCode: 255, Err: inner}
	var xe *ExitError
	if !errors.As(err, &xe) || xe.ExitCode != 255 {
		t.Fatalf("errors.As(*ExitError) failed for %v", err)
	}
	var execErr *exec.ExitError
	if !errors.As(err, &execErr) {
		t.Fatal("errors.As(*exec.ExitError) failed: Unwrap lost the os/exec error")
	}
	if got := (&ExitError{Argv: []string{"ufw", "enable"}, ExitCode: 1}).Error(); got != "host: ufw enable: exit status 1" {
		t.Errorf("Error() = %q", got)
	}
}

func TestOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	uid, gid, ok := Owner(fi)
	if !ok || uid != os.Getuid() || gid != os.Getgid() {
		t.Errorf("Owner(real) = %d, %d, %t; want %d, %d, true", uid, gid, ok, os.Getuid(), os.Getgid())
	}
	if _, _, ok := Owner(fakeInfo{}); ok {
		t.Error("Owner(info without owner) reported one")
	}
	if uid, gid, ok := Owner(fakeInfo{sys: &Ownership{UID: 1000, GID: 1001}}); !ok || uid != 1000 || gid != 1001 {
		t.Errorf("Owner(Ownership) = %d, %d, %t", uid, gid, ok)
	}
}

type fakeInfo struct {
	fs.FileInfo
	sys any
}

func (f fakeInfo) Sys() any { return f.sys }
