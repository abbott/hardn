//go:build linux || darwin

package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStdin checks that Stdin is one Reader over os.Stdin for the whole
// process, a terminal Reader exactly when os.Stdin is a terminal.
func TestStdin(t *testing.T) {
	r := Stdin()
	if r != Stdin() {
		t.Fatal("Stdin() returned a second Reader")
	}
	if r.in != os.Stdin {
		t.Errorf("Stdin() reads %v, want os.Stdin", r.in)
	}
	if r.tty != isTerminal(os.Stdin) {
		t.Errorf("Stdin().tty = %v, want %v", r.tty, isTerminal(os.Stdin))
	}
}

func TestIsTerminal(t *testing.T) {
	_, slave := openPTY(t)
	pr, _ := pipe(t)
	file, err := os.Create(filepath.Join(t.TempDir(), "answers"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	closed, _ := pipe(t)
	_ = closed.Close()
	tests := []struct {
		name string
		in   any
		want bool
	}{
		{"a pseudo-terminal", slave, true},
		{"a pipe", pr, false},
		{"a regular file", file, false},
		{"a closed file", closed, false},
		{"not a file", strings.NewReader("q\n"), false},
	}
	for _, tt := range tests {
		if got := isTerminal(tt.in); got != tt.want {
			t.Errorf("%s: isTerminal() = %v, want %v", tt.name, got, tt.want)
		}
	}
}
