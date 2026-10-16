package prompt

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

// MaxLine is the longest line ReadLine returns, in bytes, not counting its
// line ending: 64 KiB, as bufio.MaxScanTokenSize. The longest answer hardn
// asks for, a pasted SSH public key, is a few KiB, and a terminal holds
// less per line (4095 bytes on Linux, MAX_CANON 1024 on macOS); the limit
// stops a runaway pipe from exhausting memory.
const MaxLine = 64 * 1024

// chunk is the most one read of the input asks for.
const chunk = 4096

// maxEmptyReads is how many reads in a row may return no data and no
// error before ReadLine gives up with io.ErrNoProgress, as bufio does.
const maxEmptyReads = 100

// ExitInterrupted is the exit status for a run that a prompt aborted:
// 130, as a shell reports a process that Ctrl-C (SIGINT, 2) ended, 128 +
// 2. It is one of the exit codes brief Phase 3 fixes.
const ExitInterrupted = 130

var (
	// ErrAborted reports that the user aborted a prompt: the input ended
	// or Ctrl-C interrupted the read. Test for it with errors.Is.
	ErrAborted = errors.New("prompt: aborted")
	// ErrLineTooLong reports a line longer than MaxLine. The line is
	// dropped up to its line ending, so the next ReadLine returns the
	// line after it.
	ErrLineTooLong = errors.New("prompt: line too long")

	errEnded       = fmt.Errorf("%w: end of input", ErrAborted)
	errInterrupted = fmt.Errorf("%w: interrupted", ErrAborted)
	// errWoken reports that a wait ended on the wake pipe (wait).
	errWoken = errors.New("prompt: woken")
)

// ExitCode returns ExitInterrupted and true when err is or wraps
// ErrAborted, and 0 and false otherwise: the exit code for any other
// outcome (brief Phase 3: 0, 1, 2 or 3) is the caller's.
func ExitCode(err error) (int, bool) {
	if errors.Is(err, ErrAborted) {
		return ExitInterrupted, true
	}
	return 0, false
}

// Reader reads lines from the input, as the package doc describes. Build
// it with New. A Reader is not safe for concurrent use.
type Reader struct {
	in  io.Reader
	tty bool
	buf []byte // read from in and not yet returned
	err error  // in's error, reported once buf holds no more lines
	// skip is set while the rest of an over-long line is still to come
	// and to be dropped.
	skip bool
}

// New returns a Reader over in. Set tty when in is the terminal
// (term.Env.InputTTY), as os.Stdin is when hardn runs interactively: Flush
// then discards typeahead. When in is a file, such as os.Stdin or a pipe,
// Ctrl-C interrupts a read on Linux and macOS (see the package doc).
func New(in io.Reader, tty bool) *Reader {
	return &Reader{in: in, tty: tty}
}

// ReadLine reads the next line and returns it without its line ending. It
// returns an error wrapping ErrAborted at the end of the input or on
// Ctrl-C, ErrLineTooLong for a line longer than MaxLine, and the input's
// own error, wrapped, for any other failure. A last line with no line
// ending is returned before the end of the input is reported.
func (r *Reader) ReadLine() (string, error) {
	it, err := r.intercept()
	if err != nil {
		return "", err
	}
	err = r.fill(it)
	if it.stop() {
		return "", errInterrupted
	}
	if err != nil {
		return "", err
	}
	return r.take()
}

// Flush discards typeahead on a terminal Reader: the input the terminal
// holds that nothing has read yet, and the Reader's own buffered bytes. An
// end of input or error already reported by the input is kept. On a
// Reader that is not a terminal it does nothing.
func (r *Reader) Flush() error {
	if !r.tty {
		return nil
	}
	r.buf, r.skip = nil, false
	c, ok := r.in.(syscall.Conn)
	if !ok {
		return nil
	}
	rc, err := c.SyscallConn()
	if err != nil {
		return fmt.Errorf("prompt: flush: %w", err)
	}
	var ferr error
	if err := rc.Control(func(fd uintptr) { ferr = flushInput(int(fd)) }); err != nil {
		return fmt.Errorf("prompt: flush: %w", err)
	}
	if ferr != nil {
		return fmt.Errorf("prompt: flush: %w", ferr)
	}
	return nil
}

// Ask asks one question. It discards typeahead (Flush), writes prompt and
// exactly one space to w in a single write, and reads the answer
// (ReadLine). prompt is a rendered component.PromptLine, which ends at its
// colon, and w is term.Env.Writer around stdout. Ask ends the prompt's
// line itself when the input is not a terminal, or the user aborted, so
// that no echoed Enter ended it: what the caller writes next starts on a
// line of its own.
func (r *Reader) Ask(w io.Writer, prompt string) (string, error) {
	if err := r.Flush(); err != nil {
		return "", err
	}
	if _, err := io.WriteString(w, prompt+" "); err != nil {
		return "", fmt.Errorf("prompt: write: %w", err)
	}
	line, err := r.ReadLine()
	if !r.tty || errors.Is(err, ErrAborted) {
		if _, werr := io.WriteString(w, "\n"); werr != nil && err == nil {
			return "", fmt.Errorf("prompt: write: %w", werr)
		}
	}
	return line, err
}

// fill reads from the input until take can return without reading: the
// buffer holds a line ending or more than a line may hold, or the input
// has ended or failed. It waits through it, so that Ctrl-C can wake it.
func (r *Reader) fill(it *interrupt) error {
	for empty := 0; !r.ready(); {
		if err := it.wait(); err != nil {
			return err
		}
		var b [chunk]byte
		n, err := r.in.Read(b[:])
		r.buf, r.err = append(r.buf, b[:n]...), err
		if n > 0 || err != nil {
			empty = 0
		} else if empty++; empty == maxEmptyReads {
			r.err = io.ErrNoProgress
		}
	}
	return nil
}

// ready reports whether take can return without reading more. It first
// drops what has arrived of an over-long line.
func (r *Reader) ready() bool {
	if r.skip {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			r.buf = nil
			return r.err != nil
		}
		r.buf, r.skip = r.buf[i+1:], false
	}
	return r.err != nil || bytes.IndexByte(r.buf, '\n') >= 0 ||
		len(bytes.TrimSuffix(r.buf, []byte("\r"))) > MaxLine
}

// take returns the next line from the buffer, once fill has made it
// ready, or reports the end of the input or its error.
func (r *Reader) take() (string, error) {
	if r.skip {
		// The end of the input ends the over-long line too, so what comes
		// after it, as on a terminal after Ctrl-D, is a new line. Another
		// error leaves the rest of the line to drop.
		r.skip = !errors.Is(r.err, io.EOF)
		return "", r.end()
	}
	line, rest, found := bytes.Cut(r.buf, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	switch {
	case len(line) > MaxLine:
		r.buf, r.skip = rest, !found
		return "", ErrLineTooLong
	case found:
		r.buf = rest
		return string(line), nil
	case len(r.buf) > 0 && errors.Is(r.err, io.EOF):
		// The last line has no line ending. r.err stays, so the next
		// call reports the end.
		r.buf = nil
		return string(line), nil
	}
	return "", r.end()
}

// end reports the input's error once: the end of the input as an abort,
// any other error wrapped. As with bufio, the next call reads again.
func (r *Reader) end() error {
	err := r.err
	r.err = nil
	if errors.Is(err, io.EOF) {
		return errEnded
	}
	return fmt.Errorf("prompt: read: %w", err)
}

// interrupt relays os.Interrupt during one ReadLine call (see the package
// doc). Its methods do nothing on a nil interrupt, which ReadLine uses
// when the input cannot be waited on.
type interrupt struct {
	conn  syscall.Conn
	sig   chan os.Signal
	done  chan struct{}
	wg    sync.WaitGroup
	fired atomic.Bool
	// wake is the read end of a pipe whose write end, kick, the relay
	// writes to on os.Interrupt, so that a wait on the input wakes.
	wake, kick *os.File
}

// intercept starts relaying os.Interrupt for one call, when the input is
// a file this platform can wait on and SIGINT is not ignored; otherwise it
// returns nil.
func (r *Reader) intercept() (*interrupt, error) {
	c, ok := r.in.(syscall.Conn)
	if !ok || !interruptible || signal.Ignored(os.Interrupt) {
		return nil, nil
	}
	wake, kick, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("prompt: %w", err)
	}
	it := &interrupt{conn: c, sig: make(chan os.Signal, 1), done: make(chan struct{}), wake: wake, kick: kick}
	signal.Notify(it.sig, os.Interrupt)
	it.wg.Go(func() {
		select {
		case <-it.sig:
			it.fired.Store(true)
			_, _ = kick.Write([]byte{0})
		case <-it.done:
		}
	})
	return it, nil
}

// wait blocks until the input has something to read, or until
// os.Interrupt wakes it (errWoken).
func (it *interrupt) wait() error {
	if it == nil {
		return nil
	}
	return wait(it.conn, it.wake)
}

// stop gives os.Interrupt back its earlier handling, ends the relay and
// reports whether os.Interrupt arrived during the call. signal.Stop
// guarantees that a signal is either delivered to it.sig before Stop
// returns or takes its default action, so none is missed.
func (it *interrupt) stop() bool {
	if it == nil {
		return false
	}
	signal.Stop(it.sig)
	close(it.done)
	it.wg.Wait()
	_ = it.wake.Close()
	_ = it.kick.Close()
	return it.fired.Load() || len(it.sig) > 0
}
