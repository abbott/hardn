package uitest

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// failing is a writer that always fails.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("echo failed") }

// TestTypist checks what each Read returns and echoes: a line at a time,
// ended with Enter, echoed without it when the line is started, split
// across reads too small for it, then the end of the input.
func TestTypist(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		buf    int
		reads  []string // what each Read returns, up to the end of the input
		echoes []string // the echo after each Read
	}{
		{"none", nil, 8, nil, nil},
		{"one line", []string{"y"}, 8, []string{"y\n"}, []string{"y"}},
		{"an empty line", []string{""}, 8, []string{"\n"}, []string{""}},
		{"lines in order", []string{"1", "george"}, 8, []string{"1\n", "george\n"}, []string{"1", "1george"}},
		{"split across reads", []string{"bos-san-l01"}, 4, []string{"bos-", "san-", "l01\n"}, []string{"bos-san-l01", "bos-san-l01", "bos-san-l01"}},
	}
	for _, tt := range tests {
		echo := new(strings.Builder)
		ty := NewTypist(echo, tt.lines...)
		var reads, echoes []string
		for {
			p := make([]byte, tt.buf)
			n, err := ty.Read(p)
			if errors.Is(err, io.EOF) {
				if n != 0 {
					t.Errorf("%s: %d bytes with the end of the input", tt.name, n)
				}
				break
			}
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			reads, echoes = append(reads, string(p[:n])), append(echoes, echo.String())
		}
		if strings.Join(reads, "|") != strings.Join(tt.reads, "|") || strings.Join(echoes, "|") != strings.Join(tt.echoes, "|") {
			t.Errorf("%s: reads %q, echoes %q; want %q, %q", tt.name, reads, echoes, tt.reads, tt.echoes)
		}
		if ty.Unread() != 0 {
			t.Errorf("%s: %d lines unread at the end", tt.name, ty.Unread())
		}
	}
}

// TestTypistUnread checks that Unread counts the lines not yet started,
// and that a failed echo returns its error and types nothing.
func TestTypistUnread(t *testing.T) {
	ty := NewTypist(io.Discard, "1", "2", "3")
	if n := ty.Unread(); n != 3 {
		t.Errorf("before any read: %d unread, want 3", n)
	}
	p := make([]byte, 1)
	if _, err := ty.Read(p); err != nil || ty.Unread() != 2 {
		t.Errorf("one line started: %v, %d unread, want 2", err, ty.Unread())
	}
	ty = NewTypist(failing{}, "y")
	if n, err := ty.Read(make([]byte, 8)); n != 0 || err == nil || err.Error() != "echo failed" {
		t.Errorf("a failed echo: %d bytes, error %v; want 0 and the echo's error", n, err)
	}
}
