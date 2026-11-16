package term

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func TestWriterDownsamples(t *testing.T) {
	// One truecolor SGR (bold, foreground #FF0000) and a reset.
	const in = "\x1b[1;38;2;255;0;0mhardn\x1b[m"
	tests := []struct {
		profile colorprofile.Profile
		want    string
	}{
		{colorprofile.TrueColor, in},
		{colorprofile.ANSI256, "\x1b[1;38;5;196mhardn\x1b[m"},
		{colorprofile.ANSI, "\x1b[1;91mhardn\x1b[m"},
		{colorprofile.ASCII, "\x1b[1mhardn\x1b[m"},
		{colorprofile.NoTTY, "hardn"},
		{colorprofile.Unknown, "hardn"}, // the zero Env
	}
	for _, tt := range tests {
		t.Run(tt.profile.String(), func(t *testing.T) {
			var buf bytes.Buffer
			if _, err := fmt.Fprint(Env{Profile: tt.profile}.Writer(&buf), in); err != nil {
				t.Fatal(err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFixed(t *testing.T) {
	tests := []struct {
		name string
		got  Env
		want Env
	}{
		{
			name: "truecolor unicode dark",
			got:  Fixed(80, colorprofile.TrueColor, true, true),
			want: Env{Width: 80, Height: DefaultHeight, Profile: colorprofile.TrueColor, Unicode: true,
				TTY: true, InputTTY: true, Dark: true, Theme: ThemeDark},
		},
		{
			name: "no colour ascii light",
			got:  Fixed(60, colorprofile.ASCII, false, false),
			want: Env{Width: 60, Height: DefaultHeight, Profile: colorprofile.ASCII,
				TTY: true, InputTTY: true, Theme: ThemeLight},
		},
		{
			name: "plain",
			got:  Fixed(120, colorprofile.NoTTY, false, true),
			want: Env{Width: 120, Height: DefaultHeight, Profile: colorprofile.NoTTY,
				Dark: true, Theme: ThemeDark, Plain: true},
		},
		{
			name: "plain ignores unicode",
			got:  Fixed(80, colorprofile.NoTTY, true, false),
			want: Env{Width: 80, Height: DefaultHeight, Profile: colorprofile.NoTTY,
				Theme: ThemeLight, Plain: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %+v\nwant %+v", tt.got, tt.want)
			}
		})
	}
}

func TestHasColor(t *testing.T) {
	for p, want := range map[colorprofile.Profile]bool{
		colorprofile.TrueColor: true,
		colorprofile.ANSI256:   true,
		colorprofile.ANSI:      true,
		colorprofile.ASCII:     false,
		colorprofile.NoTTY:     false,
		colorprofile.Unknown:   false,
	} {
		if got := (Env{Profile: p}).HasColor(); got != want {
			t.Errorf("%v: HasColor() = %v, want %v", p, got, want)
		}
	}
}

func TestParseModes(t *testing.T) {
	colors := []struct {
		in   string
		want ColorMode
		ok   bool
	}{
		{"auto", ColorAuto, true},
		{"always", ColorAlways, true},
		{"NEVER", ColorNever, true},
		{"", ColorUnset, false},
		{"unset", ColorUnset, false},
		{"yes", ColorUnset, false},
	}
	for _, tt := range colors {
		got, err := ParseColorMode(tt.in)
		if got != tt.want || (err == nil) != tt.ok {
			t.Errorf("ParseColorMode(%q) = %v, %v; want %v, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
	themes := []struct {
		in   string
		want ThemeMode
		ok   bool
	}{
		{"dark", ThemeDark, true},
		{"Light", ThemeLight, true},
		{"auto", ThemeAuto, true},
		{"", ThemeUnset, false},
		{"solarized", ThemeUnset, false},
	}
	for _, tt := range themes {
		got, err := ParseThemeMode(tt.in)
		if got != tt.want || (err == nil) != tt.ok {
			t.Errorf("ParseThemeMode(%q) = %v, %v; want %v, ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
	if _, err := ParseColorMode("yes"); err == nil || err.Error() != `invalid color mode "yes": want auto, always or never` {
		t.Errorf("error = %v", err)
	}
	for m, want := range map[fmt.Stringer]string{
		ColorUnset: "unset", ColorAlways: "always", ColorMode(9): "ColorMode(9)",
		ThemeUnset: "unset", ThemeLight: "light", ThemeMode(-1): "ThemeMode(-1)",
	} {
		if got := m.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}

// TestClear: Clear writes the clear on a terminal, whatever its colours,
// and nothing when the Env is plain: piped output, or --plain on a
// terminal.
func TestClear(t *testing.T) {
	plainTTY := Fixed(80, colorprofile.TrueColor, true, true)
	plainTTY.Plain = true
	tty := func(height int, environ ...string) Env {
		return Resolve(Options{}, Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: height, Environ: append([]string{"TERM=xterm-256color"}, environ...)})
	}
	plainMeasured := tty(30)
	plainMeasured.Plain = true
	for _, tc := range []struct {
		name string
		env  Env
		want string
	}{
		{"truecolor terminal", Fixed(80, colorprofile.TrueColor, true, true), "\x1b[H\x1b[2J"},
		{"ASCII terminal", Fixed(80, colorprofile.ASCII, false, false), "\x1b[H\x1b[2J"},
		{"piped", Fixed(80, colorprofile.NoTTY, false, false), ""},
		{"plain on a terminal", plainTTY, ""},
		{"resolved off a terminal", Resolve(Options{}, Facts{Width: 80}), ""},
		{"resolved on a terminal", Resolve(Options{}, Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Environ: []string{"TERM=xterm-256color"}}), "\x1b[H\x1b[2J"},
		// A measured height: Height-1 newlines scroll what was printed into
		// the scrollback, then the cursor goes home; no ED 2.
		{"a terminal of 30 rows", tty(30), strings.Repeat("\n", 29) + "\x1b[H"},
		{"a terminal of 2 rows", tty(2), "\n\x1b[H"},
		{"a terminal of 1 row", tty(1), "\x1b[H\x1b[2J"},
		{"LINES only: not measured", tty(0, "LINES=30"), "\x1b[H\x1b[2J"},
		{"plain on a measured terminal", plainMeasured, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := tc.env.Clear(&b); err != nil || b.String() != tc.want {
				t.Errorf("wrote %q (%v), want %q", b.String(), err, tc.want)
			}
		})
	}
}

// TestClearThroughWriter checks what Writer does to Clear's bytes on a
// terminal that is not Plain, its height measured and not: at ASCII and
// above they pass unchanged; at NoTTY, a terminal with TERM unset, the
// cursor home and ED 2 are stripped, so a clear goes to the stream itself
// (Stream).
func TestClearThroughWriter(t *testing.T) {
	for _, p := range []colorprofile.Profile{colorprofile.NoTTY, colorprofile.ASCII, colorprofile.ANSI, colorprofile.ANSI256, colorprofile.TrueColor} {
		for _, measured := range []bool{false, true} {
			env := Env{Width: 80, Height: 24, HeightMeasured: measured, Profile: p, TTY: true}
			var raw, through, stream bytes.Buffer
			_ = env.Clear(&raw)
			_ = env.Clear(env.Writer(&through))
			_ = env.Clear(Stream(env.Writer(&stream)))
			if raw.Len() == 0 {
				t.Fatalf("%v, measured %v: Clear wrote nothing", p, measured)
			}
			if got, want := through.String() == raw.String(), p > colorprofile.NoTTY; got != want {
				t.Errorf("%v, measured %v: through Writer %q, unchanged %v; want unchanged %v", p, measured, through.String(), got, want)
			}
			if stream.String() != raw.String() {
				t.Errorf("%v, measured %v: through Stream %q, want %q", p, measured, stream.String(), raw.String())
			}
		}
	}
	if e := Resolve(Options{}, Facts{StdoutTTY: true, StdinTTY: true, Width: 80, Height: 24}); e.Plain || e.Profile != colorprofile.NoTTY {
		t.Errorf("TERM unset on a terminal: Plain %v, %v; want a terminal at NoTTY", e.Plain, e.Profile)
	}
	if w := new(bytes.Buffer); Stream(w) != w {
		t.Error("Stream(w) of a w that is no Writer's is not w")
	}
}
