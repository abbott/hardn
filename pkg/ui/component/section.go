package component

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/abbott/hardn/pkg/ui/theme"
)

// SectionHeader opens a section of a sub-screen, such as Figma's "User
// Configuration" or "SSH Configuration": the title, then a rule to the end
// of the bar width. Here with no colour and ASCII glyphs:
//
//	0 2
//	  User Configuration -------------------------------------------
//
// The title starts at column 2 (theme.Indent), where a ContextBar's text
// does (DEV-018), in Section.Title with a cell after it, then a run of
// Glyphs.Rule (╌, - in ASCII) in Section.Rule, at least one cell, across
// the content width, or the terminal's when theme.BarsFillContent is off.
// Nothing is drawn before the title: the bars' ░ stub at column 0, which it
// had (DEV-033), is a space since 2026-11-06, so the title keeps its column
// (the maintainer; DEV-073). A title too wide is cut with the ellipsis
// glyph. With no title, or on a terminal too narrow for one, it is the rule
// from column 2; under three columns, the rule alone.
type SectionHeader struct {
	// Title names the section, such as "User Configuration".
	Title string
}

// Render renders the header for c. See SectionHeader for the layout.
func (vm SectionHeader) Render(c Ctx) string {
	t, w := c.Theme.Section, c.barWidth()
	lead := strings.Repeat(" ", theme.Indent)
	title := c.text(vm.Title)
	if title == "" || w <= barFrame {
		if w <= theme.Indent {
			return c.rule(t.Rule, w).String()
		}
		return lead + c.rule(t.Rule, w-theme.Indent).String()
	}
	body := c.fit(line{{title, t.Title}}, w-barFrame)
	return lead + body + " " + c.rule(t.Rule, w-theme.Indent-1-lipgloss.Width(body)).String()
}

// Breadcrumb says where a sub-screen sits, such as "User Management ›
// Manage User": each item in Breadcrumb.Item, the last, the current
// screen, in Breadcrumb.Current, joined by Glyphs.Separator in
// Breadcrumb.Separator with a space either side. Here with ASCII glyphs:
//
//	0 2
//	  User Management > Manage User > SSH Keys
//
// It starts at theme.Indent (DEV-037). When the whole trail does not fit,
// the items between the first and the current one are replaced by one
// ellipsis glyph, then the first is too, and last the current item is cut
// with the ellipsis glyph, so the screen's own name goes last. Empty items
// are skipped, and with none it renders as the empty string. A trail names
// the sections and sub-sections alone, never the product: "hardn" is not
// one of its items.
//
// No screen draws a Breadcrumb on a line of its own. Proposal P-08,
// decided by the maintainer (2026-10-17): sub-screens open with Figma's
// TitleBar, and the trail lives inside it (TitleBar.Parents), with this
// collapse (crumbs); the screens show it only when screen.TitleTrail is
// set, which by default it is not. The component stays, with its states
// and goldens, for a screen that needs the trail apart from a bar.
type Breadcrumb struct {
	// Items are the sections from the main menu's down to the current
	// screen, such as "User Management", "Manage User".
	Items []string
}

// Render renders the trail for c. See Breadcrumb for the layout.
func (vm Breadcrumb) Render(c Ctx) string {
	items := c.items(vm.Items)
	if len(items) == 0 {
		return ""
	}
	t, w := c.Theme.Breadcrumb, c.width()
	trail := c.crumbs(items, w-theme.Indent, t.Item, t.Separator, t.Current)
	return c.fit(append(line{gap(theme.Indent)}, trail...), w)
}

// items returns the view-model strings ss as plain text (Ctx.text), the
// empty ones left out.
func (c Ctx) items(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s = c.text(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// crumbs lays out a trail of items, none empty and at least one, in at
// most limit cells where it can: the items joined by Glyphs.Separator in
// sep with a space either side, the last, the current screen, in current
// and the others in item. When the whole trail does not fit, the items
// between the first and the current one are replaced by one ellipsis
// glyph, then the first is too, and last the current item stands alone,
// which the caller cuts with fit if it is still too wide. Breadcrumb and
// TitleBar share it, so a trail collapses the same on its own line and in
// a bar.
func (c Ctx) crumbs(items []string, limit int, item, sep, current lipgloss.Style) line {
	e := c.Theme.Glyphs.Ellipsis
	last := items[len(items)-1:]
	tries := [][]string{items}
	if len(items) > 2 {
		tries = append(tries, append([]string{items[0], e}, last...))
	}
	if len(items) > 1 {
		tries = append(tries, append([]string{e}, last...))
	}
	trail := func(items []string) line {
		var out line
		for i, it := range items {
			style := item
			if i == len(items)-1 {
				style = current
			}
			if i > 0 {
				out = append(out, gap(1), span{c.Theme.Glyphs.Separator, sep}, gap(1))
			}
			out = append(out, span{it, style})
		}
		return out
	}
	for _, try := range tries {
		if l := trail(try); l.width() <= limit {
			return l
		}
	}
	return trail(last)
}
