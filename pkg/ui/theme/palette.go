package theme

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// token is one semantic colour, with a value for each colour profile. Write
// it as an unkeyed literal, token{hex, ansi256, ansi}, so that the compiler
// rejects a token that lacks a profile.
type token struct {
	// hex is the TrueColor value, "#RRGGBB".
	hex string
	// ansi256 is an xterm-256 index from 16 to 255: the nearest to hex by
	// CIE76 ΔE, unless the test lists a deliberate override. Indexes 0-15
	// are never used: they are the terminal's 16 colours, which the user's
	// palette redefines.
	ansi256 ansi.IndexedColor
	// ansi is one of the 16 terminal colours, picked by hand rather than by
	// distance, because their RGB values vary between terminals.
	ansi ansi.BasicColor
}

// palette holds every semantic token of one theme. The token tag is the
// token's name in the design-system docs and the deviation log.
type palette struct {
	TextPrimary   token `token:"text.primary"`   // labels, menu items, the wordmark
	TextMuted     token `token:"text.muted"`     // descriptions, values, prompts, echoed input
	TextFaint     token `token:"text.faint"`     // status details, uptime, URL, parentheticals
	StatusOK      token `token:"status.ok"`      // Configured, success
	StatusBad     token `token:"status.bad"`     // Not Configured, failure
	StatusWarn    token `token:"status.warn"`    // partial, risk Moderate, warnings
	StatusUnknown token `token:"status.unknown"` // a state hardn could not check
	BadgeBG       token `token:"badge.bg"`       // host and user badges
	BadgeFG       token `token:"badge.fg"`
	BarBG         token `token:"bar.bg"`      // context and title bars, section stubs, version box
	AccentOK      token `token:"accent.ok"`   // callout bar beside a success title
	AccentWarn    token `token:"accent.warn"` // callout bar beside a warning title
	Divider       token `token:"divider"`     // rules
}

// The 16-colour picks are checked against a reference palette, x/ansi's RGB
// values for the 16 colours (color.go:269-284 in v0.11.8), standing in for
// the user's unknown terminal palette: every token meets its contrast floor
// there (TestContrast), except light status.warn. Muted and faint text
// share BrightBlack, so faint merges with muted at 16 colours (DEV-001).

// darkPalette is the dark theme, the Figma target: the export's values with
// the logged deviations (DEV-NNN in docs/design-system/deviations.md). The
// accents pair with the status colours on purpose (drift D14).
//
// 16 colours: status words take the bright hues, which match the light
// Figma colours and clear 4.5:1 on black, where the normal Red, Green and
// Cyan of the reference palette do not. Warn keeps Yellow, the amber slot,
// which clears it too.
var darkPalette = palette{
	TextPrimary:   token{"#C0C4D8", 251, lipgloss.White},
	TextMuted:     token{"#81869C", 103, lipgloss.BrightBlack},
	TextFaint:     token{"#565B6B", 59, lipgloss.BrightBlack}, // DEV-001
	StatusOK:      token{"#70D47F", 78, lipgloss.BrightGreen},
	StatusBad:     token{"#E74669", 204, lipgloss.BrightRed},
	StatusWarn:    token{"#DBB853", 179, lipgloss.Yellow},
	StatusUnknown: token{"#6AB0D8", 74, lipgloss.BrightCyan}, // DEV-010
	BadgeBG:       token{"#0C2AC0", 19, lipgloss.Blue},
	BadgeFG:       token{"#DFDFDF", 254, lipgloss.BrightWhite}, // DEV-003
	BarBG:         token{"#101114", 233, lipgloss.BrightBlack}, // DEV-004
	AccentOK:      token{"#008A00", 28, lipgloss.Green},
	AccentWarn:    token{"#D2BC13", 178, lipgloss.Yellow},
	Divider:       token{"#303440", 236, lipgloss.BrightBlack}, // DEV-002
}

// lightPalette is the light theme. Figma has none, so these values are ours
// (DEV-011), readable on white: at least 4.5:1 for primary text, status
// words and badge text, and 3:1 for muted and faint text, rules and accent
// bars. It is low priority; the maintainer refines it later.
//
// 16 colours: primary text is Black, status words take the normal hues,
// which are the darker ones, and bars take White, the light gray that
// matches bar.bg.
var lightPalette = palette{
	TextPrimary:   token{"#1E2230", 235, lipgloss.Black},
	TextMuted:     token{"#575C6E", 59, lipgloss.BrightBlack},
	TextFaint:     token{"#81869C", 103, lipgloss.BrightBlack},
	StatusOK:      token{"#1A7F37", 22, lipgloss.Green},
	StatusBad:     token{"#C2254A", 161, lipgloss.Red},
	StatusWarn:    token{"#8A6100", 94, lipgloss.Yellow},
	StatusUnknown: token{"#0A6E8C", 24, lipgloss.Cyan},
	BadgeBG:       token{"#0C2AC0", 19, lipgloss.Blue},
	BadgeFG:       token{"#FFFFFF", 231, lipgloss.BrightWhite},
	BarBG:         token{"#E6E8EE", 254, lipgloss.White},
	AccentOK:      token{"#008A00", 28, lipgloss.Green},
	AccentWarn:    token{"#B88A00", 136, lipgloss.Yellow},
	Divider:       token{"#81869C", 103, lipgloss.BrightBlack},
}
