//go:build linux || darwin

package prompt

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/abbott/hardn/internal/ptytest"
)

// pause, typed, waits past escWait, so that an Escape before it stands
// alone.
const pause = "<pause>"

// choiceAsked is what AskChoice returned on a pseudo-terminal and what it
// wrote.
type choiceAsked struct {
	line, wrote string
	err         error
}

// askOn runs AskChoice on the slave s, types each of typed at the master m
// once the terminal is set for keys, and returns what AskChoice returned and
// wrote. It checks that the terminal's attributes are put back.
func askOn(t *testing.T, m, s *os.File, typed ...string) choiceAsked {
	t.Helper()
	before, err := getTermios(int(s.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	done := make(chan choiceAsked, 1)
	go func() {
		line, err := New(s, true).AskChoice(&out, "Enter selection:")
		done <- choiceAsked{line: line, err: err}
	}()
	ptytest.WaitKeys(t, s)
	for _, k := range typed {
		if k == pause {
			time.Sleep(2 * escWait)
			continue
		}
		if _, err := m.WriteString(k); err != nil {
			t.Fatal(err)
		}
	}
	var got choiceAsked
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("no answer to %q", typed)
	}
	got.wrote = out.String()
	after, err := getTermios(int(s.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if after.Lflag != before.Lflag || after.Cc != before.Cc {
		t.Errorf("%q: the terminal's attributes not put back: lflag %#x, want %#x", typed, after.Lflag, before.Lflag)
	}
	return got
}

// TestAskChoiceKeys: on a terminal a menu choice is read key by key: keys no
// answer has show nothing, what is typed is echoed, Backspace and Ctrl-U
// edit it, and Enter, keypad Enter or Ctrl-D after text gives it.
func TestAskChoiceKeys(t *testing.T) {
	for _, tt := range []struct {
		name        string
		typed       []string
		line, wrote string
	}{
		{"arrows in both modes, then a digit", []string{"\x1b[A\x1b[B\x1bOA\x1bOB1\r"}, "1", "Enter selection: 1\n"},
		{"Backspace", []string{"12\x7f3\r"}, "13", "Enter selection: 12\b \b3\n"},
		{"Ctrl-U", []string{"ab\x15c\r"}, "c", "Enter selection: ab\b \b\b \bc\n"},
		{"a lone Escape", []string{"\x1b", pause, "2\r"}, "2", "Enter selection: 2\n"},
		{"an arrow split across writes", []string{"\x1b[", "A3\r"}, "3", "Enter selection: 3\n"},
		{"the keypad in application mode", []string{"\x1bOq\x1bOp\x1bOM"}, "10", "Enter selection: 10\n"},
		{"Ctrl-D after text", []string{"q\x04"}, "q", "Enter selection: q\n"},
		{"Backspace with nothing typed", []string{"\x7f1\r"}, "1", "Enter selection: 1\n"},
		{"past maxChoice", []string{strings.Repeat("7", maxChoice+44) + "\r"}, strings.Repeat("7", maxChoice), "Enter selection: " + strings.Repeat("7", maxChoice) + "\n"},
	} {
		m, s := ptytest.Open(t)
		got := askOn(t, m, s, tt.typed...)
		if got.err != nil || got.line != tt.line || got.wrote != tt.wrote {
			t.Errorf("%s: %q, %v; wrote %q; want %q, %q", tt.name, got.line, got.err, got.wrote, tt.line, tt.wrote)
		}
	}
}

// TestAskChoiceAborts: Ctrl-C interrupts and Ctrl-D with nothing typed ends
// the input, as in a line; the prompt's line is ended.
func TestAskChoiceAborts(t *testing.T) {
	for _, tt := range []struct {
		typed string
		want  error
	}{{"1\x03", ErrInterrupted}, {"\x04", ErrEndOfInput}} {
		m, s := ptytest.Open(t)
		got := askOn(t, m, s, tt.typed)
		if !errors.Is(got.err, tt.want) || !errors.Is(got.err, ErrAborted) || !strings.HasSuffix(got.wrote, "\n") {
			t.Errorf("%q: %q, %v; wrote %q; want %v", tt.typed, got.line, got.err, got.wrote, tt.want)
		}
	}
}

// TestAskChoiceNoTerminalEcho: while keys are read the terminal echoes
// nothing, so an arrow's bytes never reach the screen; what is typed
// reaches it through AskChoice's writer alone.
func TestAskChoiceNoTerminalEcho(t *testing.T) {
	m, s := ptytest.Open(t)
	var echoed bytes.Buffer
	var mu sync.Mutex
	end := make(chan struct{})
	go func() {
		b := make([]byte, 256)
		for {
			n, err := m.Read(b)
			mu.Lock()
			echoed.Write(b[:n])
			marked := bytes.Contains(echoed.Bytes(), []byte("<end>"))
			mu.Unlock()
			if marked {
				close(end)
				return
			}
			if err != nil {
				return
			}
		}
	}()
	if got := askOn(t, m, s, "\x1b[A1\r"); got.line != "1" {
		t.Fatalf("AskChoice = %q, %v", got.line, got.err)
	}
	if _, err := s.WriteString("<end>"); err != nil { // the terminal's output, after any echo
		t.Fatal(err)
	}
	select {
	case <-end:
	case <-time.After(5 * time.Second):
		t.Fatal("the terminal's output did not arrive")
	}
	mu.Lock()
	defer mu.Unlock()
	if before, _, _ := bytes.Cut(echoed.Bytes(), []byte("<end>")); len(before) > 0 {
		t.Errorf("the terminal echoed %q", before)
	}
}

// TestAskChoiceSignal: a SIGHUP while keys are read finds the terminal put
// back before it takes its default action.
func TestAskChoiceSignal(t *testing.T) {
	m, s := ptytest.Open(t)
	go ptytest.Drain(m)
	type raised struct {
		sig       syscall.Signal
		canonical bool
	}
	got := make(chan raised, 1)
	old := raise
	t.Cleanup(func() { raise = old })
	raise = func(sig syscall.Signal) {
		tm, err := getTermios(int(s.Fd()))
		got <- raised{sig, err == nil && tm.Lflag&unix.ICANON != 0}
	}
	done := make(chan error, 1)
	go func() {
		_, err := New(s, true).AskChoice(&bytes.Buffer{}, "Enter selection:")
		done <- err
	}()
	ptytest.WaitKeys(t, s)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if r.sig != syscall.SIGHUP || !r.canonical {
			t.Errorf("raised %v with the terminal's line editing on %t; want SIGHUP, true", r.sig, r.canonical)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGHUP not raised")
	}
	if _, err := m.WriteString("\r"); err != nil { // hardn went on: the read ends at the next key
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrInterrupted) {
			t.Errorf("after a signal that did not end hardn: %v, want ErrInterrupted", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AskChoice did not return")
	}
}

// TestAskChoiceIgnoredSignals: a SIGHUP ignored when the read starts, as
// under nohup, stays ignored and ends nothing; and where SIGINT is ignored
// Ctrl-C does nothing, as in a line. It runs in a child, this test binary
// run again: a signal ignored in this process cannot be given back its
// handling (a later Notify and Stop restore signal.Ignore's SIG_IGN, which
// the tests after it, and their children, would inherit).
func TestAskChoiceIgnoredSignals(t *testing.T) {
	if os.Getenv("HARDN_PROMPT_KEYS_CHILD") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestAskChoiceIgnoredSignals$", "-test.count=1")
		cmd.Env = append(os.Environ(), "HARDN_PROMPT_KEYS_CHILD=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v\n%s", err, out)
		}
		return
	}
	m, s := ptytest.Open(t)
	go ptytest.Drain(m)
	signal.Ignore(syscall.SIGHUP, os.Interrupt)
	old := raise
	t.Cleanup(func() { raise = old })
	raised := make(chan syscall.Signal, 1)
	raise = func(sig syscall.Signal) { raised <- sig }
	done := make(chan choiceAsked, 1)
	go func() {
		line, err := New(s, true).AskChoice(&bytes.Buffer{}, "Enter selection:")
		done <- choiceAsked{line: line, err: err}
	}()
	ptytest.WaitKeys(t, s)
	if err := syscall.Kill(syscall.Getpid(), syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if ptytest.Canonical(t, s) {
		t.Error("an ignored SIGHUP put the terminal back")
	}
	if _, err := m.WriteString("\x031\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || got.line != "1" {
			t.Errorf("AskChoice = %q, %v; want 1", got.line, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AskChoice did not return")
	}
	select {
	case sig := <-raised:
		t.Errorf("raised %v", sig)
	default:
	}
}

// TestAskChoiceEndAlreadyRead: an end of input a line read before, after
// text ended by Ctrl-D, is reported at the menu, as a line reports it,
// without setting the terminal.
func TestAskChoiceEndAlreadyRead(t *testing.T) {
	_, s := openPTY(t)
	r := New(s, true)
	r.err = io.EOF
	var out bytes.Buffer
	if _, err := r.AskChoice(&out, "Enter selection:"); !errors.Is(err, ErrEndOfInput) || out.String() != "Enter selection: \n" {
		t.Errorf("AskChoice: %v; wrote %q", err, out.String())
	}
	if !ptytest.Canonical(t, s) {
		t.Error("the terminal was set for keys")
	}
}

// interruptingWriter sends the process SIGINT when the prompt is written,
// before it writes it. Its buffer is a field, not embedded, so that
// io.WriteString finds no WriteString and calls Write.
type interruptingWriter struct{ buf bytes.Buffer }

func (w *interruptingWriter) Write(p []byte) (int, error) {
	if w.buf.Len() == 0 {
		_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
		time.Sleep(50 * time.Millisecond)
	}
	return w.buf.Write(p)
}

// TestAskChoiceInterruptWhileWriting: a SIGINT from outside while the
// prompt is written, the terminal already set for keys, interrupts the
// read rather than ending hardn with the terminal so.
func TestAskChoiceInterruptWhileWriting(t *testing.T) {
	_, s := openPTY(t)
	before, err := getTermios(int(s.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(s, true).AskChoice(&interruptingWriter{}, "Enter selection:"); !errors.Is(err, ErrInterrupted) {
		t.Errorf("AskChoice: %v, want ErrInterrupted", err)
	}
	if after, err := getTermios(int(s.Fd())); err != nil || after.Lflag != before.Lflag {
		t.Errorf("the terminal's attributes not put back: %v", err)
	}
}
