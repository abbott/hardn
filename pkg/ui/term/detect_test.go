package term

import (
	"io"
	"os"
	"testing"

	"github.com/charmbracelet/colorprofile"
	xterm "github.com/charmbracelet/x/term"
)

// TestDetectPipes runs Detect on a pipe pair, as when hardn's input and
// output are redirected: the Env must be plain and colourless, and the
// terminal must never be queried, even with --theme=auto.
func TestDetectPipes(t *testing.T) {
	queries := 0
	orig := queryDarkBackground
	queryDarkBackground = func(in, out xterm.File) bool { queries++; return false }
	t.Cleanup(func() { queryDarkBackground = orig })
	t.Setenv("COLUMNS", "")
	t.Setenv("LINES", "")

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer outR.Close()

	e := Detect(Options{Theme: ThemeAuto}, inR, outW)
	if err := outW.Close(); err != nil {
		t.Fatal(err)
	}
	written, err := io.ReadAll(outR)
	if err != nil {
		t.Fatal(err)
	}

	want := Env{Width: DefaultWidth, Height: DefaultHeight, Profile: colorprofile.NoTTY,
		Dark: true, Theme: ThemeAuto, Plain: true}
	if e != want {
		t.Errorf("Detect = %+v\nwant     %+v", e, want)
	}
	if e.HasColor() {
		t.Error("HasColor() = true on a pipe")
	}
	if queries != 0 {
		t.Errorf("background queried %d times, want 0", queries)
	}
	if len(written) != 0 {
		t.Errorf("Detect wrote %q to stdout", written)
	}
}
