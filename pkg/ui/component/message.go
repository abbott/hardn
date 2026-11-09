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

// String returns the kind's word, which a Callout shows before a title
// of its own: brief 1B's "Success", "Info" and "Error", and Figma's
// "Warning" and "Cancelled" (DEV-032; proposal P-23 asks the maintainer to
// approve them). A value outside the kinds is MessageInfo's.
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

// title returns the title a Callout shows when it has none of its own,
// and so a Message's: Figma's "Operation Successful", "Warning" and
// "Operation Cancelled", and for the kinds Figma does not draw "Operation
// Failed" and "Note" (DEV-032; proposal P-23). A value outside the kinds
// is MessageInfo's.
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

// known returns k, or MessageInfo for a value outside the kinds.
func (k MessageKind) known() MessageKind {
	if k < MessageInfo || k > MessageCancelled {
		return MessageInfo
	}
	return k
}

// inline returns the kind's style in t, which colours an InlineResult's
// glyph and text: a cancelled result takes Warn, as a cancelled callout
// takes the warning's colour.
func (k MessageKind) inline(t theme.InlineStyles) lipgloss.Style {
	switch k {
	case MessageOK:
		return t.OK
	case MessageWarn, MessageCancelled:
		return t.Warn
	case MessageBad:
		return t.Bad
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

// Message is one report, such as origin/main's "✓ DNS configured
// successfully", in Figma's grammar (P-42 R5, the maintainer, 2026-10-22):
// an info message is muted prose, and any other kind its Callout, the
// kind's own title over the text. Here with no colour and ASCII glyphs:
//
//	0 2
//	# Operation Successful
//	# DNS configured successfully
//
//	  No automatic backups will be created
//
// Info is the text in Text.Muted from theme.Indent, wrapped at spaces,
// with no glyph and no word, as Figma's neutral prose ("Managing sudo
// method for: george"). Ok, warn, bad and cancelled are a Callout with no
// title of its own: "Operation Successful", "Warning", "Operation Failed"
// or "Operation Cancelled" over the text as its body (DEV-031, DEV-032),
// so the kind is told by the title's word, never by the bar's colour
// alone. A message with no text renders as the empty string. Messages
// written one after another are a Messages, which groups them.
type Message struct {
	// Kind sets prose or the callout; the zero value is MessageInfo.
	Kind MessageKind
	// Text is the report, such as "DNS configured successfully".
	Text string
}

// Render renders the message for c. See Message for the layout.
func (vm Message) Render(c Ctx) string {
	return Messages{vm}.Render(c)
}

// Messages is a run of messages written one after another, such as hardn
// confirm's clean-up report (what was done, what is left, how to finish
// it) or a plan's findings. Consecutive messages of one kind other than
// info are one Callout of that kind: its title once, and a body paragraph
// per message, in order. Consecutive info messages are one block of
// prose, a paragraph per message. Each group is set apart from the next
// by theme.SectionGap blank lines. So the caller groups a run by passing
// it whole, in the order it is to be read, rather than rendering each
// message alone, which would repeat the title over each one (P-42 R5;
// DEV-031). Messages with no text are skipped, and a run with none
// renders as the empty string.
type Messages []Message

// Render renders the run for c. See Messages for the layout.
func (vm Messages) Render(c Ctx) string {
	var blocks, body []string
	kind := MessageInfo
	flush := func() {
		switch {
		case len(body) == 0:
		case kind == MessageInfo:
			var lines []string
			for _, p := range body {
				lines = append(lines, c.prose(c.text(p))...)
			}
			blocks = append(blocks, strings.Join(lines, "\n"))
		default:
			blocks = append(blocks, Callout{Kind: kind, Body: body}.Render(c))
		}
		body = nil
	}
	for _, m := range vm {
		if c.text(m.Text) == "" {
			continue
		}
		if k := m.Kind.known(); k != kind {
			flush()
			kind = k
		}
		body = append(body, m.Text)
	}
	flush()
	return strings.Join(blocks, strings.Repeat("\n", theme.SectionGap+1))
}

// prose lays out text, already made one line of plain text, as muted
// prose (P-42 R5): Text.Muted from theme.Indent, wrapped at spaces to the
// content width, never cut but by fit at a width too narrow for a whole
// word.
func (c Ctx) prose(text string) []string {
	w := c.width()
	var out []string
	for _, l := range wrap(text, w-theme.Indent) {
		out = append(out, c.fit(line{gap(theme.Indent), {l, c.Theme.Text.Muted}}, w))
	}
	return out
}

// InlineResult is a result written on its own line under a prompt, the
// prompt then asked again: a form field's, such as its validation's
// refusal, and a line a prompt refuses (pkg/ui/nav). Here with no colour
// and ASCII glyphs:
//
//	0 2   6
//	  Enter nameserver IP address [0 or q]: 999.1.1.1
//	  <>  Invalid IP address format
//
// Glyphs.Inline (⨳, "(x)") centred in the marker column, and the text at
// theme.LabelCol, both in the kind's colour (Inline.<Kind>), regular:
// failed in status.bad, a warning in status.warn, done in status.ok,
// information in text.muted. It has no word, by the maintainer's decision
// of 2026-10-22 ("glyph and text in the state's colour, no word"; P-42,
// DEV-062): its text says what it is, so without colour every kind draws
// alike but for its words. The text is prose, so a line too long for the
// content width wraps at spaces under itself, at theme.LabelCol. A result
// with no text renders as the empty string.
type InlineResult struct {
	// Kind sets the colour; the zero value is MessageInfo.
	Kind MessageKind
	// Text is the result, such as "Invalid IP address format".
	Text string
}

// Render renders the result for c. See InlineResult for the layout.
func (vm InlineResult) Render(c Ctx) string {
	text := c.text(vm.Text)
	if text == "" {
		return ""
	}
	w, style := c.width(), vm.Kind.inline(c.Theme.Inline)
	lead := marker(c.Theme.Glyphs.Inline, style).to(theme.LabelCol)
	var out []string
	for _, l := range wrap(text, w-theme.LabelCol) {
		out = append(out, c.fit(append(lead, span{l, style}), w))
		lead = line{gap(theme.LabelCol)}
	}
	return strings.Join(out, "\n")
}

// Callout is a block that stands out from the screen, as Figma's "Warning"
// before a lockout-risk change and its "Operation Successful" and
// "Operation Cancelled" results: an accent bar beside a bold title, and
// the body lines under it. Here with no colour and ASCII glyphs:
//
//	0 2
//	# Warning
//	# Before disabling root SSH login, ensure:
//
// The bar runs down column 0 beside every line, a space painted in the
// kind's accent colour (Callout.<Kind>.Bar; D14) or, with no colour,
// Glyphs.Bar (DEV-015). The title line starts at column 2, bold in the
// kind's colour (Callout.<Kind>.Title), and is cut with the ellipsis
// glyph when too long. It has no glyph: Figma draws none, and the
// maintainer kept it so (DEV-032). With no title the kind's own title is
// shown, Figma's words where Figma draws the kind. A title of the screen's
// own follows the kind's word (MessageKind.String) and a colon, as in
// "Warning: Changing the SSH port", unless it is that word or the kind's
// own title. So whatever the title, the kind has a word and a colour, and
// never rests on the bar's colour alone (DEV-032). Each body paragraph
// starts at column 2, under the title, in Callout.Body and, being prose,
// wraps at spaces. A list that belongs to the callout, such as Figma's
// checklist under its warning, is a BulletList after it, as Figma draws
// it outside the bar.
//
// Indent moves the whole callout right by that many cells, its bar at
// column Indent and its text two cells after, the lines as much narrower:
// the main menu's alert stands with its bar in the marker column's centre
// (screen.Alert, DEV-075).
type Callout struct {
	// Kind sets the word, the default title and their colours; the zero
	// value is MessageInfo.
	Kind MessageKind
	// Title heads the callout, such as "Warning", after the kind's word
	// unless it is that word or the kind's own title. Empty shows the
	// kind's own title.
	Title string
	// Body holds the paragraphs under the title, in order; empty ones are
	// skipped.
	Body []string
	// Indent is the bar's column, 0 for the left edge; the lines are as
	// much narrower.
	Indent int
}

// Render renders the callout for c. See Callout for the layout.
func (vm Callout) Render(c Ctx) string {
	k, w := vm.Kind.callout(c.Theme.Callout), c.width()
	bar := append(line{gap(vm.Indent)}, c.calloutBar(k)...)
	title := c.text(vm.Title)
	switch {
	case title == "":
		title = vm.Kind.title()
	case title != vm.Kind.title() && title != vm.Kind.String():
		title = vm.Kind.String() + ": " + title
	}
	out := []string{c.fit(append(bar, span{title, k.Title}), w)}
	for _, p := range vm.Body {
		if p = c.text(p); p == "" {
			continue
		}
		for _, l := range wrap(p, w-bar.width()) {
			out = append(out, c.fit(append(bar, span{l, c.Theme.Callout.Body}), w))
		}
	}
	return strings.Join(out, "\n")
}

// calloutBar is the start of every line of a callout of kind k: the
// accent bar at column 0 (Ctx.paint) and a space.
func (c Ctx) calloutBar(k theme.CalloutKind) line {
	return line{c.paint(k.Bar, 1), gap(1)}
}
