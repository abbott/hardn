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
// NewWithOverrides builds the same theme with some token values replaced:
// the dev-only gallery's colour hook (internal/tools/uigallery), for
// trying new values without editing palette.go. Production code uses New,
// and TestOverrideReach keeps every package but the gallery from naming
// the hook.
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
// SubjectLine composes Text.Muted and Badge, and BulletList is KeyValue
// rows. ErrorReport composes a bad Callout, StepProgress rows, Plan.Command,
// Output and Messages. A form field's options are Menu rows, and its
// inline error is a Message of kind bad, as every refused line is
// (pkg/ui/nav), so neither has a Field slot. The cursor has no slot: the
// terminal draws its own (candidate C8).
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
	Field      FieldStyles
	Message    MessageStyles
	Callout    CalloutStyles
	KeyValue   KeyValueStyles
	Badge      lipgloss.Style // white text on blue: the host and user badges
	Section    SectionStyles
	Breadcrumb BreadcrumbStyles
	Plan       PlanStyles
	Step       StepStyles
	Output     lipgloss.Style // captured command output in an error report
	Confirm    ConfirmStyles
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
	Unknown  lipgloss.Style // "Unknown", for a distribution hardn could not read
}

// TitleBarStyles style a sub-screen's title bar, such as "Manage User",
// and the section trail it can show before the title, such as "User
// Management › Manage User" (proposal P-08).
type TitleBarStyles struct {
	Fill      lipgloss.Style
	Parent    lipgloss.Style // a section before the title: "User Management"
	Separator lipgloss.Style // the separator glyph between the sections
	Title     lipgloss.Style // bold
}

// HostStyles style the host block beside the badge.
type HostStyles struct {
	Address lipgloss.Style // the IP address
	Uptime  lipgloss.Style
	Reboot  lipgloss.Style // the pending-reboot finding (proposal P-04)
	Unknown lipgloss.Style // "Unknown", for a value hardn could not read
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
	Factor   lipgloss.Style // a contributing factor, bullet and text (P-06)
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
	Risk        lipgloss.Style // the lockout-risk marker under an item, bold
}

// PromptStyles style the prompt line: "❯ Enter your choice [0-10 or q]:".
type PromptStyles struct {
	Glyph lipgloss.Style // bold
	Label lipgloss.Style // bold
	Hint  lipgloss.Style
}

// FieldStyles style a form field: the question and the answer typed to
// it, such as "1. Username: george", and the description under a field's
// title.
type FieldStyles struct {
	Number      lipgloss.Style // "1."
	Label       lipgloss.Style // the question
	Hint        lipgloss.Style // the accepted inputs in brackets, and the colon
	Answer      lipgloss.Style // the echoed answer, muted (drift D4)
	Description lipgloss.Style // what the field asks for, muted (DEV-046)
}

// MessageStyles style one-line messages: the kind's glyph (Glyphs.OK,
// Warn, Bad, Info or Cancelled), bold in the kind's colour, then the text.
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
// Figma draws the OK, Warn and Cancelled kinds; Bad and Info take their
// bars from existing tokens, status.bad and text.muted, so that no new
// colour enters (DEV-016).
type CalloutStyles struct {
	OK        CalloutKind
	Warn      CalloutKind
	Bad       CalloutKind
	Info      CalloutKind
	Cancelled CalloutKind
	Body      lipgloss.Style
}

// CalloutKind is one kind of callout.
type CalloutKind struct {
	Bar   lipgloss.Style // the accent bar's background
	Title lipgloss.Style // bold
}

// KeyValueStyles style key-value rows: "- Sudo  Enabled, No password". A
// value that carries a state takes the state's style; a plain value takes
// Value.
type KeyValueStyles struct {
	Bullet             lipgloss.Style
	Key                lipgloss.Style
	Value              lipgloss.Style
	ValueOK            lipgloss.Style
	ValueWarn          lipgloss.Style
	ValueBad           lipgloss.Style
	ValueUnknown       lipgloss.Style
	ValueNotApplicable lipgloss.Style
}

// SectionStyles style a section header: a stub, a title and a rule, such as
// "User Configuration".
type SectionStyles struct {
	Fill  lipgloss.Style // the stub
	Title lipgloss.Style // bold
	Rule  lipgloss.Style
}

// BreadcrumbStyles style a breadcrumb: "User Management › Manage User".
type BreadcrumbStyles struct {
	Item      lipgloss.Style
	Current   lipgloss.Style // bold
	Separator lipgloss.Style
}

// PlanStyles style a PlanView: each change's title, target and commands,
// and the lines of its diff.
type PlanStyles struct {
	Title       lipgloss.Style // what the change does
	Target      lipgloss.Style // the file or service it changes
	Added       lipgloss.Style
	Removed     lipgloss.Style
	Context     lipgloss.Style // an unchanged line, and a change's bullet
	Meta        lipgloss.Style // a diff's file and hunk headers and "\ No newline" markers
	Command     lipgloss.Style // a command the change runs
	LockoutRisk lipgloss.Style // a change that could lock the user out, bold
}

// StepStyles style StepProgress: one style per step state, which colours
// the state's glyph and word, then the step's title and detail.
type StepStyles struct {
	Pending    lipgloss.Style
	Running    lipgloss.Style
	Done       lipgloss.Style
	Failed     lipgloss.Style
	RolledBack lipgloss.Style
	Skipped    lipgloss.Style
	Title      lipgloss.Style
	Detail     lipgloss.Style // such as why a step was skipped
}

// ConfirmStyles style the confirm-or-revert countdown: the glyph, word
// and time, bold, while there is time and in the last seconds; the notice;
// and the command to run from a new session, bold.
type ConfirmStyles struct {
	Waiting lipgloss.Style
	Urgent  lipgloss.Style
	Text    lipgloss.Style
	Command lipgloss.Style
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
	return build(env, p)
}

// build returns the theme for env from palette p: New's work, shared with
// NewWithOverrides.
func build(env term.Env, p *palette) *Theme {
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
			Fill: fill, Name: muted.Bold(true), Version: muted, Codename: faint, Unknown: unknown,
		},
		TitleBar: TitleBarStyles{Fill: fill, Parent: muted, Separator: faint, Title: primary.Bold(true)},
		Host:     HostStyles{Address: muted, Uptime: faint, Reboot: warn, Unknown: unknown},
		Risk: RiskStyles{
			Label: primary.Bold(true),
			Low:   ok, Moderate: warn, High: bad, Unknown: unknown,
			Gloss: faint, Factor: muted,
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
			Risk:        warn.Bold(true),
		},
		Prompt: PromptStyles{Glyph: muted.Bold(true), Label: muted.Bold(true), Hint: muted},
		Field:  FieldStyles{Number: primary, Label: primary, Hint: primary, Answer: muted, Description: muted},
		Message: MessageStyles{
			OK: ok.Bold(true), Warn: warn.Bold(true), Bad: bad.Bold(true),
			Info: primary.Bold(true), Cancelled: warn.Bold(true), Text: muted,
		},
		Callout: CalloutStyles{
			OK:        CalloutKind{Bar: bg(p.AccentOK), Title: ok.Bold(true)},
			Warn:      CalloutKind{Bar: bg(p.AccentWarn), Title: warn.Bold(true)},
			Bad:       CalloutKind{Bar: bg(p.StatusBad), Title: bad.Bold(true)},
			Info:      CalloutKind{Bar: bg(p.TextMuted), Title: primary.Bold(true)},
			Cancelled: CalloutKind{Bar: bg(p.AccentWarn), Title: warn.Bold(true)},
			Body:      muted,
		},
		KeyValue: KeyValueStyles{
			Bullet: muted, Key: primary, Value: muted,
			ValueOK: ok, ValueWarn: warn, ValueBad: bad,
			ValueUnknown: unknown, ValueNotApplicable: muted,
		},
		Badge:      fg(p.BadgeFG).Background(c(p.BadgeBG)).Bold(true),
		Section:    SectionStyles{Fill: fill, Title: muted.Bold(true), Rule: fg(p.Divider)},
		Breadcrumb: BreadcrumbStyles{Item: muted, Current: primary.Bold(true), Separator: faint},
		Plan: PlanStyles{
			Title: primary, Target: muted,
			Added: ok, Removed: bad, Context: muted, Meta: faint,
			Command: muted, LockoutRisk: warn.Bold(true),
		},
		Step: StepStyles{
			Pending: muted, Running: primary, Done: ok, Failed: bad, RolledBack: warn,
			Skipped: muted, Title: primary, Detail: faint,
		},
		Output: muted,
		Confirm: ConfirmStyles{
			Waiting: warn.Bold(true), Urgent: bad.Bold(true),
			Text: primary, Command: primary.Bold(true),
		},
	}
}
