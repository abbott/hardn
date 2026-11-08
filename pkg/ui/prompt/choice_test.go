package prompt

import (
	"errors"
	"strings"
	"testing"
)

// TestKeyDecoder: what each key's bytes decode to, in a terminal's normal
// and its application cursor and keypad modes.
func TestKeyDecoder(t *testing.T) {
	for _, tt := range []struct {
		name, in, want string // want: the characters typed, then E for Enter, X for erase, K for kill, C for Ctrl-C, D for Ctrl-D
	}{
		{"digits and Enter", "10\r", "10E"},
		{"newline is Enter too", "q\n", "qE"},
		{"arrows, normal mode", "\x1b[A\x1b[B\x1b[C\x1b[D1\r", "1E"},
		{"arrows, application cursor mode", "\x1bOA\x1bOB\x1bOC\x1bOD2\r", "2E"},
		{"a sequence with parameters", "\x1b[1;5C\x1b[3~\x1b[200~3", "3"},
		{"function keys", "\x1bOP\x1b[15~4", "4"},
		{"keypad digits and Enter, application keypad mode", "\x1bOq\x1bOp\x1bOM", "10E"},
		{"Alt and a key", "\x1bx5", "5"},
		{"Escape twice, then a sequence", "\x1b\x1b[A6", "6"},
		{"Escape, then Enter", "\x1b\r", "E"},
		{"a control byte ends a sequence and counts", "\x1b[1\x7f", "X"},
		{"Backspace and Delete", "12\x7f\x08", "12XX"},
		{"Ctrl-U and Ctrl-W", "ab\x15\x17", "abKK"},
		{"Ctrl-C", "\x03", "C"},
		{"Ctrl-D", "\x04", "D"},
		{"tab, other controls and bytes past ASCII", "\t\x01é7", "7"},
		{"the Linux console's F1 and F5", "\x1b[[A\x1b[[E8", "8"},
		{"an SS3 key with a modifier, as Konsole's", "\x1bO2P\x1bO5q9", "9"},
	} {
		var d keyDecoder
		var got strings.Builder
		for _, c := range []byte(tt.in) {
			k := d.feed(c)
			switch k.kind {
			case keyChar:
				got.WriteByte(k.c)
			case keyEnter:
				got.WriteByte('E')
			case keyErase:
				got.WriteByte('X')
			case keyKill:
				got.WriteByte('K')
			case keyInterrupt:
				got.WriteByte('C')
			case keyEnd:
				got.WriteByte('D')
			}
		}
		if got.String() != tt.want {
			t.Errorf("%s: %q decodes to %q, want %q", tt.name, tt.in, got.String(), tt.want)
		}
	}
}

// TestKeyDecoderPending: a sequence cut short leaves the decoder waiting
// for the rest, and reset drops it.
func TestKeyDecoderPending(t *testing.T) {
	var d keyDecoder
	for _, c := range []byte("\x1b[") {
		d.feed(c)
	}
	if !d.pending() {
		t.Fatal("not pending inside a sequence")
	}
	d.reset()
	if k := d.feed('1'); d.pending() || k != (key{keyChar, '1'}) {
		t.Errorf("after reset: %+v, pending %t", k, d.pending())
	}
}

// TestAskChoiceNotATerminal: where the input is not a terminal, a choice is
// a line, as Ask reads it, escape bytes and all.
func TestAskChoiceNotATerminal(t *testing.T) {
	var out strings.Builder
	r := New(strings.NewReader("\x1b[A1\n"), false)
	line, err := r.AskChoice(&out, "Enter selection:")
	if err != nil || line != "\x1b[A1" || out.String() != "Enter selection: \n" {
		t.Errorf("AskChoice = %q, %v; wrote %q", line, err, out.String())
	}
	if _, err := r.AskChoice(&out, "Enter selection:"); !errors.Is(err, ErrEndOfInput) {
		t.Errorf("at the end: %v", err)
	}
}
