package term

import (
	"bytes"
	"fmt"
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			if err := tc.env.Clear(&b); err != nil || b.String() != tc.want {
				t.Errorf("wrote %q (%v), want %q", b.String(), err, tc.want)
			}
		})
	}
}
