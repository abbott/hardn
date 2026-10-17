package uitest

import "io"

// Typist plays the user at a terminal, for tests that answer prompts
// through the line reader (pkg/ui/prompt) on scripted input. Each Read
// that needs a new line takes the next one, writes it to the echo writer
// as a terminal echoes what is typed, and returns it ended with Enter
// ("\n"). The echo holds no line break: the reader writes the one after
// an answer read without a terminal (prompt.Reader.Ask). With no line
// left, Read reports the end of the input (io.EOF), as Ctrl-D on an
// empty line or the end of a pipe does, which the reader turns into
// prompt.ErrEndOfInput.
//
// Pass the transcript's writer as the echo, the one the code under test
// writes to, so that each answer shows after its prompt as on a terminal:
//
//	out := new(strings.Builder)
//	in := prompt.New(uitest.NewTypist(out, "1", "y"), false)
type Typist struct {
	lines   []string
	pending []byte
	echo    io.Writer
}

// NewTypist returns a Typist that types lines in order, echoing each to
// echo.
func NewTypist(echo io.Writer, lines ...string) *Typist {
	return &Typist{lines: lines, echo: echo}
}

// Read returns what is left of the line being typed, starting the next
// line, and echoing it, once the last is read whole. A failed echo
// returns its error, and the line is dropped untyped.
func (t *Typist) Read(p []byte) (int, error) {
	if len(t.pending) == 0 {
		if len(t.lines) == 0 {
			return 0, io.EOF
		}
		line := t.lines[0]
		t.lines = t.lines[1:]
		if _, err := io.WriteString(t.echo, line); err != nil {
			return 0, err
		}
		t.pending = []byte(line + "\n")
	}
	n := copy(p, t.pending)
	t.pending = t.pending[n:]
	return n, nil
}

// Unread returns the number of lines not yet started, so that a test can
// check that every answer it scripted was read.
func (t *Typist) Unread() int { return len(t.lines) }
