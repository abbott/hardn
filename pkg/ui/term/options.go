package term

import (
	"fmt"
	"strings"
)

// ColorMode is the value of the --color option. The zero value, ColorUnset,
// means the option was not given; Resolve then behaves as for ColorAuto.
type ColorMode int

// Color modes.
const (
	// ColorUnset means no --color option was given.
	ColorUnset ColorMode = iota
	// ColorAuto colours a terminal and leaves other output plain, honouring
	// NO_COLOR and TERM=dumb.
	ColorAuto
	// ColorAlways colours output even when it is not a terminal and even
	// when NO_COLOR is set.
	ColorAlways
	// ColorNever drops colour but keeps bold and underline on a terminal.
	ColorNever
)

var colorModeNames = []string{"unset", "auto", "always", "never"}

// String returns the option spelling of m ("auto", "always", "never"), or
// "unset" for ColorUnset.
func (m ColorMode) String() string { return modeName("ColorMode", colorModeNames, int(m)) }

// ParseColorMode parses a --color value: auto, always or never, in any case.
func ParseColorMode(s string) (ColorMode, error) {
	i, err := parseMode("color mode", colorModeNames, s)
	return ColorMode(i), err
}

// ThemeMode is the value of the --theme option or HARDN_THEME. The zero
// value, ThemeUnset, means neither was given; Resolve then picks ThemeDark.
type ThemeMode int

// Theme modes.
const (
	// ThemeUnset means no theme was chosen.
	ThemeUnset ThemeMode = iota
	// ThemeDark selects the dark theme, the Figma target.
	ThemeDark
	// ThemeLight selects the light theme.
	ThemeLight
	// ThemeAuto asks Detect to query the terminal's background colour.
	ThemeAuto
)

var themeModeNames = []string{"unset", "dark", "light", "auto"}

// String returns the option spelling of m ("dark", "light", "auto"), or
// "unset" for ThemeUnset.
func (m ThemeMode) String() string { return modeName("ThemeMode", themeModeNames, int(m)) }

// ParseThemeMode parses a --theme or HARDN_THEME value: dark, light or auto,
// in any case.
func ParseThemeMode(s string) (ThemeMode, error) {
	i, err := parseMode("theme", themeModeNames, s)
	return ThemeMode(i), err
}

// Options holds the render choices the user made explicitly: the future
// --color, --ascii, --plain and --theme flags (not wired yet; maintainer
// decision §3.14). Every zero value means "not given", so the matching
// environment variable or detection decides instead.
type Options struct {
	Color ColorMode // --color
	ASCII bool      // --ascii
	Plain bool      // --plain
	Theme ThemeMode // --theme
}

// parseMode returns the index of s in names, ignoring case. Index 0 is the
// unset value, which is never accepted.
func parseMode(kind string, names []string, s string) (int, error) {
	for i := 1; i < len(names); i++ {
		if strings.EqualFold(s, names[i]) {
			return i, nil
		}
	}
	last := len(names) - 1
	return 0, fmt.Errorf("invalid %s %q: want %s or %s", kind, s, strings.Join(names[1:last], ", "), names[last])
}

func modeName(typ string, names []string, i int) string {
	if i < 0 || i >= len(names) {
		return fmt.Sprintf("%s(%d)", typ, i)
	}
	return names[i]
}
