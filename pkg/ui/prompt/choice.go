package prompt

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"
)

// maxChoice is the longest menu choice AskChoice keeps, in bytes: a menu's
// answers are a number or a letter, and the cap keeps a held key or a
// pasted block from growing the answer without end.
const maxChoice = 256

// escWait is how long AskChoice waits for the rest of a key's escape
// sequence when what it has read ends inside one, before taking what came
// as a key of its own and dropping it: a lone Escape. A terminal sends a
// key's sequence in one write, so its bytes arrive together; 100 ms is
// vim's ttimeoutlen in defaults.vim.
const escWait = 100 * time.Millisecond

// AskChoice asks for a menu choice. It is Ask, but where the Reader is a
// terminal on Linux or macOS it reads keys for this one prompt rather than
// a line (see the package doc, Menu choices): the terminal's own echo and
// line editing are off, so a key that is not an answer, such as an arrow,
// shows nothing; it echoes what is typed to w itself, and ends the
// prompt's line with "\n" when the answer is given or the prompt aborted.
// Anywhere else, and where the terminal's attributes cannot be read or set,
// it asks as Ask does. It returns what Ask returns: the answer, or an error
// wrapping ErrAborted on Ctrl-C (ErrInterrupted) or on Ctrl-D with nothing
// typed (ErrEndOfInput).
func (r *Reader) AskChoice(w io.Writer, prompt string) (string, error) {
	if err := r.Flush(); err != nil {
		return "", err
	}
	if r.err != nil { // an end of input or an error already read: a line reports it
		return r.lineChoice(w, prompt)
	}
	// os.Interrupt is relayed, as ReadLine relays it, for as long as the
	// terminal is set for keys and a little longer, so that a SIGINT from
	// outside, while the prompt is written too, wakes the read rather than
	// ending hardn with the terminal so.
	it, err := r.intercept()
	if err != nil {
		return "", err
	}
	km := r.startKeys()
	if km == nil {
		it.stop()
		return r.lineChoice(w, prompt)
	}
	var line string
	var fired bool
	func() {
		defer func() { fired = it.stop() }()
		line, err = r.choose(w, prompt, km, it)
	}()
	if fired {
		return "", ErrInterrupted
	}
	return line, err
}

// lineChoice asks for a choice as Ask asks, its flush done.
func (r *Reader) lineChoice(w io.Writer, prompt string) (string, error) {
	if _, err := io.WriteString(w, prompt+" "); err != nil {
		return "", fmt.Errorf("prompt: write: %w", err)
	}
	return r.answer(w)
}

// choose writes prompt, reads one choice key by key and ends the prompt's
// line, with the terminal set for keys, which it puts back however it
// returns.
func (r *Reader) choose(w io.Writer, prompt string, km *keyMode, it *interrupt) (string, error) {
	defer km.restore()
	if _, err := io.WriteString(w, prompt+" "); err != nil {
		return "", fmt.Errorf("prompt: write: %w", err)
	}
	r.asked = true
	line, err := r.keys(w, km, it)
	if _, werr := io.WriteString(w, "\n"); werr != nil && err == nil {
		return "", fmt.Errorf("prompt: write: %w", werr)
	}
	return line, err
}

// keys reads and decodes keys until Enter, Ctrl-C or Ctrl-D, echoing to w
// what changes the answer. Ctrl-C does nothing where SIGINT is ignored, as
// in a line the terminal's SIGINT then does nothing. A keySignal that put
// the terminal back and did not end hardn ends the read.
func (r *Reader) keys(w io.Writer, km *keyMode, it *interrupt) (string, error) {
	var d keyDecoder
	var line []byte
	ignoreInt := signal.Ignored(os.Interrupt)
	echo := func(s string) error {
		if _, err := io.WriteString(w, s); err != nil {
			return fmt.Errorf("prompt: write: %w", err)
		}
		return nil
	}
	for empty := 0; ; {
		var timeout time.Duration
		if d.pending() {
			timeout = escWait
		}
		ready, err := km.wait(it, timeout)
		switch {
		case km.ended():
			return "", ErrInterrupted
		case errors.Is(err, errWoken):
			return "", ErrInterrupted
		case err != nil:
			return "", err
		case !ready: // what came of a sequence was a key alone, as a lone Escape
			d.reset()
			continue
		}
		var b [chunk]byte
		n, rerr := r.in.Read(b[:])
		for _, c := range b[:n] {
			k := d.feed(c)
			var err error
			switch k.kind {
			case keyEnter:
				return string(line), nil
			case keyInterrupt:
				if !ignoreInt {
					return "", ErrInterrupted
				}
			case keyEnd:
				if len(line) == 0 {
					return "", ErrEndOfInput
				}
				return string(line), nil
			case keyErase:
				if len(line) > 0 {
					line, err = line[:len(line)-1], echo("\b \b")
				}
			case keyKill:
				if len(line) > 0 {
					err, line = echo(strings.Repeat("\b \b", len(line))), line[:0]
				}
			case keyChar:
				if len(line) < maxChoice {
					line, err = append(line, k.c), echo(string(k.c))
				}
			}
			if err != nil {
				return "", err
			}
		}
		switch {
		case errors.Is(rerr, io.EOF):
			return "", ErrEndOfInput
		case rerr != nil:
			return "", fmt.Errorf("prompt: read: %w", rerr)
		}
		if n > 0 {
			empty = 0
		} else if empty++; empty == maxEmptyReads {
			return "", fmt.Errorf("prompt: read: %w", io.ErrNoProgress)
		}
	}
}

// keyKind is what a key does to a choice being typed.
type keyKind uint8

const (
	keyNone      keyKind = iota // nothing: a key no answer has, or part of a sequence
	keyChar                     // c, a printable ASCII character, is typed
	keyEnter                    // the answer is given: Enter, Return, or the keypad's Enter
	keyErase                    // the last character goes: Backspace or Delete
	keyKill                     // every character goes: Ctrl-U or Ctrl-W
	keyInterrupt                // Ctrl-C
	keyEnd                      // Ctrl-D
)

// key is one decoded key.
type key struct {
	kind keyKind
	c    byte
}

// The states of a keyDecoder: none; after an Escape; after a control
// sequence's introducer (Escape '['), then inside its parameters; after the
// Linux console's Escape '[' '[', whose one byte more ends it; after SS3
// (Escape 'O'), then inside its parameters.
const (
	stNone uint8 = iota
	stEsc
	stCSI
	stCSIParams
	stLinuxF
	stSS3
	stSS3Params
)

// keyDecoder turns the bytes a terminal sends for keys into keys. Control
// sequences (Escape '[', parameters, a final byte) and SS3 sequences
// (Escape 'O', parameters such as Konsole's modifier, and one byte) are the
// arrows, the function and editing keys, in a terminal's normal and its
// application cursor mode alike, as is the Linux console's F1 to F5
// (Escape '[' '[' and a letter): they are dropped, but for the keypad's
// digits and Enter in application keypad mode (SS3 'p' to 'y', 'M'), which
// are the keys they are. Escape and a printable character is Alt and that
// key: dropped too. The zero keyDecoder is ready.
type keyDecoder struct{ state uint8 }

// pending reports whether a sequence has begun and not ended.
func (d *keyDecoder) pending() bool { return d.state != stNone }

// reset ends the sequence begun: what came of it is dropped.
func (d *keyDecoder) reset() { d.state = stNone }

// feed decodes the next byte.
func (d *keyDecoder) feed(c byte) key {
	switch d.state {
	case stEsc:
		switch c {
		case '[':
			d.state = stCSI
			return key{}
		case 'O':
			d.state = stSS3
			return key{}
		case 0x1b:
			return key{}
		}
		d.state = stNone
		if c >= 0x20 && c < 0x7f {
			return key{} // Alt and a key
		}
		return d.plain(c)
	case stCSI, stCSIParams:
		switch {
		case d.state == stCSI && c == '[': // the Linux console's F1 to F5
			d.state = stLinuxF
			return key{}
		case c >= 0x20 && c <= 0x3f: // parameter and intermediate bytes
			d.state = stCSIParams
			return key{}
		case c >= 0x40 && c <= 0x7e: // the final byte: the sequence's key is dropped
			d.state = stNone
			return key{}
		}
		d.state = stNone
		return d.plain(c)
	case stLinuxF:
		d.state = stNone
		if c >= 0x20 && c < 0x7f {
			return key{}
		}
		return d.plain(c)
	case stSS3, stSS3Params:
		bare := d.state == stSS3
		switch {
		case c >= 0x30 && c <= 0x3f: // a parameter, as Konsole's modifier
			d.state = stSS3Params
			return key{}
		case c >= 0x20 && c < 0x7f:
			d.state = stNone
			switch {
			case bare && c >= 'p' && c <= 'y':
				return key{keyChar, '0' + c - 'p'}
			case bare && c == 'M':
				return key{kind: keyEnter}
			}
			return key{} // an arrow or a function key
		}
		d.state = stNone
		return d.plain(c)
	}
	return d.plain(c)
}

// plain decodes a byte outside any sequence.
func (d *keyDecoder) plain(c byte) key {
	switch {
	case c == 0x1b:
		d.state = stEsc
		return key{}
	case c == '\r' || c == '\n':
		return key{kind: keyEnter}
	case c == 0x03:
		return key{kind: keyInterrupt}
	case c == 0x04:
		return key{kind: keyEnd}
	case c == 0x7f || c == 0x08:
		return key{kind: keyErase}
	case c == 0x15 || c == 0x17:
		return key{kind: keyKill}
	case c >= 0x20 && c < 0x7f:
		return key{keyChar, c}
	}
	return key{} // another control character, or a byte past ASCII, which no answer has
}
