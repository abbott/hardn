package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/abbott/hardn/pkg/ui/term"
)

// WriteJSON writes d to w as exactly one JSON value, indented, and a
// newline, in one write. HTML is not escaped: a diff's "<" stays "<".
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
// newline, HTML not escaped.
func marshal(v any, indent string) ([]byte, error) {
	var b jsonBuffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", indent)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("output: json: %w", err)
	}
	return b, nil
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

// Streams are where a run writes: stdout and stderr, each with the
// render environment detected for it (term.Detect).
type Streams struct {
	Stdout io.Writer
	Out    term.Env
	Stderr io.Writer
	Err    term.Env
}

// Emit writes run r's report. Human output that is not the report (the
// configuration's deprecated values) goes to stderr, in either format. As
// FormatJSON, stdout gets exactly one JSON document and nothing else, so a
// script can parse it whole; everything for a person goes to stderr. As
// FormatHuman, stdout gets Human's report through s.Out's writer. now is
// the countdown's clock.
func Emit(s Streams, f Format, r Run, now time.Time) error {
	var errs []error
	if w := Deprecations(s.Err, r.ConfigPath, r.Deprecations); w != "" {
		if _, err := io.WriteString(s.Err.Writer(s.Stderr), w); err != nil {
			errs = append(errs, fmt.Errorf("output: stderr: %w", err))
		}
	}
	switch f {
	case FormatJSON:
		errs = append(errs, WriteJSON(s.Stdout, Build(r)))
	default:
		if h := Human(s.Out, r, now); h != "" {
			if _, err := io.WriteString(s.Out.Writer(s.Stdout), h); err != nil {
				errs = append(errs, fmt.Errorf("output: stdout: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}
