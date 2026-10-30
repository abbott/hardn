package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/abbott/hardn/pkg/ui/term"
)

// WriteJSON writes d to w as exactly one JSON value, indented, and a
// newline, in one write. HTML is not escaped: a diff's "<" stays "<". What a
// terminal would act on is, whatever a string holds (marshal).
func WriteJSON(w io.Writer, d Document) error {
	b, err := marshal(d, "  ")
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("output: write: %w", err)
	}
	return nil
}

// marshal is v as JSON, indented by indent ("" for one line), with a
// newline, HTML not escaped, and what a terminal would act on escaped
// (escapeTerminal), as stdout may be one.
func marshal(v any, indent string) ([]byte, error) {
	var b jsonBuffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("output: json: %w", err)
	}
	return escapeTerminal(b), nil
}

// escapeTerminal writes as \uXXXX what encoding/json leaves raw and a terminal
// would act on: DEL, the C1 controls (U+009B is CSI) and the format characters,
// bidi overrides among them. It escapes C0 itself. Each is past ASCII's printing
// characters, so only ever inside a string, whose value this does not change: a
// script reads what the host holds, and a person's terminal stays its own. b is
// the encoder's, valid UTF-8.
func escapeTerminal(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		switch {
		case r < 0x7f || !unicode.IsControl(r) && !unicode.Is(unicode.Cf, r):
			out = append(out, b[:n]...)
		case r > 0xffff:
			hi, lo := utf16.EncodeRune(r)
			out = fmt.Appendf(out, `\u%04x\u%04x`, hi, lo)
		default:
			out = fmt.Appendf(out, `\u%04x`, r)
		}
		b = b[n:]
	}
	return out
}

// jsonBuffer collects what an Encoder writes.
type jsonBuffer []byte

func (b *jsonBuffer) Write(p []byte) (int, error) { *b = append(*b, p...); return len(p), nil }

// Format is how a run reports.
type Format uint8

// The formats.
const (
	FormatHuman Format = iota // for a person, through pkg/ui
	FormatJSON                // the JSON document on stdout (brief Phase 3)
)

// Streams are where a run's report goes: stdout, with the render
// environment detected for it (term.Detect).
type Streams struct {
	Stdout io.Writer
	Out    term.Env
}

// Emit writes run r's report on stdout. As FormatJSON, stdout gets exactly
// one JSON document and nothing else, so a script can parse it whole;
// Emit writes nothing for a person, and what a caller shows a person
// besides (progress, notices) goes to stderr. As FormatHuman, stdout gets
// Human's report through s.Out's writer; after a flow, write AfterFlow
// instead. The configuration's deprecated values are findings of the
// document (CONFIG_DEPRECATED_VALUE), and not warned about here: the
// warning on stderr is cmd/hardn's, once, as the configuration loads
// (refactor plan §3.17). now is the countdown's clock.
func Emit(s Streams, f Format, r Run, now time.Time) error {
	if f == FormatJSON {
		return WriteJSON(s.Stdout, Build(r))
	}
	if h := Human(s.Out, r, now); h != "" {
		if _, err := io.WriteString(s.Out.Writer(s.Stdout), h); err != nil {
			return fmt.Errorf("output: stdout: %w", err)
		}
	}
	return nil
}
