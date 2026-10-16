package prompt

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/abbott/hardn/pkg/ui/component"
	"github.com/abbott/hardn/pkg/ui/keys"
	"github.com/abbott/hardn/pkg/ui/term"
)

// Outcomes as readAll records them, next to the lines themselves.
const (
	aborted = "<aborted>"
	tooLong = "<too long>"
)

// show is a line as readAll records it: a long one by its length, so
// that a failure stays readable.
func show(line string) string {
	if len(line) > 40 {
		return fmt.Sprintf("<%d bytes>", len(line))
	}
	return line
}

// readAll calls ReadLine until it aborts, at most 20 times, and records
// each line or outcome.
func readAll(t *testing.T, r *Reader) []string {
	t.Helper()
	var got []string
	for range 20 {
		line, err := r.ReadLine()
		switch {
		case errors.Is(err, ErrAborted):
			return append(got, aborted)
		case errors.Is(err, ErrLineTooLong):
			got = append(got, tooLong)
		case err != nil:
			t.Fatalf("ReadLine: unexpected error %v", err)
		default:
			got = append(got, show(line))
		}
	}
	t.Fatalf("ReadLine did not abort: %q", got)
	return nil
}

// readers wrap a scripted input as differently behaved io.Readers: whole,
// a byte at a time (a CRLF split across reads), half at a time, and with
// the last data and io.EOF in one read.
var readers = []struct {
	name string
	wrap func(io.Reader) io.Reader
}{
	{"whole", func(r io.Reader) io.Reader { return r }},
	{"one byte", iotest.OneByteReader},
	{"half", iotest.HalfReader},
	{"data with EOF", iotest.DataErrReader},
}

// resumed reads first until it reports io.EOF, then then: an input that
// goes on after its end, as a terminal does after Ctrl-D.
type resumed struct{ first, then io.Reader }

func (r *resumed) Read(p []byte) (int, error) {
	if r.first == nil {
		return r.then.Read(p)
	}
	n, err := r.first.Read(p)
	if errors.Is(err, io.EOF) {
		r.first = nil
	}
	return n, err
}

func TestReadLine(t *testing.T) {
	long := strings.Repeat("x", MaxLine)
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"lines", "a\nb\nc\n", []string{"a", "b", "c", aborted}},
		{"CRLF", "a\r\nb\r\n", []string{"a", "b", aborted}},
		{"mixed line endings", "a\r\nb\nc\r\n", []string{"a", "b", "c", aborted}},
		{"no line ending at the end", "a\nb", []string{"a", "b", aborted}},
		{"EOF mid-line", "first\nsec", []string{"first", "sec", aborted}},
		{"empty lines", "\n\nx\n\r\n", []string{"", "", "x", "", aborted}},
		{"empty input", "", []string{aborted}},
		{"spaces kept", "  1 \n", []string{"  1 ", aborted}},
		{"a carriage return inside a line kept", "a\rb\n", []string{"a\rb", aborted}},
		{"a last carriage return ends the line", "a\r", []string{"a", aborted}},
		{"MaxLine bytes", long + "\nnext\n", []string{show(long), "next", aborted}},
		{"MaxLine bytes and CRLF", long + "\r\nnext\n", []string{show(long), "next", aborted}},
		{"MaxLine bytes at the end", long, []string{show(long), aborted}},
		{"one byte too long", long + "y\nnext\n", []string{tooLong, "next", aborted}},
		{"too long by a carriage return", long + "\r\r\nnext\n", []string{tooLong, "next", aborted}},
		{"too long at the end", long + "y", []string{tooLong, aborted}},
		{"too long, then more", long + long + "\nnext\n", []string{tooLong, "next", aborted}},
	}
	for _, tt := range tests {
		for _, rd := range readers {
			t.Run(tt.name+"/"+rd.name, func(t *testing.T) {
				// The input goes on after its end, as a terminal does
				// after Ctrl-D: the line after the end is read whole.
				in := &resumed{rd.wrap(strings.NewReader(tt.in)), rd.wrap(strings.NewReader("after\n"))}
				r := New(in, false)
				got := append(readAll(t, r), readAll(t, r)...)
				if want := slices.Concat(tt.want, []string{"after", aborted}); fmt.Sprint(got) != fmt.Sprint(want) {
					t.Errorf("lines = %q, want %q", got, want)
				}
			})
		}
	}
}

// endless is a runaway input: x forever, never a line ending.
type endless struct{}

func (endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

// script returns its reads in turn, then io.EOF. A read's data longer
// than p comes over several reads, its error with the last of it.
type script []struct {
	data string
	err  error
}

func (s *script) Read(p []byte) (int, error) {
	if len(*s) == 0 {
		return 0, io.EOF
	}
	next := &(*s)[0]
	n := copy(p, next.data)
	if next.data = next.data[n:]; next.data != "" {
		return n, nil
	}
	err := next.err
	*s = (*s)[1:]
	return n, err
}

// empty reads nothing and reports nothing, forever.
type empty struct{}

func (empty) Read([]byte) (int, error) { return 0, nil }

func TestReadLineFailures(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		in   io.Reader
		want error
	}{
		{"a runaway input stops at MaxLine", endless{}, ErrLineTooLong},
		{"a read error", iotest.ErrReader(boom), boom},
		{"a read error mid-line", io.MultiReader(strings.NewReader("part"), iotest.ErrReader(boom)), boom},
		{"no progress", empty{}, io.ErrNoProgress},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, err := New(tt.in, false).ReadLine()
			if !errors.Is(err, tt.want) || errors.Is(err, ErrAborted) || line != "" {
				t.Errorf("ReadLine() = %q, %v; want %v, not an abort", line, err, tt.want)
			}
		})
	}
	t.Run("a read error leaves the rest of a long line to drop", func(t *testing.T) {
		r := New(&script{{strings.Repeat("x", MaxLine+1), nil}, {"", boom}, {"tail\nnext\n", nil}}, false)
		for _, want := range []error{ErrLineTooLong, boom} {
			if line, err := r.ReadLine(); !errors.Is(err, want) {
				t.Fatalf("ReadLine() = %q, %v; want %v", line, err, want)
			}
		}
		if line, err := r.ReadLine(); line != "next" || err != nil {
			t.Errorf("after the error, ReadLine() = %q, %v; want the line after the long one", line, err)
		}
	})
}

// TestAbortErrors checks that a caller can tell the end of the input from
// Ctrl-C, and that both are aborts.
func TestAbortErrors(t *testing.T) {
	_, err := New(strings.NewReader(""), false).ReadLine()
	if !errors.Is(err, ErrEndOfInput) || errors.Is(err, ErrInterrupted) || err.Error() != "prompt: aborted: end of input" {
		t.Errorf("at EOF: %v", err)
	}
	if !errors.Is(ErrInterrupted, ErrAborted) || errors.Is(ErrInterrupted, ErrEndOfInput) || ErrInterrupted.Error() != "prompt: aborted: interrupted" {
		t.Errorf("on Ctrl-C: %v", ErrInterrupted)
	}
	if !errors.Is(ErrEndOfInput, ErrAborted) {
		t.Errorf("%v does not wrap ErrAborted", ErrEndOfInput)
	}
}

func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int
		ok   bool
	}{
		{"nil", nil, 0, false},
		{"ErrAborted", ErrAborted, 130, true},
		{"end of input", ErrEndOfInput, 130, true},
		{"interrupted", ErrInterrupted, 130, true},
		{"wrapped", fmt.Errorf("asking: %w", ErrEndOfInput), 130, true},
		{"io.EOF itself", io.EOF, 0, false},
		{"another error", ErrLineTooLong, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code, ok := ExitCode(tt.err); code != tt.code || ok != tt.ok {
				t.Errorf("ExitCode(%v) = %d, %v; want %d, %v", tt.err, code, ok, tt.code, tt.ok)
			}
		})
	}
}

// writes records each Write as one string.
type writes []string

func (w *writes) Write(p []byte) (int, error) {
	*w = append(*w, string(p))
	return len(p), nil
}

// failing fails every Write.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("closed") }

// mainPrompt is Figma's main-menu prompt, rendered as a screen would.
func mainPrompt() string {
	accepts := keys.Set{{Keys: []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}}, keys.Quit()}
	env := term.Fixed(80, colorprofile.TrueColor, true, true)
	return component.PromptLine{Label: "Enter your choice", Accepts: accepts}.Render(component.NewCtx(env))
}

func TestAsk(t *testing.T) {
	// The prompt ends at its colon, its style closed: Ask adds a plain
	// space, so no colour is left on while the terminal echoes (DEV-043).
	p := mainPrompt()
	if !strings.HasSuffix(ansi.Strip(p), ":") || !strings.HasSuffix(p, ansi.ResetStyle) {
		t.Fatalf("PromptLine.Render = %q, want it to end at the colon, its style reset", p)
	}
	tests := []struct {
		name   string
		in     string
		tty    bool
		answer string
		err    error
		want   []string // the writes
	}{
		{"a terminal echoes the Enter", "q\n", true, "q", nil, []string{p + " "}},
		{"an answer Ctrl-D ended on a terminal", "q", true, "q", nil, []string{p + " ", "\n"}},
		{"without a terminal Ask ends the line", "q\n", false, "q", nil, []string{p + " ", "\n"}},
		{"an abort ends the line", "", true, "", ErrAborted, []string{p + " ", "\n"}},
		{"too long on a terminal", strings.Repeat("x", MaxLine+1), true, "", ErrLineTooLong, []string{p + " "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w writes
			got, err := New(strings.NewReader(tt.in), tt.tty).Ask(&w, p)
			if got != tt.answer || !errors.Is(err, tt.err) {
				t.Errorf("Ask() = %q, %v; want %q, %v", got, err, tt.answer, tt.err)
			}
			if fmt.Sprintf("%q", w) != fmt.Sprintf("%q", tt.want) {
				t.Errorf("writes = %q, want %q", w, tt.want)
			}
		})
	}
	t.Run("a failed write asks nothing", func(t *testing.T) {
		r := New(strings.NewReader("q\n"), false)
		if _, err := r.Ask(failing{}, p); err == nil {
			t.Fatal("Ask() succeeded on a failed write")
		}
		if line, err := r.ReadLine(); line != "q" || err != nil {
			t.Errorf("the answer was read: next ReadLine() = %q, %v", line, err)
		}
	})
}

func TestFlush(t *testing.T) {
	t.Run("scripted input loses no line", func(t *testing.T) {
		r := New(strings.NewReader("1\n2\n3\n"), false)
		var w writes
		var got []string
		for range 4 {
			line, err := r.Ask(&w, ">")
			if err != nil {
				got = append(got, aborted)
				break
			}
			got = append(got, line)
		}
		if want := []string{"1", "2", "3", aborted}; fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("answers = %q, want %q", got, want)
		}
	})
	t.Run("a terminal Reader drops its own buffer", func(t *testing.T) {
		// One read takes both lines into the buffer; a strings.Reader
		// has no descriptor, so only the buffer is flushed.
		r := New(strings.NewReader("1\n2\n"), true)
		if line, err := r.ReadLine(); line != "1" || err != nil {
			t.Fatalf("ReadLine() = %q, %v", line, err)
		}
		if err := r.Flush(); err != nil {
			t.Fatalf("Flush() = %v", err)
		}
		if line, err := r.ReadLine(); !errors.Is(err, ErrAborted) {
			t.Errorf("after Flush, ReadLine() = %q, %v; want the end of input", line, err)
		}
	})
	t.Run("Ask flushes a terminal Reader", func(t *testing.T) {
		r := New(strings.NewReader("1\n2\n"), true)
		if line, err := r.ReadLine(); line != "1" || err != nil {
			t.Fatalf("ReadLine() = %q, %v", line, err)
		}
		if line, err := r.Ask(io.Discard, ">"); !errors.Is(err, ErrAborted) {
			t.Errorf("Ask() = %q, %v; want the typeahead dropped and the end of input", line, err)
		}
	})
	t.Run("the end of input survives a flush", func(t *testing.T) {
		// The input ends with a last line, then would go on: a flush
		// that dropped the end would read "more".
		r := New(&script{{"last", io.EOF}, {"more\n", nil}}, true)
		if line, err := r.ReadLine(); line != "last" || err != nil {
			t.Fatalf("ReadLine() = %q, %v", line, err)
		}
		if err := r.Flush(); err != nil {
			t.Fatalf("Flush() = %v", err)
		}
		if line, err := r.ReadLine(); !errors.Is(err, ErrAborted) {
			t.Errorf("after Flush, ReadLine() = %q, %v; want the end of input", line, err)
		}
	})
}

// TestImports holds the package to the standard library and x/sys (see
// the package doc), so the legacy menus can use it without linking Lip
// Gloss. It reads every non-test file, whatever its build tags.
func TestImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") && path != "golang.org/x/sys/unix" {
				t.Errorf("%s imports %s: only the standard library and golang.org/x/sys/unix", name, path)
			}
		}
	}
}
