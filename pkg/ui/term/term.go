// Package term is hardn's render environment (RenderEnv): terminal size,
// colour profile, glyph set, TTY status, background and plain mode.
//
// Detect builds an Env once at startup and the caller passes it to every
// renderer; renderers never inspect the terminal or the environment
// themselves. Resolve holds every precedence rule as a pure function, and
// Fixed builds an Env for tests and goldens.
//
// All rendered output is written through Env.Writer, which downsamples colours
// to Env.Profile. Never use lipgloss.Print*, Fprint* or Sprint*: they use a
// profile Lip Gloss detected itself from os.Stdout and the environment
// (Fprint* detects again on every call), ignoring Options.
package term

import (
	"io"

	"github.com/charmbracelet/colorprofile"
)

// The size Resolve falls back to when neither the terminal nor COLUMNS and
// LINES give one: the 80x24 terminal the design targets.
const (
	DefaultWidth  = 80
	DefaultHeight = 24
)

// Env is the render environment. Build it with Detect at startup, or with
// Fixed in tests, and pass it to every renderer.
type Env struct {
	// Width and Height are the terminal size in cells.
	Width, Height int
	// Profile is the colour profile Writer downsamples to: TrueColor,
	// ANSI256 or ANSI keep colour; ASCII keeps only bold, underline and
	// other attributes; NoTTY strips every escape sequence.
	Profile colorprofile.Profile
	// Unicode selects the Unicode glyph set; false selects the ASCII set.
	Unicode bool
	// TTY reports whether stdout is a terminal.
	TTY bool
	// InputTTY reports whether stdin is a terminal.
	InputTTY bool
	// Dark selects the dark theme; false selects the light theme.
	Dark bool
	// Theme is the theme mode Resolve settled on: ThemeDark, ThemeLight or
	// ThemeAuto. ThemeAuto asks Detect to query the terminal background;
	// Dark holds the answer, or true when no query was made.
	Theme ThemeMode
	// Plain is plain mode (--plain, HARDN_PLAIN, or stdout not a terminal):
	// ASCII glyphs, no animation and, unless --color=always, no colour or
	// text attributes.
	Plain bool
	// Animate allows spinners and in-place progress updates: stdout is a
	// terminal with cursor control (see Resolve). When false, print one line
	// per step instead.
	Animate bool
}

// Fixed returns an Env for tests, goldens, the gallery and output off a terminal
// (component.OffTerminal), built only from its arguments: it never reads the
// terminal or the environment. Height is
// DefaultHeight and Animate is false, so output is deterministic. A profile
// of NoTTY or lower makes the Env plain with no TTY, as Resolve does for piped
// output; any other profile is an interactive terminal (TTY and InputTTY
// true). Unicode is used as given, except that a plain Env always uses ASCII
// glyphs, as from Resolve. Set fields directly for other combinations.
func Fixed(width int, profile colorprofile.Profile, unicode, dark bool) Env {
	plain := profile <= colorprofile.NoTTY
	theme := ThemeLight
	if dark {
		theme = ThemeDark
	}
	return Env{
		Width:    width,
		Height:   DefaultHeight,
		Profile:  profile,
		Unicode:  unicode && !plain,
		TTY:      !plain,
		InputTTY: !plain,
		Dark:     dark,
		Theme:    theme,
		Plain:    plain,
	}
}

// HasColor reports whether output keeps colour (Profile ANSI or better), so
// a component can pick a colour-free fallback, such as a rule in place of a
// background-coloured bar.
func (e Env) HasColor() bool {
	return e.Profile >= colorprofile.ANSI
}

// Writer returns a writer that forwards to w with every SGR sequence
// downsampled to e.Profile (see the Profile field). The profile is fixed:
// nothing is detected again. All rendered output goes through this writer;
// renderers return full-colour strings and never downsample themselves.
// e.Profile was resolved for stdout: wrap stdout with it. Another stream,
// such as a redirected stderr, needs its own profile decision, which this
// package does not make yet.
//
// colorprofile parses each Write on its own (writer.go:54-80 in v0.4.3), so
// write whole rendered strings: an escape sequence split across two writes
// is not downsampled. At ASCII an SGR that held only colours becomes a full
// reset (ESC[m; x/ansi style.go:45-47), so attributes must share the SGR of
// their colour, as they do within one Lip Gloss style. A zero Env has an
// unknown profile, which strips everything.
func (e Env) Writer(w io.Writer) io.Writer {
	return &colorprofile.Writer{Forward: w, Profile: e.Profile}
}
