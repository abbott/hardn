//go:build linux || darwin

package prompt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// guard keeps SIGINT from ending the test binary while a test sends it:
// one that lands outside ReadLine's relay reaches guard rather than taking
// its default action. None of this package's tests runs in parallel, so
// no other ReadLine can take a signal meant for the one under test.
func guard(t *testing.T) {
	c := make(chan os.Signal, 64)
	signal.Notify(c, os.Interrupt)
	t.Cleanup(func() { signal.Stop(c) })
}

// interrupted runs r.ReadLine and sends SIGINT to the process every 10 ms
// until it returns, as from Ctrl-C: the first that lands while ReadLine
// waits ends it. It fails the test unless ReadLine reports the interrupt.
func interrupted(t *testing.T, r *Reader) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := r.ReadLine()
		done <- err
	}()
	timeout := time.After(10 * time.Second)
	for {
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if !errors.Is(err, ErrInterrupted) {
				t.Fatalf("ReadLine() = %v, want an interrupt", err)
			}
			return
		case <-time.After(10 * time.Millisecond):
		case <-timeout:
			t.Fatal("ReadLine did not return on SIGINT")
		}
	}
}

// pipe returns the two ends of an os.Pipe, closed when the test ends.
func pipe(t *testing.T) (r, w *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	return r, w
}

// write writes s to w.
func write(t *testing.T, w io.Writer, s string) {
	t.Helper()
	if _, err := io.WriteString(w, s); err != nil {
		t.Fatal(err)
	}
}

// wantLines reads len(want) lines from r and checks them.
func wantLines(t *testing.T, r *Reader, want ...string) {
	t.Helper()
	for _, w := range want {
		if line, err := r.ReadLine(); line != w || err != nil {
			t.Fatalf("ReadLine() = %q, %v; want %q", line, err, w)
		}
	}
}

func TestInterrupt(t *testing.T) {
	guard(t)
	t.Run("no read outlives the call", func(t *testing.T) {
		pr, pw := pipe(t)
		r := New(pr, false)
		interrupted(t, r)
		// Anything still reading the pipe would race this direct read.
		write(t, pw, "direct\n")
		if err := pr.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, len("direct\n"))
		if _, err := io.ReadFull(pr, b); err != nil || string(b) != "direct\n" {
			t.Fatalf("direct read = %q, %v: another reader took the line", b, err)
		}
		if err := pr.SetReadDeadline(time.Time{}); err != nil {
			t.Fatal(err)
		}
		write(t, pw, "one\ntwo\n")
		wantLines(t, r, "one", "two")
		_ = pw.Close()
		if _, err := r.ReadLine(); !errors.Is(err, ErrEndOfInput) {
			t.Errorf("at the end of the pipe, ReadLine() = %v", err)
		}
	})
	t.Run("a partial line is kept", func(t *testing.T) {
		pr, pw := pipe(t)
		r := New(pr, false)
		write(t, pw, "par")
		interrupted(t, r)
		write(t, pw, "tial\n")
		wantLines(t, r, "partial")
	})
	t.Run("on a terminal", func(t *testing.T) {
		m, s := openPTY(t)
		r := New(s, true)
		interrupted(t, r)
		write(t, m, "after\n")
		wantLines(t, r, "after")
		write(t, m, "\x04") // Ctrl-D (VEOF) on an empty line
		if _, err := r.ReadLine(); !errors.Is(err, ErrEndOfInput) {
			t.Fatalf("after Ctrl-D, ReadLine() = %v, want the end of input", err)
		}
		write(t, m, "again\n") // a terminal goes on after Ctrl-D
		wantLines(t, r, "again")
	})
}

// TestInterruptChild runs this test binary as a child process, in one of
// childModes, to check what SIGINT does once no test guards against it,
// and that Reraise ends the process by SIGINT, or with ExitInterrupted
// where SIGINT is ignored.
func TestInterruptChild(t *testing.T) {
	if mode := os.Getenv("HARDN_PROMPT_CHILD"); mode != "" {
		childModes[mode]()
		os.Exit(0)
	}
	tests := []struct {
		mode     string
		signaled bool // the child died of SIGINT; else it exited with code
		code     int
	}{
		{"restored", true, 0},
		{"ignored", false, 0},
		{"reraise", true, 0},
		{"reraise-ignored", false, ExitInterrupted},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptChild$")
			cmd.Env = append(os.Environ(), "HARDN_PROMPT_CHILD="+tt.mode)
			err := cmd.Run()
			var signaled bool
			code := 0
			if exit := (*exec.ExitError)(nil); errors.As(err, &exit) {
				ws, ok := exit.Sys().(syscall.WaitStatus)
				signaled = ok && ws.Signaled() && ws.Signal() == syscall.SIGINT
				code = exit.ExitCode()
			} else if err != nil {
				t.Fatalf("child: %v", err)
			}
			if signaled != tt.signaled || !signaled && code != tt.code {
				t.Errorf("child: %v; want it ended by SIGINT: %v, else exit status %d", err, tt.signaled, tt.code)
			}
		})
	}
}

// childModes are what TestInterruptChild's child does. Each returns for
// the child to exit 0, or exits with another code on a failure.
var childModes = map[string]func(){
	// SIGINT takes its default action again once ReadLine returns: the
	// child dies of it.
	"restored": func() {
		pr, pw, err := os.Pipe()
		if err != nil {
			os.Exit(3)
		}
		_, _ = pw.Write([]byte("x\n"))
		if line, err := New(pr, false).ReadLine(); line != "x" || err != nil {
			os.Exit(4)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		time.Sleep(10 * time.Second)
	},
	// An ignored SIGINT stays ignored: it neither ends the child nor
	// interrupts the read.
	"ignored": func() {
		signal.Ignore(os.Interrupt)
		pr, pw, err := os.Pipe()
		if err != nil {
			os.Exit(3)
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
			time.Sleep(100 * time.Millisecond)
			_, _ = pw.Write([]byte("x\n"))
		}()
		if line, err := New(pr, false).ReadLine(); line != "x" || err != nil {
			os.Exit(4)
		}
	},
	// Reraise ends the child by SIGINT, undoing the signal.Notify that
	// would otherwise catch it.
	"reraise": func() {
		signal.Notify(make(chan os.Signal, 1), os.Interrupt)
		Reraise()
	},
	// With SIGINT ignored, Reraise cannot end the child by it, and exits
	// with ExitInterrupted instead.
	"reraise-ignored": func() {
		signal.Ignore(os.Interrupt)
		Reraise()
	},
}

// waitInput waits up to 5 s until fd has a line to read.
func waitInput(t *testing.T, f *os.File) {
	t.Helper()
	var set unix.FdSet
	fd := int(f.Fd())
	set.Set(fd)
	tv := unix.Timeval{Sec: 5}
	if n, err := unix.Select(fd+1, &set, nil, nil, &tv); n != 1 || err != nil {
		t.Fatalf("no input on the terminal: %d, %v", n, err)
	}
}

func TestTerminalTypeahead(t *testing.T) {
	tests := []struct {
		name string
		tty  bool
		want string
	}{
		{"flushed on a terminal Reader", true, "answer"},
		{"kept on a Reader not told it is a terminal", false, "stray"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, s := openPTY(t)
			write(t, m, "stray\nmore stray\npartial")
			waitInput(t, s)
			r := New(s, tt.tty)
			if err := r.Flush(); err != nil {
				t.Fatalf("Flush() = %v", err)
			}
			write(t, m, "answer\n")
			wantLines(t, r, tt.want)
		})
	}
	t.Run("the flush is a terminal's", func(t *testing.T) {
		// One read takes both lines; the failed flush keeps the second.
		pr, pw := pipe(t)
		write(t, pw, "first\nkept\n")
		_ = pw.Close()
		r := New(pr, true)
		wantLines(t, r, "first")
		if err := r.Flush(); !errors.Is(err, syscall.ENOTTY) {
			t.Errorf("Flush() on a pipe called a terminal = %v, want ENOTTY", err)
		}
		wantLines(t, r, "kept")
	})
	t.Run("a pipe is never flushed", func(t *testing.T) {
		// Scripted answers queued on a pipe, as with hardn < answers:
		// Ask takes each in turn, then the end of the input aborts.
		pr, pw := pipe(t)
		write(t, pw, "1\n2\n3\n")
		_ = pw.Close()
		r := New(pr, false)
		for _, want := range []string{"1", "2", "3"} {
			if line, err := r.Ask(io.Discard, ">"); line != want || err != nil {
				t.Fatalf("Ask() = %q, %v; want %q", line, err, want)
			}
		}
		if _, err := r.Ask(io.Discard, ">"); !errors.Is(err, ErrEndOfInput) {
			t.Errorf("at the end of the pipe, Ask() = %v", err)
		}
	})
}

// typist records Ask's writes and, at the first, the prompt, types keys
// at the terminal's master, as the user does once the prompt shows: after
// Ask's flush.
type typist struct {
	t    *testing.T
	m    *os.File
	keys string
	writes
}

func (ty *typist) Write(p []byte) (int, error) {
	if len(ty.writes) == 0 {
		write(ty.t, ty.m, ty.keys)
	}
	return ty.writes.Write(p)
}

// TestTerminalAsk checks on a pseudo-terminal where Ask ends the prompt's
// line: only where the terminal echoed no Enter.
func TestTerminalAsk(t *testing.T) {
	tests := []struct {
		name   string
		keys   string
		answer string
		err    error
		want   []string // the writes
	}{
		{"Enter", "abc\n", "abc", nil, []string{"Q: "}},
		{"Ctrl-D after the answer", "abc\x04\x04", "abc", nil, []string{"Q: ", "\n"}},
		{"Ctrl-D on an empty line", "\x04", "", ErrAborted, []string{"Q: ", "\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, s := openPTY(t)
			w := &typist{t: t, m: m, keys: tt.keys}
			got, err := New(s, true).Ask(w, "Q:")
			if got != tt.answer || !errors.Is(err, tt.err) {
				t.Errorf("Ask() = %q, %v; want %q, %v", got, err, tt.answer, tt.err)
			}
			if fmt.Sprintf("%q", w.writes) != fmt.Sprintf("%q", tt.want) {
				t.Errorf("writes = %q, want %q", w.writes, tt.want)
			}
		})
	}
}
