package output_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/abbott/hardn/pkg/output"
	"github.com/abbott/hardn/pkg/ui/term"
	"github.com/abbott/hardn/pkg/ui/uitest"
)

// updating reports whether the test runs with -update (defined by
// pkg/ui/uitest, which this package's tests link: make golden-update passes
// it), so the JSON goldens are rewritten with the human ones.
func updating() bool {
	f := flag.Lookup("update")
	return f != nil && f.Value.String() == "true"
}

// golden compares got with testdata/<name>, or rewrites it under -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if updating() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run make golden-update to create it)", err)
	}
	if !bytes.Equal(want, got) {
		t.Errorf("%s does not match.\n--- want\n%s\n--- got\n%s\nIf the change is intended, run make golden-update and give the reason in the review description.", path, want, got)
	}
}

// plainEnv is the core matrix's plain cell: no escape sequences, ASCII
// glyphs, 80 columns, as for a stream that is not a terminal.
func plainEnv() term.Env {
	for _, c := range uitest.Core() {
		if c.Name == "plain-80" {
			return c.Env
		}
	}
	panic("uitest: no plain-80 cell")
}

// TestDocuments pins the JSON document of each fixture: a check, a plan, a
// refused plan, an applied lockout-risk change waiting for confirmation
// (its deadline approximate) and a failed apply rolled back
// (docs/reference/output.md shows them).
func TestDocuments(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := output.WriteJSON(&b, output.Build(fx.run)); err != nil {
				t.Fatal(err)
			}
			golden(t, "json/"+fx.name+".json", b.Bytes())
		})
	}
}

// TestStdoutIsOneDocument: with JSON output, stdout parses as exactly one
// JSON document, whatever the run, holding the nine fields; the
// configuration's deprecated value is a finding of it, and no line of text
// beside it (the warning on stderr is cmd/hardn's, as the configuration
// loads).
func TestStdoutIsOneDocument(t *testing.T) {
	plain := plainEnv()
	for _, fx := range fixtures() {
		t.Run(fx.name, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := output.Emit(output.Streams{Stdout: &stdout, Out: plain}, output.FormatJSON, fx.run, fixtureNow); err != nil {
				t.Fatal(err)
			}
			dec := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
			var doc map[string]any
			if err := dec.Decode(&doc); err != nil {
				t.Fatalf("stdout is not a JSON document: %v", err)
			}
			if err := dec.Decode(&doc); !errors.Is(err, io.EOF) {
				t.Errorf("stdout holds more than one document: %v", err)
			}
			for _, k := range []string{"schemaVersion", "hardnVersion", "host", "mode", "result", "changes", "status", "findings", "errors"} {
				if _, ok := doc[k]; !ok {
					t.Errorf("no %q in %v", k, doc)
				}
			}
			if len(doc) != 9 {
				t.Errorf("%d top-level fields, want the nine", len(doc))
			}
			if len(fx.run.Deprecations) > 0 && !strings.Contains(stdout.String(), `"code": "CONFIG_DEPRECATED_VALUE"`) {
				t.Errorf("the deprecated value is not a finding:\n%s", stdout.String())
			}
			if strings.Contains(stdout.String(), "Warning:") {
				t.Errorf("a warning on stdout:\n%s", stdout.String())
			}
		})
	}
}

// TestHuman pins the human report of each fixture, through pkg/ui's
// components, on the core cells.
func TestHuman(t *testing.T) {
	for _, fx := range fixtures() {
		t.Run(fx.name, func(t *testing.T) {
			uitest.Golden(t, "human/"+fx.name, uitest.Core(), func(env term.Env) string {
				return output.Human(env, fx.run, fixtureNow)
			})
		})
	}
}

// TestEmitHuman: the human report goes to stdout, through the Env's
// writer (no escape sequences for a stdout that is not a terminal), and
// says nothing of the deprecated value, which cmd/hardn warned about as
// the configuration loaded.
func TestEmitHuman(t *testing.T) {
	fx := fixtures()[0]
	plain := plainEnv()
	var stdout bytes.Buffer
	if err := output.Emit(output.Streams{Stdout: &stdout, Out: plain}, output.FormatHuman, fx.run, fixtureNow); err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if _, err := io.WriteString(plain.Writer(&want), output.Human(plain, fx.run, fixtureNow)); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != want.String() {
		t.Errorf("stdout = %q, want Human's, written through the Env %q", stdout.String(), want.String())
	}
	if strings.Contains(stdout.String(), "deprecated") || strings.Contains(stdout.String(), "\x1b") {
		t.Errorf("stdout %q", stdout.String())
	}
}
