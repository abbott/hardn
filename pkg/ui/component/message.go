package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// MessageKind is what a Message or Callout reports: brief 1B's success,
// info, warn and error, and Figma's cancelled.
type MessageKind int

// The message kinds. The zero value is MessageInfo, which says nothing
// went right or wrong.
const (
	MessageInfo MessageKind = iota
	MessageOK
	MessageWarn
	MessageBad
	MessageCancelled
)

// String returns the kind's word, which a Callout shows when it has no
// title: Figma's "Operation Successful", "Warning" and "Operation
// Cancelled", and for the kinds Figma does not draw "Operation Failed" and
// "Note" (DEV-032; proposal P-23 asks the maintainer to approve them). A
// value outside the kinds is MessageInfo's.
func (k MessageKind) String() string {
	switch k {
	case MessageOK:
		return "Operation Successful"
	case MessageWarn:
		return "Warning"
	case MessageBad:
		return "Operation Failed"
	case MessageCancelled:
		return "Operation Cancelled"
	}
	return "Note"
}

// glyph returns the kind's glyph in g.
func (k MessageKind) glyph(g theme.Glyphs) string {
	switch k {
	case MessageOK:
		return g.OK
	case MessageWarn:
		return g.Warn
	case MessageBad:
		return g.Bad
	case MessageCancelled:
		return g.Cancelled
	}
	return g.Info
}

// style returns the kind's style in t, which colours its glyph.
func (k MessageKind) style(t theme.MessageStyles) lipgloss.Style {
	switch k {
	case MessageOK:
		return t.OK
	case MessageWarn:
		return t.Warn
	case MessageBad:
		return t.Bad
	case MessageCancelled:
		return t.Cancelled
	}
	return t.Info
}

// callout returns the kind's bar and title styles in t.
func (k MessageKind) callout(t theme.CalloutStyles) theme.CalloutKind {
	switch k {
	case MessageOK:
		return t.OK
	case MessageWarn:
		return t.Warn
	case MessageBad:
		return t.Bad
	case MessageCancelled:
		return t.Cancelled
	}
	return t.Info
}

// Message is a one-line report, such as origin/main's "✓ DNS configured
// successfully": the kind's glyph, then the text. Here with no colour and
// ASCII glyphs:
//
//	0 2   6
//	  [+] DNS configured successfully
//	  [!] Failed to configure sudo: exit status 1
//
// The glyph is centred in the marker column (DEV-020), bold in the kind's
// colour, and the text starts at theme.LabelCol in Message.Text. Each
// kind has its own glyph, so the kind never rests on colour alone
// (DEV-031). The text is prose, so a line too long for the content width
// wraps at spaces under the first, at theme.LabelCol, rather than being
// cut. A message with no text renders as the empty string.
type Message struct {
	// Kind sets the glyph and its colour; the zero value is MessageInfo.
	Kind MessageKind
	// Text is the report, such as "DNS configured successfully".
	Text string
}

// Render renders the message for c. See Message for the layout.
func (vm Message) Render(c Ctx) string {
	text := clean(vm.Text)
	if text == "" {
		return ""
	}
	w := c.width()
	lead := marker(vm.Kind.glyph(c.Theme.Glyphs), vm.Kind.style(c.Theme.Message)).to(theme.LabelCol)
	var out []string
	for _, l := range wrap(text, w-theme.LabelCol) {
		out = append(out, c.fit(append(lead, span{l, c.Theme.Message.Text}), w))
		lead = line{gap(theme.LabelCol)}
	}
	return strings.Join(out, "\n")
}

// Callout is a block that stands out from the screen, as Figma's "Warning"
// before a lockout-risk change and its "Operation Successful" and
// "Operation Cancelled" results: an accent bar beside a bold title and the
// body lines under it. Here with no colour and ASCII glyphs:
//
//	0 2
//	# Warning
//	# Before disabling root SSH login, ensure:
//
// The bar runs down column 0 beside every line, a space painted in the
// kind's accent colour (Callout.<Kind>.Bar; D14) or, with no colour,
// Glyphs.Bar (DEV-015). The title starts at column 2 in the kind's
// colour, bold, and is cut with the ellipsis glyph when too long; with no
// title it is the kind's word (MessageKind.String), so the kind never
// rests on the bar's colour alone (DEV-032). Each body paragraph starts
// under the title in Callout.Body and, being prose, wraps at spaces. A
// list that belongs to the callout, such as Figma's checklist under its
// warning, is a BulletList after it, as Figma draws it outside the bar.
type Callout struct {
	// Kind sets the bar's colour, the title's and its default word; the
	// zero value is MessageInfo.
	Kind MessageKind
	// Title heads the callout, such as "Warning". Empty shows the kind's
	// word.
	Title string
	// Body holds the paragraphs under the title, in order; empty ones are
	// skipped.
	Body []string
}

// Render renders the callout for c. See Callout for the layout.
func (vm Callout) Render(c Ctx) string {
	k, w := vm.Kind.callout(c.Theme.Callout), c.width()
	bar := line{c.paint(k.Bar, 1), gap(1)}
	title := clean(vm.Title)
	if title == "" {
		title = vm.Kind.String()
	}
	out := []string{c.fit(append(bar, span{title, k.Title}), w)}
	for _, p := range vm.Body {
		if p = clean(p); p == "" {
			continue
		}
		for _, l := range wrap(p, w-bar.width()) {
			out = append(out, c.fit(append(bar, span{l, c.Theme.Callout.Body}), w))
		}
	}
	return strings.Join(out, "\n")
}
