package term

import (
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/colorprofile"
)

// Facts is what Detect observed about the process. Resolve takes it as input,
// so every rule can be tested without a terminal.
type Facts struct {
	StdoutTTY bool     // stdout is a terminal
	StdinTTY  bool     // stdin is a terminal
	Width     int      // terminal columns; 0 if unknown
	Height    int      // terminal rows; 0 if unknown
	Environ   []string // the environment as "KEY=value" entries, as os.Environ returns it
}

// Environment variables Resolve reads itself. colorprofile.Env also reads
// TERM, COLORTERM, CLICOLOR and CLICOLOR_FORCE (see Resolve).
const (
	envPlain   = "HARDN_PLAIN"
	envASCII   = "HARDN_ASCII"
	envTheme   = "HARDN_THEME"
	envNoColor = "NO_COLOR"
	envTerm    = "TERM"
	envColumns = "COLUMNS"
	envLines   = "LINES"
)

// localeVars are the variables that select the character set, in the order
// POSIX consults them.
var localeVars = []string{"LC_ALL", "LC_CTYPE", "LANG"}

// Resolve computes the render environment from the user's options and the
// observed facts. It is pure: it reads nothing but its arguments. An option
// beats an environment variable, which beats detection. A boolean variable
// is true when its value is 1, true, yes or on, in any case.
//
// Plain: --plain, HARDN_PLAIN, or stdout is not a terminal.
//
// Profile, by --color:
//   - never: ASCII (no colour; bold and underline kept), or NoTTY when Plain.
//   - always: colorprofile.Env, raised to at least ANSI, even when Plain.
//     NO_COLOR is removed from the environment first, so a 256-colour
//     terminal keeps 256 colours.
//   - auto or unset: NoTTY when Plain; else ASCII when NO_COLOR is set to
//     any non-empty value (no-color.org, so NO_COLOR=0 also disables colour)
//     or TERM is dumb; else colorprofile.Env.
//
// colorprofile.Env (v0.4.3, env.go:70-202) reads only the environment: no
// isatty, terminfo or tmux. TERM unset or dumb gives NoTTY. Any other TERM
// starts at ANSI (an empty one at NoTTY) and is raised to ANSI256 by
// *256color, tmux* or screen*, and to TrueColor by *direct, by a TERM
// containing kitty, wezterm, alacritty, ghostty, foot, contour, rio or "st"
// (so nsterm counts), by COLORTERM=truecolor, 24bit, yes or true (ignored
// for tmux* and screen*), or by a non-empty WT_SESSION or a true
// GOOGLE_CLOUD_SHELL. It parses NO_COLOR, CLICOLOR_FORCE and CLICOLOR with
// strconv.ParseBool, so NO_COLOR=0 or NO_COLOR=yes leave colour on. A true
// NO_COLOR caps the profile at ASCII and beats CLICOLOR_FORCE. A true
// CLICOLOR_FORCE raises it to at least ANSI, even for TERM=dumb, and
// colorprofile.Detect also honours it when output is not a terminal. A true
// CLICOLOR raises a terminal to ANSI; CLICOLOR=0 is ignored. Our rules win
// where these differ: NO_COLOR, TERM=dumb and the TTY check are decided
// above before colorprofile is asked, so CLICOLOR_FORCE can neither colour
// piped output nor a dumb terminal (--color=always does), and --color=never
// beats it.
//
// Unicode is false with --ascii, HARDN_ASCII or Plain; when TERM is linux or
// dumb; when the effective locale (the first non-empty of LC_ALL, LC_CTYPE
// and LANG) does not name a UTF-8 codeset (".UTF-8" or ".utf8", in any
// case), which covers an unset locale, C, POSIX and a name with no codeset;
// and when the locale's language is zh, ja or ko, because CJK terminals draw
// the ambiguous-width glyphs two cells wide. C.UTF-8 is UTF-8.
//
// Theme: --theme, else a valid HARDN_THEME (an invalid value is ignored),
// else dark. Dark is false only for light; auto leaves Dark true and Theme
// ThemeAuto for Detect to act on.
//
// Width and Height: the terminal size when stdout is a terminal and the size
// is known, else COLUMNS and LINES when they are positive integers, else
// DefaultWidth and DefaultHeight. Each dimension is resolved on its own.
//
// Animate: stdout is a terminal and the Env is not Plain.
func Resolve(opts Options, f Facts) Env {
	env := environMap(f.Environ)
	e := Env{TTY: f.StdoutTTY, InputTTY: f.StdinTTY}
	e.Plain = opts.Plain || truthy(env[envPlain]) || !f.StdoutTTY
	e.Profile = resolveProfile(opts.Color, e.Plain, env, f.Environ)
	e.Unicode = !opts.ASCII && !truthy(env[envASCII]) && !e.Plain && unicodeTerminal(env)
	e.Theme = opts.Theme
	if e.Theme == ThemeUnset {
		e.Theme, _ = ParseThemeMode(env[envTheme]) // ThemeUnset when invalid
	}
	if e.Theme == ThemeUnset {
		e.Theme = ThemeDark
	}
	e.Dark = e.Theme != ThemeLight
	e.Width = dimension(f.StdoutTTY, f.Width, env[envColumns], DefaultWidth)
	e.Height = dimension(f.StdoutTTY, f.Height, env[envLines], DefaultHeight)
	e.Animate = f.StdoutTTY && !e.Plain
	return e
}

func resolveProfile(mode ColorMode, plain bool, env map[string]string, environ []string) colorprofile.Profile {
	switch mode {
	case ColorNever:
		if plain {
			return colorprofile.NoTTY
		}
		return colorprofile.ASCII
	case ColorAlways:
		environ = slices.DeleteFunc(slices.Clone(environ), func(kv string) bool {
			return strings.HasPrefix(kv, envNoColor+"=")
		})
		return max(colorprofile.Env(environ), colorprofile.ANSI)
	}
	switch {
	case plain:
		return colorprofile.NoTTY
	case env[envNoColor] != "", env[envTerm] == "dumb":
		return colorprofile.ASCII
	}
	return colorprofile.Env(environ)
}

// unicodeTerminal applies the TERM and locale parts of the Unicode rule.
func unicodeTerminal(env map[string]string) bool {
	if t := env[envTerm]; t == "linux" || t == "dumb" {
		return false
	}
	var locale string
	for _, v := range localeVars {
		if locale = env[v]; locale != "" {
			break
		}
	}
	// language[_territory][.codeset][@modifier]
	name, _, _ := strings.Cut(locale, "@")
	lang, codeset, _ := strings.Cut(name, ".")
	if strings.ReplaceAll(strings.ToLower(codeset), "-", "") != "utf8" {
		return false
	}
	lang, _, _ = strings.Cut(lang, "_")
	return !slices.Contains([]string{"zh", "ja", "ko"}, strings.ToLower(lang))
}

// dimension picks one of Width or Height (see Resolve).
func dimension(tty bool, measured int, envValue string, fallback int) int {
	if tty && measured > 0 {
		return measured
	}
	if n, err := strconv.Atoi(envValue); err == nil && n > 0 {
		return n
	}
	return fallback
}

func truthy(v string) bool {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// environMap indexes "KEY=value" entries. A later duplicate wins, as in
// colorprofile, so both read the same TERM.
func environMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}
