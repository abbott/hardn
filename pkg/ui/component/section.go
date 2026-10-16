package component

import (
	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// SectionHeader opens a section of a sub-screen, such as Figma's "User
// Configuration" or "SSH Configuration": a one-cell stub, the title, then
// a rule to the end of the bar width. Here with no colour and ASCII
// glyphs:
//
//	# User Configuration ------------------------------------------
//
// It is laid out as a ContextBar is (DEV-018): the stub at column 0, the
// title from column 2 in Section.Title with a cell either side, then a run
// of Glyphs.Rule in Section.Rule, at least one cell, across the content
// width, or the terminal's when theme.BarsFillContent is off. The stub is
// a space painted in bar.bg or, with no colour, Glyphs.Bar (C13, DEV-033).
// A title too wide is cut with the ellipsis glyph. With no title, or on a
// terminal too narrow for one, it is the stub and the rule.
type SectionHeader struct {
	// Title names the section, such as "User Configuration".
	Title string
}

// Render renders the header for c. See SectionHeader for the layout.
func (vm SectionHeader) Render(c Ctx) string {
	t, w := c.Theme.Section, c.barWidth()
	stub := c.paint(t.Fill, 1).String()
	title := clean(vm.Title)
	if title == "" || w <= barFrame {
		if w < 3 {
			return c.paint(t.Fill, w).String()
		}
		return stub + " " + c.rule(t.Rule, w-2).String()
	}
	body := c.fit(line{{title, t.Title}}, w-barFrame)
	return stub + " " + body + " " + c.rule(t.Rule, w-3-lipgloss.Width(body)).String()
}

// Breadcrumb says where a sub-screen sits, such as "hardn › Firewall":
// each item in Breadcrumb.Item, the last, the current screen, in
// Breadcrumb.Current, joined by Glyphs.Separator in Breadcrumb.Separator
// with a space either side. Here with ASCII glyphs:
//
//	0 2
//	  hardn > User Management > Manage User
//
// It starts at theme.Indent (DEV-037). When the whole trail does not fit,
// the items between the first and the current one are replaced by one
// ellipsis glyph, then the first is too, and last the current item is cut
// with the ellipsis glyph, so the screen's own name goes last. Empty items
// are skipped, and with none it renders as the empty string.
//
// Figma's sub-screens show a TitleBar and no breadcrumb. The sub-menu and
// workflow-step layouts (pkg/ui/screen) open with the Header and a
// Breadcrumb, as brief 1B has them, while proposal P-08 asks which they
// use.
type Breadcrumb struct {
	// Items are the screens from the main menu to the current one, such as
	// "hardn", "Firewall".
	Items []string
}

// Render renders the trail for c. See Breadcrumb for the layout.
func (vm Breadcrumb) Render(c Ctx) string {
	var items []string
	for _, it := range vm.Items {
		if it = clean(it); it != "" {
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		return ""
	}
	w, e := c.width(), c.Theme.Glyphs.Ellipsis
	current := items[len(items)-1:]
	tries := [][]string{items}
	if len(items) > 2 {
		tries = append(tries, append([]string{items[0], e}, current...))
	}
	if len(items) > 1 {
		tries = append(tries, append([]string{e}, current...))
	}
	for _, try := range tries {
		if l := vm.trail(c, try); l.width() <= w {
			return l.String()
		}
	}
	return c.fit(vm.trail(c, current), w)
}

// trail lays out items from theme.Indent, joined by the separator, the
// last in Breadcrumb.Current.
func (Breadcrumb) trail(c Ctx, items []string) line {
	t := c.Theme.Breadcrumb
	out := line{gap(theme.Indent)}
	for i, it := range items {
		style := t.Item
		if i == len(items)-1 {
			style = t.Current
		}
		if i > 0 {
			out = append(out, gap(1), span{c.Theme.Glyphs.Separator, t.Separator}, gap(1))
		}
		out = append(out, span{it, style})
	}
	return out
}
