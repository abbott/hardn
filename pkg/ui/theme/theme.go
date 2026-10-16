// Package theme is hardn's design-system theme: the semantic colour tokens
// of the dark and light palettes, the named style slots that components
// render with, the Unicode and ASCII glyph sets, and the grid.
//
// New builds a Theme for a render environment, as huh's ThemeFunc builds
// one for a background: the palette follows Env.Dark, colours resolve for
// Env.Profile through lipgloss.Complete, and the glyphs follow Env.Unicode.
// Components take their styles, glyphs and columns from here and pick none
// themselves.
//
// The values derive from the Figma export, which is a reference and never
// loaded; departures are logged in docs/design-system/deviations.md.
//
// Rules the tests enforce:
//   - Every token has a value for truecolor, 256 colours and 16 colours.
//   - Muted and faint text are gray tokens. No slot sets SGR faint, blink or
//     reverse (DEV-009).
//   - Slots carry colour and text attributes only. Layout (a size or maximum
//     size, padding, margins, a border, alignment, inline) belongs to the
//     components and the grid constants.
//
// By convention, and unchecked, each palette is one struct literal in
// palette.go.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/abbott/hardn/pkg/ui/term"
)

// Theme holds the glyph set and a named style for every component and
// state, grouped by component. Build it with New.
//
// Not here yet, because their components come later: form fields and
// inline validation errors (next/p1c-form), ErrorReport and the confirm
// countdown (next/p1b-plan), and the glyphs of PlanView and StepProgress
// (next/p1b-plan). SubjectLine and BulletList compose Text, Badge and
// KeyValue.Bullet. The cursor has no slot: the terminal draws its own
// (candidate C8).
type Theme struct {
	Glyphs Glyphs

	Text       TextStyles
	Header     HeaderStyles
	ContextBar ContextBarStyles
	TitleBar   TitleBarStyles
	Host       HostStyles
	Risk       RiskStyles
	Status     StatusStyles
	Divider    lipgloss.Style // the rule between the status list and the menu
	Mode       ModeStyles
	Menu       MenuStyles
	Prompt     PromptStyles
	Message    MessageStyles
	Callout    CalloutStyles
	KeyValue   KeyValueStyles
	Badge      lipgloss.Style // white text on blue: the host and user badges
	Section    SectionStyles
	Breadcrumb BreadcrumbStyles
	Plan       PlanStyles
	Step       StepStyles
}

// TextStyles are the plain text roles, for runs no component names, such
// as the comma in "Enabled, No password".
type TextStyles struct {
	Primary, Muted, Faint lipgloss.Style
}

// HeaderStyles style the first line of the main menu: "hardn v0.4.0" and
// the repository link.
type HeaderStyles struct {
	Wordmark lipgloss.Style // "hardn", bold
	Version  lipgloss.Style // in a bar.bg box above 16 colours (DEV-005)
	URL      lipgloss.Style
}

// ContextBarStyles style the distro bar: "Debian 12 (Bookworm)".
type ContextBarStyles struct {
	Fill     lipgloss.Style // the bar's stub and fill
	Name     lipgloss.Style // "Debian", bold
	Version  lipgloss.Style // "12"
	Codename lipgloss.Style // "(Bookworm)"
}

// TitleBarStyles style a sub-screen's title bar, such as "User Management".
type TitleBarStyles struct {
	Fill  lipgloss.Style
	Title lipgloss.Style // bold
}

// HostStyles style the host block beside the badge.
type HostStyles struct {
	Address lipgloss.Style // the IP address
	Uptime  lipgloss.Style
	Reboot  lipgloss.Style // the pending-reboot finding (proposal P-04)
}

// RiskStyles style the risk summary. The bands are named by tone, so the
// status model maps levels onto them: origin/main's Critical and High use
// High, Moderate uses Moderate, and Low and Minimal use Low.
type RiskStyles struct {
	Label    lipgloss.Style // "Risk Level", bold
	Low      lipgloss.Style // the band's glyph and level word
	Moderate lipgloss.Style
	High     lipgloss.Style
	Unknown  lipgloss.Style
	Gloss    lipgloss.Style // "≈ medium security"
}

// StatusStyles style a StatusRow: marker, label, state word and detail. A
// state's style colours both its glyph and its word, so the state is never
// shown by colour alone.
type StatusStyles struct {
	Label         lipgloss.Style
	Detail        lipgloss.Style // "non-root, sudo"
	Configured    lipgloss.Style
	NotConfigured lipgloss.Style
	Partial       lipgloss.Style
	NotApplicable lipgloss.Style
	Unknown       lipgloss.Style
}

// ModeStyles style the mode indicator: "Dry-run Mode Disabled".
type ModeStyles struct {
	Label  lipgloss.Style
	Live   lipgloss.Style // changes apply: amber, as Figma's "Disabled"
	DryRun lipgloss.Style // changes are only previewed
}

// MenuStyles style a numbered menu and its heading.
type MenuStyles struct {
	Heading     lipgloss.Style // "Select an option", underlined
	Key         lipgloss.Style // "1)", bold
	Label       lipgloss.Style
	Description lipgloss.Style
	Aside       lipgloss.Style // a parenthetical in a description: "(sudo, SSH keys)"
	ExitKey     lipgloss.Style // the "0)" of Exit or Return, bold
	Exit        lipgloss.Style // the Exit or Return label and description
	Disabled    lipgloss.Style // every part of an item that cannot be chosen now
}

// PromptStyles style the prompt line: "❯ Enter your choice [0-10 or q]:".
type PromptStyles struct {
	Glyph lipgloss.Style // bold
	Label lipgloss.Style // bold
	Hint  lipgloss.Style
	Input lipgloss.Style // the echoed answer (drift D4)
}

// MessageStyles style one-line messages: a bold word for the kind, with its
// glyph, then the text.
type MessageStyles struct {
	OK        lipgloss.Style
	Warn      lipgloss.Style
	Bad       lipgloss.Style
	Info      lipgloss.Style
	Cancelled lipgloss.Style
	Text      lipgloss.Style
}

// CalloutStyles style a callout: an accent bar beside a bold title, with
// body lines under the title, such as "Warning" or "Operation Successful".
type CalloutStyles struct {
	OK        CalloutKind
	Warn      CalloutKind
	Cancelled CalloutKind
	Body      lipgloss.Style
}

// CalloutKind is one kind of callout.
type CalloutKind struct {
	Bar   lipgloss.Style // the accent bar's background
	Title lipgloss.Style // bold
}

// KeyValueStyles style key-value rows: "- Sudo  Enabled, No password". A
// value that carries a state takes ValueOK, ValueWarn or ValueBad.
type KeyValueStyles struct {
	Bullet    lipgloss.Style
	Key       lipgloss.Style
	Value     lipgloss.Style
	ValueOK   lipgloss.Style
	ValueWarn lipgloss.Style
	ValueBad  lipgloss.Style
}

// SectionStyles style a section header: a stub, a title and a rule, such as
// "User Configuration".
type SectionStyles struct {
	Fill  lipgloss.Style // the stub
	Title lipgloss.Style // bold
	Rule  lipgloss.Style
}

// BreadcrumbStyles style a breadcrumb: "hardn › Firewall".
type BreadcrumbStyles struct {
	Item      lipgloss.Style
	Current   lipgloss.Style // bold
	Separator lipgloss.Style
}

// PlanStyles style the lines of a PlanView.
type PlanStyles struct {
	Added       lipgloss.Style
	Removed     lipgloss.Style
	Context     lipgloss.Style
	LockoutRisk lipgloss.Style // a change that could lock the user out, bold
}

// StepStyles style StepProgress, one style per step state.
type StepStyles struct {
	Pending    lipgloss.Style
	Running    lipgloss.Style
	Done       lipgloss.Style
	Failed     lipgloss.Style
	RolledBack lipgloss.Style
}

// New returns the theme for env: the dark palette when env.Dark is true,
// otherwise the light one; colours for env.Profile; and the Unicode glyphs
// when env.Unicode is true, otherwise the ASCII ones. New reads nothing but
// env.
//
// Each colour is the token's value for the profile, as lipgloss.Complete
// picks it: the hex at TrueColor, the 256-colour index at ANSI256 and the
// 16-colour index at ANSI (color.go:265-277 in lipgloss v2.0.6).
// Env.Writer leaves them as they are, so the deliberate picks reach the
// terminal: it does not parse TrueColor output, it keeps an indexed colour
// at ANSI256 and a basic one at ANSI (colorprofile v0.4.3 writer.go:40-46,
// profile.go:88-96). At ASCII and NoTTY, Complete returns
// lipgloss.NoColor, which Style.Render does not emit (style.go:353-371), so
// only bold and underline remain.
func New(env term.Env) *Theme {
	p := &lightPalette
	if env.Dark {
		p = &darkPalette
	}
	complete := lipgloss.Complete(env.Profile)
	c := func(t token) color.Color {
		return complete(t.ansi, t.ansi256, lipgloss.Color(t.hex))
	}
	fg := func(t token) lipgloss.Style { return lipgloss.NewStyle().Foreground(c(t)) }
	bg := func(t token) lipgloss.Style { return lipgloss.NewStyle().Background(c(t)) }

	primary, muted, faint := fg(p.TextPrimary), fg(p.TextMuted), fg(p.TextFaint)
	ok, bad, warn, unknown := fg(p.StatusOK), fg(p.StatusBad), fg(p.StatusWarn), fg(p.StatusUnknown)
	fill := bg(p.BarBG)

	// DEV-005: at 16 colours bar.bg is BrightBlack, the colour of muted
	// text, so the version box is dropped there.
	version := muted
	if env.Profile > colorprofile.ANSI {
		version = version.Background(c(p.BarBG))
	}

	glyphs := asciiGlyphs
	if env.Unicode {
		glyphs = unicodeGlyphs
	}

	return &Theme{
		Glyphs: glyphs,
		Text:   TextStyles{Primary: primary, Muted: muted, Faint: faint},
		Header: HeaderStyles{Wordmark: primary.Bold(true), Version: version, URL: faint},
		ContextBar: ContextBarStyles{
			Fill: fill, Name: muted.Bold(true), Version: muted, Codename: faint,
		},
		TitleBar: TitleBarStyles{Fill: fill, Title: primary.Bold(true)},
		Host:     HostStyles{Address: muted, Uptime: faint, Reboot: warn},
		Risk: RiskStyles{
			Label: primary.Bold(true),
			Low:   ok, Moderate: warn, High: bad, Unknown: unknown,
			Gloss: faint,
		},
		Status: StatusStyles{
			Label: primary, Detail: faint,
			Configured: ok, NotConfigured: bad, Partial: warn,
			NotApplicable: muted, Unknown: unknown,
		},
		Divider: fg(p.Divider),
		Mode:    ModeStyles{Label: muted, Live: warn, DryRun: ok},
		Menu: MenuStyles{
			Heading:     muted.Underline(true),
			Key:         primary.Bold(true),
			Label:       primary,
			Description: muted,
			Aside:       faint,
			ExitKey:     muted.Bold(true),
			Exit:        muted,
			Disabled:    faint,
		},
		Prompt: PromptStyles{Glyph: muted.Bold(true), Label: muted.Bold(true), Hint: muted, Input: muted},
		Message: MessageStyles{
			OK: ok.Bold(true), Warn: warn.Bold(true), Bad: bad.Bold(true),
			Info: primary.Bold(true), Cancelled: warn.Bold(true), Text: muted,
		},
		Callout: CalloutStyles{
			OK:        CalloutKind{Bar: bg(p.AccentOK), Title: ok.Bold(true)},
			Warn:      CalloutKind{Bar: bg(p.AccentWarn), Title: warn.Bold(true)},
			Cancelled: CalloutKind{Bar: bg(p.AccentWarn), Title: warn.Bold(true)},
			Body:      muted,
		},
		KeyValue: KeyValueStyles{
			Bullet: muted, Key: primary, Value: muted,
			ValueOK: ok, ValueWarn: warn, ValueBad: bad,
		},
		Badge:      fg(p.BadgeFG).Background(c(p.BadgeBG)).Bold(true),
		Section:    SectionStyles{Fill: fill, Title: muted.Bold(true), Rule: fg(p.Divider)},
		Breadcrumb: BreadcrumbStyles{Item: muted, Current: primary.Bold(true), Separator: faint},
		Plan:       PlanStyles{Added: ok, Removed: bad, Context: muted, LockoutRisk: warn.Bold(true)},
		Step: StepStyles{
			Pending: muted, Running: primary, Done: ok, Failed: bad, RolledBack: warn,
		},
	}
}
