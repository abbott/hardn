package config

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/ui/prompt"
)

// exited is what the stubbed exit panics with, in place of ending the
// test binary: the exit status hardn would have ended with, or sigint.
type exited int

// sigint is what the stubbed reraise panics with: hardn ended by SIGINT
// after Ctrl-C (prompt.Reraise), which a parent sees as a signal, not an
// exit status.
const sigint exited = -2

// useReader makes the config prompts read from r and stubs exit and
// reraise, until the test ends.
func useReader(t *testing.T, r *prompt.Reader) {
	t.Helper()
	prevInput, prevExit, prevReraise := input, exit, reraise
	input = func() *prompt.Reader { return r }
	exit = func(code int) { panic(exited(code)) }
	reraise = func() { panic(sigint) }
	t.Cleanup(func() { input, exit, reraise = prevInput, prevExit, prevReraise })
}

// ask runs fn with stdout captured and returns what it printed, and the
// exit status it ended hardn with, if it did.
func ask(t *testing.T, fn func()) (out string, code int, ended bool) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	func() {
		defer func() {
			os.Stdout = orig
			if r := recover(); r != nil {
				e, ok := r.(exited)
				if !ok {
					panic(r)
				}
				code, ended = int(e), true
			}
		}()
		fn()
	}()
	_ = w.Close()
	return <-done, code, ended
}

// TestShouldCreateDefaultConfig checks the first-run question on the
// shared reader: its words and its answers are origin/main's, and, as
// with the bufio reader it replaced, the end of the input takes the
// default, yes, and nothing more is printed.
func TestShouldCreateDefaultConfig(t *testing.T) {
	const question = "No configuration file found. Would you like to create one? [Y/n]\n"
	tests := []struct {
		name   string
		script string
		want   bool
	}{
		{"Enter takes the default", "\n", true},
		{"y", "y\n", true},
		{"YES with spaces and CRLF", " YES \r\n", true},
		{"n", "n\n", false},
		{"no", "no\n", false},
		{"anything else", "nope\n", false},
		{"a last line with no Enter", "n", false},
		{"the end of the input takes the default", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useReader(t, prompt.New(strings.NewReader(tt.script), false))
			var got bool
			out, code, ended := ask(t, func() { got = ShouldCreateDefaultConfig() })
			if ended || got != tt.want {
				t.Errorf("ShouldCreateDefaultConfig() = %v (ended %v, %d), want %v", got, ended, code, tt.want)
			}
			if out != question {
				t.Errorf("printed %q, want %q", out, question)
			}
		})
	}
	t.Run("each answer is read once", func(t *testing.T) {
		useReader(t, prompt.New(strings.NewReader("n\ny\n"), false))
		var first, second bool
		ask(t, func() { first, second = ShouldCreateDefaultConfig(), ShouldCreateDefaultConfig() })
		if first || !second {
			t.Errorf("answers read as %v, %v; want false, true", first, second)
		}
	})
	// On a terminal Reader, the second answer, already in its buffer
	// when the first is read, is typeahead. The question can follow a
	// long step, so it is dropped: the second question meets the end of
	// the input and takes the default, yes. prompt's tests flush a
	// pseudo-terminal's queue.
	t.Run("typeahead is dropped on a terminal", func(t *testing.T) {
		useReader(t, prompt.New(strings.NewReader("n\nn\n"), true))
		var first, second bool
		ask(t, func() { first, second = ShouldCreateDefaultConfig(), ShouldCreateDefaultConfig() })
		if first || !second {
			t.Errorf("answers read as %v, %v; want false, true", first, second)
		}
	})
}

// TestReadAnswer checks the read behind CreateDefaultConfig's questions,
// which writes /etc/hardn/hardn.yml.example and is not driven here: the
// questions are asked back to back, so an answer typed ahead on a
// terminal is kept for the question after it, as origin/main's bufio
// reader kept it; the end of the input reads as an empty answer.
func TestReadAnswer(t *testing.T) {
	useReader(t, prompt.New(strings.NewReader("alice\n2230"), true))
	var got []string
	out, _, ended := ask(t, func() {
		for range 3 {
			got = append(got, readAnswer())
		}
	})
	if want := []string{"alice", "2230", ""}; !slices.Equal(got, want) || ended {
		t.Errorf("read %q (ended %v), want %q", got, ended, want)
	}
	if out != "" {
		t.Errorf("printed %q, want nothing", out)
	}
}
