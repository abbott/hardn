package component

import (
	"math"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// Badge is a name in the badge colours on a line of its own, such as the
// user badge that heads a user's details on Figma's User Management
// screen; here with no colour:
//
//	0 2
//	  [george]
//
// It starts at theme.Indent. With colour it is the Badge slot with a cell
// of padding either side; with none, Glyphs.BadgeOpen and BadgeClose
// stand in for the padding (DEV-019). A name wider than the content is cut
// inside them with the ellipsis glyph. A badge with no text renders as the
// empty string: it names what the screen already holds, such as a user
// read from the password file, so there is no unknown state to show.
type Badge struct {
	// Text is the name shown in the badge, such as "george".
	Text string
}

// Render renders the badge for c. See Badge for the layout.
func (vm Badge) Render(c Ctx) string {
	s := c.text(vm.Text)
	if s == "" {
		return ""
	}
	w := c.width()
	return c.fit(line{gap(theme.Indent), c.badge(s, w-theme.Indent)}, w)
}

// SubjectLine names what a screen acts on: a muted label and its colon,
// then the subject in a badge, as Figma's "Managing: george" and
// "Managing SSH keys for: george"; here with no colour:
//
//	0 2
//	  Managing: [george]
//
// The label starts at theme.Indent in Text.Muted and the badge follows
// one cell after the colon (DEV-035). When the whole line does not fit,
// the badge goes under the label at theme.StackCol, cut inside its
// padding or brackets with the ellipsis glyph if it is still too wide, so
// neither the label nor the subject is cut to make room for the other.
// With no label it is the Badge alone, and with no subject it renders as
// the empty string.
type SubjectLine struct {
	// Label says what is done to the subject, such as "Managing" or
	// "Managing sudo method for"; give it without the colon.
	Label string
	// Subject is shown in the badge, such as "george".
	Subject string
}

// Render renders the line for c. See SubjectLine for the layout.
func (vm SubjectLine) Render(c Ctx) string {
	subject, label := c.text(vm.Subject), c.text(vm.Label)
	switch {
	case subject == "":
		return ""
	case label == "":
		return Badge{Text: subject}.Render(c)
	}
	w := c.width()
	head := line{gap(theme.Indent), {label + ":", c.Theme.Text.Muted}}
	if beside := append(head.to(head.width()+1), c.badge(subject, math.MaxInt)); beside.width() <= w {
		return beside.String()
	}
	return c.fit(head, w) + "\n" + c.fit(line{gap(theme.StackCol), c.badge(subject, w-theme.StackCol)}, w)
}
