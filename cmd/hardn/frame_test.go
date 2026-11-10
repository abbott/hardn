package main

import (
	"bytes"
	"os"
	"testing"
)

// TestFrame: what hardn writes to a terminal starts after a blank line and
// ends with one (the maintainer, 2026-11-10). stdout and stderr share the
// frame: the opening blank line before the first byte either writes, the
// closing one on the stream written last, as many newlines as leave one
// blank line.
func TestFrame(t *testing.T) {
	for _, tt := range []struct {
		name             string
		writes           []string // "o:" to stdout, "e:" to stderr
		wantOut, wantErr string
	}{
		{name: "a refusal on stderr", writes: []string{"e:Operation Failed\nRun `doas hardn`\n"},
			wantErr: "\nOperation Failed\nRun `doas hardn`\n\n"},
		{name: "warnings on stderr, the report on stdout", writes: []string{"e:Warning\n", "o:System Details\n", "o:Memory\n"},
			wantErr: "\nWarning\n", wantOut: "System Details\nMemory\n\n"},
		{name: "a blank line already first", writes: []string{"o:\nreport\n"}, wantOut: "\nreport\n\n"},
		{name: "a blank line already last (cobra's usage error)", writes: []string{"e:Error: unknown flag\nUsage:\n\n"},
			wantErr: "\nError: unknown flag\nUsage:\n\n"},
		{name: "a prompt left open", writes: []string{"o:Type the hostname: "}, wantOut: "\nType the hostname: \n\n"},
		{name: "the last newline in its own write", writes: []string{"o:done", "o:\n"}, wantOut: "\ndone\n\n"},
		{name: "nothing written"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &frame{}
			var out, errb bytes.Buffer
			o, e := framed{f, &out}, framed{f, &errb}
			for _, w := range tt.writes {
				s := framed{}
				switch w[:2] {
				case "o:":
					s = o
				case "e:":
					s = e
				}
				if n, err := s.Write([]byte(w[2:])); err != nil || n != len(w)-2 {
					t.Fatalf("Write(%q): %d, %v", w[2:], n, err)
				}
			}
			f.close()
			if out.String() != tt.wantOut || errb.String() != tt.wantErr {
				t.Errorf("stdout %q, stderr %q; want %q, %q", out.String(), errb.String(), tt.wantOut, tt.wantErr)
			}
		})
	}
}

// TestFrameAnswered: a prompt answered on the terminal ends its line by the
// echoed Enter, which the frame never sees written, so one newline leaves
// the blank line, not two.
func TestFrameAnswered(t *testing.T) {
	var answers uint64
	f := &frame{echoed: func() uint64 { return answers }}
	var out bytes.Buffer
	_, _ = framed{f, &out}.Write([]byte("Type the hostname to confirm: "))
	answers++ // q, and Enter, echoed
	f.close()
	if want := "\nType the hostname to confirm: \n"; out.String() != want {
		t.Errorf("%q, want %q", out.String(), want)
	}
}

// TestFrameDisarmed: the main menu and system-details' JSON keep their own
// spacing, and a frame closed twice, or written after it closed, adds
// nothing more.
func TestFrameDisarmed(t *testing.T) {
	f := &frame{}
	var out bytes.Buffer
	f.disarm()
	_, _ = framed{f, &out}.Write([]byte("{}\n"))
	f.close()
	if out.String() != "{}\n" {
		t.Errorf("disarmed: %q", out.String())
	}

	f, out = &frame{}, bytes.Buffer{}
	_, _ = framed{f, &out}.Write([]byte("a\n"))
	f.close()
	f.close()
	_, _ = framed{f, &out}.Write([]byte("b\n"))
	if want := "\na\n\nb\n"; out.String() != want {
		t.Errorf("closed: %q, want %q", out.String(), want)
	}
}

// TestFrameOffTerminal: a pipe is never inside the frame, so redirected
// output, JSON and the journal stay as they were.
func TestFrameOffTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if _, ok := terminalWriter(w).(framed); ok {
		t.Error("terminalWriter framed a pipe")
	}
	if frameTerminal(w) {
		t.Error("a pipe reads as a terminal")
	}
}
