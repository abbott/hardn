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

// String returns the kind's word, which a Message shows between its glyph
// and its text, and a Callout before a title of its own: brief 1B's
// "Success", "Info" and "Error", and Figma's "Warning" and "Cancelled"
// (DEV-031; proposal P-23 asks the maintainer to approve them). A value
// outside the kinds is MessageInfo's.
func (k MessageKind) String() string {
	switch k {
	case MessageOK:
		return "Success"
	case MessageWarn:
		return "Warning"
	case MessageBad:
		return "Error"
	case MessageCancelled:
		return "Cancelled"
	}
	return "Info"
}

// title returns the title a Callout shows when it has none of its own:
// Figma's "Operation Successful", "Warning" and "Operation Cancelled", and
// for the kinds Figma does not draw "Operation Failed" and "Note" (DEV-032;
// proposal P-23). A value outside the kinds is MessageInfo's.
func (k MessageKind) title() string {
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

// style returns the kind's style in t, which colours its glyph and word.
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
// successfully": the kind's glyph, the kind's word and a colon, then the
// text. Here with no colour and ASCII glyphs:
//
//	0 2   6
//	  [+] Success: DNS configured successfully
//	  [!] Error: Failed to configure sudo: exit status 1
//
// The glyph is centred in the marker column (DEV-020), and the word
// (MessageKind.String) starts at theme.LabelCol; both are bold in the
// kind's colour (Message.<Kind>). The text follows the word's colon after
// a space, in Message.Text. So each kind has a glyph, a word and a colour,
// as brief 1B asks, and never rests on colour alone (DEV-031). The text is
// prose, so a line too long for the content width wraps at spaces under
// the word, at theme.LabelCol, rather than being cut. A message with no
// text renders as the empty string.
type Message struct {
	// Kind sets the glyph, the word and their colour; the zero value is
	// MessageInfo.
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
	w, style := c.width(), vm.Kind.style(c.Theme.Message)
	lead := marker(vm.Kind.glyph(c.Theme.Glyphs), style).to(theme.LabelCol)
	// word is what is left to show of the kind's word and its colon. It
	// opens the first line, or the first lines when the content width is
	// too narrow for it whole: no space in it lets the wrap break it.
	word := vm.Kind.String() + ":"
	var out []string
	for _, l := range wrap(word+" "+text, w-theme.LabelCol) {
		n := 0
		for n < len(word) && n < len(l) && l[n] == word[n] {
			n++
		}
		rest := strings.TrimLeft(l[n:], " ")
		row := append(lead, span{l[:n], style}, gap(len(l)-n-len(rest)), span{rest, c.Theme.Message.Text})
		out = append(out, c.fit(row, w))
		word, lead = word[n:], line{gap(theme.LabelCol)}
	}
	return strings.Join(out, "\n")
}

// Callout is a block that stands out from the screen, as Figma's "Warning"
// before a lockout-risk change and its "Operation Successful" and
// "Operation Cancelled" results: an accent bar beside the kind's glyph and
// a bold title, and the body lines under them. Here with no colour and
// ASCII glyphs:
//
//	0 2
//	# [*] Warning
//	# Before disabling root SSH login, ensure:
//
// The bar runs down column 0 beside every line, a space painted in the
// kind's accent colour (Callout.<Kind>.Bar; D14) or, with no colour,
// Glyphs.Bar (DEV-015). The title line starts at column 2 with the kind's
// glyph, as a Message has it, then a space and the title, all bold in the
// kind's colour (Callout.<Kind>.Title), and is cut with the ellipsis
// glyph when too long. With no title the kind's own title is shown,
// Figma's words where Figma draws the kind. A title of the screen's own
// follows the kind's word (MessageKind.String) and a colon, as in
// "Warning: Changing the SSH port", unless it is that word or the kind's
// own title. So whatever the title, the kind has a glyph, a word and a
// colour, and never rests on the bar's colour alone (DEV-032). Each body
// paragraph starts at column 2, under the glyph, in Callout.Body and,
// being prose, wraps at spaces. A list that belongs to the callout, such
// as Figma's checklist under its warning, is a BulletList after it, as
// Figma draws it outside the bar.
type Callout struct {
	// Kind sets the glyph, the word, the default title and their colours;
	// the zero value is MessageInfo.
	Kind MessageKind
	// Title heads the callout, such as "Warning", after the kind's word
	// unless it is that word or the kind's own title. Empty shows the
	// kind's own title.
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
	switch {
	case title == "":
		title = vm.Kind.title()
	case title != vm.Kind.title() && title != vm.Kind.String():
		title = vm.Kind.String() + ": " + title
	}
	out := []string{c.fit(append(bar, span{vm.Kind.glyph(c.Theme.Glyphs) + " " + title, k.Title}), w)}
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
