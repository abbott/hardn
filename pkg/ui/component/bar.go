package component

// ContextBar is the main menu's distribution bar, here as drawn with no
// colour:
//
//	█ Debian 12 (Bookworm) █████████████████████████████████████████
//
// It spans the content width, or the terminal's when
// theme.BarsFillContent is off: a one-cell stub, the text on the
// terminal's background with a cell either side, then the fill, painted in
// bar.bg, or drawn with Glyphs.Bar when there is no colour (C13, DEV-015,
// DEV-018). Text too long for the bar is cut with the ellipsis glyph.
type ContextBar struct {
	// Name is the distribution, in bold, such as "Debian". Empty shows
	// "Unknown".
	Name string
	// Version is the release, such as "12"; empty shows none.
	Version string
	// Codename is shown in parentheses, as given, such as "Bookworm"; empty
	// shows none.
	Codename string
}

// Render renders the bar for c. See ContextBar for the layout.
func (vm ContextBar) Render(c Ctx) string {
	t := c.Theme.ContextBar
	text := c.known(vm.Name, t.Name, t.Unknown)
	if v := c.text(vm.Version); v != "" {
		text = append(text, gap(1), span{v, t.Version})
	}
	if cn := c.text(vm.Codename); cn != "" {
		text = append(text, gap(1), span{"(" + cn + ")", t.Codename})
	}
	return c.bar(t.Fill, text)
}

// TitleBar is a sub-screen's title bar, as Figma's sub-screens open with
// one, such as "Manage User" on USER-MANAGE_USER-MANAGEMENT: a ContextBar
// with a bold title in place of the distribution. Here with no colour and
// ASCII glyphs, with a trail:
//
//	# User Management > Manage User ################################
//
// Before the title it can show the trail of sections the screen sits
// under, its Parents, each in TitleBar.Parent, joined by Glyphs.Separator
// in TitleBar.Separator with a space either side, the title last in
// TitleBar.Title. A trail too long for the bar collapses as a
// Breadcrumb's does (crumbs): the parents between the first and the title
// give way to one ellipsis glyph, then the first does too, then the
// parents are dropped, and last the title is cut with the ellipsis glyph.
// With no title the bar is all fill, whatever its parents.
//
// Proposal P-08, decided by the maintainer (2026-10-17): sub-screens open
// with this bar, wrapped by bar.bg as Figma draws it, and the trail lives
// in it, as "User Management › Manage User", never with the product name.
// The screens show the trail only when screen.TitleTrail is set, which by
// default it is not, so their bars show the title alone, as Figma's
// frames do; with no Parents the bar is that.
type TitleBar struct {
	// Title is the screen's title, such as "Manage User". Empty draws the
	// bar with no text.
	Title string
	// Parents are the sections above the screen, outermost first, such as
	// "User Management" above "Manage User"; never "hardn". Empty ones
	// are skipped.
	Parents []string
}

// Render renders the bar for c. See ContextBar and TitleBar for the
// layout.
func (vm TitleBar) Render(c Ctx) string {
	t := c.Theme.TitleBar
	title := c.text(vm.Title)
	if title == "" {
		return c.bar(t.Fill, nil)
	}
	items := append(c.items(vm.Parents), title)
	return c.bar(t.Fill, c.crumbs(items, c.barWidth()-barFrame, t.Parent, t.Separator, t.Title))
}
