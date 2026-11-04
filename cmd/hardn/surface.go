package main

// The render flags (refactor plan §3.14, approved 2026-10-17, wired by
// Phase 7's step 5 after the maintainer's "adopt all", 2026-11-04):
// --color, --ascii, --plain and --theme, each with its environment
// variable as term.Resolve reads it (docs/design-system/README.md, the
// term.Resolve table), --no-color an alias of --color=never with a
// deprecation warning (§3.11).

import (
	"io"
	"os"
	"sync"

	"github.com/spf13/pflag"

	"github.com/abbott/hardn/pkg/ui/term"
)

// The render flags' values: the zero values are "not given", so that
// HARDN_ASCII, HARDN_PLAIN, HARDN_THEME, NO_COLOR and TERM decide
// (term.Resolve).
var (
	colorMode   term.ColorMode // --color
	asciiOutput bool           // --ascii
	plainOutput bool           // --plain
	themeMode   term.ThemeMode // --theme
)

// colorValue is --color's pflag.Value: auto, always or never
// (term.ParseColorMode); anything else is a usage error before anything
// runs, as cobra reports a bad flag value.
type colorValue struct{ m *term.ColorMode }

func (v colorValue) String() string {
	if *v.m == term.ColorUnset {
		return term.ColorAuto.String()
	}
	return v.m.String()
}
func (v colorValue) Set(s string) (err error) { *v.m, err = term.ParseColorMode(s); return err }
func (colorValue) Type() string               { return "auto|always|never" }

// themeValue is --theme's pflag.Value: dark, light or auto
// (term.ParseThemeMode).
type themeValue struct{ m *term.ThemeMode }

func (v themeValue) String() string {
	if *v.m == term.ThemeUnset {
		return ""
	}
	return v.m.String()
}
func (v themeValue) Set(s string) (err error) { *v.m, err = term.ParseThemeMode(s); return err }
func (themeValue) Type() string               { return "dark|light|auto" }

// addSurfaceFlags adds the render flags to flags, the root command's
// persistent flags, so that every command takes them.
// The words are the flags' help (reported to p7-docs).
func addSurfaceFlags(flags *pflag.FlagSet) {
	flags.Var(colorValue{&colorMode}, "color", "When to colour the output; auto colours a terminal unless NO_COLOR is set or TERM is dumb")
	flags.BoolVar(&asciiOutput, "ascii", false, "Use ASCII glyphs only (or set HARDN_ASCII=1)")
	flags.BoolVar(&plainOutput, "plain", false, "Plain output: ASCII glyphs, no colour, no animation (or set HARDN_PLAIN=1)")
	flags.Var(themeValue{&themeMode}, "theme", "The colour theme; auto asks the terminal for its background (or set HARDN_THEME; default dark)")
	flags.BoolVar(&noColor, "no-color", false, "Deprecated: use --color=never")
	noColorFlag = flags.Lookup("no-color")
}

// noColorFlag is --no-color as the root command's persistent flags hold
// it: one *pflag.Flag for every command, so its Changed says it was given
// to any of them.
var noColorFlag *pflag.Flag

// noColorDeprecated is --no-color's warning (refactor plan §3.11), said
// once whenever the flag is given, by every command (warnNoColor).
const noColorDeprecated = "--no-color is now --color=never"

// noColorGiven reports whether --no-color was given, true or false, to
// any command (noColorFlag).
func noColorGiven() bool {
	return noColorFlag != nil && noColorFlag.Changed
}

// warnNoColor says noColorDeprecated on stderr, a warning, where
// --no-color was given (noColorGiven): from the root command's
// PersistentPreRun, so before anything else is said, by -p, --version and
// system-details too, and from confirm's and rollback's (noRevertNotices).
func warnNoColor() {
	if !noColorGiven() {
		return
	}
	if w := deprecationWarnings(stderrEnv(), "", nil, noColorDeprecated); w != "" {
		_, _ = io.WriteString(stderrEnv().Writer(stderr), w)
	}
}

// renderOptions are the render choices given on the command line, for
// term.Detect: --no-color as --color=never where --color is not given.
func renderOptions() term.Options {
	o := term.Options{Color: colorMode, ASCII: asciiOutput, Plain: plainOutput, Theme: themeMode}
	if noColor && o.Color == term.ColorUnset {
		o.Color = term.ColorNever
	}
	return o
}

// The auto theme's answer, once the terminal has been asked (themeFor).
var (
	themeMu       sync.Mutex
	themeAnswered term.ThemeMode // ThemeDark or ThemeLight; ThemeUnset until asked
)

// streamEnv is pkg/ui/term's detection for the stream f, as it detects
// stdout: f a terminal, TERM, NO_COLOR, HARDN_PLAIN, HARDN_ASCII,
// HARDN_THEME and the render flags (renderOptions). Where the theme
// resolves to auto (--theme=auto or HARDN_THEME=auto), the terminal is
// asked for its background once, by the first stream that can ask
// (term.Detect: stdin and f terminals, not plain), and every Env after it
// takes that answer, so that hardn never asks twice.
func streamEnv(f *os.File) term.Env {
	opts := renderOptions()
	themeMu.Lock()
	defer themeMu.Unlock()
	if themeAnswered != term.ThemeUnset {
		opts.Theme = themeAnswered
		return term.Detect(opts, os.Stdin, f)
	}
	e := term.Detect(opts, os.Stdin, f)
	if e.Theme == term.ThemeAuto && e.TTY && e.InputTTY && !e.Plain { // asked (term.Detect)
		themeAnswered = term.ThemeLight
		if e.Dark {
			themeAnswered = term.ThemeDark
		}
	}
	return e
}
