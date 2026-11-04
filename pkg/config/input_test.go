package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
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

// useTerminals sets whether the first-run question finds stdin and stdout
// terminals, until the test ends.
func useTerminals(t *testing.T, tty bool) {
	t.Helper()
	was := terminals
	terminals = func() bool { return tty }
	t.Cleanup(func() { terminals = was })
}

// TestShouldCreateDefaultConfig checks the first-run question on the
// prompt kit (Ask): on a terminal its words, then Enter, y or yes create
// hardn.yml and any other answer does not; the end of the input is no,
// where origin/main took it for yes (refactor plan, next/p3-startup).
func TestShouldCreateDefaultConfig(t *testing.T) {
	const question = FirstRunQuestion + " "
	tests := []struct {
		name   string
		script string
		want   bool
		out    string // after the question
	}{
		{"Enter takes the default", "\n", true, ""},
		{"y", "y\n", true, ""},
		{"YES with spaces and CRLF", " YES \r\n", true, ""},
		{"n", "n\n", false, ""},
		{"no", "no\n", false, ""},
		{"anything else", "nope\n", false, ""},
		{"a last line with no Enter", "n", false, "\n"},
		{"the end of the input is no", "", false, "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTerminals(t, true)
			useReader(t, prompt.New(strings.NewReader(tt.script), true))
			var got bool
			out, code, ended := ask(t, func() { got = ShouldCreateDefaultConfig() })
			if ended || got != tt.want {
				t.Errorf("ShouldCreateDefaultConfig() = %v (ended %v, %d), want %v", got, ended, code, tt.want)
			}
			if out != question+tt.out {
				t.Errorf("printed %q, want %q", out, question+tt.out)
			}
		})
	}
	// Without a terminal, stdin's or stdout's, nothing is asked or read:
	// no, so hardn goes on with the defaults.
	t.Run("no terminal", func(t *testing.T) {
		useTerminals(t, false)
		r := prompt.New(strings.NewReader("y\n"), false)
		useReader(t, r)
		var got bool
		out, _, ended := ask(t, func() { got = ShouldCreateDefaultConfig() })
		if got || ended || out != "" || r.Asked() {
			t.Errorf("ShouldCreateDefaultConfig() = %v (ended %v, asked %v); printed %q", got, ended, r.Asked(), out)
		}
		if line, err := r.ReadLine(); line != "y" || err != nil {
			t.Errorf("the input was read: next line %q, %v", line, err)
		}
	})
	t.Run("each answer is read once", func(t *testing.T) {
		useTerminals(t, true)
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
	// the input: no. prompt's tests flush a pseudo-terminal's queue.
	t.Run("typeahead is dropped on a terminal", func(t *testing.T) {
		useTerminals(t, true)
		useReader(t, prompt.New(strings.NewReader("y\ny\n"), true))
		var first, second bool
		ask(t, func() { first, second = ShouldCreateDefaultConfig(), ShouldCreateDefaultConfig() })
		if !first || second {
			t.Errorf("answers read as %v, %v; want true, false", first, second)
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

// TestCreateDefaultConfigUsername: the first-run save, on a terminal,
// writes no username where the question is answered with Enter alone (no
// "george" default: refactor plan §3.11), and the name typed where one
// is; the question offers no default.
func TestCreateDefaultConfigUsername(t *testing.T) {
	prevInteractive, prevExample := isInteractive, ensureExample
	isInteractive = func() bool { return true }
	ensureExample = func() error { return errors.New("test: /etc/hardn not written") }
	t.Cleanup(func() { isInteractive, ensureExample = prevInteractive, prevExample })
	for _, tc := range []struct {
		name, answers, want string
	}{
		{"Enter alone", "\n\n\n\n", ""},
		{"a name typed", "alice\n\n\n\n", "alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useReader(t, prompt.New(strings.NewReader(tc.answers), true))
			path := filepath.Join(t.TempDir(), "hardn.yml")
			var err error
			out, _, ended := ask(t, func() { err = CreateDefaultConfig(path, DefaultConfig()) })
			if err != nil || ended {
				t.Fatalf("CreateDefaultConfig: %v (ended %v)", err, ended)
			}
			if !strings.Contains(out, "Enter default username (optional, press Enter to skip): ") || strings.Contains(out, "george") {
				t.Errorf("the question: %q", out)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			c, err := LoadConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if c.Username != tc.want || strings.Contains(string(b), "george") {
				t.Errorf("username %q, want %q; the file:\n%s", c.Username, tc.want, b)
			}
		})
	}
}

// TestFirstRun: LoadConfig with no hardn.yml found writes one only where
// the first-run question is answered yes on a terminal. With no terminal,
// at the end of the input, or answered no, it writes nothing and returns
// the defaults, which cmd/hardn says it uses (NoneLoaded). HOME is the
// test's, where a user other than root creates hardn.yml
// (GetDefaultConfigLocation); -f names a file that is not there, so no
// other location is searched.
func TestFirstRun(t *testing.T) {
	prevInteractive, prevExample := isInteractive, ensureExample
	isInteractive = func() bool { return false } // no questions after the first
	ensureExample = func() error { return errors.New("test: /etc/hardn not written") }
	t.Cleanup(func() { isInteractive, ensureExample = prevInteractive, prevExample })
	for _, tc := range []struct {
		name, script string
		tty, created bool
	}{
		{"no terminal", "y\n", false, false},
		{"the end of the input", "", true, false},
		{"n", "n\n", true, false},
		{"y", "y\n", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.created && os.Geteuid() == 0 {
				t.Skip("as root hardn.yml is created in /etc/hardn")
			}
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("HARDN_CONFIG", "")
			useTerminals(t, tc.tty)
			useReader(t, prompt.New(strings.NewReader(tc.script), true))
			var c *Config
			var err error
			ask(t, func() { c, err = LoadConfig(filepath.Join(home, "absent.yml")) })
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(home, ".config", "hardn", "hardn.yml")
			_, statErr := os.Stat(path)
			if created := statErr == nil; created != tc.created || (c.Path() == path) != tc.created {
				t.Errorf("hardn.yml written %v, loaded from %q; want written %v", created, c.Path(), tc.created)
			}
			if !tc.created {
				if entries, _ := os.ReadDir(home); len(entries) != 0 {
					t.Errorf("wrote %v in HOME", entries)
				}
			}
		})
	}
}
