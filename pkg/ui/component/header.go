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
type Header struct {
	// Version is shown as given, such as "v0.4.0"; empty shows none.
	Version string
	// URL is the repository link; empty shows none.
	URL string
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
	url := c.text(vm.URL)
	switch {
	case url == "":
		return c.fit(head, w)
	case !c.stacked() && head.width() < theme.ValueCol && theme.ValueCol+lipgloss.Width(url) <= w:
		return c.fit(append(head.to(theme.ValueCol), span{url, t.URL}), w)
	}
	return c.fit(head, w) + "\n" + c.fit(line{{url, t.URL}}, w)
}
