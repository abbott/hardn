package component

import (
	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// wordmark is hardn's name as the header shows it, in lower case.
const wordmark = "hardn"

// Header is the first line of the main menu: the wordmark, the version and
// the repository link.
//
//	hardn  v0.4.0              https://github.com/abbott/hardn
//
// The wordmark starts at column 0 and the version box one cell after it,
// with a cell of padding either side. At 16 colours and below the version
// is plain muted text at the same column (DEV-005). The URL sits at
// theme.ValueCol (C14, DEV-017). Below theme.StackBelow, or when the URL
// does not fit beside the version, it moves to the next line at column 0.
//
// A Notice takes the URL's place, and moves down as it does:
//
//	hardn  v0.4.0              v0.4.1 available
//
// With NoticeBelow (the release check's: screen.Release) the
// URL stays and the Notice is the line under it, at the URL's column:
//
//	hardn  v0.4.0              https://github.com/abbott/hardn
//	                           v0.4.1 available
type Header struct {
	// Version is shown as given, such as "v0.4.0"; empty shows none.
	Version string
	// URL is the repository link; empty shows none.
	URL string
	// Notice is one short fact in the URL's place, such as "v0.4.1
	// available", in the primary text colour; empty shows the URL.
	Notice string
	// NoticeMuted draws the Notice muted: a fact that asks for nothing.
	NoticeMuted bool
	// NoticeBelow keeps the URL and draws the Notice on its own line
	// under it. Unset, or with no URL or no Notice, nothing changes.
	NoticeBelow bool
}

// Render renders the header for c. See Header for the layout.
func (vm Header) Render(c Ctx) string {
	t := c.Theme.Header
	w := c.width()
	head := line{{wordmark, t.Wordmark}}
	if v := c.text(vm.Version); v != "" {
		if t.Version.GetBackground() != (lipgloss.NoColor{}) {
			head = append(head, gap(1), span{" " + v + " ", t.Version})
		} else {
			head = append(head, gap(2), span{v, t.Version})
		}
	}
	second := span{c.text(vm.URL), t.URL}
	var under span
	if n := c.text(vm.Notice); n != "" {
		notice := span{n, c.Theme.Text.Primary}
		if vm.NoticeMuted {
			notice.style = c.Theme.Text.Muted
		}
		if vm.NoticeBelow && second.text != "" {
			under = notice
		} else {
			second = notice
		}
	}
	beside := func(s span) bool {
		return !c.stacked() && head.width() < theme.ValueCol && theme.ValueCol+lipgloss.Width(s.text) <= w
	}
	var out string
	switch {
	case second.text == "":
		return c.fit(head, w)
	case beside(second):
		out = c.fit(append(head.to(theme.ValueCol), second), w)
	default:
		out = c.fit(head, w) + "\n" + c.fit(line{second}, w)
	}
	switch {
	case under.text == "":
	case beside(second) && beside(under):
		out += "\n" + c.fit(append(line{}.to(theme.ValueCol), under), w)
	default:
		out += "\n" + c.fit(line{under}, w)
	}
	return out
}
